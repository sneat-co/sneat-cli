# `sneat chat` — Interactive Chat Interface Design

**Date:** 2026-07-15
**Status:** Approved

## Goal

Add `sneat chat`: an interactive, Telegram-style chat interface to the Sneat CLI. It renders bot replies with inline buttons, accepts slash commands, and operates on the real signed-in user's data.

The message-processing layer is designed as a swappable seam from day one, so a future server-side path (via `bots-go-framework`) is an additive implementation rather than a rewrite.

## Context

Today `sneat convo` (`cmd/sneat/commands/convo.go`) is batch in/out, not interactive: `convo say` takes messages as CLI args, `convo replay` reads a script file. Both run against a **sandbox** — mock LLM, fake `space1`/`user1`, in-memory or OpenVaultDB. Separately, `sneat ui` (`internal/tui`) is a full-screen navigator over the **real** signed-in user's Firestore data.

`sneat chat` is a third thing: interactive like `ui`, conversational like `convo`, on real data.

## Scope

### In scope (v1)

- `sneat chat` command, gated on TTY + signed-in session.
- Inline/scrollback Bubble Tea UI with a bottom-pinned input.
- Slash commands: `/spaces`, `/help`.
- Inline buttons (Telegram-style), arrow-navigable.
- Selecting a space sets the session's active space.
- A `Processor` seam with one implementation: local, in-process.

### Out of scope (deferred to Spec 2)

- `bots-api-sneat-messenger` and `bots-fw-sneat-messenger`.
- The remote `Processor` implementation.
- Free-text (non-slash) messages. Typing plain text replies:
  `Free-text chat isn't wired up yet — try /spaces or /help.`

### Why free-text is deferred

`convoruntime` (the LLM/action engine behind `convo say`) is wired only to the sandbox. There is no real-data LLM pathway. Routing free text to it would mix real space listings with sandbox-only action execution in one session — different backing data, same transcript. v1 is slash-commands-only until a real-data pathway exists.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Command name | `sneat chat` | `convo` is cryptic; `chat` is immediately clear. Internal `convo*` packages keep their names. |
| Data source | Real signed-in spaces | Reuses `SpacesReader` + session auth that `sneat ui` already uses. |
| Screen mode | Inline/scrollback | Matches the reference UX (Claude Code, aider); preserves terminal scrollback, selection, and search. |
| Package | New `internal/chattui` | Chat's interaction model differs from `internal/tui`'s list-navigation screen stack. |
| Button vocabulary | `bots-go-core/botkb` | Already exactly the needed model; zero-dep module; what `bots-fw` speaks. |
| Callback data | URL convention | `bots-fw`'s router parses callback data as a URL and matches `URL.Path` as the command code. |
| Adapter | Deferred to Spec 2 | ~800–1200 LOC minimum; needs upstream `bots-fw` changes. Build the seam first. |

## Framework findings

These findings shaped the design and are recorded because they are non-obvious:

1. **`bots-fw-viber` and `bots-fw-fbm` are empty stubs** — zero Go files. `bots-fw-telegram` (~2900 non-test LOC) is the only adapter that exists. `sneat-messenger` would be the second platform adapter ever built.

2. **`bots-fw`'s generic layer is Telegram's model, untested by a second platform.** Every `botkb.KeyboardType` is annotated "Used by: Telegram"; `botmsg.AnswerCallbackQuery`'s doc comment states it is modeled on Telegram's API; `router.go:809` string-matches Telegram error text under a `// TODO: This checks are specific to Telegram`. Spec 2 should expect to push changes upstream, not merely consume.

3. **HTTP is assumed by the driver, not by the router.** `botswebhook.webhookDriver.HandleWebhook` panics on nil `*http.Request` (`driver.go:63,66`), and `WebhookContextBase` panics on `args.HttpRequest == nil` (`webhook_context_base.go:308`). But `Router.Dispatch(handler, responder, whc)` takes no HTTP. In-process dispatch is achievable; `CreateWebhookContextArgs.HttpRequest` already carries an upstream `// TODO: Can we get rid of it?`.

4. **`botsfwconst.Platform` is a closed enum** (`botsfwconst/paltform.go` — filename typo is upstream's). A new platform requires a constant merged into `bots-fw`.

5. **`bots-go-core` has a 3-line `go.mod` with zero dependencies.** Importing `botkb` is free. `bots-fw` is not — it pulls dalgo, i18n, and the webhook stack.

## Architecture

### Command layer — `cmd/sneat/commands/chat.go`

`func Chat(env Env) *cobra.Command`, registered in `main.go` beside `commands.Ui(env)`. Mirrors `runSpaceUI` (`spaces.go:107`):

1. Gate on `env.IsTerminal()` → `chat requires a terminal`.
2. `env.Store.Load()` for the session/uid.
3. `env.NewSpacesReader(cfg)`.
4. Delegate to `env.RunChat(spaces SpacesReader, uid string) error` — a new `Env` field beside `RunTUI`, keeping the command layer thin and testable.

### UI layer — `internal/chattui`

- `Run(spaces SpacesReader, uid string) error` — builds and runs the `tea.Program`. **No `tea.WithAltScreen()`.**
- `Model` — the root Bubble Tea model. No screen stack; one self-contained model.
- Styles: local `lipgloss` vars, copied rather than imported from `internal/tui`'s unexported ones, keeping the packages independent.

### Rendering model

Bubble Tea inline mode splits the terminal into a **live region** (rendered by `View()`, repainted each frame, pinned at the bottom) and **scrollback** (emitted via `tea.Println`, printed above the live region, never touched again).

One rule governs the whole UI:

> A turn commits to scrollback once done. A bot message with buttons is not done until its buttons stop being focusable — i.e. when the next user input is sent.

- User input line → commits immediately on submit.
- Bot reply **without** a keyboard → commits immediately.
- Bot reply **with** a keyboard → becomes `live`; commits (buttons inert) when superseded.

Live region = `[live reply + buttons, if any] + input line + footer hint`.

**Accepted trade-off:** committed scrollback does not reflow on terminal resize — the same trade-off Claude Code and aider make. In exchange, tall output scrolls naturally with no viewport height math, and past turns stay selectable and searchable with the terminal's own tooling.

## The seam

```go
// Processor turns user input into bot replies. The seam between the chat UI
// and whatever processes messages.
//
// Implementations:
//   local  (v1)    — in-process slash-command handlers over SpacesReader
//   remote (later) — sends to sneat-messenger; bots-fw routes it server-side
type Processor interface {
	// SendText delivers a typed message.          (~ botinput.TextMessage   → TextAction)
	SendText(ctx context.Context, text string) ([]Reply, error)
	// PressButton delivers an inline-button press. (~ botinput.CallbackQuery → CallbackAction)
	PressButton(ctx context.Context, data string) ([]Reply, error)
}

// Reply is one bot message.
type Reply struct {
	Text     string
	Keyboard botkb.Keyboard // from bots-go-core — zero-dep, what bots-fw already speaks
}
```

Two methods rather than one, mirroring the framework's own split between `botinput.TextMessage`/`TextAction` and `botinput.CallbackQuery`/`CallbackAction`.

`Reply` is deliberately ours rather than `botmsg.MessageFromBot`: importing `bots-fw` would drag in dalgo, i18n, and the webhook stack for v1's local path. Because `botmsg.TextMessageFromBot.Keyboard` **is** a `botkb.Keyboard`, Spec 2's translation is a field copy, not a mapping layer.

### Why this makes Spec 2 additive

Speaking `botkb` and URL-convention callback data in v1 means the remote path is a second `Processor` implementation. The UI layer never learns which one it holds.

## Data flow

### Local processor

```go
type localProcessor struct {
	spaces SpacesReader
	uid    string
	active coretypes.SpaceID // set by pressing a space button
}
```

**`SendText`:** text without a `/` prefix returns the deferred-free-text reply. Otherwise the command word routes through a `map[string]handler` (`spaces`, `help`). Deliberately a plain map, not a copy of `bots-fw`'s `Command` struct — v1 has two commands, and Spec 2 replaces this layer.

**`PressButton`:** `url.Parse(data)`, switch on `URL.Path`. `space` reads `id` from the query and sets `active`.

`/spaces` emits `botkb.NewMessageKeyboard(botkb.KeyboardTypeInline, rows...)`, one row per space, each `botkb.NewDataButton("Family", "space?id=family1")`.

### One turn

1. Enter pressed → `tea.Println("You: /spaces")` commits the user line; `pending = true`.
2. A `tea.Cmd` calls `proc.SendText` off the UI thread (it does network I/O via `ListSpaces`) → `repliesMsg`.
3. On `repliesMsg`: commit every reply to scrollback **except** a trailing one carrying a keyboard — that becomes `live`.
4. Next submission commits `live` first (buttons inert), then repeats.

Button presses mirror this, echoing the pressed label:

```
You: /spaces

Sneat: You have 2 spaces:
[  Family  ]
[ Personal ]

You: [Family]

Sneat: Active space is now Family.

> _
```

### Model state

`proc`, `input` (`bubbles/textinput`), `live *Reply`, `focus` (`focusInput|focusButtons`), a `(row, col)` cursor, `pending`, `width`.

Only the **most recent** bot message's buttons are focusable; earlier ones are inert scrollback text.

Key handling:

| Key | In `focusInput` | In `focusButtons` |
|---|---|---|
| `down` | Enter the button block at row 0 (if `live` has buttons) | Next row |
| `up` | — | Previous row; past row 0 returns to the input |
| `left`/`right` | — | Move within a row |
| `enter` | Submit the input | Press the focused button |
| `esc` | Quit | Return focus to the input |
| `ctrl+c` | Quit | Quit |

While `pending` is true, all input is ignored — the same guard `confirm_screen.go:35` uses while a delete is in flight.

## Error handling

**Startup errors abort; in-session errors become bot messages.** A failed `ListSpaces` mid-chat must not kill the program and discard the transcript.

| Condition | Behavior |
|---|---|
| Not a terminal | `fmt.Errorf("chat requires a terminal")` before start, matching `runSpaceUI:108` |
| Not signed in | Whatever `env.Store.Load()` returns, matching `runSpaceUI:112` |
| `ListSpaces` fails mid-session | In-transcript `Sneat: Couldn't load your spaces: <err>`; session continues |
| Unknown command | `Sneat: Unknown command /foo. Try /help.` |
| Unparseable callback data | In-session error reply, never a panic |

Since `SendText` returns `([]Reply, error)`, the model decides presentation — the processor never formats user-facing error text.

## Testing

The seam makes everything testable without a terminal, the same choice `internal/tui` already made.

- **Command layer** (`chat_test.go`) — mirrors `ui_test.go`: a capturing `env.RunChat` asserting it is called with the right uid, and not called when `IsTerminal` is false.
- **`localProcessor`** — a fake `SpacesReader` (the pattern `internal/tui/tui_test.go` uses). Assert `/spaces` returns a keyboard with one `DataButton` per space with `Data == "space?id=<id>"`; `/help` and unknown commands reply correctly; `PressButton("space?id=family1")` sets `active`.
- **Bubble Tea model** — a fake `Processor` returning canned replies. Drive `Update()` with `tea.KeyMsg` values; assert state and `View()` output: down-arrow moves focus to buttons, enter fires `PressButton` with the right data, a reply with no keyboard leaves `live == nil`.

## Spec 2 (future)

Deferred, in dependency order:

1. Upstream `bots-fw`: add a `botsfwconst.Platform` constant; make `CreateWebhookContextArgs.HttpRequest` optional, or add a non-HTTP driver entry point.
2. `bots-api-sneat-messenger` — platform message types (pattern: `bots-api-telegram` depends only on `bots-go-core`).
3. `bots-fw-sneat-messenger` — the adapter. Must implement `BotPlatform`, `WebhookHandler` (6 methods), `WebhookResponder` (2 methods, where the bulk of the work is), `WebhookContext` (5 methods atop `WebhookContextBase`), `BotRecordsFieldsSetter`, `botinput.Entry`, plus one `botinput.*` wrapper per supported message type.
4. `remoteProcessor` in `internal/chattui`.

Telegram feature parity is the north star, approached incrementally: simple messages and inline buttons first.
