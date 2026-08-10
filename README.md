<div align="center">

# omc

**oh-my-commit — one command, no flags. Signed, AI-described commits your team can actually trust.**

`omc` (spoken: *"oh-my-commit"*) is a plain, stupid-simple git auto commit 
sign tag push utility, with agent/human role seperation for signed commits
including llm generated commit message and final review/push for humans.
utility written in pure Go. It takes **zero command line arguments** — every
behavior is an environment variable — and inside any git working tree it does
the equivalent of:

```console
git add -A && git commit -asm <detailed llm generated commit message content> && git tag <old_semver+1>
```

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Pure Go](https://img.shields.io/badge/Pure_Go-no_git_binary-7DD3FC?logo=go&logoColor=white)](https://github.com/paepckehh/omc)
[![License: MIT](https://img.shields.io/badge/License-MIT-22C55E?logo=opensourceinitiative&logoColor=white)](LICENSE)
[![Static Binary](https://img.shields.io/badge/Binary-static-F59E0B?logo=go&logoColor=white)](https://github.com/paepckehh/omc)
[![No Telemetry](https://img.shields.io/badge/Telemetry-none-EF4444?logo=privatedirection&logoColor=white)](#-environment-only-design)

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
Repository discovery, `git add -A`, unified diffs, commit creation, and annotated tags all run through [go-git]. No external `git` process at runtime — only one static binary. History output is left to your own `git log` invocation.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🔑 SSH-signed commits
Point `OMC_SIGN_KEY_PATH` at an SSH private key and the commit carries an armored `BEGIN SSH SIGNATURE` header, byte-compatible with `git commit -S`. `git log --show-signature` and `git verify-commit` confirm it.

</td>
<td width="50%" valign="top">

### 🤖 AI commit messages
With `OLLAMA_DESC_URL` set, the staged diff is sent to your **local** model twice: once for a rich explanatory body, once to condense it into a one-line, imperative TL;DR. Your code never leaves your machine.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 👤 Flexible identity
`OMC_NAME`/`OMC_EMAIL` win, then the standard `GIT_AUTHOR_*`/`GIT_COMMITTER_*` variables, then the repo's git config, then a built-in default.

</td>
<td width="50%" valign="top">

### 🛡️ Always does the right, minimal thing
No repo → clear error. No Ollama → `update`. No key → unsigned. It never leaves a task half-done over infrastructure.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🏷️ Auto semver tagging
Every successful commit is immediately tagged with the next patch version (`vX.Y.N+1`). The latest `v*.*.*` tag is discovered, the patch is bumped, and an annotated tag is created on the new commit — SSH-signed when a key is configured, just like the commit itself. Set `OMC_TAG` to name the tag yourself.

</td>
<td width="50%" valign="top">

### ✏️ Message & tag overrides
Set `OMC_SUBJECT` / `OMC_MESSAGE` to bypass the LLM and write the commit text yourself, and `OMC_TAG` to name the tag. Perfect for pipelines that already know what the change is. Subject-only → subject doubles as the body; message-only → first line becomes the subject.

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🔁 Idempotent & predictable
No flags to remember, no prompts to answer. Run it twice, get two commits and two bumped tags (`v0.0.1`, then `v0.0.2`). Tags never collide; the patch always advances.

</td>
<td width="50%" valign="top">

</td>
</tr>
</table>

---

## 🚀 Quick Start

> Run from **anywhere inside a repo**. That's it.

```console
go run paepcke.de/omc/cmd/omc@latest
```

### Install

```console
go install paepcke.de/omc/cmd/omc@latest
```

Or build from source:

```console
git clone https://github.com/paepckehh/omc
cd omc
make build
sudo install -m0755 omc /usr/local/bin/
```

### Run

```console
export OLLAMA_DESC_URL=http://127.0.0.1:11434   # optional: AI messages
export OMC_SIGN_KEY_PATH=~/.ssh/agent           # optional: SSH signing
omc
```

### Requirements

- Go 1.26+ to build (the binary itself is a single static executable).
- **Optional**: a running [Ollama] server for AI-generated commit messages.
- **Optional**: an SSH private key to sign commits.

---

## 🎬 Demo

When stderr is a terminal, `omc` renders a structured, timestamped TUI
built on [charmbracelet/bubbles] + [lipgloss]: every line is a structured log
record of the form `<HH:MM:SS> <LEVEL> omc [<step>] <message> [key=value ...]`,
with animated spinners per pipeline step, a gradient progress bar for the
two-stage LLM generation, and color-coded levels (OK / INFO / WARN / FAIL).

```console
$ export OLLAMA_DESC_URL=http://127.0.0.1:11434
$ export OMC_SIGN_KEY_PATH=~/.ssh/agent
$ omc
12:04:07 INFO  omc open   detecting repository
12:04:07 OK    omc open   done
12:04:07 INFO  omc stage  staging all changes
12:04:07 OK    omc stage  done
12:04:07 INFO  omc diff   reading staged diff
12:04:07 OK    omc diff   done
12:04:07 INFO  omc diff   changed files  count=2
  › internal/ollama/ollama.go
  › cmd/omc/main.go
12:04:07 INFO  omc ollama probing local ollama at http://127.0.0.1:11434
12:04:07 OK    omc ollama done
12:04:07 INFO  omc ollama generating commit message  progress=  0%
12:04:08 INFO  omc ollama condensing to TL;DR         progress= 50%
12:04:08 INFO  omc ollama commit message ready        progress=100%
12:04:08 INFO  omc msg    commit message:
sign commit payloads with git's SSH signature format
 - Adds an armored BEGIN SSH SIGNATURE header to the commit object...
12:04:08 INFO  omc load   loading ssh signing key
12:04:08 OK    omc load   done
12:04:08 INFO  omc sign   signing commit with ssh key  key=/home/me/.ssh/agent
12:04:08 INFO  omc commit committing as Ada Lovelace <ada@example.com> (signed)
12:04:09 OK    omc commit done
12:04:09 OK    omc commit committed  hash=9d3f2ab signed=true
12:04:09 INFO  omc tag    bumping semver patch
12:04:09 OK    omc tag    done
12:04:09 OK    omc tag    tagged  tag=v0.3.9 hash=9d3f2ab signed=true
```

Piped or non-interactive output (CI logs, captured tests) automatically falls
back to the same structured, greppable line format without color codes:

```console
$ omc
12:04:07 INFO omc open detecting repository
12:04:07 OK omc open done
12:04:07 INFO omc stage staging all changes
12:04:07 OK omc stage done
12:04:07 INFO omc diff reading staged diff
12:04:07 OK omc diff done
12:04:07 INFO omc diff changed files count=2
  - internal/ollama/ollama.go
  - cmd/omc/main.go
12:04:07 INFO omc ollama probing local ollama at http://127.0.0.1:11434
12:04:07 OK omc ollama done
12:04:07 INFO omc ollama generating commit message progress=  0%
12:04:08 INFO omc ollama condensing to TL;DR progress= 50%
12:04:08 INFO omc ollama commit message ready progress=100%
12:04:08 INFO omc msg subject: sign commit payloads with git's SSH signature format
12:04:08 INFO omc msg body:
 - Adds an armored BEGIN SSH SIGNATURE header to the commit object...
12:04:08 INFO omc load loading ssh signing key
12:04:08 OK omc load done
12:04:08 INFO omc sign signing commit with ssh key key=/home/me/.ssh/agent
12:04:08 INFO omc commit committing as Ada Lovelace <ada@example.com> (signed)
12:04:09 OK omc commit done
12:04:09 OK omc commit committed hash=9d3f2ab signed=true
12:04:09 INFO omc tag bumping semver patch
12:04:09 OK omc tag done
12:04:09 OK omc tag tagged tag=v0.3.9 hash=9d3f2ab signed=true
```

Every field is a separate, greppable token — `tag=v0.3.9`, `hash=9d3f2ab`,
`signed=true` — so downstream pipelines and log aggregators can parse them
without ambiguous whitespace. The commit and tag results go to stdout; all
diagnostics and progress go to stderr.

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
it holding the same keys you use to **push** to the remote. `omc` solves
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

This is the scenario `omc` was built for. You're running an AI coding agent
— Cursor, Claude Code, Crush, a CI bot — and you want it to *commit* without
giving it the power to *publish*.

`omc` makes **separation of duties** trivial.

### 1. Separate signing keys from push keys (rights separation)

Your agent's shell gets only a **signing** key:

```console
export OMC_SIGN_KEY_PATH=~/.ssh/agent           # can SIGN commits, cannot push
export OLLAMA_DESC_URL=http://127.0.0.1:11434   # lets it describe its own work
omc
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
OMC_SIGN_KEY_PATH=~/.ssh/keys/agent-commit omc

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
omc
```

| Variable | Effect |
| -------- | ------ |
| `OMC_SIGN_KEY_PATH` | Path to an SSH **private** key. When set and valid, the commit is SSH-signed. If set but unusable, warns and commits unsigned. |
| `OLLAMA_DESC_URL` | Base URL of a local Ollama REST API, e.g. `http://127.0.0.1:11434`. When set and reachable, generates the commit message from the staged diff. |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional). Defaults to `llama3.2`. |
| `OMC_NAME` | Commit author/committer name (optional). Falls back to git config, then `OMC, Git Commiter`. |
| `OMC_EMAIL` | Commit author/committer email (optional). Falls back to git config, then `git@omc.local`. |
| `OMC_SUBJECT` | Override the commit subject. When set, **no LLM generation runs**. See [Message & tag overrides](#-message--tag-overrides). |
| `OMC_MESSAGE` | Override the commit body. When set, **no LLM generation runs**. See [Message & tag overrides](#-message--tag-overrides). |
| `OMC_TAG` | Override the tag name. Used only when it is strict semver `vMAJOR.MINOR.PATCH`; otherwise the auto-bump runs. See [Message & tag overrides](#-message--tag-overrides). |

> **Signing:** only passphrase-less keys are supported (there is no interactive
> prompt, by design). `ssh-keygen -t ed25519 -N "" -C agent@paepcke.de -f ~/.ssh/agent`
> is your friend. Scheduling note for CI: protect that key with filesystem
> permissions and rotate it like any credential.

<details>
<summary><b>🤖 AI message flow — how the commit body is generated</b></summary>

When `OLLAMA_DESC_URL` is set **and** the server answers `/api/tags`:

1. `omc` stages everything and builds the staged diff (what `git diff`
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

If the server is unreachable or generation fails, `omc` logs a warning and
falls back to the default subject `update` — it never blocks a commit on the
network.

</details>

<details>
<summary><b>✏️ Message & tag overrides — OMC_SUBJECT / OMC_MESSAGE / OMC_TAG</b></summary>

Sometimes you already know what the commit (or the tag) should say — the LLM
should not run. `omc` reads three optional override variables from the
environment, and **any** of them skips the Ollama two-pass generation:

```console
OMC_SUBJECT="feat: harden login against timing attacks" \
OMC_MESSAGE="Adds constant-time comparison for the token check, ..."   \
OMC_TAG="v1.4.0" \
omc
```

### Subject / message pairing rules

| `OMC_SUBJECT` | `OMC_MESSAGE` | Subject used | Body used |
| ------------- | ------------- | ------------ | --------- |
| set | set | `OMC_SUBJECT` | `OMC_MESSAGE` |
| set | unset | `OMC_SUBJECT` | `OMC_SUBJECT` |
| unset | set | first line of `OMC_MESSAGE` (≤72 chars) | full `OMC_MESSAGE` |
| unset | unset | LLM TL;DR (or `update`) | LLM detail |

Whitespace around the values is trimmed. When only `OMC_MESSAGE` is set,
its first non-empty line becomes the subject (mirroring the ≤72-char TL;DR
contract the LLM path uses); the full message is still kept as the body.

### Tag override

`OMC_TAG` names the tag explicitly instead of bumping the patch of the
latest semver tag. It is used **only** when it parses as strict semver
`vMAJOR.MINOR.PATCH` — an optional leading `v`, three non-negative integer
segments without leading zeros, no pre-release/build suffix. Arbitrarily
large values in any segment are accepted, so jumps like `v999.0.0` are fine.

- A bare `1.2.3` is normalized to `v1.2.3`.
- An invalid override (e.g. `v1.2`, `v1.2.3-rc.1`, `latest`, `v01.2.3`) is
  **not** used: `omc` logs a warning and falls back to the normal
  `LatestSemverTag` + `NextSemverTag` auto-bump. The commit is never rolled
  back over a bad tag override.

</details>

<details>
<summary><b>🏷️ Auto semver tagging — how the tag is created</b></summary>

Immediately after a successful commit, `omc` tags that commit with a semver tag:

1. If `OMC_TAG` is set and parses as strict semver `vMAJOR.MINOR.PATCH`,
   that name (with a leading `v` added for bare versions) is used verbatim.
   Otherwise the auto-bump path runs.
2. All existing `refs/tags/v*.*.*` refs are scanned; the highest semver version
   is selected (pre-release suffixes like `-rc.1` are ignored for comparison).
3. The patch segment is bumped by one (`v1.2.3` → `v1.2.4`). When no semver
   tag exists yet, the first tag is `v0.0.1`.
4. An **annotated** tag object is created on the new commit. The tag message is
   the commit's subject line.
5. When `OMC_SIGN_KEY_PATH` is set and the key is valid, the tag is **SSH-signed**
   with the same key used for the commit — the armored `BEGIN SSH SIGNATURE`
   block is embedded in the tag object, byte-compatible with `git tag -s`.
   When no key is configured, an unsigned annotated tag is created instead.

The tag step always runs after a commit. If the tag step fails (e.g. a tag of
that name already exists), `omc` logs a warning and exits 0 — the commit
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

1. `OMC_NAME` / `OMC_EMAIL` (omc's own variables)
2. `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL` then `GIT_COMMITTER_*` (standard git
   variables)
3. `user.name` / `user.email` from the repository's git config (read via
   go-git, no external binary)
4. Defaults: `OMC, Git Commiter <git@omc.local>`

Environment always wins over git config. Config files are only consulted for
the identity fallback; nothing else depends on them.

</details>

---

## ⚙️ How it works

```
┌──────────────────────────────────────────────────────────────┐
│ omc (pure Go, no git binary, no CLI args)                    │
│                                                              │
│  1. find enclosing repo  (go-git PlainOpen, walk up)         │
│  2. stage all            (Worktree.AddWithOptions All)       │
│  3. build staged diff    (HEAD tree → index tree)            │
│  4. optional: Ollama two-pass message generation             │
│  5. optional: SSH sign   (hiddeco/sshsig, "git" ns, sha512)  │
│  6. create commit        (object.Commit, advance HEAD)       │
│  7. print result         (structured, timestamped log line)  │
│  8. auto-tag             (latest v*.*.* → patch+1, signed)   │
└──────────────────────────────────────────────────────────────┘
```

The commit object is written exactly as git writes it: tree, parents,
author/committer, message, and — when signing — a `gpgsig`-style SSH signature
header covering the header-less payload. The result is a first-class signed
commit that `git log --show-signature` and `git verify-commit` accept. `omc`
builds its own trees from the index, byte-identical to what git would write, so
verification never hiccups. The tool itself no longer prints a `git log`-style
history block; it emits a single structured `committed` record and leaves
history inspection to the user.

<details>
<summary><b>🏗️ Where things live (project layout)</b></summary>

```
cmd/omc/               entry point (env → pipeline → output)
internal/config/       config.FromEnv() reads the env vars
internal/gitops/       PlainOpen detection, StageAll, StagedDiff, Commit,
                       SignedCommit, ResolveIdentity, index→tree writer,
                       semver tag discovery, CreateTag, SignedTag
internal/sign/         sign.Load(keyPath), signer.Sign(payload) → armored SSH sig
internal/ollama/       Client.Available(), DescribeDetail(), SummarizeTLDR()
internal/output/       UI: stdout = structured results, stderr = structured diagnostics
```

</details>

---

## 🌍 Environment-only design

`omc` deliberately parses no command line parameters because it's built for
places a human isn't watching:

- **Scriptable** — `OMC_SIGN_KEY_PATH=… OLLAMA_DESC_URL=… omc` anywhere.
- **Hookable** — drop it in a `pre-commit` hook or a global `alias commit=omc`.
- **Composable** — nothing to remember, nothing to prompt for, plugs straight into
  the non-interactive `$PROMPT_COMMAND` / agent shells where flags are a liability.
- **Air-gapped by default** — the only network call is to your own Ollama, so the
  tool ships no telemetry and phones no home.

---

## 🛠️ Development

```console
make test        # go test ./...
make build       # go build -o omc ./cmd/omc
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
