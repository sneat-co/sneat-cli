---
format: https://specscore.md/feature-specification
status: Approved
---

# Feature: Chat Messenger

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-messenger?op=explore) | [Edit](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-messenger?op=edit) | [Ask question](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-messenger?op=ask) | [Request change](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-messenger?op=request-change) |
**Status:** Approved
**Source Ideas:** —

## Summary

Platform-agnostic conversational contract for Sneat chat: the Processor seam, botkb inline-button vocabulary, URL-shaped callback data, and the slash commands that operate on the signed-in user's real data. Rendered by chat-tui today; reusable by a future web messenger.

## Contents

| Directory | Description |
|---|---|
| [_tests/](_tests/README.md) | Test scenarios for the chat messenger contract |

### _tests

Scenarios validating this Feature's requirements — the seam's button and callback-data vocabulary, error-return discipline, and slash-command behavior against real spaces. All are pending implementation.

## Problem

The CLI has two conversational surfaces, and neither is an interactive chat on real data.

`sneat convo` is batch in/out: `convo say` takes messages as command-line arguments, `convo replay` reads a script file. Both run against a sandbox — a mock LLM and a fake `space1`/`user1`, backed by an in-memory or OpenVaultDB store. It is a dev and agent tool, not a user-facing chat. `sneat ui` is interactive against real Firestore data, but it is a list navigator, not a conversation.

A user who wants to talk to Sneat from a terminal has nowhere to do it.

Two axes of change are expected, and either would tear apart a naively-built chat:

- **Where messages are processed.** Handling is expected to move server-side onto `bots-go-framework` eventually.
- **How messages are rendered.** The terminal UI is the first renderer; a web messenger may follow.

This Feature owns everything independent of both axes — what a message is, what a button is, what pressing one means, and what the commands do. Renderers (`chat-tui` today) and processors (in-process today, server-backed later) plug into it. Keeping this contract in its own Feature is what lets a second renderer reuse these requirements verbatim rather than extracting them from a terminal-specific spec.

## Behavior

### The processing seam

A renderer never handles messages itself. It holds an interface, so a server-backed implementation is additive rather than a rewrite.

#### REQ: processor-seam

A renderer MUST depend on a `Processor` interface exposing `SendText(ctx, text)` and `PressButton(ctx, data)`, each returning `([]Reply, error)`. A renderer MUST NOT reference any concrete implementation. The two methods mirror the framework's own split between `botinput.TextMessage`/`TextAction` and `botinput.CallbackQuery`/`CallbackAction`.

A `Reply` carries the bot's message text and an optional keyboard (see REQ: botkb-vocabulary). Both methods return `[]Reply` rather than a single `Reply` because a chat turn may produce more than one message: a confirmation flow — which Sneat's conversational runtime already models as a staged action a transport renders with Yes/Cancel inline buttons — is a reply plus a prompt. A renderer commits them in order. Every command specified here returns exactly one reply; the plural keeps a multi-message turn from becoming a seam-wide signature change later.

#### REQ: errors-are-returned-not-formatted

A `Processor` MUST return failures as an `error` and MUST NOT format user-facing error text. Presentation belongs to the renderer, the only layer that knows how errors should look on its surface.

### Button and callback vocabulary

Buttons are modeled on Telegram's, using the framework's own types rather than local equivalents.

#### REQ: botkb-vocabulary

A `Reply` MUST carry its buttons as a `botkb.Keyboard` from `bots-go-core`, not a locally-defined button type. Keyboards are built as rows of buttons (`[][]botkb.Button`), matching Telegram's `inline_keyboard` shape. `bots-go-core` is a zero-dependency module, so this costs nothing today and makes a future `botmsg.MessageFromBot` translation a field copy rather than a mapping layer.

#### REQ: callback-data-url

Inline-button callback data MUST be URL-shaped — `<command>?<arg>=<value>` — where the URL path is the command code and the query string carries arguments. This matches `bots-fw`'s router, which parses callback data with `url.Parse` and dispatches on `URL.Path`.

#### REQ: unrecognized-callback-data

Callback data that does not parse, names an unknown path, or omits a required argument MUST produce a reply saying the action could not be handled, or an error — never a silent no-op or a crash. This is the press-side counterpart of REQ: slash-command-routing's rule for unknown typed commands.

A renderer that makes committed buttons inert cannot easily produce such a press, but that is a property of one renderer rather than of this contract: a web renderer could re-press a button from earlier in the transcript, and a server-backed processor may not recognize data a previous version emitted. The contract must hold without assuming a particular renderer's discipline.

### Conversation commands

Slash commands only. Free text is explicitly deferred, not silently routed.

#### REQ: slash-command-routing

Text beginning with `/` MUST route by its first word to a command handler. An unrecognized command MUST produce a reply naming the unknown command and pointing at `/help`, never a silent no-op or a crash.

#### REQ: spaces-command

`/spaces` MUST list the signed-in user's real spaces, rendering one inline button per space, one button per row. Each button's callback data MUST be `space?id=<spaceID>`.

Each button's label MUST be the space's title. When the title is empty the label MUST fall back to the space's **type**, first letter capitalized, followed by the space ID in parentheses — `Family (vaoyj)`, `Private (ao58m)`. Only when both title and type are empty is the label the bare ID.

The empty title is the common case rather than an edge: Sneat creates a user's built-in spaces without one, so a real signed-in user's buttons are all fallbacks. A bare ID (`ao58m`) tells that user nothing. The ID stays in the fallback rather than being dropped because built-in spaces share a type — two `family` spaces would otherwise render identically, and the button's whole job is to distinguish them.

Buttons MUST be ordered by space ID. Ordering is load-bearing rather than cosmetic: the spaces source is a Go map (`SpacesReader.ListSpaces` returns `map[string]any`), whose iteration order is randomized, so an unordered implementation would reshuffle the user's buttons on every invocation. The existing `spaceItemsFrom` resolves the same problem the same way, sorting IDs before reading each brief.

When the user has no spaces, the reply MUST say so and MUST carry no keyboard — not an empty one. A renderer branches on keyboard presence to decide whether a reply is focusable, so an empty-but-present keyboard would leave it with a focus block containing nothing to focus.

#### REQ: help-command

`/help` MUST list the available commands.

#### REQ: free-text-deferred

Text not beginning with `/` MUST return a reply stating that free-text chat is not yet available and naming the working commands. It MUST NOT be routed to `convoruntime`. That runtime is wired only to the sandbox (mock LLM, fake space and user), so routing real-data chat into it would mix sandbox-only action execution with real space listings in one transcript.

#### REQ: active-space-selection

Pressing a button whose callback data path is `space` MUST set the session's active space to the `id` argument, and MUST return at least one `Reply` naming the newly active space. The active space is session state that later space-scoped commands read. Returning no reply is not permitted: a renderer would have nothing to show, leaving the user unable to tell whether the press registered.

When the `id` names no space the user can currently see — a stale button, or a space revoked mid-session — the processor MUST NOT change the active space and MUST return an error. This case is deliberately not covered by REQ: unrecognized-callback-data, whose triggers are structural: such data parses cleanly, names a known path, and carries its required argument. It fails only on the lookup that naming the space requires.

## Acceptance Criteria

### AC: processing-is-swappable

**Requirements:** chat-messenger#req:processor-seam, chat-messenger#req:errors-are-returned-not-formatted, chat-messenger#req:botkb-vocabulary, chat-messenger#req:callback-data-url

A renderer is decoupled from where messages are processed. Replacing the in-process implementation with a server-backed one requires no renderer change, because both speak the same framework-compatible vocabulary for buttons and callback data, and both hand failures back rather than rendering them. This is the property that makes the deferred messenger adapter additive rather than a rewrite.

### AC: conversation-input-is-handled-honestly

**Requirements:** chat-messenger#req:slash-command-routing, chat-messenger#req:spaces-command, chat-messenger#req:help-command, chat-messenger#req:active-space-selection, chat-messenger#req:free-text-deferred, chat-messenger#req:unrecognized-callback-data

Every input a user can produce — typed or pressed — receives an honest answer. Supported commands act on the signed-in user's actual spaces rather than sandbox fixtures, present results as pressable inline buttons in a stable order, and carry selections into session state. Unsupported input — an unknown command, an unhandleable press, or free text whose capability does not exist yet — is answered plainly and never reaches the sandbox-only conversational runtime. A user is never shown a reply that appears to act on real data while actually acting on fixtures, and never left unsure whether their input registered.

## Out of Scope

Deferred to follow-on Features, in dependency order:

- **Upstream `bots-fw` changes.** `botsfwconst.Platform` is a closed enum, so a new platform needs a constant merged upstream. `CreateWebhookContextArgs.HttpRequest` would need to become optional (it already carries an upstream `// TODO: Can we get rid of it?`), or a non-HTTP driver entry point added.
- **`bots-api-sneat-messenger`** — platform message types, following `bots-api-telegram`, which depends only on `bots-go-core`.
- **`bots-fw-sneat-messenger`** — the framework adapter. Must implement `BotPlatform`, `WebhookHandler` (6 methods), `WebhookResponder` (2 methods, where most of the work is), `WebhookContext` (5 methods atop `WebhookContextBase`), `BotRecordsFieldsSetter`, `botinput.Entry`, plus one `botinput.*` wrapper per supported message type. Realistically 800–1200 LOC minimum; `bots-fw-telegram` is ~2900.
- **The remote `Processor` implementation.**
- **A web renderer.** Would depend on this Feature and reuse its requirements unchanged.
- **Free-text chat**, pending a real-data pathway for `convoruntime`.

Telegram feature parity is the direction of travel, approached incrementally — simple messages and inline buttons first.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
