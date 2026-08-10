<div align="center">

# omc

**oh-my-commit — one command, no flags. Signed, AI-described commits your team can actually trust.**

`omc` (spoken: *"oh-my-commit"*) is a plain, stupid-simple git auto commit 
sign tag push utility, with agent/human role seperation for signed commits
including llm generated commit message and final review/push for humans.
utility written in pure Go. It takes **zero command line arguments**, 
zero runtime dependencies: not even legacy git installed - every behavior
is an environment variable — and inside any git working tree 

one 3 letter command does the equivalent of:

```script 
#!/bin/sh
git add -A 
git commit -S -m <generate detailed commit message/analysis> 
git tag -s <old_semver+1> 
[opt: git push]
[opt: git push --tags]
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

**Smartcard (FIDO2) keys** — `id_ed25519_sk` / `id_ecdsa_sk` from `ssh-keygen -t ed25519-sk` — are supported too: point `OMC_SIGN_KEY_PATH` at the key handle and omc signs through your **ssh-agent** (the agent talks to the device, which enforces the touch/PIN). On a TTY an animated `🔑 TOUCH YOUR SECURITY KEY` countdown runs while the signature waits for the device touch, so the urgency is unmistakable. No agent loaded with the key → omc warns and commits unsigned; it never blocks.

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
No flags to remember, no prompts to answer. No Tags that collide. Just configure (when needed env) then 3 letter Autopilot everywhere.

</td>
<td width="50%" valign="top">

### 🚀 Optional push
Set `OMC_PUSH_KEY_PATH` to an SSH key and `omc` pushes the new commit and its tag to the default remote after tagging — `git push; git push --tags`, all in-process via go-git. Failures degrade: the commit and tag stay local, never rolled back. When the working tree is clean, the push still runs so tags left behind by a previously failed push get published.

Like signing, the push key accepts a **FIDO2 security-key handle** (`~/.ssh/id_ed25519_sk`): omc authenticates via the ssh-agent and your smartcard, showing the same animated touch countdown during the SSH handshake. Without an agent the push degrades to a warning.

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
export OMC_SIGN_KEY_PATH=~/.ssh/id_ed25519      # optional: SSH signing
omc
```

A one-shot signed commit with an explicit subject and tag:

```console
OMC_SIGN_KEY_PATH=~/.ssh/id_ed25519 \
OMC_SUBJECT="fix: harden login against timing attacks" \
OMC_TAG="v1.4.0" \
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
two-stage LLM generation, color-coded levels (OK / INFO / WARN / FAIL), and
an animated touch countdown when a FIDO2 security key is used.

The blocks below are real captured output (no Ollama running, so the message
falls back to `update`). Diagnostics and progress go to stderr; the final
commit and tag results go to stdout.

### 1. Software signing key

```console
$ export OMC_SIGN_KEY_PATH=~/.ssh/agent
$ echo 'func main() { fmt.Println("hi") }' >> main.go
$ omc
14:18:31 ℹ️ INFO omc omc v0.1.31
14:18:31 ℹ️ INFO omc config detected environment count=1 OMC_SIGN_KEY_PATH=~/.ssh/agent
14:18:31 ℹ️ INFO omc config verified config count=1 sign_key=valid=true
14:18:31 ℹ️ INFO omc 📂 open detecting repository
14:18:31 ✅ OK omc 📂 open done
14:18:31 ℹ️ INFO omc 📥 stage staging all changes
14:18:31 ✅ OK omc 📥 stage done
14:18:31 ℹ️ INFO omc 🔍 diff reading staged diff
14:18:31 ✅ OK omc 🔍 diff done
14:18:31 ℹ️ INFO omc 🔍 diff changed files count=2
  - .gitignore
  - main.go
14:18:31 ℹ️ INFO omc 🔑 load key loading ssh signing key
14:18:31 ✅ OK omc 🔑 load key done
14:18:31 ℹ️ INFO omc signing with ~/.ssh/agent (ssh-ed25519)
14:18:31 ℹ️ INFO omc ✍️ sign signing commit with ssh key key=~/.ssh/agent
14:18:31 ℹ️ INFO omc 📝 commit committing as Ada Lovelace <ada@example.com> (signed)
14:18:31 ✅ OK omc 📝 commit done
14:18:31 ℹ️ INFO omc 🏷️ tag bumping semver patch
14:18:31 ✅ OK omc 🏷️ tag done
14:18:31 ℹ️ INFO omc 💬 msg subject: update
14:18:31 ✅ OK omc 📝 commit committed hash=3f8bc54 signed=true
14:18:31 ✅ OK omc 🏷️ tag tagged tag=v0.0.1 hash=3f8bc54 signed=true
```

### 2. Smartcard (FIDO2) signing key — touch countdown

With `OMC_SIGN_KEY_PATH` pointing at an `id_ed25519_sk` handle, omc signs
through the ssh-agent. On a TTY a live countdown animates while the signature
waits for the device touch:

```console
$ ssh-add ~/.ssh/id_ed25519_sk
$ export OMC_SIGN_KEY_PATH=~/.ssh/id_ed25519_sk
$ omc
...
14:22:09 ⚠️ WARN omc warning: ssh key ~/.ssh/id_ed25519_sk is a smartcard security key; signing via the ssh-agent
14:22:09 ℹ️ INFO omc ✍️ sign signing commit with ssh-agent security key key=~/.ssh/id_ed25519_sk mode=smartcard algo=sk-ssh-ed25519@openssh.com
14:22:09 ℹ️ INFO omc 🔐 touch security key detected: touch your smartcard/yubikey when it blinks to authorise the commit signing key=~/.ssh/id_ed25519_sk mode=smartcard action=the commit signing
🔑 TOUCH YOUR SECURITY KEY  ▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱  ⏱ 0:28
🔑 TOUCH YOUR SECURITY KEY  ▰▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱  ⏱ 0:21
14:22:14 ✅ OK omc 📝 commit done
14:22:14 ℹ️ INFO omc 🔐 touch security key detected: touch your smartcard/yubikey when it blinks to authorise the tag signing key=~/.ssh/id_ed25519_sk mode=smartcard action=the tag signing
🔑 TOUCH YOUR SECURITY KEY  ▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱  ⏱ 0:29
14:22:18 ✅ OK omc 🏷️ tag done
14:22:18 ✅ OK omc 📝 commit committed hash=5c91e2a signed=true
14:22:18 ✅ OK omc 🏷️ tag tagged tag=v0.0.2 hash=5c91e2a signed=true
```

When the agent has no matching identity, omc degrades — never blocks:

```console
14:23:55 ⚠️ WARN omc warning: ssh key ~/.ssh/id_ed25519_sk is a smartcard security key, but no ssh-agent identity matches
(connect to ssh-agent for security key ~/.ssh/id_ed25519_sk (is ssh-agent running?): dial unix: missing address); committing unsigned
14:23:55 ✅ OK omc ✍️ sign committing unsigned signed=false
```

### 3. Subject / message / tag override

Skip the LLM entirely and write the commit text + tag name yourself:

```console
$ OMC_SUBJECT="feat: add greeting to main" \
  OMC_MESSAGE='Adds a fmt.Println("hi") entrypoint so the binary does something on run.' \
  OMC_TAG="v1.0.0" \
  OMC_SIGN_KEY_PATH=~/.ssh/agent \
  omc
...
14:24:10 ℹ️ INFO omc config detected environment count=4 OMC_SIGN_KEY_PATH=~/.ssh/agent OMC_SUBJECT=feat: add greeting to main OMC_MESSAGE=Adds a fmt.Println("hi") entrypoint so the binary does something on run. OMC_TAG=v1.0.0
14:24:10 ℹ️ INFO omc config verified config count=2 sign_key=valid=true tag_override=valid=true
...
14:24:10 ℹ️ INFO omc message override active (OMC_SUBJECT/OMC_MESSAGE)
14:24:10 ℹ️ INFO omc ✍️ sign signing commit with ssh key key=~/.ssh/agent
14:24:10 ℹ️ INFO omc 📝 commit committing as Ada Lovelace <ada@example.com> (signed)
14:24:11 ✅ OK omc 📝 commit done
14:24:11 ℹ️ INFO omc 🏷️ tag tagging v1.0.0
14:24:11 ✅ OK omc 🏷️ tag done
14:24:11 ✅ OK omc 📝 commit committed hash=3691ad8 signed=true
14:24:11 ✅ OK omc 🏷️ tag tagged tag=v1.0.0 hash=3691ad8 signed=true

$ git show --stat --oneline HEAD
3691ad8 feat: add greeting to main
 main.go | 4 ++++
 1 file changed, 4 insertions(+)
```

### 4. Optional push

With `OMC_PUSH_KEY_PATH` set, the new commit and tag are pushed to the
default remote after tagging. A security-key push path shows the same touch
countdown during SSH authentication:

```console
$ export OMC_PUSH_KEY_PATH=~/.ssh/id_ed25519
$ omc
...
14:25:02 ✅ OK omc 🏷️ tag tagged tag=v1.0.1 hash=3691ad8 signed=true
14:25:02 ℹ️ INFO omc 🚀 push pushing commit and tags to remote
14:25:03 ✅ OK omc 🚀 push done
14:25:03 ✅ OK omc 🚀 push pushed remote=origin branch=main tags=true
```

Piped or non-interactive output (CI logs, captured tests) automatically falls
back to the same structured, greppable line format without color codes:

```console
$ omc 2>&1 | cat
12:04:07 INFO omc omc v0.1.31
12:04:07 INFO omc config detected environment count=2 OMC_SIGN_KEY_PATH=~/.ssh/agent OLLAMA_DESC_URL=http://127.0.0.1:11434
12:04:07 INFO omc config verified config count=2 sign_key=valid=true ollama=configured=true
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
12:04:08 INFO omc ollama generating commit message
12:04:09 INFO omc ollama condensing to TL;DR
12:04:09 INFO omc msg subject: sign commit payloads with git's SSH signature format
12:04:09 INFO omc msg body:
 - Adds an armored BEGIN SSH SIGNATURE header to the commit object...
12:04:09 INFO omc load key loading ssh signing key
12:04:09 OK omc load key done
12:04:09 INFO omc sign signing commit with ssh key key=~/.ssh/agent
12:04:09 INFO omc commit committing as Ada Lovelace <ada@example.com> (signed)
12:04:10 OK omc commit done
12:04:10 OK omc commit committed hash=9d3f2ab signed=true
12:04:10 INFO omc tag bumping semver patch
12:04:10 OK omc tag done
12:04:10 OK omc tag tagged tag=v0.3.9 hash=9d3f2ab signed=true
```

Every field is a separate, greppable token — `tag=v0.3.9`, `hash=9d3f2ab`,
`signed=true` — so downstream pipelines and log aggregators can parse them
without ambiguous whitespace. The commit and tag results go to stdout; all
diagnostics and progress go to stderr. When a FIDO2 security key is used on a
non-TTY, the animated countdown is suppressed and only the structured `touch`
notice line is emitted, so CI logs stay greppable.

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
export OMC_SIGN_KEY_PATH=~/.ssh/keys/agent-commit   # can SIGN commits, cannot push
export OLLAMA_DESC_URL=http://127.0.0.1:11434       # lets it describe its own work
omc
```

The **push** to the remote uses a *different* credential — your human
`~/.ssh/id_ed25519`, a deploy key, or a short-lived CI token — that the agent
never sees.

|| Activity | Credential | Holder |
|| -------- | ---------- | ------ |
|| Author + sign a commit locally | `agent-commit` signing key | the agent |
|| Push to the remote | your personal SSH key / deploy key | **you** |

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

> **Opt-in automation:** if you *do* want the agent to publish, hand it a
> dedicated, low-privilege push key via `OMC_PUSH_KEY_PATH`. The push still
> happens only after the commit and tag are created, and a failed push never
> rolls back the local work — so the worst case is a local commit you can
> review and push yourself.

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
| `OMC_SIGN_KEY_PATH` | Path to an SSH **private** key. When set and valid, the commit is SSH-signed. A **FIDO2 security-key handle** (`id_ed25519_sk` / `id_ecdsa_sk`) is supported too: omc signs via the ssh-agent and your smartcard. If set but unusable, warns and commits unsigned. |
| `OLLAMA_DESC_URL` | Base URL of a local Ollama REST API, e.g. `http://127.0.0.1:11434`. When set and reachable, generates the commit message from the staged diff. |
| `OLLAMA_DESC_MODEL` | Ollama model name (optional). Defaults to `llama3.2`. |
| `OMC_NAME` | Commit author/committer name (optional). Falls back to git config, then `OMC, Git Commiter`. |
| `OMC_EMAIL` | Commit author/committer email (optional). Falls back to git config, then `git@omc.local`. |
| `OMC_SUBJECT` | Override the commit subject. When set, **no LLM generation runs**. See [Message & tag overrides](#-message--tag-overrides). |
| `OMC_MESSAGE` | Override the commit body. When set, **no LLM generation runs**. See [Message & tag overrides](#-message--tag-overrides). |
| `OMC_TAG` | Override the tag name. Used only when it is strict semver `vMAJOR.MINOR.PATCH`; otherwise the auto-bump runs. See [Message & tag overrides](#-message--tag-overrides). |
| `OMC_PUSH_KEY_PATH` | Path to an SSH **private** key. When set and readable, pushes the new commit and tags to the default remote after tagging (`git push; git push --tags`). A **FIDO2 security-key handle** is supported (auth via ssh-agent + smartcard). If set but unusable, or the push fails, warns and leaves the commit/tag local. |

> **Signing:** only passphrase-less software keys are supported (there is no
> interactive prompt, by design). `ssh-keygen -t ed25519 -N "" -C agent@paepcke.de -f ~/.ssh/agent`
> is your friend. Scheduling note for CI: protect that key with filesystem
> permissions and rotate it like any credential. For **smartcard keys** you
> need your ssh-agent running and the key loaded (`ssh-add ~/.ssh/id_ed25519_sk`);
> the device then enforces the touch/PIN prompt itself.

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
<summary><b>🚀 Pushing — OMC_PUSH_KEY_PATH</b></summary>

After the commit and the semver tag are created, `omc` can push them to the
repository's default remote — the go-git equivalent of `git push; git push
--tags`, with no external `git` binary.

1. `OMC_PUSH_KEY_PATH` must be set **and** the key readable. If it is
   unset, no push happens. If it is set but unreadable/unparseable, `omc`
   logs a warning and skips the push.
2. The current branch is pushed first (`refs/heads/<branch>`), then all
   local tags (`+refs/tags/*`), mirroring `git push --tags`.
3. The key authenticates over SSH. For non-SSH remotes (https/file) the key
   is not applicable and go-git's default auth is used. When the key path
   is empty, go-git falls back to its default auth (SSH agent).
4. **Security-key (FIDO2) keys** work too: `OMC_PUSH_KEY_PATH=~/.ssh/id_ed25519_sk`
   is detected (by the conventional `id_*_sk` name and/or the `.pub`
   adjacent file) and authentication is delegated to the ssh-agent, which
   forwards the challenge to your smartcard — the same way `git push` with
   `IdentitiesOnly` + a security key works. If the agent is missing or does
   not hold the key, the push is skipped with a warning.
5. `NoErrAlreadyUpToDate` is treated as success. Any other failure (no
   remote, non-fast-forward, network) logs a warning and exits 0 — the
   commit and tag are never rolled back over a push problem.
6. The push also runs when there is nothing to commit or tag (clean
   working tree): a previous run may already have committed and tagged
   locally while its push was skipped or failed, so the pending tags are
   published now. Failures degrade exactly like the main push step.

```console
$ export OMC_PUSH_KEY_PATH=~/.ssh/id_ed25519
$ omc
...
14:25:02 ✅ OK omc 🏷️ tag tagged tag=v1.0.1 hash=3691ad8 signed=true
14:25:02 ℹ️ INFO omc 🚀 push pushing commit and tags to remote
14:25:03 ✅ OK omc 🚀 push done
14:25:03 ✅ OK omc 🚀 push pushed remote=origin branch=main tags=true
```

With a security-key push path the SSH handshake shows the touch countdown:

```console
$ export OMC_PUSH_KEY_PATH=~/.ssh/id_ed25519_sk
$ omc
...
14:26:10 ✅ OK omc 🏷️ tag tagged tag=v1.0.2 hash=5c91e2a signed=true
14:26:10 ℹ️ INFO omc 🔐 touch security key detected: touch your smartcard/yubikey when it blinks to authorise the push key=~/.ssh/id_ed25519_sk mode=smartcard action=the push
🔑 TOUCH YOUR SECURITY KEY  ▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱  ⏱ 0:27
14:26:15 ✅ OK omc 🚀 push done
14:26:15 ✅ OK omc 🚀 push pushed remote=origin branch=main tags=true
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
│  5b. optional: smartcard touch countdown (FIDO2 sk keys)    │
│  6. create commit        (object.Commit, advance HEAD)       │
│  7. print result         (structured, timestamped log line)  │
│  8. auto-tag             (latest v*.*.* → patch+1, signed)   │
│  9. optional push         (OMC_PUSH_KEY_PATH → git push --tags)│
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
                       semver tag discovery, CreateTag, SignedTag,
                       PushToRemote
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
