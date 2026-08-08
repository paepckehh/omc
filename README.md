# ocommit

**One command. No flags. Signed, AI-described commits your team can actually trust.**

`ocommit` is a plain, stupid-simple git commit utility written in pure Go. It
takes **zero command line arguments** — every behavior is an environment
variable — and inside any git working tree it does the equivalent of:

```console
git add -A && git commit -asm update && git log
```

Except the commit message isn't `update`. When a local [Ollama] instance is
running, `ocommit` reads the staged diff, writes a detailed commit body, and
boils it down to a crisp TL;DR subject. And when you hand it an SSH key, the
commit comes out cryptographically signed — in real git SSH format that
`git log --show-signature` and `git verify-commit` accept.

[![Go Version](https://img.shields.io/github/go-mod/go-version/your-org/ocommit)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Pure Go](https://img.shields.io/badge/pure%20go-100%25-29BEB0.svg)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/go/paepcke.de/ocommit)](https://goreportcard.com/report/go/paepcke.de/ocommit)

---

## Why another commit tool?

Commit hygiene is a **security control**, not a style preference. Signed,
meaningful commits are how you:

- **Hold agents accountable** — every change is attributable to a key you control.
- **Keep history readable** — "fix typo in login" becomes `fix: harden the login
  flow against timing attacks`.
- **Guard the supply chain** — a break in the signing chain is a break in the
  trust your `main` depends on.

The problem: your AI coding agent happily runs `git commit`, but you don't want
it holding the same keys you use to **push** to the remote. `ocommit` solves
exactly that — details under [Secure agentic workflows](#secure-agentic-workflows).

## Demo

```console
$ export OLLAMA_DESC_URL=http://127.0.0.1:11434
$ export OCOMMIT_KEY_PATH=~/.ssh/keys/agent-commit
$ ocommit
ocommit: staging all changes
ocommit: reading staged diff
ocommit: ollama reachable at http://127.0.0.1:11434, generating commit message
ocommit: signing commit with ssh key /home/me/.ssh/keys/agent-commit
ocommit: committing (as Ada Lovelace <ada@example.com>, signed)
ocommit: committed 9d3f2ab
9d3f2ab  Ada Lovelace <ada@example.com>  2026-08-08
    sign commit payloads with git's SSH signature format
    signed: yes
```

No arguments were typed. No `git` binary was spawned. No prompts were answered.
Everything that matters came from the environment.

## Requirements

- Go 1.26+ to build (the binary itself is a single static executable).
- A git repository — discovered by walking up parent directories, exactly like git.
- **Optional**: a running [Ollama] server for AI-generated commit messages.
- **Optional**: an SSH private key to sign commits.

## Install

```console
go install paepcke.de/ocommit@latest
```

Or build from source:

```console
git clone https://github.com/your-org/ocommit
cd ocommit
make build              # produces ./ocommit
sudo install -m0755 ocommit /usr/local/bin/
```

Then just:

```console
ocommit
```

From anywhere inside a repo. That's it.

## Features

- **Zero arguments, zero TTY.** Every option is an environment variable, so it
  composes in scripts, aliases, pre-commit hooks, and — critically — inside an
  **unattended agent** that has no keyboard and must not be asked for one.
- **Native git in Go.** Repository discovery, `git add -A`, unified diffs,
  commit creation, and `git log` all run through [go-git]. No external `git`
  process at runtime — only one static binary.
- **SSH-signed commits.** Point `OCOMMIT_KEY_PATH` at an SSH private key and
  the commit carries an armored `BEGIN SSH SIGNATURE` header, byte-compatible
  with `git commit -S`. `git log --show-signature` and `git verify-commit`
  confirm it. If the key is configured but unusable, `ocommit` logs why and
  proceeds unsigned — it degrades, it never blocks.
- **AI commit messages.** With `OLLAMA_DESC_URL` set, the staged diff is sent
  to your local model twice: once for a rich explanatory body, once to condense
  it into a one-line, imperative TL;DR that becomes the subject. If the server
  is down, it warns and falls back to `update`. Your code never leaves your
  machine.
- **Flexible identity.** `OCOMMIT_NAME`/`OCOMMIT_EMAIL` win, then the standard
  `GIT_AUTHOR_*`/`GIT_COMMITTER_*` variables, then the repo's git config, then
  a built-in default.
- **Always does the right, minimal thing.** No repo → clear error. No Ollama →
  `update`. No key → unsigned. It never leaves a task half-done over infrastructure.

---

## Secure agentic workflows

This is the scenario `ocommit` was built for. You're running an AI coding agent
— Cursor, Claude Code, Crush, a CI bot — and you want it to *commit* without
giving it the power to *publish*.

`ocommit` makes **separation of duties** trivial:

### 1. Separate signing keys from push keys (rights separation)

Your agent's shell gets only a **signing** key:

```console
export OCOMMIT_KEY_PATH=~/.ssh/keys/host-key   # can SIGN commits, cannot push
export OLLAMA_DESC_URL=http://127.0.0.1:11434  # lets it describe its own work
ocommit
```

The **push** to the remote uses a *different* credential — your human
`~/.ssh/id_ed25519`, a deploy key, or a short-lived CI token — that the agent
never sees. The result:

| Activity | Credential | Holder |
| -------- | ---------- | ------ |
| Author + sign a commit locally | `agent-commit` signing key | the agent |
| Push to the remote | your personal SSH key / deploy key | **you** |

Even if the agent is compromised, its key can authenticate against `main` all
it wants — it can't publish a single object. The signing key signs; only your
push key publishes.

### 2. Human review *before* anything touches the remote

Because the agent only ever produces a **local signed commit**, the state of the
remote is still fully yours to decide:

```console
# 1. Agent does the work and signs it locally:
OCOMMIT_KEY_PATH=~/.ssh/keys/agent-commit ocommit

# 2. YOU review the actual signed object:
git log --show-signature -1

# 3. Satisfied? Then — and only then — you push with your own key:
git push
```

Nothing has left your machine until step 3. The agent can't sneak work past you;
every change is gated on a human verifying the signature *and* the diff.

### 3. Attestation for audit trails

Signed, described commits give you a cryptographically verifiable record of
"what the agent did, in the agent's own words, under the agent's own key." When
`OLLAMA_DESC_URL` is set, the commit body is the model's own explanation of the
change — so the commit message *is* the change's justification, auditable
forever in history.

### Real-world use cases

- **Agentic refactors at scale** — let the agent layer up small, descriptive,
  signed commits for a whole refactor, then review the *set* before a single
  push.
- **CI / cron automation** — scheduled dependency bumps `git add -A` and commit
  with their own signing key; humans stay in charge of the merge.
- **Paired human+agent** — the agent drafts and signs, you review with
  `git show --show-signature`, then push under your identity. Clean provenance
  for who *wrote* it vs. who *published* it.
- **Local-first AI** — the LLM talks to Ollama over localhost; your code, diffs,
  and commit bodies never touch a third-party API.

## Usage

All behavior is opt-in via environment variables. Nothing else.

```console
ocommit
```

| Variable | Effect |
| -------- | ------ |
| `OCOMMIT_KEY_PATH` | Path to an SSH **private** key. When set and valid, the commit is SSH-signed. If set but unusable, warns and commits unsigned. |
| `OLLAMA_DESC_URL` | Base URL of a local Ollama REST API, e.g. `http://127.0.0.1:11434`. When set and reachable, generates the commit message from the staged diff. |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional). Defaults to `llama3.2`. |
| `OCOMMIT_NAME` | Commit author/committer name (optional). Falls back to git config, then `OCOMMIT, Git Commiter`. |
| `OCOMMIT_EMAIL` | Commit author/committer email (optional). Falls back to git config, then `git@ocommit.local`. |

> **Signing:** only passphrase-less keys are supported (there is no interactive
> prompt, by design). `ssh-keygen -t ed25519 -N "" -f ~/.ssh/keys/agent-commit`
> is your friend. Scheduling note for CI: protect that key with filesystem
> permissions and rotate it like any credential.

### AI message flow

When `OLLAMA_DESC_URL` is set **and** the server answers `/api/tags`:

1. `ocommit` stages everything and builds the staged diff (what `git diff`
   would show against HEAD, with rename detection).
2. The diff is sent to the model, asking for a detailed, explanatory commit
   message — what changed and why.
3. That message is sent back in a fresh request, asking for a one-line TL;DR
   (max 72 chars, imperative mood).
4. The TL;DR becomes the subject; the full details follow below it.

If the server is unreachable or generation fails, `ocommit` logs a warning and
falls back to the default subject `update` — it never blocks a commit on the
network.

## How it works

```
┌──────────────────────────────────────────────────────────────┐
│ ocommit (pure Go, no git binary, no CLI args)                 │
│                                                              │
│  1. find enclosing repo  (go-git PlainOpen, walk up)         │
│  2. stage all            (Worktree.AddWithOptions All)       │
│  3. build staged diff    (HEAD tree → index tree)            │
│  4. optional: Ollama two-pass message generation             │
│  5. optional: SSH sign   (hiddeco/sshsig, "git" ns, sha512)  │
│  6. create commit        (object.Commit, advance HEAD)       │
│  7. print git log                                            │
└──────────────────────────────────────────────────────────────┘
```

The commit object is written exactly as git writes it: tree, parents,
author/committer, message, and — when signing — a `gpgsig`-style SSH signature
header covering the header-less payload. The result is a first-class signed
commit that `git log --show-signature` and `git verify-commit` accept. `ocommit`
builds its own trees from the index, byte-identical to what git would write, so
verification never hiccups.

## Environment-only design

`ocommit` deliberately parses no command line parameters because it's built for
places a human isn't watching:

- **Scriptable** — `OCOMMIT_KEY_PATH=… OLLAMA_DESC_URL=… ocommit` anywhere.
- **Hookable** — drop it in a `pre-commit` hook or a global `alias commit=ocommit`.
- **Composable** — nothing to remember, nothing to prompt for, plugs straight into
  the non-interactive `$PROMPT_COMMAND` / agent shells where flags are a liability.
- **Air-gapped by default** — the only network call is to your own Ollama, so the
  tool ships no telemetry and phones no home.

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

[Ollama]: https://ollama.com
[go-git]: https://github.com/go-git/go-git