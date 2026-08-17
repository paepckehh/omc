# AGENTS.md

Guidance for human and AI agents working on the `omc` codebase.

## FIXED REQUIREMENT — EVERY CHANGE, NO EXCEPTIONS

1. **Format** — `gofmt -s -w .`
2. **Build** — `go build -o omc ./cmd/omc` must succeed.
3. **Test** — `go test ./...` must pass.
4. **Vet** — `go vet ./...` must pass.
5. **Version** — bump `internal/version/version.go` `Version` to match the new tag.
6. **Commit** — `git add . && git commit -m '<descriptive message>'`
7. **Tag** — bump the patch segment only: `vX.Y.<N+1>`.
8. **Push** — `git pull && git pull --tags && git push && git push --tags`

## Project

`omc` (spoken: *"oh-my-commit"*) is a plain, stupid-simple git commit
utility written in pure Go. It takes **no command line arguments**: every
behavior is controlled by environment variables.

Core promise: inside a git working tree it performs the equivalent of
`git add -A && git commit -asm update` — with optional SSH commit
signing and optional LLM-generated commit messages via a local Ollama
instance. Every line of output is a structured, timestamped log record.

For user-facing documentation (features, quick start, demos, env var
table, detailed override rules), see **README.md**. This file is the
developer contract.

## Hard constraints

- **No CLI flags.** All configuration comes from the environment.
- **No runtime dependency on external binaries.** No `git`, no `ssh-keygen`.
  Everything uses libraries: `github.com/go-git/go-git/v5` for repository
  operations, `github.com/hiddeco/sshsig` + `golang.org/x/crypto/ssh` for
  SSH signing. FIDO2 security-key keys (`id_ed25519_sk` / `id_ecdsa_sk`)
  sign through the running ssh-agent (`golang.org/x/crypto/ssh/agent`),
  which talks to the smartcard; no agent required for ordinary software
  keys.
- **Always degrade, never block.** If Ollama is unreachable or the LLM
  call fails, fall back to `update`. If the key file is missing/invalid
  when configured, log a warning and commit unsigned. A configured
  security-key handle without a matching ssh-agent identity degrades the
  same way. If `OMC_PUSH_KEY_PATH` is set but unusable, or the push fails,
  log a warning and leave the commit and tag local (never rolled back).
  Not being in a repo is an error.
- **Pure Go, single static binary.** Any new dependency must be pure Go.

## Where things live

```
cmd/omc/main.go       pipeline: repo → stage → diff → (overrides|ollama) → (sign) → commit → tag → (push)
internal/config/      config.FromEnv() reads the env vars
internal/gitops/      PlainOpen, StageAll, StagedDiff, Commit, SignedCommit,
                      ResolveIdentity, Scout, index→tree writer,
                      LatestSemverTag, NextSemverTag, CreateTag, SignedTag,
                      ValidSemverTag, NormalizeTag, PushToRemote
internal/sign/        sign.Load, sign.SecurityKeySigner, signer.Sign → armored SSH sig;
                      FIDO2 detection & agent delegation (DetectKind, IsSecurityKeyPath, HandleMatches)
internal/ollama/      Client.Available, DescribeDetail, SummarizeTLDR
internal/output/      UI: stdout = structured results, stderr = structured diagnostics;
                      animated scramble spinner, touch countdown, nested log groups
internal/version/     Version string: hardwired semver, overridable via linker -X
```

## Output contract

Every line emitted to stdout or stderr is a structured log record of the
form `<HH:MM:SS> <LEVEL> omc [<step>] <message> [key=value ...]`.
On a TTY the records are styled with color, emoji, animated spinners, and
nested tree connectors (`┌─` first, `├─` middle, `└─` last) tying related
steps under group headers (`📂 preparing repository`, `💬 message
generation`, `🔑 signing key`, `📦 committing & publishing`). Group
headers themselves are timestamped structured lines. The only exception
is the commit message preview, rendered inside a rounded-border frame.
Off a TTY the same records are emitted as flat, greppable text. The text
tokens (`OK`, `INFO`, `WARN`, `FAIL`, step names) are always intact so
consumers can grep regardless of TTY mode.

Right after the repository is opened, a `repo` step emits a context
diagnostic with `dir=` (worktree root), `latest_tag=`, and one
`<remote>_url=` field per configured remote URL.

## Release / version stamping

The program version lives in `internal/version/version.go` as a single
package-level `var Version = "vX.Y.Z"` and is printed as a structured
INFO record at startup.

**When tagging a release, the version in code MUST be bumped to match the
new git tag** before committing and tagging:

1. Edit `internal/version/version.go` and set `Version` to the new tag.
2. Commit that change.
3. `git tag` the commit with the same semver value.

The Makefile overrides the constant at link time with the git tag via
`-ldflags "-X paepcke.de/omc/internal/version.Version=$(VERSION)"`, so a
`make build` always stamps the binary with the exact tag; a plain `go
build` uses the hardwired constant. Either way the version printed at
startup is correct as long as the constant and the latest tag agree.

## Auto semver tagging

After every successful commit, `omc` tags that commit with a semver tag.
If `OMC_TAG` is set and parses as strict semver, that name (normalized
with a leading `v`) is used verbatim; otherwise the auto-bump path scans
all `refs/tags/v*.*.*` refs and bumps the patch segment. When no prior
tag exists, the first tag is `v0.0.1`. The tag is annotated and
SSH-signed with the same key used for the commit when a signer is
available. If the tag step fails, `omc` logs a warning and exits 0; the
commit is never rolled back.

## Commit message format

```
<TL;DR subject, ≤72 chars>

<full detailed description from the LLM>
```

The subject is the LLM TL;DR or `update` (fallback). The body is the
LLM detail or empty. When `OMC_SUBJECT`/`OMC_MESSAGE` overrides are set,
the LLM is skipped and the overrides land verbatim. See README.md for
the subject/message pairing rules.

## Git identity

`gitops.ResolveIdentity()` resolves in this order:
`OMC_NAME`/`OMC_EMAIL` → `GIT_AUTHOR_*`/`GIT_COMMITTER_*` → repo git
config (`user.name`/`user.email` via go-git) → defaults
(`OMC, Git Commiter <git@omc.local>`). Environment always wins over
config. Config files are only consulted for the identity fallback.

## Worktree/OS notes

- Repositories are opened with `PlainOpenWithOptions(DetectDotGit: true)`
  from the current directory — subdirectories work.
- `writeIndexTree` builds tree objects from the index byte-identical to
  git so `git log --show-signature` and `git verify-commit` accept signed
  commits. `SignedTag` matches what `git tag -s` signs; `git verify-tag`
  accepts the result.
- Tests use `t.TempDir()` + `os.Chdir(dir)`; they create real git repos
  with `git.PlainInit` and never shell out to git except the e2e binary
  test (which skips if `git` is unavailable).

## Testing

- `go test ./...` — full suite (config, sign, ollama, gitops, output, e2e).
- `-short` skips the binary e2e test that compiles the CLI.
- `make test` / `make vet` are the shortcuts.

## Definition of done

1. `gofmt -s -w .`
2. `go build ./...` passes
3. `go test ./...` passes
4. `go vet ./...` passes
5. `internal/version/version.go` `Version` matches the latest git tag.
6. Behavior honored: repo detection + context diagnostic, stage-all,
   optional signing, optional Ollama message generation, structured
   timestamped log output (starting with the version banner), and auto
   semver tagging of the new commit (signed when a key is configured).
