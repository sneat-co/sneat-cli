---
format: https://specscore.md/plan-specification
status: Approved
---
# Plan: Chat Messenger

**Status:** Approved
**Source Feature:** chat-messenger
**Date:** 2026-07-15
**Owner:** alex
**Supersedes:** —

## Summary

Implements the platform-agnostic conversational contract in a new `internal/chat` package: the `Processor` seam and `Reply` type, URL-shaped callback data, and the in-process processor that serves `/spaces`, `/help`, unknown commands, free text, and button presses against the signed-in user's real spaces.

No terminal code is in scope. The renderer is planned separately from `chat-tui`, which depends on this Feature.

## Approach

The package placement is a hard constraint rather than a preference. `chat-messenger#req:free-text-deferred` is guarded by a package-granularity import check (`_tests/free-text-returns-deferred-notice.md`: "it does not import `convoruntime`"), and `cmd/sneat/commands` already imports `convoruntime` in `convo.go`. Putting the processor there would make that guard unimplementable. So everything here lands in a new `internal/chat`, a sibling of `internal/tui`.

Ordering follows the dependency chain rather than the Feature's section order. The seam and its types come first because every later task returns them (Task 1). Callback-data helpers come next because both the button-emitting and the button-handling task encode and parse the same format, and splitting that across them would duplicate it (Task 2). Routing precedes the individual commands so each command lands in an existing dispatch path (Task 3). `/spaces` precedes `PressButton` because the press handler consumes the callback data `/spaces` emits, and its scenario presses a button `/spaces` produced (Tasks 4, 5).

Both ACs are covered; none are deferred. Every task carries the tests that prove its slice, following the scenarios in `spec/features/chat-messenger/_tests/`, so no task depends on a later "write the tests" step.

## Tasks

### Task 1: Create internal/chat with the Processor seam and Reply type

**Verifies:** chat-messenger#ac:processing-is-swappable
**Depends-On:** —
**Status:** planning

Create the `internal/chat` package and define `Processor` (`SendText(ctx, text)` and `PressButton(ctx, data)`, each returning `([]Reply, error)`) plus `Reply` (message text and an optional `botkb.Keyboard`). Add the `bots-go-core` dependency for `botkb`. Establish the error discipline here: the interface returns failures as `error`, and no implementation formats user-facing error prose, per `chat-messenger#req:errors-are-returned-not-formatted`.

Note the seam has a renderer-side half that is **not** in this Plan: `_tests/processor-returns-errors-unformatted.md`'s second block asserts the renderer imports `Processor` and names no concrete implementation. That check belongs to `chat-tui`, whose composition root builds the concrete processor.

### Task 2: Encode and parse URL-shaped callback data

**Verifies:** chat-messenger#ac:processing-is-swappable
**Depends-On:** 1
**Status:** planning

Add helpers that build callback data in `<command>?<arg>=<value>` form and parse it back to a command path plus arguments, using `net/url` so the format matches what `bots-fw`'s router does (`url.Parse`, then dispatch on `URL.Path`). Test that `space?id=family1` round-trips to path `space` and argument `id=family1`, per `_tests/buttons-use-botkb-and-url-callback-data.md`.

### Task 3: Add the local processor with slash routing, /help, and free-text deferral

**Verifies:** chat-messenger#ac:conversation-input-is-handled-honestly
**Depends-On:** 1
**Status:** planning

Implement the in-process `Processor` over a `SpacesReader` and uid, with a command map keyed by the first word of `/`-prefixed text. Cover `/help`, the unknown-command reply naming the command and pointing at `/help`, and the free-text reply stating the capability does not exist yet. The package must not import `convoruntime` — that structural guard is the point of the placement decision above, and is asserted by `_tests/free-text-returns-deferred-notice.md`.

Keep the concrete processor type unexported and expose it through an **exported** constructor returning the `Processor` interface — not the concrete type, which would be legal Go but lint-flagged. That visibility split is load-bearing rather than stylistic: it is what lets the renderer package import this one for the interface while remaining structurally unable to name an implementation, satisfying `_tests/processor-returns-errors-unformatted.md`'s second block by compiler enforcement rather than by discipline. The `chat-tui` Plan's composition root calls this constructor.

`_tests/slash-commands-act-on-real-spaces.md` spans three tasks rather than one: its `/help` and unknown-command blocks land here, its `/spaces` listing blocks in Task 4, and its press blocks in Task 5.

### Task 4: Implement /spaces with ordered, titled inline buttons

**Verifies:** chat-messenger#ac:conversation-input-is-handled-honestly, chat-messenger#ac:processing-is-swappable
**Depends-On:** 2, 3
**Status:** planning

Render the user's real spaces as one `botkb.DataButton` per row via `botkb.NewMessageKeyboard(botkb.KeyboardTypeInline, ...)`, labelled with each space's title and falling back to its ID when empty, ordered by space ID, each carrying `space?id=<spaceID>`. Sorting is required rather than cosmetic: `ListSpaces` returns a `map[string]any`, whose iteration order Go randomizes — `internal/tui/items.go`'s `spaceItemsFrom` solves the same problem with `sortedKeys`. A user with no spaces gets a reply saying so and carrying no keyboard at all, since a renderer branches on keyboard presence.

This task also carries the first block of `_tests/processor-returns-errors-unformatted.md`: a failing spaces reader makes `SendText("/spaces")` return a non-nil error and no reply carrying error prose. Task 1 states that discipline in the abstract; `/spaces` is the first command that can actually exercise it.

### Task 5: Handle button presses — active space selection and unrecognized data

**Verifies:** chat-messenger#ac:conversation-input-is-handled-honestly
**Depends-On:** 2, 3, 4
**Status:** planning

Implement `PressButton`: dispatch on the parsed callback path, set the session's active space for `space?id=<id>`, and return at least one reply naming the newly active space. Return an error without changing the active space when the ID names no space the user can currently see. Callback data that fails to parse, names an unknown path, or omits a required argument returns a reply saying the action could not be handled, or an error — never a panic and never a silent no-op.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/plan-specification*
