# ocommit

**Plain, stupid-simple git commit utility.** Zero CLI arguments, environment
variables only, pure Go statement.

`ocommit` runs inside a git working tree and performs the equivalent of:

```console
git add -A && git commit -asm update && git log
```

The commit message is generated for you: when a local Ollama instance is
reachable, the staged diff is summarized into a detailed description and a
short TL;DR subject. Commits can be signed with an SSH key, all without a
single command line flag.

No git binary is required at runtime. Repository handling (detect, stage,
diff, commit, log) is built on [go-git]; SSH commit signing is implemented in
pure Go with [hiddeco/sshsig]; the LLM integration talks to Ollama's REST API
directly.

[go-git]: https://github.com/go-git/go-git
[hiddeco/sshsig]: https://github.com/hiddeco/sshsig

## Features

- **Zero arguments** — `ocommit` parses no command line parameter. Every
  option is an environment variable, so it composes cleanly in scripts,
  aliases and pre-commit hooks.
- **Native git in Go** — uses go-git for repository discovery, `git add -A`,
  unified diffs, commit creation and `git log` output. No external `git`
  process at runtime.
- **SSH-signed commits** — set `OCOMMIT_KEY_PATH` to an SSH private key and
  the commit is signed in git's SSH signing format (`BEGIN SSH SIGNATURE`),
  the same format `git commit -S` with an SSH key produces. If the key is
  configured but unusable, ocommit logs a warning and commits unsigned.
- **AI commit messages** — point `OLLAMA_DESC_URL` at a local Ollama and the
  staged diff is sent to the model twice: once to write a detailed commit
  description, once to condense it into a one-line TL;DR used as the commit
  subject.
- **Flexible identity** — `OCOMMIT_NAME`/`OCOMMIT_EMAIL` set the author and
  committer explicitly; otherwise the standard `GIT_AUTHOR_*`/
  `GIT_COMMITTER_*` variables, then the repository's git config, then
  `OCOMMIT, Git Commiter <git@ocommit.local>`.
- **Always succeeds** — if no repo is found, or Ollama is unreachable, or no
  signing key is configured, ocommit still does the right, minimal thing.

## Requirements

- Go 1.26+ to build.
- A git repository (discovered by walking up from the current directory,
  exactly like git does).
- Optional: a running [Ollama] server for AI messages.
- Optional: an SSH private key for signed commits.

[Ollama]: https://ollama.com

## Install

```console
go install paepcke.de/ocommit@latest
```

Or build from source:

```console
git clone https://example.invalid/ocommit   # wherever you host it
cd ocommit
make build            # produces ./ocommit
sudo install -m0755 ocommit /usr/local/bin/
```

## Usage

```console
$ ocommit
ocommit: staging all changes
ocommit: reading staged diff
ocommit: committing
ocommit: committed abc1234
abc1234  Test User <test@example.com>  2026-08-08
    update
```

`ocommit` never blocks on arguments. All behavior is opt-in via environment
variables:

| Variable             | Effect                                                            |
| -------------------- | ----------------------------------------------------------------- |
| `OCOMMIT_KEY_PATH`   | Path to an SSH **private** key. When set and valid, the commit is SSH-signed. If set but unusable, warns and commits unsigned. |
| `OLLAMA_DESC_URL`    | Base URL of a local Ollama REST API, e.g. `http://127.0.0.1:11434`. When set and reachable, generates the commit message from the staged diff. |
| `OLLAMA_DESC_MODEL`  | Ollama model name to use (optional). Defaults to `llama3.2`.      |
| `OCOMMIT_NAME`       | Commit author/committer name (optional). Falls back to git config, then `OCOMMIT, Git Commiter`. |
| `OCOMMIT_EMAIL`      | Commit author/committer email (optional). Falls back to git config, then `git@ocommit.local`. |

> Signing: only passphrase-less keys are supported (there is no interactive
> prompt, by design). `ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_ocommit` is
> your friend. A missing or invalid key at the configured path never blocks a
> commit: ocommit logs the problem and proceeds unsigned.

### AI message flow

When `OLLAMA_DESC_URL` is set **and** the server answers `/api/tags`:

1. ocommit stages everything and builds the staged diff (the same text
   `git diff` would show against HEAD, with rename detection).
2. The diff is sent to the model with a prompt asking for a detailed,
   explanatory commit message (what changed and why).
3. That detailed message is sent back to the model in a fresh request asking
   for a one-line TL;DR (max 72 chars, imperative mood).
4. The TL;DR becomes the commit subject; the full details follow below it.

If the server is unreachable or generation fails, ocommit logs a warning and
falls back to the default subject `update` — it never blocks a commit on the
network.

## How it works

```
┌──────────────────────────────────────────────────────────┐
│ ocommit (pure Go, no git binary, no CLI args)             │
│                                                          │
│  1. find enclosing repo  (go-git PlainOpen, walk up)     │
│  2. stage all            (Worktree.AddWithOptions All)   │
│  3. build staged diff    (HEAD tree → index tree)        │
│  4. optional: Ollama two-pass message generation         │
│  5. optional: SSH sign   (hiddeco/sshsig, "git" ns, sha512)│
│  6. create commit        (object.Commit, advance HEAD)   │
│  7. print git log                                        │
└──────────────────────────────────────────────────────────┘
```

The commit object is written exactly as git writes it: tree, parents,
author/committer, message, and — when signing — a `gpgsig`-style SSH
signature header covering the header-less payload. The result is a
first-class signed commit that `git log --show-signature` and `git
verify-commit` can check.

## Environment-only design

`ocommit` deliberately does not parse any command line parameter. This keeps
it:

- **Scriptable** — `OLLAMA_DESC_URL=http://… ocommit` inside any repo.
- **Hookable** — drop it in a git `pre-commit` or a global `alias
  commit='ocommit'`.
- **Composable** — no flags to remember, no `--help` to read.

## Development

```console
make test        # go test ./...
make build       # go build -o ocommit ./cmd/ocommit
make vet         # go vet ./...
```

Layout:

```
cmd/ocommit/            entry point (env → pipeline → output)
internal/config/        env var config
internal/gitops/        repo detection, staging, diff, commit, log
internal/sign/          SSH key loading + armored signing
internal/ollama/        Ollama REST client + prompt template
internal/output/        stream-appropriate formatting
```

## License

MIT. See [LICENSE](LICENSE).
