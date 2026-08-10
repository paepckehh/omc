# AGENTS.md

Guidance for human and AI agents working on the `omc` codebase.

## Project

`omc` (spoken: *"oh-my-commit"*) is a plain, stupid-simple git commit
utility written in pure Go. It takes **no command line arguments**: every
behavior is controlled by environment variables.

Core promise: inside a git working tree it performs the equivalent of
`git add -A && git commit -asm update` — with optional SSH commit
signing and optional LLM-generated commit messages via a local Ollama
instance. Every line of output is a structured, timestamped log record.

## Hard constraints

- **No CLI flags.** All configuration comes from the environment:
  `OMC_SIGN_KEY_PATH`, `OLLAMA_DESC_URL`, `OLLAMA_DESC_MODEL`,
  `OMC_NAME`, `OMC_EMAIL`, `OMC_SUBJECT`, `OMC_MESSAGE`,
  `OMC_TAG`, `OMC_PUSH_KEY_PATH`.
- **No runtime dependency on external binaries.** No `git`, no `ssh-keygen`.
  Everything uses libraries: `github.com/go-git/go-git/v5` for repository
  operations, `github.com/hiddeco/sshsig` + `golang.org/x/crypto/ssh` for
  SSH signing. FIDO2 security-key keys (id_ed25519_sk / id_ecdsa_sk) sign
  through the running ssh-agent (`golang.org/x/crypto/ssh/agent`), which
  talks to the smartcard; no agent required for ordinary software keys.
- **Always degrade, never block.** If Ollama is unreachable or the LLM call
  fails, fall back to the default subject `update` and continue. If the key
  file is missing/invalid when configured, log a warning and commit
  unsigned. A configured security-key handle without a matching ssh-agent
  identity degrades the same way (warn + unsigned commit). If
  `OMC_PUSH_KEY_PATH` is set but unusable, or the push fails, log a warning
  and leave the commit and tag local (never rolled back). Not being in a
  repo is an error.
- **Pure Go, single static binary.** Any new dependency must be pure Go.

## Where things live

```
cmd/omc/main.go       pipeline: repo → stage → diff → (overrides|ollama) → (sign) → commit → tag → (push)
internal/config/      config.FromEnv() reads the nine env vars
internal/gitops/      PlainOpen detection, StageAll, StagedDiff, Commit,
                      SignedCommit, ResolveIdentity, index→tree writer,
                      LatestSemverTag, NextSemverTag, CreateTag, SignedTag,
                      ValidSemverTag, NormalizeTag, PushToRemote
internal/sign/        sign.Load(keyPath), sign.SecurityKeySigner(keyPath),
                      signer.Sign(payload) → armored SSH sig; security-key
                      (FIDO2) detection & agent delegation (sign.DetectKind,
                      sign.IsSecurityKeyPath, sign.HandleMatches)
internal/ollama/      Client.Available(), DescribeDetail(), SummarizeTLDR()
internal/output/      UI: stdout = structured results, stderr = structured diagnostics
internal/version/     Version string: hardwired semver, overridable via linker -X
```

## Release / version stamping

The program version lives in `internal/version/version.go` as a single
package-level `var Version = "vX.Y.Z"` and is printed as a structured
INFO record at startup (see `output.UI.Startup`).

**When tagging a release, the version in code MUST be bumped to match the
new git tag** before committing and tagging. The two must always agree:

1. Edit `internal/version/version.go` and set `Version` to the new tag
   (e.g. `v0.1.16`).
2. Commit that change.
3. `git tag` the commit with the same semver value.

The hardwired constant is the fallback. The Makefile overrides it at link
time with the git tag via `-ldflags "-X
paepcke.de/omc/internal/version.Version=$(VERSION)"`, where `VERSION ?=
$(shell git describe --tags --abbrev=0 ...)`. So a `make build` always
stamps the binary with the exact tag it was built from; a plain `go
build` uses the hardwired constant. Either way the version printed at
startup is correct as long as the constant and the latest tag agree.

## Auto semver tagging

After every successful commit, `omc` tags that commit with a semver tag:

1. If `OMC_TAG` is set and parses as strict semver, that name (with a
   leading `v` added for bare versions) is used verbatim. Otherwise the
   auto-bump path runs.
2. `gitops.LatestSemverTag(repo)` scans all `refs/tags/v*.*.*` refs and
   returns the highest semver version (pre-release suffixes ignored).
3. `gitops.NextSemverTag(latest)` bumps the patch segment. An empty latest
   (no prior tag) yields `v0.0.1`.
4. `gitops.CreateTag` / `gitops.SignedTag` creates an **annotated** tag object
   on the new commit. The tag message is the commit's subject line. When a
   signer is available the tag is SSH-signed with the same key used for the
   commit — the armored `BEGIN SSH SIGNATURE` block is appended to the tag
   object content, byte-compatible with `git tag -s`.
5. If the tag step fails (e.g. name collision), `omc` logs a warning and
   exits 0. The commit is never rolled back over a tag failure.

## Commit message format

The final message is:

```
<TL;DR subject, ≤72 chars>

<full detailed description from the LLM>
```

- First line: the shortened TL;DR (LLM) or `update` (fallback or no Ollama).
- Blank line, then the full detail body.
- When signing, an SSH signature header is embedded in the commit object;
  the payload signed is the commit without that header (git-conformant).
- The body is optional: if the LLM returns only a subject, that is used.

## Message & tag overrides (OMC_SUBJECT / OMC_MESSAGE / OMC_TAG)

The subject, body, and tag can be supplied directly from the environment,
which **wins over LLM generation**: when either `OMC_SUBJECT` or
`OMC_MESSAGE` is set, the Ollama two-pass generation is skipped entirely
and the override text lands verbatim in the commit object.

### Subject / message pairing rules

| `OMC_SUBJECT` | `OMC_MESSAGE` | Subject used       | Body used               |
| ------------- | ------------- | ------------------ | ----------------------- |
| set           | set           | `OMC_SUBJECT`      | `OMC_MESSAGE`           |
| set           | unset         | `OMC_SUBJECT`      | `OMC_SUBJECT`           |
| unset         | set           | first line of `OMC_MESSAGE` (shortened to ≤72 chars) | full `OMC_MESSAGE` |
| unset         | unset         | LLM TL;DR or `update` | LLM detail (if any)  |

Whitespace around the override values is trimmed. When only
`OMC_MESSAGE` is set, its first non-empty line becomes the subject
(mirroring the ≤72-char TL;DR contract of the LLM path); the full message
is still used as the body.

### Tag override

`OMC_TAG` lets the caller name the tag explicitly instead of bumping the
patch of the latest semver tag. The value is used **only** when it parses as
strict semver `vMAJOR.MINOR.PATCH` (an optional leading `v`; three
non-negative integer segments without leading zeros; no pre-release/build
suffix). Arbitrarily large values in any of the three segments are accepted,
so jumps like `v999.0.0` are valid.

- A bare `1.2.3` is normalized to `v1.2.3`.
- An invalid override (e.g. `v1.2`, `v1.2.3-rc.1`, `latest`, `v01.2.3`) is
  **not** used: `omc` logs a warning and falls back to the normal
  `LatestSemverTag` + `NextSemverTag` auto-bump path. The commit is never
  rolled back over a bad tag override.

`gitops.ValidSemverTag(name)` is the gate; `gitops.NormalizeTag(name)` adds
the leading `v` for bare versions.

## Environment variables

| Variable            | Meaning                                              | Behavior if set but broken                |
| ------------------- | ---------------------------------------------------- | ----------------------------------------- |
| `OMC_SIGN_KEY_PATH` | Path to SSH private key; security-key handles (`id_ed25519_sk` / `id_ecdsa_sk`) sign via the ssh-agent + smartcard | Warn, commit unsigned                     |
| `OLLAMA_DESC_URL`   | Base URL of local Ollama REST API                    | Warn and use default `update` subject     |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional, default `llama3.2`)     | Default used                              |
| `OMC_NAME`          | Commit author/committer name (optional)              | Falls back to git config, then default    |
| `OMC_EMAIL`         | Commit author/committer email (optional)             | Falls back to git config, then default    |
| `OMC_SUBJECT`       | Override the commit subject (skips LLM generation)   | Trimmed; see pairing rules above          |
| `OMC_MESSAGE`       | Override the commit body (skips LLM generation)      | Trimmed; see pairing rules above          |
| `OMC_TAG`           | Override the tag name (strict semver only)           | Invalid → warn + auto-bump fallback       |
| `OMC_PUSH_KEY_PATH` | Path to SSH private key for pushing to the remote; security-key handles authenticate via the ssh-agent | Unreadable/unusable or push fails → warn, leave local |

## Pushing (OMC_PUSH_KEY_PATH)

After the commit and the semver tag are created, `omc` optionally pushes
them to the repository's default remote — the go-git equivalent of
`git push; git push --tags`:

1. `OMC_PUSH_KEY_PATH` must be set **and** the key readable. If it is
   unset, no push happens. If it is set but unreadable/unparseable, `omc`
   logs a warning and skips the push.
2. The current branch is pushed first (`refs/heads/<branch>`), then all
   local tags (`+refs/tags/*`), mirroring `git push --tags`.
3. The key authenticates over SSH. For non-SSH remotes (https/file) the key
   is not applicable and go-git's default auth is used. When the key path is
   empty, go-git falls back to its default auth (SSH agent).
4. **Security-key (FIDO2) keys**: a push key path that `sign.IsSecurityKeyPath`
   recognizes (e.g. `~/.ssh/id_ed25519_sk`) switches to agent-based auth —
   the ssh-agent offers the matching identity (found via `sign.HandleMatches`
   against the handle / its `.pub` file) and the smartcard enforces the
   touch check. When no agent is running or the identity is not loaded, the
   push is skipped with a warning; the commit and tag stay local.
5. `NoErrAlreadyUpToDate` is treated as success. Any other failure (no
   remote, non-fast-forward, network) logs a warning and exits 0 — the
   commit and tag are never rolled back over a push problem.
6. When there is nothing to commit or tag (clean working tree), the push
   step still runs: a previous run may already have committed and tagged
   locally while its push was skipped or failed, so the pending tags are
   published now. Failures degrade exactly like the main push step.

`gitops.PushToRemote(repo, keyPath)` returns a `PushResult{Remote, Branch,
Tags}`; `output.UI.PushResult` renders the structured `pushed` record.

## Security keys (FIDO2)

`omc` supports FIDO2 security-key keys (`ssh-keygen -t ed25519-sk` /
`-t ecdsa-sk`) for both commit/tag signing and pushing. The file
`~/.ssh/id_ed25519_sk` is **not** a private key: it references a key bound
to a smartcard or platform authenticator. All of this stays pure Go — no
PKCS#11, no `ssh-agent` binary, no CGO:

- **Detection** — `sign.IsSecurityKeyPath(path)` recognizes the
  conventional `id_ed25519_sk` / `id_ecdsa_sk` file names (full path or
  basename, with suffix tolerance like `_rk`). `sign.DetectKind(path)` refines
  this by actually probing the file: it parses as a software key
  (`KindSoftware`), as a security-key handle via the adjacent `.pub` file
  (`KindSecurityKey`), or as neither (`KindBroken`).
- **Signing** — `sign.Load` refuses security-key files with
  `ErrSecurityKeyOnly` (the private half is not on disk). The CLI's
  `loadSigner` detects the kind first and calls
  `sign.SecurityKeySigner(keyPath)`: it connects to the ssh-agent via
  `SSH_AUTH_SOCK` (pure Go, `x/crypto/ssh/agent`), finds the identity whose
  public blob matches the handle/`.pub` via `sign.HandleMatches`, and
  returns a signer that delegates every signature to the agent. The
  authenticator enforces the touch/PIN prompt itself. If the agent is
  unreachable or does not hold the identity, omc warns and commits unsigned
  (the usual "always degrade" contract).
- **Pushing** — a security-key path in `OMC_PUSH_KEY_PATH` builds a
  `sshpkg.PublicKeysCallback` from the agent's matching signers, so the push
  authenticates with the device key instead of a file key. No agent → warn
  and skip the push.
- **Signature format** — the agent-side signer produces signatures under
  the `sk-*` algorithm; `Signer.PublicAlgorithm()` reports e.g.
  `sk-ssh-ed25519@openssh.com` so the structured notices show the actual
  algorithm. Armored signatures stay byte-compatible with `git commit -S` /
  `git tag -s` (git accepts them; `git log --show-signature` /
  `git verify-commit` work).
- **Touch notice** — every smartcard-bound step (commit signing, tag
  signing, and the SSH push when `OMC_PUSH_KEY_PATH` names a security-key
  handle) is preceded by a structured `touch` notice on stderr via
  `output.UI.SecurityKeyTouchNotice`, telling the user to touch their
  smartcard/yubikey when it blinks to authorise the operation. Each
  signature/auth challenge forwarded by the ssh-agent to the authenticator
  is a fresh user-presence check, so the notice is emitted once per step
  that needs a touch (commit and tag are separate touches).
- **Touch countdown** — on a TTY, each smartcard-bound step additionally
  runs an animated, rewriting countdown (`output.UI.stepTouch` via
  `StepTouchCommit` / `StepTouchTag` / `StepTouchPush`) while the blocking
  operation waits for the device: a bold `🔑 TOUCH YOUR SECURITY KEY`
  prompt, a shrinking progress bar, and an `M:SS` timer starting at 30s.
  The countdown runs concurrently with the signature/push call and stops
  the moment the operation returns, so the urgency of the pending touch is
  unmistakable. On a non-TTY it degrades to the ordinary structured
  `INFO`/`OK`/`FAIL` step records so captured logs stay greppable.
- **Limitations** — resident/discoverable or verify-required (-O
  verify-required) keys cannot be *enforced* by omc; they behave like
  ordinary sk keys, with whatever user verification the device and agent
  policy implements. Without `ssh-add`ed identity the security-key paths
  degrade to unsigned commit / skipped push.

## Git identity

`internal/gitops.ResolveIdentity()` resolves the commit identity in this
order:

1. `OMC_NAME` / `OMC_EMAIL` (omc's own variables)
2. `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL` then `GIT_COMMITTER_*` (standard git
   variables)
3. `user.name` / `user.email` from the repository's git config (read via
   go-git, no external binary)
4. Defaults: `OMC, Git Commiter <git@omc.local>`

Environment always wins over git config. Config files are only consulted
for the identity fallback; nothing else depends on them.

## Worktree/OS notes

- Repositories are opened with `PlainOpenWithOptions(DetectDotGit: true)`
  from the current directory — subdirectories work.
- Tests `t.TempDir()` + `os.Chdir(dir)`; they create real git repos with
  `git.PlainInit` and never shell out to git except the e2e binary test
  (which skips if `git` is unavailable).
- `writeIndexTree` builds tree objects from the index; it must produce trees
  byte-identical to git so that `git log --show-signature` and `git
  verify-commit` accept signed commits.
- `SignedTag` builds tag objects with an appended SSH signature block; the
  signed payload is the full tag object content (object/type/tag/tagger/message)
  before the signature, matching what `git tag -s` signs. `git verify-tag`
  accepts the result.

## Testing

- `go test ./...` — full suite (config, sign, ollama, gitops, binary e2e).
- `-short` skips the binary e2e test that compiles the CLI.
- Always run `go vet ./...` after changes; `make test` / `make vet` are the
  shortcuts.

## Definition of done

1. `gofmt -s -w .`
2. `go build ./...` passes
3. `go test ./...` passes
4. `go vet ./...` passes
5. `internal/version/version.go` `Version` matches the latest git tag
   (see "Release / version stamping").
6. Behavior honored: repo detection, stage-all, optional signing, optional
   Ollama message generation, structured timestamped log output (starting
   with the version banner), and auto semver tagging of the new commit
   (signed when a key is configured).
