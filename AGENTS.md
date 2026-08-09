# AGENTS.md

Guidance for human and AI agents working on the `ocommit` codebase.

## Project

`ocommit` is a plain, stupid-simple git commit utility written in pure Go.
It takes **no command line arguments**: every behavior is controlled by
environment variables.

Core promise: inside a git working tree it performs the equivalent of
`git add -A && git commit -asm update && git log` — with optional SSH commit
signing and optional LLM-generated commit messages via a local Ollama
instance.

## Hard constraints

- **No CLI flags.** All configuration comes from the environment:
  `OCOMMIT_KEY_PATH`, `OLLAMA_DESC_URL`, `OLLAMA_DESC_MODEL`,
  `OCOMMIT_NAME`, `OCOMMIT_EMAIL`.
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
cmd/ocommit/main.go   pipeline: repo → stage → diff → (ollama) → (sign) → commit → log → tag
internal/config/      config.FromEnv() reads the five env vars
internal/gitops/      PlainOpen detection, StageAll, StagedDiff, Commit,
                      SignedCommit, Log, ResolveIdentity, index→tree writer,
                      LatestSemverTag, NextSemverTag, CreateTag, SignedTag
internal/sign/        sign.Load(keyPath), signer.Sign(payload) → armored SSH sig
internal/ollama/      Client.Available(), DescribeDetail(), SummarizeTLDR()
internal/output/      UI: stdout = results, stderr = diagnostics
```

## Auto semver tagging

After every successful commit, `ocommit` tags that commit with the next
patch-bumped semver tag (`vX.Y.N+1`):

1. `gitops.LatestSemverTag(repo)` scans all `refs/tags/v*.*.*` refs and
   returns the highest semver version (pre-release suffixes ignored).
2. `gitops.NextSemverTag(latest)` bumps the patch segment. An empty latest
   (no prior tag) yields `v0.0.1`.
3. `gitops.CreateTag` / `gitops.SignedTag` creates an **annotated** tag object
   on the new commit. The tag message is the commit's subject line. When a
   signer is available the tag is SSH-signed with the same key used for the
   commit — the armored `BEGIN SSH SIGNATURE` block is appended to the tag
   object content, byte-compatible with `git tag -s`.
4. If the tag step fails (e.g. name collision), `ocommit` logs a warning and
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

## Environment variables

| Variable            | Meaning                                              | Behavior if set but broken                |
| ------------------- | ---------------------------------------------------- | ----------------------------------------- |
| `OCOMMIT_KEY_PATH`  | Path to SSH private key                              | Warn, commit unsigned                     |
| `OLLAMA_DESC_URL`   | Base URL of local Ollama REST API                    | Warn and use default `update` subject     |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional, default `llama3.2`)     | Default used                              |
| `OCOMMIT_NAME`      | Commit author/committer name (optional)              | Falls back to git config, then default    |
| `OCOMMIT_EMAIL`     | Commit author/committer email (optional)             | Falls back to git config, then default    |

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
   Ollama message generation, git log output, and auto semver tagging of the
   new commit (signed when a key is configured).
