// Command omc is a plain, stupid-simple git commit helper. It takes no
// command line arguments at all: every behavior is controlled by
// environment variables.
//
// Behaviors (all opt-in via env):
//
//	OMC_SIGN_KEY_PATH   path to an SSH private key; if set and valid, the
//	                   commit is signed with it (git's SSH signing format).
//	                   FIDO2 security-key handles (id_ed25519_sk and
//	                   id_ecdsa_sk, bound to a smartcard) are supported:
//	                   they are signed through the ssh-agent, which talks
//	                   to the device (touch/PIN enforced there).
//	                   If set but unusable, omc logs a warning and
//	                   commits unsigned.
//	OMC_NAME       commit author/committer name; falls back to git
//	                   config, then "OMC, Git Commiter"
//	OMC_EMAIL      commit author/committer email; falls back to git
//	                   config, then "git@omc.local"
//	OLLAMA_DESC_URL    base URL of a local Ollama REST API; if set and
//	                   reachable, the staged diff is turned into an LLM
//	                   commit message (detailed body + TL;DR subject)
//	OLLAMA_DESC_MODEL  Ollama model name; optional, defaults to llama3.2
//	OMC_SUBJECT    override the commit subject; skips LLM generation.
//	                   See AGENTS.md for the subject/message pairing rules.
//	OMC_MESSAGE    override the commit body; skips LLM generation.
//	                   See AGENTS.md for the subject/message pairing rules.
//	OMC_TAG        override the tag name; used only when it parses as
//	                   strict semver vMAJOR.MINOR.PATCH, otherwise the
//	                   normal auto-bump path runs.
//	OMC_PUSH_KEY_PATH  path to an SSH private key; if set and readable,
//	                   omc pushes the new commit and tags to the default
//	                   remote after tagging ("git push; git push --tags").
//	                   FIDO2 security-key handles are supported and
//	                   authenticate through the ssh-agent.
//	                   If set but unusable, the push is skipped with a
//	                   warning; the commit and tag are never rolled back.
//
// omc runs inside a git repository and performs the equivalent of
//
//	git add -A && git commit && git log
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/gitops"
	"paepcke.de/omc/internal/ollama"
	"paepcke.de/omc/internal/output"
	"paepcke.de/omc/internal/sign"
	"paepcke.de/omc/internal/version"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func main() { os.Exit(run()) }

func run() int {
	ui := output.New(os.Stdout, os.Stderr)
	ui.Startup(version.Version)
	cfg := config.FromEnv()
	ui.ConfigNotice(configReport(cfg))

	// 1. Find the repository we are inside of.
	var (
		repo *git.Repository
		wt   *git.Worktree
	)
	ui.BeginGroup("📂 preparing repository", 3)
	if err := ui.Step("open", "detecting repository", func() error {
		r, w, err := gitops.Open()
		if err != nil {
			return err
		}
		repo, wt = r, w
		return nil
	}); err != nil {
		ui.EndGroup()
		ui.Error(err)
		return 1
	}

	// 2. Stage everything ("git add -A").
	if err := ui.Step("stage", "staging all changes", func() error {
		return gitops.StageAll(wt)
	}); err != nil {
		ui.EndGroup()
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
		ui.EndGroup()
		ui.Error(err)
		return 1
	}
	ui.EndGroup()

	// 3b. Nothing to commit: inform, still run the optional push (there
	// may be nothing new on the branch but local tags could be ahead of
	// the remote), and exit cleanly.
	if strings.TrimSpace(diffText) == "" {
		ui.CleanTree()
		pushNothing(ui, repo, cfg)
		return 0
	}

	// Show the changed file list as a compact diagnostic.
	ui.FileList(files)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 4. Resolve the commit message. Environment overrides
	// (OMC_SUBJECT / OMC_MESSAGE) win over LLM generation: when
	// either is set, no Ollama call is made. See AGENTS.md for the
	// pairing rules.
	msg := gitops.CommitMessage{Subject: "update"}
	if override, ok := overrideMessage(cfg); ok {
		msg = override
		ui.Info("message override active (OMC_SUBJECT/OMC_MESSAGE)")
	} else if cfg.OllamaURL != "" {
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
	skMode := false
	if cfg.KeyPath != "" {
		signer, skMode = loadSigner(ui, cfg)
	}

	// Commit signed state is derived from the signer that was actually
	// established, including the security-key (smartcard) degrade path.
	// Loader warnings already explain the details; the notice line states
	// the final mode so the record is unambiguous.
	if skMode && signer != nil {
		ui.SecurityKeyModeNotice(cfg.KeyPath, signer.PublicAlgorithm())
		ui.SecurityKeyTouchNotice(cfg.KeyPath, "the commit signing")
	} else {
		ui.SigningNotice(cfg.KeyPath, signer != nil)
	}

	// 6. Create the commit, signing it when a key is available.
	id := gitops.ResolveIdentity(repo)
	var hash plumbing.Hash
	commitStep := ui.Step
	if skMode && signer != nil {
		commitStep = ui.StepTouchCommit
	}
	// The commit, its semver tag, and the optional push form one logical
	// "commit & publish" group: commit step + result, tag step + result,
	// and (when configured) the push step. lineCount accounts for the
	// optional push so the colored tree closes with └─ on the last line;
	// when a push is configured we also reserve a slot for the degradation
	// WARN emitted on push failure so it stays under the tree.
	publishLines := 4
	if cfg.PushKeyPath != "" {
		publishLines += 2
	}
	ui.BeginGroup("📦 committing & publishing", publishLines)
	commitErr := commitStep("commit", commitLabel(id, signer != nil), func() error {
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
		ui.EndGroup()
		ui.Error(commitErr)
		return 1
	}

	// 7. Show what was committed (structured record; the git log is left to
	// the user's own `git log` invocation).
	ui.CommitResult(hash.String()[:7], signer != nil)

	// 8. Tag the new commit with the next semver patch (vX.Y.N+1). The tag
	// message is the commit subject; the tag is SSH-signed when a signer is
	// available, otherwise unsigned.
	tagSubject := msg.Subject
	if tagSubject == "" {
		tagSubject = "update"
	}

	var tagName string
	tagStep := ui.Step
	if signer != nil && skMode {
		tagStep = ui.StepTouchTag
	}
	if err := tagStep("tag", tagStepLabel(cfg), func() error {
		name, skipped, terr := resolveTagName(repo, cfg)
		if skipped {
			ui.Warn("OMC_TAG %q is not strict semver (vMAJOR.MINOR.PATCH); falling back to auto-bump", cfg.Tag)
		}
		if terr != nil {
			return terr
		}
		tagName = name
		if signer != nil {
			_, terr = gitops.SignedTag(repo, hash, tagName, tagSubject, id, func(payload []byte) ([]byte, error) {
				return signer.Sign(payload)
			})
			return terr
		}
		_, terr = gitops.CreateTag(repo, hash, tagName, tagSubject, id)
		return terr
	}); err != nil {
		ui.EndGroup()
		ui.Warn("tag: failed to tag %s: %v", tagName, err)
		return 0
	}

	ui.TagResult(tagName, hash.String()[:7], signer != nil)

	// 9. Optional push: when OMC_PUSH_KEY_PATH is set and the key file is
	// readable, push the new commit and the new tag to the default remote
	// ("git push; git push --tags"). All in-process via go-git. Failures
	// degrade: the commit and tag are never rolled back over a push
	// problem, matching the "always degrade, never block" contract.
	if cfg.PushKeyPath != "" {
		pushStep := ui.Step
		if sign.IsSecurityKeyPath(cfg.PushKeyPath) {
			pushStep = ui.StepTouchPush
		}
		if err := pushStep("push", "pushing commit and tags to remote", func() error {
			_, err := gitops.PushToRemote(repo, cfg.PushKeyPath)
			return err
		}); err != nil {
			ui.Warn("push failed (%v); commit and tag left local", err)
		}
	}
	ui.EndGroup()

	return 0
}

// configReport derives the startup diagnostic summary shown right after the
// version banner. detected lists the environment variables that were set
// (non-empty); verified lists the config options that passed a startup
// verification (key files loadable, tag override strict semver, Ollama
// configured). Values are collapsed to a single line so each variable keeps
// the structured record on one greppable line.
func configReport(cfg config.Config) (detected, verified []output.Field) {
	set := func(env, val string) {
		if strings.TrimSpace(val) != "" {
			detected = append(detected, output.F(env, oneLine(val)))
		}
	}
	set("OMC_SIGN_KEY_PATH", cfg.KeyPath)
	set("OLLAMA_DESC_URL", cfg.OllamaURL)
	set("OLLAMA_DESC_MODEL", cfg.OllamaModel)
	set("OMC_NAME", cfg.Name)
	set("OMC_EMAIL", cfg.Email)
	set("OMC_SUBJECT", cfg.Subject)
	set("OMC_MESSAGE", cfg.Message)
	set("OMC_TAG", cfg.Tag)
	set("OMC_PUSH_KEY_PATH", cfg.PushKeyPath)

	if cfg.KeyPath != "" {
		verified = append(verified, output.F("sign_key", fmt.Sprintf("valid=%v", sign.DetectKind(cfg.KeyPath) != sign.KindBroken)))
	}
	if cfg.PushKeyPath != "" {
		verified = append(verified, output.F("push_key", fmt.Sprintf("valid=%v", sign.DetectKind(cfg.PushKeyPath) != sign.KindBroken)))
	}
	if cfg.Tag != "" {
		verified = append(verified, output.F("tag_override", fmt.Sprintf("valid=%v", gitops.ValidSemverTag(cfg.Tag))))
	}
	if cfg.OllamaURL != "" {
		verified = append(verified, output.F("ollama", "configured=true"))
	}
	return detected, verified
}

// oneLine collapses a value to a single trimmed line (newlines and runs of
// whitespace become single spaces) and caps it at 120 chars so an override
// such as a multi-line OMC_MESSAGE cannot break the structured log record.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

// pushNothing performs the push step when there was nothing to commit or
// tag. This covers the case where only local tags are ahead of the remote
// (a previous run already committed and tagged locally, and the push that
// was supposed to publish them failed or was skipped). All failures
// degrade exactly like the main push step: warn and exit 0.
func pushNothing(ui *output.UI, repo *git.Repository, cfg config.Config) {
	if cfg.PushKeyPath == "" {
		return
	}
	pushStep := ui.Step
	if sign.IsSecurityKeyPath(cfg.PushKeyPath) {
		pushStep = ui.StepTouchPush
	}
	var res gitops.PushResult
	err := pushStep("push", "pushing commit and tags to remote", func() error {
		var perr error
		res, perr = gitops.PushToRemote(repo, cfg.PushKeyPath)
		return perr
	})
	if err != nil {
		ui.Warn("push failed (%v); nothing pushed", err)
		return
	}
	ui.PushResult(res.Remote, res.Branch, res.Tags)
}

// resolveTagName decides the tag name for the new commit. When OMC_TAG
// is set and parses as strict semver it is normalized (a leading "v" is added
// for bare "0.1.2") and used verbatim; otherwise the normal patch-bump path
// runs. An invalid override is logged as a warning so the user knows the
// override was ignored rather than silently dropped.
// resolveTagName decides the tag name for the new commit. When OMC_TAG
// is set and parses as strict semver it is normalized (a leading "v" is added
// for bare "0.1.2") and used verbatim; otherwise the normal patch-bump path
// runs. The returned overrideSkipped flag is true when OMC_TAG was set
// but rejected, so the caller can warn once without re-checking validity.
func resolveTagName(repo *git.Repository, cfg config.Config) (name string, overrideSkipped bool, err error) {
	if cfg.Tag != "" {
		if gitops.ValidSemverTag(cfg.Tag) {
			return gitops.NormalizeTag(cfg.Tag), false, nil
		}
		overrideSkipped = true
	}
	latest, err := gitops.LatestSemverTag(repo)
	if err != nil {
		return "", false, err
	}
	return gitops.NextSemverTag(latest), overrideSkipped, nil
}

// tagStepLabel renders the label for the tag step based on the override.
func tagStepLabel(cfg config.Config) string {
	if cfg.Tag != "" && gitops.ValidSemverTag(cfg.Tag) {
		return "tagging " + gitops.NormalizeTag(cfg.Tag)
	}
	return "bumping semver patch"
}

// overrideMessage resolves the commit message from the OMC_SUBJECT and
// OMC_MESSAGE environment overrides. It returns the message and true
// when an override is active (either variable set); false means the caller
// should fall back to LLM generation / the default subject.
//
// Pairing rules:
//   - both set:   subject = OMC_SUBJECT, body = OMC_MESSAGE.
//   - subject only: subject = OMC_SUBJECT, body = OMC_SUBJECT
//     (the subject stands in for the body).
//   - message only: subject = first line of OMC_MESSAGE (shortened),
//     body = full OMC_MESSAGE.
func overrideMessage(cfg config.Config) (gitops.CommitMessage, bool) {
	subject := strings.TrimSpace(cfg.Subject)
	body := strings.TrimSpace(cfg.Message)
	switch {
	case subject == "" && body == "":
		return gitops.CommitMessage{}, false
	case body == "":
		return gitops.CommitMessage{Subject: subject, Body: subject}, true
	case subject == "":
		return gitops.CommitMessage{Subject: shortenSubject(body), Body: body}, true
	default:
		return gitops.CommitMessage{Subject: subject, Body: body}, true
	}
}

// shortenSubject returns a single-line subject derived from a multi-line
// message body: the first non-empty line, trimmed and capped at 72 chars.
// This mirrors the TL;DR contract the LLM path uses for the subject line.
func shortenSubject(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 72 {
				line = line[:72]
			}
			return line
		}
	}
	// All lines empty; fall back to the trimmed whole body.
	return strings.TrimSpace(body)
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
// description, then TL;DR summary) while showing an animated scramble
// spinner whose label updates per phase. files is the list of changed paths,
// used as a compact summary so the model can ground its message in the file
// names without re-reading the whole diff twice.
func generateMessageProgress(ui *output.UI, ctx context.Context, client *ollama.Client, diff string, files []string) (gitops.CommitMessage, error) {
	ui.SpinnerStart("ollama", "generating commit message")

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
		ui.SpinnerStop()
		return gitops.CommitMessage{}, err
	}
	ui.SpinnerUpdate("condensing to TL;DR")

	tldr, err := client.SummarizeTLDR(ctx, detail)
	if err != nil {
		ui.SpinnerStop()
		return gitops.CommitMessage{}, err
	}
	ui.SpinnerStop()

	return gitops.CommitMessage{Subject: tldr, Body: detail}, nil
}

// loadSigner resolves the commit signing backend for the configured ssh key
// path, degrading like git does and never blocking the pipeline:
//
//   - software keys (id_ed25519, id_rsa, ...) are loaded directly and signed
//     in pure Go;
//   - FIDO2 security-key keys (id_ed25519_sk, id_ecdsa_sk) cannot be loaded
//     in pure Go (the private half lives on the smartcard), so omc falls
//     back to the ssh-agent, which forwards the signature request to the
//     authenticator; if the agent does not hold the identity, omc warns and
//     commits unsigned;
//   - anything else (missing, unreadable, corrupt, passphrase-encrypted) is
//     warned about and commits unsigned.
//
// It returns nil for "no usable signer" so callers fall through to an
// unsigned commit; the returned bool reports whether the signer path used
// the smartcard (security-key) backend, so the caller can render the
// correct notice.
func loadSigner(ui *output.UI, cfg config.Config) (*sign.Signer, bool) {
	// A FIDO2 security-key handle (id_ed25519_sk) is not a private key at
	// all: the file only references a smartcard-held key, and pure Go
	// cannot parse it. Detect that up front (DetectKind also checks the
	// adjacent .pub file) and route straight to the ssh-agent.
	if sign.DetectKind(cfg.KeyPath) == sign.KindSecurityKey {
		if s, err := sign.SecurityKeySigner(cfg.KeyPath); err == nil {
			ui.Warn("ssh key %s is a smartcard security key; signing via the ssh-agent", cfg.KeyPath)
			return s, true
		} else {
			ui.Warn("ssh key %s is a smartcard security key, but no ssh-agent identity matches (%v); committing unsigned", cfg.KeyPath, err)
			return nil, true
		}
	}

	var signer *sign.Signer
	stepErr := ui.Step("load key", "loading ssh signing key", func() error {
		var err error
		signer, err = sign.Load(cfg.KeyPath)
		return err
	})
	if stepErr == nil {
		ui.Info("signing with %s (%s)", cfg.KeyPath, signer.PublicAlgorithm())
		return signer, false
	}

	if errors.Is(stepErr, sign.ErrSecurityKeyOnly) {
		if s, err := sign.SecurityKeySigner(cfg.KeyPath); err == nil {
			ui.Warn("ssh key %s is a smartcard security key; signing via the ssh-agent", cfg.KeyPath)
			return s, true
		} else {
			ui.Warn("ssh key %s is a smartcard security key, but no ssh-agent identity matches (%v); committing unsigned", cfg.KeyPath, err)
			return nil, true
		}
	}

	ui.Warn("ssh key %s unusable (%v); committing unsigned", cfg.KeyPath, stepErr)
	return nil, false
}

// commitLabel renders the step label shown while the commit is being created.
func commitLabel(id gitops.Identity, signed bool) string {
	if signed {
		return "committing as " + id.Name + " <" + id.Email + "> (signed)"
	}
	return "committing as " + id.Name + " <" + id.Email + ">"
}
