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
instance, or an OpenAI-compatible endpoint as a fallback. Every line of
output is a structured, timestamped log record.

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
- **Always degrade, never block.** Every smartcard (FIDO2) touch wait is
  bounded by `OMC_TOUCH_TIMEOUT` (default 60s, 0 = forever) and degrades on
  timeout exactly like an unusable key: warn, commit unsigned, warn on
  tag/pull/push. Touch prompts (records, TTY countdown, optional desktop
  notification via D-Bus — `github.com/godbus/dbus/v5`, pure Go) attach to
  the signature requests themselves, so the commit, the tag, the upstream
  tag pull and the push all prompt identically. The D-Bus notification
  backend is only probed when a security-key path (sign or push key) is
  configured, so non-smartcard runs never touch D-Bus at all. If Ollama is unreachable or the LLM
  call fails, fall back to the OpenAI-compatible endpoint
  (`OPENAPI_*`, with `OPENAPI_DESC_*` legacy aliases); if that is also unavailable or fails, fall back to
  `update`. If the key file is missing/invalid
  when configured, log a warning and commit unsigned. A configured
  security-key handle without a matching ssh-agent identity degrades the
  same way. If `OMC_PUSH_KEY_PATH` is set but unusable, or the push fails,
  log a warning and leave the commit and tag local (never rolled back).
  The pre-tag upstream tag pull (`gitops.PullTags`) is best-effort too: a
  repo without a remote has nothing to pull, and a failed pull only warns —
  the tag is then bumped from the local tag state, and the push-side
  collision guard (see "Auto semver tagging") makes that degrade loudly
  instead of clobbering the upstream tag.
  Not being in a repo is an error.
- **Pure Go, single static binary.** Any new dependency must be pure Go.

## Where things live

```
cmd/omc/main.go       pipeline: repo → stage → diff → (overrides|llm) → (sign) → commit → tag → (push)
                      llm = Ollama first, OpenAI-compatible endpoint fallback (llm.Generator)
internal/config/      config.FromEnv() reads the env vars
internal/gitops/      PlainOpen, StageAll, StagedDiff, Commit, SignedCommit,
                      ResolveIdentity, Scout, index→tree writer,
                      LatestSemverTag, NextSemverTag (digit-string semver,
                      overflow-proof), CreateTag, SignedTag,
                      ValidSemverTag, NormalizeTag, PullTags (upstream tag
                      pull, git fetch --tags), PushToRemote + pushTags
                      (tag pass that never rewrites an existing remote tag;
                      reports held-back names in PushResult.SkippedTags);
                      both accept a sign.TouchConfig so their SSH auth is
                      touch-bounded
internal/sign/        sign.Load, sign.SecurityKeySigner, signer.Sign → armored SSH sig;
                      FIDO2 detection & agent delegation (DetectKind, IsSecurityKeyPath, HandleMatches);
                      WrapTouch wraps every agent signature with the touch
                      prompt + enforced timeout (ErrTouchTimeout)
internal/touch/       shared TouchEvent/TouchHook vocabulary for smartcard
                      touch waits: sign produces the events, output consumes
internal/notify/     runtime-detected desktop notifications over D-Bus
                      (org.freedesktop.Notifications via godbus; pure Go)
internal/llm/         shared commit-message prompts + Generator interface (all backends)
internal/ollama/      Client.Available, DescribeDetail, SummarizeTLDR (implements llm.Generator)
internal/openai/      OpenAI-compatible fallback: /chat/completions + /models (implements llm.Generator)
internal/output/      UI: stdout = structured results, stderr = structured diagnostics;
                      animated scramble spinner, touch prompts (records +
                      countdown + deferred desktop notification; the UI
                      implements touch.Hook), nested log groups
internal/version/     Version string: hardwired semver, overridable via linker -X
docs/branding/        icon (omc-icon.svg), transparent mark (omc-mark.svg),
                      brand sheet (palette, geometry, usage); the icon heads
                      README.md
docs/hero-dark.svg    dark-theme README banner: brand lockup, value chips,
docs/hero-light.svg   pipeline dashboard, command box; light twin same layout.
                      README.md selects them with <picture>/prefers-color-scheme
docs/established-*.svg theme-aware "established" strip under the README nav:
                      release count, current version, zero-flag promise.
docs/screenshots/     terminal captures used by README.md; run.svg is a
                      verbatim capture of a real signed run
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
consumers can grep regardless of TTY mode. Field values that contain
whitespace, quotes, or non-printable runes are quoted (Go `strconv.Quote`
syntax) so every record stays a single space-separated token per field.
Animated lines (step spinners, touch countdowns) rewrite their line in
place with `\r` and erase it with `\r ESC[K` when they stop; the erase is
never terminated with a newline — that would leave the erased line in
the terminal as a stray empty line, one per spinner stop. The next
record overwrites the erased line instead and so starts at the start of
its own fresh line; the `internal/output` tests render the raw byte
stream through a mini terminal interpreter (`renderVT`) to pin both the
no-stray-empty-lines and the fresh-line-start contracts down.

**Empty lines are forbidden anywhere, TTY or not.** Log groups close
silently (`EndGroup` and the budget auto-close write no separator line),
record messages are sanitized so a message with a stray newline can
never split a record (and its `\n\n` never lands as a blank line), blank
lines are dropped from the LLM body in the flat log, and the spinner
erase is newline-free as above. `TestNoEmptyLinesAnywhere` pins the
whole contract: a full pipeline flow (nested groups, spinners, touch
wait, file list, results, preview frame) must render without a single
empty line on either path.

Right after the repository is opened, a `repo` step emits a context
diagnostic with `dir=` (worktree root), `latest_tag=`, and one
`<remote>_url=` field per configured remote URL.

Security-key touch waits emit their own `touch` step records: an INFO
record when the wait starts (key=, algo=, action=, timeout=), an OK
`touch confirmed` record on success (elapsed=, outcome=confirmed), and a
WARN `touch timed out` record on timeout (outcome=timeout). The animated
countdown, terminal bell, and the optional desktop notification are
TTY/DE extras on top; the records are always emitted, TTY or not.

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

## Release pipeline (GoReleaser)

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs
GoReleaser (pinned `v2.18.2`) on `ubuntu-24.04` with the Go version from
`go.mod`. The build matrix in `.goreleaser.yml` is **linux, darwin,
openbsd × amd64, arm64** (CGO disabled). freebsd is excluded because
`internal/notify` depends on `github.com/godbus/dbus/v5`, which does not
build there; windows builds but is not shipped. Before adding a target,
verify it with `CGO_ENABLED=0 GOOS=x GOARCH=y go build ./cmd/omc`, and
sanity-check the config with `goreleaser check` or a
`goreleaser release --snapshot --clean` dry run.

## Auto semver tagging

After every successful commit, `omc` tags that commit with a semver tag.
If `OMC_TAG` is set and parses as strict semver, that name (normalized
with a leading `v`) is used verbatim; otherwise the auto-bump path scans
all valid `refs/tags/v*.*.*` refs (pre-release/build suffixes use their
base version, malformed trailing segments are ignored) and bumps the patch
segment. When no prior tag exists, the first tag is `v0.0.1`. The tag is
annotated and SSH-signed with the same key used for the commit when a
signer is available. If the tag step fails, `omc` logs a warning and
exits 0; the commit is never rolled back.

### Tag sync invariants (bug hunt v0.1.62)

The pull → bump → create → push sequence is fail-proof against every
failure mode found in the hunt; the guards are pinned by tests in
`internal/gitops/push_test.go`, `internal/gitops/gitops_test.go`, and
`cmd/omc/main_push_test.go`:

1. **Fresh state first (upstream pull).** Before any tag name is
   generated, the pipeline pulls the upstream tag state (`gitops.PullTags`,
   the go-git equivalent of `git fetch --tags` against the default remote,
   authenticated with the push key on SSH remotes; a smartcard touch wait
   when the push key is an FIDO2 handle — prompted and bounded through the
   same touch policy, a timeout degrades to warn + local tag state). After
   a successful pull every local tag either matches the remote or is new
   on it, so the subsequent tag push is provably collision-free.
2. **Bump never rewinds (overflow-proof semver).** Components are compared
   and bumped as decimal digit strings, never `strconv.Atoi`: a component
   larger than any machine integer (`v0.0.99999999999999999999`) used to
   parse as 0, which misranked the tag and made the "next" tag rewind to
   an existing name (or wrap to a negative value). Now the successor is
   exact (`…99999` → `…00000` with the carry).
3. **Remote tags are never silently overwritten.** `ensureTagAbsent`
   (CreateTag/Signing) only guards LOCAL refs, so a failed upstream pull
   can still produce a colliding name. The push's tag pass is therefore
   non-forced, exactly like `git push --tags` (git also refuses to move a
   tag without `--force`): go-git rejects the collision client-side while
   building the request, aborting the whole wildcard pass — so
   `gitops.pushTags` retries per tag on that failure, holding back only
   the colliding name (`PushResult.SkippedTags`; the pipeline warns per
   tag: "exists on the remote pointing elsewhere; left the remote tag
   untouched") while every other pending tag still publishes. The old
   forced refspec (`+refs/tags/*:refs/tags/*`) silently clobbered the
   upstream tag in exactly this case. The per-tag retry costs one extra
   session per local tag but only ever runs in the collision case, never
   on the happy path (one wildcard session = one smartcard touch).
4. **Residual race, documented.** A tag appearing on the remote between
   the pull and the push advertisement is caught by the advertisement
   itself (guard 3). Only a change inside the same push session could
   still slip through — inherent to the git wire protocol without
   force-with-lease, which go-git supports for branches only.

The clean-tree push path (`pushNothing`) routes through the same
`pushPhase`/`PushToRemote`, so stranded local tags get the same protection
without a preceding pull.

## Commit message format

```
<TL;DR subject, ≤72 chars>

<full detailed description from the LLM>
```

The subject is the LLM TL;DR or `update` (fallback). The body is the
LLM detail or empty. When `OMC_SUBJECT`/`OMC_MESSAGE` overrides are set,
the LLM is skipped and the overrides land verbatim. See README.md for
the subject/message pairing rules.

The staged diff fed to the LLM suppresses lockfile content: `flake.lock`
(see `gitops.suppressedDiffFiles`, matched case-insensitively on the
base name, so subdirectories are covered) is machine-generated pin-hash
noise — a large list of hashes that trips the model — so its patch body
is replaced by a one-line stub (`<path> updated`, content suppressed).
The lockfile itself is staged and committed byte-for-byte like any other
file (the commit tree is built from the index, not from the diff); it
also keeps its entry in the changed-file list and the FileList UI. Only
the AI prompt never sees its content.

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
  test (which skips if `git` is unavailable). Push and pull tests run
  against an in-process git server (go-git `transport/server` +
  `MapLoader`), so no network and no binaries are involved.

## Testing

- `go test ./...` — full suite (config, sign, ollama, openai, gitops, output, e2e).
- `CGO_ENABLED=1 go test -race ./...` — exercises the spinner/touch/signing
  concurrency under the race detector.
- `-short` skips the binary e2e test that compiles the CLI.
- `make test` / `make vet` are the shortcuts.

## Definition of done

1. `gofmt -s -w .`
2. `go build ./...` passes
3. `go test ./...` passes
4. `go vet ./...` passes
5. `internal/version/version.go` `Version` matches the latest git tag.
6. Behavior honored: repo detection + context diagnostic, stage-all,
   optional signing, optional LLM message generation (Ollama, with an
   OpenAI-compatible endpoint fallback), structured timestamped log
   output (starting with the version banner), upstream tag pull before
   bumping, and auto semver tagging of the new commit (signed when a key
   is configured).
7. Security-key touch policy honored everywhere: prompts fire from the
   signature requests (commit, tag, pull, push), the wait is bounded by
   `OMC_TOUCH_TIMEOUT`, a timeout degrades, and desktop notifications are
   runtime-detected (`OMC_TOUCH_NOTIFY`) and never block the run.
