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
  `OCOMMIT_KEY_PATH`, `OLLAMA_DESC_URL`, `OLLAMA_DESC_MODEL`.
- **No runtime dependency on external binaries.** No `git`, no `ssh-keygen`.
  Everything uses libraries: `github.com/go-git/go-git/v5` for repository
  operations, `github.com/hiddeco/sshsig` + `golang.org/x/crypto/ssh` for
  SSH signing.
- **Always degrade, never block.** If Ollama is unreachable or the LLM call
  fails, fall back to the default subject `update` and continue. If the key
  file is missing/invalid when configured, that is a hard error (user asked
  for signing). Not being in a repo is an error.
- **Pure Go, single static binary.** Any new dependency must be pure Go.

## Where things live

```
cmd/ocommit/main.go   pipeline: repo → stage → diff → (ollama) → (sign) → commit → log
internal/config/      config.FromEnv() reads the three env vars
internal/gitops/      PlainOpen detection, StageAll, StagedDiff, Commit,
                      SignedCommit, Log, index→tree writer
internal/sign/        sign.Load(keyPath), signer.Sign(payload) → armored SSH sig
internal/ollama/      Client.Available(), DescribeDetail(), SummarizeTLDR()
internal/output/      UI: stdout = results, stderr = diagnostics
```

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
| `OCOMMIT_KEY_PATH`  | Path to SSH private key                              | Hard error; abort before committing       |
| `OLLAMA_DESC_URL`   | Base URL of local Ollama REST API                    | Warn and use default `update` subject     |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional, default `llama3.2`)     | Default used                              |

## Git identity

`internal/gitops.signature()` reads `GIT_AUTHOR_*` / `GIT_COMMITTER_*` env
vars and falls back to `ocommit <ocommit@localhost>`. It does NOT read git
config; keep it that way unless a change explicitly requires config files
(maintaining zero-runtime-dependency on git is the priority).

## Worktree/OS notes

- Repositories are opened with `PlainOpenWithOptions(DetectDotGit: true)`
  from the current directory — subdirectories work.
- Tests `t.TempDir()` + `os.Chdir(dir)`; they create real git repos with
  `git.PlainInit` and never shell out to git except the e2e binary test
  (which skips if `git` is unavailable).
- `writeIndexTree` builds tree objects from the index; it must produce trees
  byte-identical to git so that `git log --show-signature` and `git
  verify-commit` accept signed commits.

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
   Ollama message generation, git log output.
