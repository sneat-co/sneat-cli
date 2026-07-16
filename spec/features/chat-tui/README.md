---
format: https://specscore.md/feature-specification
status: Stable
---

# Feature: Chat TUI

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-tui?op=explore) | [Edit](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-tui?op=edit) | [Ask question](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-tui?op=ask) | [Request change](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-tui?op=request-change) |
**Status:** Stable
**Source Ideas:** —

## Summary

First renderer of the Sneat chat messenger: the `sneat chat` command and its inline (non-alt-screen) Bubble Tea terminal UI, with a bottom-pinned input and arrow-navigable inline buttons.

## Contents

| Directory | Description |
|---|---|
| [_tests/](_tests/README.md) | Test scenarios for the chat terminal UI |

### _tests

Scenarios validating this Feature's requirements — startup preconditions, the scrollback commit rules for both submitted text and the `/spaces` → press-a-space flow, in-transcript failure reporting, and focus and key handling. All are pending implementation.

## Problem

[chat-messenger](../chat-messenger/README.md) defines what a Sneat conversation is, but nothing renders it. A user cannot reach it.

The existing `internal/tui` package is a full-screen navigator over a stack of list screens (spaces, contacts, contact cards). Chat does not fit that shape: it has no sibling screens to navigate to, and its focus model spans a text input and a block of buttons rather than a single list. Reusing that screen stack would bend it to no benefit.

A chat renderer must also decide how it occupies the terminal. Taking over the screen (as `sneat ui` does) discards the conversation on exit and puts nothing in the terminal's scrollback, which is the wrong trade for a transcript the user may want to keep, copy, or search.

## Behavior

### Command entry

`sneat chat` is a thin cobra command that validates preconditions and delegates, mirroring how `sneat ui` delegates to `env.RunTUI`.

#### REQ: chat-command

The CLI MUST register a `chat` command that launches the interactive chat session. The command layer MUST delegate to an injected `env.RunChat(spaces SpacesReader, uid string) error` rather than constructing the terminal program itself, so the command stays unit-testable without a terminal.

`RunChat` is the composition root: it constructs the concrete `Processor` from the reader and uid, and hands the renderer only the interface. The renderer package itself MUST NOT name a concrete implementation, per [chat-messenger#req:processor-seam](../chat-messenger/README.md#req-processor-seam).

#### REQ: startup-preconditions

The command MUST refuse to start when stdin is not a terminal, and MUST load the signed-in session from the session store, aborting when the store returns an error — the same gate `runSpaceUI` applies today. Both checks run before any terminal program is created, and each failure returns an error from the command rather than opening a session.

### Rendering

The chat draws inline, in the terminal's normal buffer, rather than taking over the screen.

#### REQ: inline-rendering

The terminal program MUST NOT use the alternate screen. It MUST render a live region pinned at the bottom of the terminal, containing the focusable reply and its buttons (when present), the input line, and a footer hint.

#### REQ: scrollback-commit

A completed turn MUST be committed to terminal scrollback and never repainted. A bot reply carrying a keyboard is not complete until its buttons stop being focusable, at which point it commits with its buttons rendered inert. A reply with no keyboard commits immediately.

Submitting text and pressing a button are both user input for this purpose. Each MUST commit the live reply, then commit an echo of what the user did — the submitted text, or the pressed button's label — before the resulting replies render. A button press therefore always ends the previous reply's focusability, so no live reply is ever stranded.

#### REQ: errors-render-in-transcript

An error returned by the `Processor` during a session MUST be rendered as a bot message in the transcript, and the session MUST continue. It MUST NOT terminate the program, because doing so would discard the user's transcript. This is the renderer's half of [chat-messenger#req:errors-are-returned-not-formatted](../chat-messenger/README.md#req-errors-are-returned-not-formatted).

### Interaction

#### REQ: focus-and-keys

Only the most recent bot message's buttons are focusable; buttons already committed to scrollback are inert text. Focus is either the input or the button block.

The button block renders **above** the input, so focus moves toward it with `up` and away from it with `down`. `up` from the input MUST enter the block at its **last** row — the row physically nearest the input — not its first. `down` from the last row MUST return to the input. Within the block, `up` and `down` move one row at a time; `up` at the first row stays there (the block's top edge); `left`/`right` move within a row.

The direction is not arbitrary and the first cut had it inverted: `down` entered a block sitting above the input, and entering at the *first* row meant landing on the button furthest from where the cursor just was. Both read as backwards the moment the layout is on screen, which no model-state assertion could show.

`enter` submits the input or presses the focused button according to focus; `esc` quits from the input and returns focus to the input from the button block; `ctrl+c` always quits.

When the command palette is open (REQ: command-palette), it holds the focus instead: `up`/`down` move its highlight, `enter` runs the highlighted command, `esc` dismisses it, and the button block is unreachable — the user is choosing a command, not a space. `up` therefore has one meaning per state, as everywhere else: the palette when it is open, the button block when it is not.

#### REQ: command-palette

When the input holds a `/` command being typed — a leading `/` and no space yet — a palette MUST open above the input listing the commands whose names that text is a prefix of, drawn from the same registry `/help` reads (chat-messenger#req:command-registry). Each row shows the command's name, its summary, and its argument hint when it has one, so the list teaches what each command does rather than only completing its name.

The palette filters as the user types: `/` lists every command, `/sp` narrows to those starting `/sp`. It holds the focus while open (REQ: focus-and-keys): `up`/`down` move a highlight that stays within the list, `enter` runs the highlighted command, and `esc` dismisses the palette without running anything — after which it stays closed until the typed command changes, so a user who dismissed it is not fought by its reopening on the next keystroke.

Once the text stops being a bare command name — a space typed to begin an argument, or the leading `/` removed — the palette closes and `enter` submits the line as written, so `/contacts family` is typed and sent without the palette intercepting it.

While a reply is in flight, keyboard input MUST be ignored, with the sole exception of `ctrl+c`, which MUST still quit. This mirrors the guard the existing confirm screen uses while a delete is in flight, except that the confirm screen ignores every key — a chat session may block on a slow backend, so the user must always retain a way out.

#### REQ: pending-is-visible

While a reply is in flight, the live region MUST show an **animated** indicator, above the input, stating that the bot is composing a reply. It MUST appear for every in-flight turn — typed or pressed — and MUST disappear once the turn resolves, whether it resolved into replies or into an error.

Changing the footer hint alone is not sufficient, and shipping only that was a real defect: the footer sits at the edge of the screen while the user's attention is on the transcript, so a turn that takes a moment reads as the session having ignored the input entirely. The indicator must be animated rather than static text, because a frozen line is indistinguishable from a hung program — motion is what says *working*, not *stuck*.

This is the terminal equivalent of the `typing…` chat action Telegram shows for exactly this state, which is the model this Feature follows.

## Dependencies

- chat-messenger

## Acceptance Criteria

### AC: chat-starts-only-when-usable

**Requirements:** chat-tui#req:chat-command, chat-tui#req:startup-preconditions

A chat session becomes available only when the environment can support it: a real terminal and an authenticated user. When either precondition fails, the CLI reports why and no session is drawn. The command layer's delegation boundary is observable without a terminal.

### AC: transcript-is-durable-terminal-text

**Requirements:** chat-tui#req:inline-rendering, chat-tui#req:scrollback-commit, chat-tui#req:errors-render-in-transcript

The conversation accumulates as ordinary terminal scrollback, so past turns remain selectable, copyable, and searchable with the terminal's own tooling, and survive exit. Only the region that can still change is repainted. A backend failure joins the transcript as a message rather than destroying it.

### AC: interaction-is-unambiguous

**Requirements:** chat-tui#req:focus-and-keys, chat-tui#req:input-locked-while-pending, chat-tui#req:pending-is-visible, chat-tui#req:command-palette

At any moment exactly one target holds focus, every key has one defined meaning for that focus, and focus moves in the direction the layout implies. Input that could race an in-flight reply is refused — and while it is refused the user can see that the session is working rather than ignoring them. A user is never left unsure whether their input registered.

## Out of Scope

- **A `chat` screen inside `internal/tui`.** That package's screen-stack abstraction serves list navigation; chat has no sibling screens and a different focus model.
- **Reflowing committed scrollback on terminal resize.** Inline rendering cannot reflow text already handed to the terminal. This is the same trade-off Claude Code and aider make, accepted in exchange for durable, selectable history.
- **A web renderer.** A sibling of this Feature, depending on [chat-messenger](../chat-messenger/README.md) rather than on this Feature.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
