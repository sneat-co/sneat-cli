---
format: https://specscore.md/scenario-specification
---

# Scenario: a callback press that appends commits the live reply and echoes the press

**Validates:** [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:scrollback-commit](../README.md#req-scrollback-commit), [chat-tui#req:card-edit-in-place](../README.md#req-card-edit-in-place), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys)

## Steps

GIVEN a chat model whose live reply carries inline buttons
AND focus is on a callback button whose press will be answered by a reply that appends rather than editing the card
WHEN the `enter` key is pressed
THEN the press commits nothing to scrollback yet — its commit is deferred, because whether the reply edits the card or supersedes it is not yet known
AND the pressed card stays live meanwhile

WHEN the appending reply returns
THEN the superseded live reply commits to scrollback with its buttons rendered inert
AND an echo of the press naming the pressed button commits to scrollback after it
AND the appending reply renders afterwards
AND no live reply from before the press remains focusable

GIVEN a chat model whose callback press was answered by a reply carrying no keyboard
WHEN the resulting reply renders
THEN it commits to scrollback immediately
AND there is no live reply
AND focus is the input

## TODO

- [ ] Implement by driving `Update` with a `tea.KeyMsg` for `enter` while focus is the button block, then pumping the reply back through `Update`.
- [ ] This pins the press half of REQ: scrollback-commit for the case a callback **appends**; the card-edit case (a callback that **edits** in place, committing nothing) is [card-edit-navigates-in-place](card-edit-navigates-in-place.md).
- [ ] The commit is deferred rather than eager precisely because a callback may edit or append, and only the returned reply says which — so the eager sequence a submit uses would freeze a card that a card edit means to rewrite silently.

---
*This document follows the https://specscore.md/scenario-specification*
