# AGENTS.md

Guidance for human and AI agents working on the `ocommit` codebase.

## Project

`ocommit` is a plain, stupid-simple git commit utility written in pure Go.
It takes **no command line arguments**: every behavior is controlled by
environment variables.

Core promise: inside a git working tree it performs the equivalent of
`git add -A && git commit -asm update` — with optional SSH commit
signing and optional LLM-generated commit messages via a local Ollama
instance. Every line of output is a structured, timestamped log record.

## Hard constraints

- **No CLI flags.** All configuration comes from the environment:
  `OCOMMIT_KEY_PATH`, `OLLAMA_DESC_URL`, `OLLAMA_DESC_MODEL`,
  `OCOMMIT_NAME`, `OCOMMIT_EMAIL`, `OCOMMIT_SUBJECT`, `OCOMMIT_MESSAGE`,
  `OCOMMIT_TAG`.
- **No runtime dependency on external binaries.** No `git`, no `ssh-keygen`.
  Everything uses libraries: `github.com/go-git/go-git/v5` for repository
  operations, `github.com/hiddeco/sshsig` + `golang.org/x/crypto/ssh` for
  SSH signing.
- **Always degrade, never block.** If Ollama is unreachable or the LLM call
  fails, fall back to the default subject `update` and continue. If the key
  file is missing/invalid when configured, log a warning and commit
  unsigned. Not being in a repo is an error.
- **Pure Go, single static binary.** Any new dependency must be pure Go.

## Where things live

```
cmd/ocommit/main.go   pipeline: repo → stage → diff → (overrides|ollama) → (sign) → commit → tag
internal/config/      config.FromEnv() reads the eight env vars
internal/gitops/      PlainOpen detection, StageAll, StagedDiff, Commit,
                      SignedCommit, ResolveIdentity, index→tree writer,
                      LatestSemverTag, NextSemverTag, CreateTag, SignedTag,
                      ValidSemverTag, NormalizeTag
internal/sign/        sign.Load(keyPath), signer.Sign(payload) → armored SSH sig
internal/ollama/      Client.Available(), DescribeDetail(), SummarizeTLDR()
internal/output/      UI: stdout = structured results, stderr = structured diagnostics
```

## Auto semver tagging

After every successful commit, `ocommit` tags that commit with a semver tag:

1. If `OCOMMIT_TAG` is set and parses as strict semver, that name (with a
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
5. If the tag step fails (e.g. name collision), `ocommit` logs a warning and
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

## Message & tag overrides (OCOMMIT_SUBJECT / OCOMMIT_MESSAGE / OCOMMIT_TAG)

The subject, body, and tag can be supplied directly from the environment,
which **wins over LLM generation**: when either `OCOMMIT_SUBJECT` or
`OCOMMIT_MESSAGE` is set, the Ollama two-pass generation is skipped entirely
and the override text lands verbatim in the commit object.

### Subject / message pairing rules

| `OCOMMIT_SUBJECT` | `OCOMMIT_MESSAGE` | Subject used       | Body used               |
| ------------------ | ----------------- | ------------------ | ----------------------- |
| set                | set               | `OCOMMIT_SUBJECT`  | `OCOMMIT_MESSAGE`       |
| set                | unset             | `OCOMMIT_SUBJECT`  | `OCOMMIT_SUBJECT`       |
| unset              | set               | first line of `OCOMMIT_MESSAGE` (shortened to ≤72 chars) | full `OCOMMIT_MESSAGE` |
| unset              | unset             | LLM TL;DR or `update` | LLM detail (if any)  |

Whitespace around the override values is trimmed. When only
`OCOMMIT_MESSAGE` is set, its first non-empty line becomes the subject
(mirroring the ≤72-char TL;DR contract of the LLM path); the full message
is still used as the body.

### Tag override

`OCOMMIT_TAG` lets the caller name the tag explicitly instead of bumping the
patch of the latest semver tag. The value is used **only** when it parses as
strict semver `vMAJOR.MINOR.PATCH` (an optional leading `v`; three
non-negative integer segments without leading zeros; no pre-release/build
suffix). Arbitrarily large values in any of the three segments are accepted,
so jumps like `v999.0.0` are valid.

- A bare `1.2.3` is normalized to `v1.2.3`.
- An invalid override (e.g. `v1.2`, `v1.2.3-rc.1`, `latest`, `v01.2.3`) is
  **not** used: `ocommit` logs a warning and falls back to the normal
  `LatestSemverTag` + `NextSemverTag` auto-bump path. The commit is never
  rolled back over a bad tag override.

`gitops.ValidSemverTag(name)` is the gate; `gitops.NormalizeTag(name)` adds
the leading `v` for bare versions.

## Environment variables

| Variable            | Meaning                                              | Behavior if set but broken                |
| ------------------- | ---------------------------------------------------- | ----------------------------------------- |
| `OCOMMIT_KEY_PATH`  | Path to SSH private key                              | Warn, commit unsigned                     |
| `OLLAMA_DESC_URL`   | Base URL of local Ollama REST API                    | Warn and use default `update` subject     |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional, default `llama3.2`)     | Default used                              |
| `OCOMMIT_NAME`      | Commit author/committer name (optional)              | Falls back to git config, then default    |
| `OCOMMIT_EMAIL`     | Commit author/committer email (optional)             | Falls back to git config, then default    |
| `OCOMMIT_SUBJECT`   | Override the commit subject (skips LLM generation)    | Trimmed; see pairing rules above          |
| `OCOMMIT_MESSAGE`   | Override the commit body (skips LLM generation)       | Trimmed; see pairing rules above          |
| `OCOMMIT_TAG`       | Override the tag name (strict semver only)           | Invalid → warn + auto-bump fallback       |

## Git identity

`internal/gitops.ResolveIdentity()` resolves the commit identity in this
order:

1. `OCOMMIT_NAME` / `OCOMMIT_EMAIL` (ocommit's own variables)
2. `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL` then `GIT_COMMITTER_*` (standard git
   variables)
3. `user.name` / `user.email` from the repository's git config (read via
   go-git, no external binary)
4. Defaults: `OCOMMIT, Git Commiter <git@ocommit.local>`

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
5. Behavior honored: repo detection, stage-all, optional signing, optional
   Ollama message generation, structured timestamped log output, and auto
   semver tagging of the new commit (signed when a key is configured).
