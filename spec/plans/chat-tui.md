---
format: https://specscore.md/plan-specification
status: Approved
---
# Plan: Chat TUI

**Status:** Executing
**Source Feature:** chat-tui
**Date:** 2026-07-15
**Owner:** alex
**Supersedes:** —

## Summary

Implements the first renderer of the chat messenger: the `sneat chat` command and an inline (non-alt-screen) Bubble Tea UI in a new `internal/chattui` package, with a bottom-pinned input, arrow-navigable inline buttons, and a transcript that accumulates as ordinary terminal scrollback.

Depends on the `chat-messenger` Plan, which delivers the `Processor` seam this renderer consumes. No message-handling logic is in scope here.

## Approach

The renderer holds only the `Processor` interface, never a concrete implementation (`chat-messenger#req:processor-seam`). The mechanism is Go visibility rather than discipline: `internal/chat` exports the interface and a constructor while keeping the concrete processor unexported, so `internal/chattui` is structurally unable to name it. The composition root is the `Env.RunChat` closure in `main.go` — mirroring how `Env.RunTUI` is assigned there today and delegates to `tui.Run` — which calls the constructor and passes the renderer an interface value. This satisfies the structural check in `chat-messenger/_tests/processor-returns-errors-unformatted.md` without the renderer needing to avoid an import.

`internal/chattui` is a new package rather than a screen inside `internal/tui`, per the Feature's Out of Scope: that package's screen stack serves list navigation, and chat has no sibling screens and a different focus model.

Ordering runs outside-in. The command and its preconditions come first because they are testable without a terminal and pin the `Env.RunChat` signature every later task builds behind (Task 1). The model skeleton and inline program follow (Task 2). Rendering then splits along the Feature's own commit rule: text submission first (Task 3), because it is the simpler half and establishes the commit machinery; focus and keys next (Task 4), because pressing a button requires focus to exist; and the press-commit path last (Task 5), since it composes the commit rule from Task 3 with the focus model from Task 4. Failure rendering comes last (Task 6) — it needs a transcript to join.

All three ACs are covered; none are deferred. Each task carries the scenarios that prove it, from `spec/features/chat-tui/_tests/`.

## Tasks

### Task 1: Add the sneat chat command and Env.RunChat

**Verifies:** chat-tui#ac:chat-starts-only-when-usable
**Depends-On:** —
**Status:** complete

Add `Chat(env Env) *cobra.Command` in `cmd/sneat/commands/chat.go` and add `RunChat func(spaces SpacesReader, uid string) error` to the `Env` struct next to `RunTUI`. Gate on `env.IsTerminal()`, load the session from the store aborting on its error, then resolve the reader via `env.NewSpacesReader(cfg)` and abort on its error — the three steps `runSpaceUI` performs before it delegates. All run before any terminal program exists, so the whole task is testable with no TTY, per `_tests/chat-refuses-without-terminal.md`.

Registering the command in `main.go` is deliberately deferred to Task 2. `RunChat`'s closure cannot be assigned until `internal/chattui` exists, so registering here would leave `sneat chat` panicking on a nil func against a real terminal for the gap between the two tasks — invisible to this task's tests, which inject a capturing `RunChat`.

### Task 2: Create internal/chattui with an inline Bubble Tea program

**Verifies:** chat-tui#ac:transcript-is-durable-terminal-text
**Depends-On:** 1
**Status:** complete

Create `internal/chattui` with `Run(proc chat.Processor) error` and the root `Model`, mirroring `internal/tui/run.go`'s shape but **without** `tea.WithAltScreen()` — `_tests/buttoned-reply-commits-when-superseded.md` asserts the program is constructed without that option. Assign `main.go`'s `RunChat` closure to build the concrete processor via `internal/chat`'s exported constructor and hand `Run` only the interface, then register `commands.Chat(env)` beside `commands.Ui(env)` — the registration Task 1 deferred to here. Model state: the processor, a `bubbles/textinput`, the live reply, focus, a button cursor, and a pending flag.

### Task 3: Render the live region and commit submitted turns to scrollback

**Verifies:** chat-tui#ac:transcript-is-durable-terminal-text
**Depends-On:** 2
**Status:** complete

Implement the commit rule for text submission: `tea.Println` the user's line on submit, call `SendText` off the UI thread as a `tea.Cmd`, and on the reply commit every one to scrollback except a trailing reply carrying a keyboard, which becomes the live reply. `View()` renders only the live region — live reply and buttons, input line, footer hint. Covers `_tests/buttoned-reply-commits-when-superseded.md`.

### Task 4: Implement focus movement, key handling, and the pending lock

**Verifies:** chat-tui#ac:interaction-is-unambiguous
**Depends-On:** 3
**Status:** planning

Implement the focus enum and button cursor: `down` from the input enters the button block, `up` past row 0 returns, `left`/`right` move within a row, `enter` submits or presses by focus, `esc` quits from the input but returns focus from the button block, and `ctrl+c` always quits. While a reply is in flight ignore every key except `ctrl+c` — note the confirm screen this mirrors (`internal/tui/confirm_screen.go`) ignores *all* keys, so do not copy it verbatim. Covers `_tests/focus-moves-between-input-and-buttons.md`.

### Task 5: Commit the live reply on button press and echo the press

**Verifies:** chat-tui#ac:transcript-is-durable-terminal-text, chat-tui#ac:interaction-is-unambiguous
**Depends-On:** 3, 4
**Status:** planning

Treat a button press as user input for the commit rule: commit the live reply with its buttons rendered inert, commit an echo naming the pressed button's label, then render the replies `PressButton` returns. A press therefore always ends the previous reply's focusability. Covers `_tests/press-commits-live-reply.md`, including the branch where the resulting reply carries no keyboard and focus returns to the input.

### Task 6: Render in-session failures as transcript messages

**Verifies:** chat-tui#ac:transcript-is-durable-terminal-text
**Depends-On:** 3
**Status:** planning

When a `Processor` call returns an error, render it as a bot message in the transcript and keep the session running — never quit, since quitting would discard the transcript. The renderer owns the wording, because the processor returns a bare error by contract (`chat-messenger#req:errors-are-returned-not-formatted`). Covers `_tests/failure-renders-in-transcript.md`, including that earlier committed turns survive and the input still accepts a further message.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/plan-specification*
