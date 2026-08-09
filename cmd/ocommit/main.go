// Command ocommit is a plain, stupid-simple git commit helper. It takes no
// command line arguments at all: every behavior is controlled by
// environment variables.
//
// Behaviors (all opt-in via env):
//
//	OCOMMIT_KEY_PATH   path to an SSH private key; if set and valid, the
//	                   commit is signed with it (git's SSH signing format).
//	                   If set but unusable, ocommit logs a warning and
//	                   commits unsigned.
//	OCOMMIT_NAME       commit author/committer name; falls back to git
//	                   config, then "OCOMMIT, Git Commiter"
//	OCOMMIT_EMAIL      commit author/committer email; falls back to git
//	                   config, then "git@ocommit.local"
//	OLLAMA_DESC_URL    base URL of a local Ollama REST API; if set and
//	                   reachable, the staged diff is turned into an LLM
//	                   commit message (detailed body + TL;DR subject)
//	OLLAMA_DESC_MODEL  Ollama model name; optional, defaults to llama3.2
//
// ocommit runs inside a git repository and performs the equivalent of
//
//	git add -A && git commit && git log
package main

import (
	"context"
	"os"
	"strings"
	"time"

	"paepcke.de/ocommit/internal/config"
	"paepcke.de/ocommit/internal/gitops"
	"paepcke.de/ocommit/internal/ollama"
	"paepcke.de/ocommit/internal/output"
	"paepcke.de/ocommit/internal/sign"

	"github.com/go-git/go-git/v5/plumbing"
)

func main() {
	os.Exit(run())
}

func run() int {
	ui := output.New(os.Stdout, os.Stderr)
	cfg := config.FromEnv()

	// 1. Find the repository we are inside of.
	repo, wt, err := gitops.Open()
	if err != nil {
		return fail(ui, err)
	}

	// 2. Stage everything ("git add -A").
	ui.Infof("ocommit: staging all changes")
	if err := gitops.StageAll(wt); err != nil {
		return fail(ui, err)
	}

	// 3. Build the staged diff for the LLM and for the user.
	ui.Infof("ocommit: reading staged diff")
	diffText, files, err := gitops.StagedDiff(repo, wt)
	if err != nil {
		return fail(ui, err)
	}

	// 3b. Nothing to commit: inform and exit cleanly.
	if strings.TrimSpace(diffText) == "" {
		ui.Infof("ocommit: nothing to commit, working tree clean")
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 4. Optional LLM commit message via local Ollama.
	msg := gitops.CommitMessage{Subject: "update"}
	if cfg.OllamaURL != "" {
		client := ollama.New(cfg.OllamaURL, cfg.OllamaModel)
		if client.Available(ctx) {
			ui.Infof("ocommit: ollama reachable at %s, generating commit message", cfg.OllamaURL)
			if msg, err = generateMessage(ctx, client, diffText, files); err != nil {
				ui.Infof("ocommit: warning: llm message generation failed: %v", err)
				msg = gitops.CommitMessage{Subject: "update"}
			}
		} else {
			ui.Infof("ocommit: Ollama not reachable at %s, using default message", cfg.OllamaURL)
		}
	}

	// 5. Sign the commit with the configured SSH key (opt-in).
	var signer *sign.Signer
	if cfg.KeyPath != "" {
		signer, err = sign.Load(cfg.KeyPath)
		if err != nil {
			// The user asked for signing but the key cannot be used:
			// degrade to an unsigned commit and record why.
			ui.Infof("ocommit: warning: ssh key %s unusable (%v); committing unsigned", cfg.KeyPath, err)
			signer = nil
		} else {
			ui.Infof("ocommit: signing commit with ssh key %s", cfg.KeyPath)
		}
	}

	// 6. Create the commit, signing it when a key is available.
	id := gitops.ResolveIdentity(repo)
	var hash plumbing.Hash
	if signer != nil {
		ui.Infof("ocommit: committing (as %s <%s>, signed)", id.Name, id.Email)
		hash, err = gitops.SignedCommit(repo, wt, msg, func(payload []byte) ([]byte, error) {
			return signer.Sign(payload)
		})
	} else {
		ui.Infof("ocommit: committing (as %s <%s>)", id.Name, id.Email)
		hash, err = gitops.Commit(repo, wt, msg)
	}
	if err != nil {
		return fail(ui, err)
	}

	// 7. Show what was committed.
	logOut, err := gitops.Log(repo, 5)
	if err != nil {
		return fail(ui, err)
	}
	ui.Printf("ocommit: committed %s\n%s", hash.String()[:7], logOut)
	return 0
}

// generateMessage performs the two-step LLM conversation: detailed
// description, then TL;DR summary used as the commit subject. files is the
// list of changed paths, used as a compact summary so the model can ground
// its message in the file names without re-reading the whole diff twice.
func generateMessage(ctx context.Context, client *ollama.Client, diff string, files []string) (gitops.CommitMessage, error) {
	stat := "(no file changes)"
	if len(files) > 0 {
		var b strings.Builder
		for i, f := range files {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("  - ")
			b.WriteString(f)
		}
		stat = b.String()
	}
	detail, err := client.DescribeDetail(ctx, diff, stat)
	if err != nil {
		return gitops.CommitMessage{}, err
	}
	tldr, err := client.SummarizeTLDR(ctx, detail)
	if err != nil {
		return gitops.CommitMessage{}, err
	}
	return gitops.CommitMessage{Subject: tldr, Body: detail}, nil
}

func fail(ui *output.UI, err error) int {
	ui.Infof("ocommit: %v", err)
	return 1
}
