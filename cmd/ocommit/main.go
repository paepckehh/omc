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

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func main() { os.Exit(run()) }

func run() int {
	ui := output.New(os.Stdout, os.Stderr)
	cfg := config.FromEnv()

	// 1. Find the repository we are inside of.
	var (
		repo *git.Repository
		wt   *git.Worktree
	)
	if err := ui.Step("open", "detecting repository", func() error {
		r, w, err := gitops.Open()
		if err != nil {
			return err
		}
		repo, wt = r, w
		return nil
	}); err != nil {
		ui.Error(err)
		return 1
	}

	// 2. Stage everything ("git add -A").
	if err := ui.Step("stage", "staging all changes", func() error {
		return gitops.StageAll(wt)
	}); err != nil {
		ui.Error(err)
		return 1
	}

	// 3. Build the staged diff for the LLM and for the user.
	var (
		diffText string
		files    []string
	)
	if err := ui.Step("diff", "reading staged diff", func() error {
		d, f, err := gitops.StagedDiff(repo, wt)
		if err != nil {
			return err
		}
		diffText, files = d, f
		return nil
	}); err != nil {
		ui.Error(err)
		return 1
	}

	// 3b. Nothing to commit: inform and exit cleanly.
	if strings.TrimSpace(diffText) == "" {
		ui.CleanTree()
		return 0
	}

	// Show the changed file list as a compact diagnostic.
	ui.FileList(files)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 4. Optional LLM commit message via local Ollama.
	msg := gitops.CommitMessage{Subject: "update"}
	if cfg.OllamaURL != "" {
		client := ollama.New(cfg.OllamaURL, cfg.OllamaModel)
		if reachable := ollamaReachable(ui, ctx, client, cfg.OllamaURL); reachable {
			var genErr error
			msg, genErr = generateMessageProgress(ui, ctx, client, diffText, files)
			if genErr != nil {
				ui.Warn("llm message generation failed: %v", genErr)
				msg = gitops.CommitMessage{Subject: "update"}
			}
		} else {
			ui.Warn("ollama not reachable at %s, using default message", cfg.OllamaURL)
		}
	}

	// Preview the resolved commit message.
	ui.Summary(msg.Subject, msg.Body)

	// 5. Sign the commit with the configured SSH key (opt-in).
	var signer *sign.Signer
	if cfg.KeyPath != "" {
		if err := ui.Step("load key", "loading ssh signing key", func() error {
			s, err := sign.Load(cfg.KeyPath)
			if err != nil {
				return err
			}
			signer = s
			return nil
		}); err != nil {
			// The user asked for signing but the key cannot be used:
			// degrade to an unsigned commit and record why.
			ui.Warn("ssh key %s unusable (%v); committing unsigned", cfg.KeyPath, err)
			signer = nil
		} else {
			ui.SigningNotice(cfg.KeyPath, true)
		}
	} else {
		ui.SigningNotice("", false)
	}

	// 6. Create the commit, signing it when a key is available.
	id := gitops.ResolveIdentity(repo)
	var hash plumbing.Hash
	commitErr := ui.Step("commit", commitLabel(id, signer != nil), func() error {
		if signer != nil {
			h, err := gitops.SignedCommit(repo, wt, msg, func(payload []byte) ([]byte, error) {
				return signer.Sign(payload)
			})
			hash = h
			return err
		}
		h, err := gitops.Commit(repo, wt, msg)
		hash = h
		return err
	})
	if commitErr != nil {
		ui.Error(commitErr)
		return 1
	}

	// 7. Show what was committed.
	var logOut string
	if err := ui.Step("log", "reading recent history", func() error {
		out, err := gitops.Log(repo, 5)
		if err != nil {
			return err
		}
		logOut = out
		return nil
	}); err != nil {
		ui.Error(err)
		return 1
	}

	ui.CommitResult(hash.String()[:7], logOut)

	// 8. Tag the new commit with the next semver patch (vX.Y.N+1). The tag
	// message is the commit subject; the tag is SSH-signed when a signer is
	// available, otherwise unsigned.
	tagCommit, err := repo.CommitObject(hash)
	if err != nil {
		ui.Warn("tag: read commit for subject: %v", err)
		return 0
	}
	tagSubject, _, _ := strings.Cut(tagCommit.Message, "\n")
	if tagSubject == "" {
		tagSubject = "update"
	}

	var tagName string
	if err := ui.Step("tag", "bumping semver patch", func() error {
		latest, lerr := gitops.LatestSemverTag(repo)
		if lerr != nil {
			return lerr
		}
		tagName = gitops.NextSemverTag(latest)
		if signer != nil {
			_, terr := gitops.SignedTag(repo, hash, tagName, tagSubject, id, func(payload []byte) ([]byte, error) {
				return signer.Sign(payload)
			})
			return terr
		}
		_, terr := gitops.CreateTag(repo, hash, tagName, tagSubject, id)
		return terr
	}); err != nil {
		ui.Warn("tag: failed to tag %s: %v", tagName, err)
		return 0
	}

	ui.TagResult(tagName, hash.String()[:7], signer != nil)
	return 0
}

// ollamaReachable probes Ollama while showing a spinner step. An unreachable
// Ollama is not a pipeline failure (it is the normal degrade path), so the
// step is reported as successful either way.
func ollamaReachable(ui *output.UI, ctx context.Context, client *ollama.Client, url string) bool {
	var ok bool
	_ = ui.Step("ollama", "probing local ollama at "+url, func() error {
		ok = client.Available(ctx)
		return nil
	})
	return ok
}

// generateMessageProgress performs the two-step LLM conversation (detailed
// description, then TL;DR summary) while showing a progress bar that advances
// 0% -> 50% -> 100%. files is the list of changed paths, used as a compact
// summary so the model can ground its message in the file names without
// re-reading the whole diff twice.
func generateMessageProgress(ui *output.UI, ctx context.Context, client *ollama.Client, diff string, files []string) (gitops.CommitMessage, error) {
	ui.Progress("generating commit message", 0.0)

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
	ui.Progress("condensing to TL;DR", 0.5)

	tldr, err := client.SummarizeTLDR(ctx, detail)
	if err != nil {
		return gitops.CommitMessage{}, err
	}
	ui.Progress("commit message ready", 1.0)

	return gitops.CommitMessage{Subject: tldr, Body: detail}, nil
}

// commitLabel renders the step label shown while the commit is being created.
func commitLabel(id gitops.Identity, signed bool) string {
	if signed {
		return "committing as " + id.Name + " <" + id.Email + "> (signed)"
	}
	return "committing as " + id.Name + " <" + id.Email + ">"
}
