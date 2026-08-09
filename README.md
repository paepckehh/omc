<div align="center">

# ocommit

**One command. No flags. Signed, AI-described commits your team can actually trust.**

`ocommit` is a plain, stupid-simple git auto-commit utility written in pure Go.
It takes **zero command line arguments** — every behavior is an environment
variable — and inside any git working tree it does the equivalent of:

```console
git add -A && git commit -asm <detailed llm generated commit message content> && git log
```

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Pure Go](https://img.shields.io/badge/Pure_Go-no_git_binary-7DD3FC?logo=go&logoColor=white)](https://github.com/paepckehh/ocommit)
[![License: MIT](https://img.shields.io/badge/License-MIT-22C55E?logo=opensourceinitiative&logoColor=white)](LICENSE)
[![Static Binary](https://img.shields.io/badge/Binary-static-F59E0B?logo=go&logoColor=white)](https://github.com/paepckehh/ocommit)
[![No Telemetry](https://img.shields.io/badge/Telemetry-none-EF4444?logo=privatedirection&logoColor=white)](#environment-only-design)

[Features](#-features) · [Quick Start](#-quick-start) · [Demo](#-demo) · [Why](#-why-another-commit-tool) · [Agentic Workflows](#-secure-agentic-workflows) · [How it works](#-how-it-works)

</div>

---

## ✨ Features

<table>
<tr>
<td width="50%" valign="top">

### 🚫 Zero arguments, zero TTY
Every option is an environment variable, so it composes in scripts, aliases, pre-commit hooks, and — critically — inside an **unattended agent** that has no keyboard and must not be asked for one.

</td>
<td width="50%" valign="top">

### 🟢 Native git in Go
Repository discovery, `git add -A`, unified diffs, commit creation, annotated tags, and `git log` all run through [go-git]. No external `git` process at runtime — only one static binary.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🔑 SSH-signed commits
Point `OCOMMIT_KEY_PATH` at an SSH private key and the commit carries an armored `BEGIN SSH SIGNATURE` header, byte-compatible with `git commit -S`. `git log --show-signature` and `git verify-commit` confirm it.

</td>
<td width="50%" valign="top">

### 🤖 AI commit messages
With `OLLAMA_DESC_URL` set, the staged diff is sent to your **local** model twice: once for a rich explanatory body, once to condense it into a one-line, imperative TL;DR. Your code never leaves your machine.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 👤 Flexible identity
`OCOMMIT_NAME`/`OCOMMIT_EMAIL` win, then the standard `GIT_AUTHOR_*`/`GIT_COMMITTER_*` variables, then the repo's git config, then a built-in default.

</td>
<td width="50%" valign="top">

### 🛡️ Always does the right, minimal thing
No repo → clear error. No Ollama → `update`. No key → unsigned. It never leaves a task half-done over infrastructure.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🏷️ Auto semver tagging
Every successful commit is immediately tagged with the next patch version (`vX.Y.N+1`). The latest `v*.*.*` tag is discovered, the patch is bumped, and an annotated tag is created on the new commit — SSH-signed when a key is configured, just like the commit itself.

</td>
<td width="50%" valign="top">

### 🔁 Idempotent & predictable
No flags to remember, no prompts to answer. Run it twice, get two commits and two bumped tags (`v0.0.1`, then `v0.0.2`). Tags never collide; the patch always advances.

</td>
</tr>
</table>

---

## 🚀 Quick Start

> Run from **anywhere inside a repo**. That's it.

```console
go run paepcke.de/ocommit/cmd/ocommit@latest
```

### Install

```console
go install paepcke.de/ocommit/cmd/ocommit@latest
```

Or build from source:

```console
git clone https://github.com/paepckehh/ocommit
cd ocommit
make build
sudo install -m0755 ocommit /usr/local/bin/
```

### Run

```console
export OLLAMA_DESC_URL=http://127.0.0.1:11434   # optional: AI messages
export OCOMMIT_KEY_PATH=~/.ssh/agent            # optional: SSH signing
ocommit
```

### Requirements

- Go 1.26+ to build (the binary itself is a single static executable).
- **Optional**: a running [Ollama] server for AI-generated commit messages.
- **Optional**: an SSH private key to sign commits.

---

## 🎬 Demo

When stderr is a terminal, `ocommit` renders a small, modern TUI built on
[charmbracelet/bubbles] + [lipgloss]: animated spinners per pipeline step,
a gradient progress bar for the two-stage LLM generation, and a styled
commit-result block with the recent git log.

```console
$ export OLLAMA_DESC_URL=http://127.0.0.1:11434
$ export OCOMMIT_KEY_PATH=~/.ssh/agent
$ ocommit
 ocommit  ✓ open
 ocommit  ✓ stage
 ocommit  ✓ diff
 ocommit  › internal/ollama/ollama.go
         › cmd/ocommit/main.go
 ocommit  ✓ ollama probing local ollama at http://127.0.0.1:11434
 ocommit  generating commit message ████████████████████████████
 ocommit  condensing to TL;DR        ████████████
 ocommit  commit message ready       ████████████████████████████
 ocommit  commit message:
 sign commit payloads with git's SSH signature format
 - Adds an armored BEGIN SSH SIGNATURE header to the commit object...
 ocommit  ✓ load key
 ocommit  🔑 signing with /home/me/.ssh/agent
 ocommit  ✓ commit  committing as Ada Lovelace <ada@example.com> (signed)
 ocommit  ✓ log
 ocommit  ✓ committed 9d3f2ab
 ocommit  ✓ tag  bumping semver patch
 ocommit  ✓ tagged v0.3.8 9d3f2ab (signed)
9d3f2ab  Ada Lovelace <ada@example.com>  2026-08-08
    sign commit payloads with git's SSH signature format
    signed: yes
```

Piped or non-interactive output (CI logs, captured tests) automatically falls
back to the original greppable line format:

```console
$ ocommit
ocommit: open detecting repository
ocommit: stage staging all changes
ocommit: diff reading staged diff
ocommit: ollama probing local ollama at http://127.0.0.1:11434
ocommit: ollama generating commit message 0%
ocommit: ollama condensing to TL;DR 50%
ocommit: ollama commit message ready 100%
ocommit: signing commit with ssh key /home/me/.ssh/agent
ocommit: committed 9d3f2ab
ocommit: tagged v0.3.8 9d3f2ab (signed)
9d3f2ab  Ada Lovelace <ada@example.com>  2026-08-08
    sign commit payloads with git's SSH signature format
    signed: yes
```

No arguments were typed. No `git` binary was spawned. No prompts were answered.
Everything that matters came from the environment.

---

## 🤔 Why another commit tool?

Commit hygiene is a **security control**, not a style preference. Signed,
meaningful commits are how you:

- **Hold agents accountable** — every change is attributable to a key you control.
- **Keep history readable** — "fix typo in login" becomes `fix: harden the login
  flow against timing attacks`.
- **Guard the supply chain** — a break in the signing chain is a break in the
  trust your `main` depends on.

The problem: your AI coding agent happily runs `git commit`, but you don't want
it holding the same keys you use to **push** to the remote. `ocommit` solves
exactly that — details under [Secure agentic workflows](#-secure-agentic-workflows).

---

## 🧭 Table of Contents

- [Features](#-features)
- [Quick Start](#-quick-start)
- [Demo](#-demo)
- [Why another commit tool?](#-why-another-commit-tool)
- [Secure agentic workflows](#-secure-agentic-workflows)
- [Usage](#-usage)
- [How it works](#-how-it-works)
- [Environment-only design](#-environment-only-design)
- [Development](#-development)
- [License](#-license)

---

## 🛡️ Secure agentic workflows

This is the scenario `ocommit` was built for. You're running an AI coding agent
— Cursor, Claude Code, Crush, a CI bot — and you want it to *commit* without
giving it the power to *publish*.

`ocommit` makes **separation of duties** trivial.

### 1. Separate signing keys from push keys (rights separation)

Your agent's shell gets only a **signing** key:

```console
export OCOMMIT_KEY_PATH=~/.ssh/agent           # can SIGN commits, cannot push
export OLLAMA_DESC_URL=http://127.0.0.1:11434  # lets it describe its own work
ocommit
```

The **push** to the remote uses a *different* credential — your human
`~/.ssh/id_ed25519`, a deploy key, or a short-lived CI token — that the agent
never sees.

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

---

## 📖 Usage

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
> prompt, by design). `ssh-keygen -t ed25519 -N "" -C agent@paepcke.de -f ~/.ssh/agent`
> is your friend. Scheduling note for CI: protect that key with filesystem
> permissions and rotate it like any credential.

<details>
<summary><b>🤖 AI message flow — how the commit body is generated</b></summary>

When `OLLAMA_DESC_URL` is set **and** the server answers `/api/tags`:

1. `ocommit` stages everything and builds the staged diff (what `git diff`
   would show against HEAD, with rename detection).
2. The diff is sent to the model, asking for a detailed, explanatory commit
   message — what changed and why.
3. That message is sent back in a fresh request, asking for a one-line TL;DR
   (max 72 chars, imperative mood).
4. The TL;DR becomes the subject; the full details follow below it.

The final message format is:

```
<TL;DR subject, ≤72 chars>

<full detailed description from the LLM>
```

- First line: the shortened TL;DR (LLM) or `update` (fallback or no Ollama).
- Blank line, then the full detail body.
- When signing, an SSH signature header is embedded in the commit object;
  the payload signed is the commit without that header (git-conformant).
- The body is optional: if the LLM returns only a subject, that is used.

If the server is unreachable or generation fails, `ocommit` logs a warning and
falls back to the default subject `update` — it never blocks a commit on the
network.

</details>

<details>
<summary><b>🏷️ Auto semver tagging — how the tag is created</b></summary>

Immediately after a successful commit, `ocommit` tags that commit with the next
patch-level semver tag:

1. All existing `refs/tags/v*.*.*` refs are scanned; the highest semver version
   is selected (pre-release suffixes like `-rc.1` are ignored for comparison).
2. The patch segment is bumped by one (`v1.2.3` → `v1.2.4`). When no semver
   tag exists yet, the first tag is `v0.0.1`.
3. An **annotated** tag object is created on the new commit. The tag message is
   the commit's subject line.
4. When `OCOMMIT_KEY_PATH` is set and the key is valid, the tag is **SSH-signed**
   with the same key used for the commit — the armored `BEGIN SSH SIGNATURE`
   block is embedded in the tag object, byte-compatible with `git tag -s`.
   When no key is configured, an unsigned annotated tag is created instead.

The tag step always runs after a commit. If the tag step fails (e.g. a tag of
that name already exists), `ocommit` logs a warning and exits 0 — the commit
itself is not affected.

```console
$ git tag -l 'v*'
v0.0.1
$ git tag -v v0.0.1    # verify the SSH signature
object 9d3f2ab...
type commit
tag v0.0.1
tagger Ada Lovelace <ada@example.com>

sign commit payloads with git's SSH signature format
tagger signature verified:
```

</details>

<details>
<summary><b>👤 Git identity resolution order</b></summary>

`internal/gitops.ResolveIdentity()` resolves the commit identity in this order:

1. `OCOMMIT_NAME` / `OCOMMIT_EMAIL` (ocommit's own variables)
2. `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL` then `GIT_COMMITTER_*` (standard git
   variables)
3. `user.name` / `user.email` from the repository's git config (read via
   go-git, no external binary)
4. Defaults: `OCOMMIT, Git Commiter <git@ocommit.local>`

Environment always wins over git config. Config files are only consulted for
the identity fallback; nothing else depends on them.

</details>

---

## ⚙️ How it works

```
┌──────────────────────────────────────────────────────────────┐
│ ocommit (pure Go, no git binary, no CLI args)                │
│                                                              │
│  1. find enclosing repo  (go-git PlainOpen, walk up)         │
│  2. stage all            (Worktree.AddWithOptions All)       │
│  3. build staged diff    (HEAD tree → index tree)            │
│  4. optional: Ollama two-pass message generation             │
│  5. optional: SSH sign   (hiddeco/sshsig, "git" ns, sha512)  │
│  6. create commit        (object.Commit, advance HEAD)        │
│  7. print git log                                            │
│  8. auto-tag             (latest v*.*.* → patch+1, signed)   │
└──────────────────────────────────────────────────────────────┘
```

The commit object is written exactly as git writes it: tree, parents,
author/committer, message, and — when signing — a `gpgsig`-style SSH signature
header covering the header-less payload. The result is a first-class signed
commit that `git log --show-signature` and `git verify-commit` accept. `ocommit`
builds its own trees from the index, byte-identical to what git would write, so
verification never hiccups.

<details>
<summary><b>🏗️ Where things live (project layout)</b></summary>

```
cmd/ocommit/            entry point (env → pipeline → output)
internal/config/        config.FromEnv() reads the five env vars
internal/gitops/        PlainOpen detection, StageAll, StagedDiff, Commit,
                        SignedCommit, Log, ResolveIdentity, index→tree writer,
                        semver tag discovery, CreateTag, SignedTag
internal/sign/          sign.Load(keyPath), signer.Sign(payload) → armored SSH sig
internal/ollama/        Client.Available(), DescribeDetail(), SummarizeTLDR()
internal/output/        UI: stdout = results, stderr = diagnostics
```

</details>

---

## 🌍 Environment-only design

`ocommit` deliberately parses no command line parameters because it's built for
places a human isn't watching:

- **Scriptable** — `OCOMMIT_KEY_PATH=… OLLAMA_DESC_URL=… ocommit` anywhere.
- **Hookable** — drop it in a `pre-commit` hook or a global `alias commit=ocommit`.
- **Composable** — nothing to remember, nothing to prompt for, plugs straight into
  the non-interactive `$PROMPT_COMMAND` / agent shells where flags are a liability.
- **Air-gapped by default** — the only network call is to your own Ollama, so the
  tool ships no telemetry and phones no home.

---

## 🛠️ Development

```console
make test        # go test ./...
make build       # go build -o ocommit ./cmd/ocommit
make vet         # go vet ./...
```

<details>
<summary><b>🧪 Testing notes</b></summary>

- `go test ./...` — full suite (config, sign, ollama, gitops, binary e2e).
- `-short` skips the binary e2e test that compiles the CLI.
- Tests use `t.TempDir()` + `os.Chdir(dir)`; they create real git repos with
  `git.PlainInit` and never shell out to git except the e2e binary test (which
  skips if `git` is unavailable).
- `writeIndexTree` builds tree objects from the index; it must produce trees
  byte-identical to git so that `git log --show-signature` and `git
  verify-commit` accept signed commits.
- Always run `go vet ./...` after changes.

</details>

<details>
<summary><b>🧭 Worktree / OS notes</b></summary>

- Repositories are opened with `PlainOpenWithOptions(DetectDotGit: true)`
  from the current directory — subdirectories work.
- Not being in a repo is an error.
- If the key file is missing/invalid when configured, log a warning and commit
  unsigned. If Ollama is unreachable or the LLM call fails, fall back to the
  default subject `update` and continue.

</details>

---

## 📄 License

MIT. See [LICENSE](LICENSE).

<div align="center">

<sub>Built with [go-git] · [charmbracelet/bubbles] · [lipgloss] · [hiddeco/sshsig] · [Ollama]</sub>

</div>

[Ollama]: https://ollama.com
[go-git]: https://github.com/go-git/go-git
[charmbracelet/bubbles]: https://github.com/charmbracelet/bubbles
[lipgloss]: https://github.com/charmbracelet/lipgloss
[hiddeco/sshsig]: https://github.com/hiddeco/sshsig
