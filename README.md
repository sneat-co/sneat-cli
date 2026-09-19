# sneat-cli

Text-based User Interface for [Sneat.app](https://sneat.app)

## Install

No Go installation is needed.

### Ask an AI agent

Send this prompt to your agent:

```text
Install and set up the Sneat.app CLI on this machine.

Follow the official instructions:
https://sneat.app/en/agent-instructions/install/
```

### Homebrew (macOS or Linux)

```shell
brew install --cask sneat-co/tap/sneat
```

### Direct install (macOS or Linux)

```shell
(p=$(mktemp) && trap 'rm -f "$p"' EXIT && curl -fsSL https://sneat.app/install.sh -o "$p" && sh "$p")
```

### Direct install (Windows PowerShell)

```powershell
$p=Join-Path $env:TEMP ("sneat-"+[guid]::NewGuid()+".ps1"); try { irm https://sneat.app/install.ps1 -OutFile $p -EA Stop; & $p } finally { Remove-Item $p -EA SilentlyContinue }
```

The direct installers select the current operating system and architecture,
download the latest official GitHub release, and verify its SHA-256 checksum
before replacing `sneat`. See all options at [sneat.app/en/install/](https://sneat.app/en/install/).

## Commands

Human-facing commands (`auth`, `space`/`spaces`, `contact`/`contacts`, `chat`, `ui`, `convo`) work
against Firestore directly or the sneat-go API and print a table by default (`--format json|yaml|csv`
to switch). The Action Protocol family below is agent-facing: every command prints JSON by default,
talks to `api.sneat.cloud/v0/sneatai/*` (see
[sneat-ai-backend](https://github.com/sneat-co/sneat-ai-backend)), and holds a pending action as a
local draft file (`<UserConfigDir>/sneat/actions/<actionID>.json`) unless `--server` is given.

| Command | Flags | Does |
|---|---|---|
| `sneat action new` | `--json`, `--utterance`, `--language`, `--space`, `--server` | Start an action from a semantic candidate; validates it (client-held) or creates it on the server (`--server`) |
| `sneat action add <id>` | `--json`, `--utterance`, `--language` | Apply a semantic patch — merged locally and re-validated for a client-held draft, sent to `action_patch` for a server-held one |
| `sneat action show <id>` | — | Print the local draft, or fetch a server-held action |
| `sneat action validate <id>` | — | Re-run validation without changing the semantic candidate |
| `sneat action commit <id>` | — | Commit the action; exits 2 with `{"error": "..."}` on stdout when the server refuses because validation still needs input |
| `sneat action cancel <id>` | — | Cancel the action (deletes the local draft, or calls `action_cancel`) |
| `sneat action list` | — | List local action drafts (client-held and server-held index entries) |
| `sneat action schema` | `--offline` | Print the Action Protocol's machine-readable semantic schema (fetched by default, or compiled-in with `--offline`) |
| `sneat context` | `--space` | Print a Space snapshot (contacts, lists, happenings, now/today) for an agent to read before acting |
| `sneat query` | `--json`, `--space` | Ask what to buy/schedule for contacts before a horizon |

<!-- dev-approach:v1 -->
## Our approach to development

We build with our own tooling:

- **[SpecScore](https://specscore.md)** — specify requirements as `SpecScore.md` artifacts
- **[SpecStudio](https://specscore.studio)** — author & manage specs across their lifecycle
- **[inGitDB](https://ingitdb.com)** — store structured data in Git where applicable
- **[DALgo](https://dalgo.io)** — data access layer for Go
- **[cover100.dev](https://cover100.dev)** — drive toward 100% test coverage
- **[DataTug](https://datatug.io)** — query & explore data
<!-- /dev-approach -->
