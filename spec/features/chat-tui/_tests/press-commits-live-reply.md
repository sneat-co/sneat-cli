---
format: https://specscore.md/scenario-specification
---

# Scenario: pressing a button commits the live reply and echoes the press

**Validates:** [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:scrollback-commit](../README.md#req-scrollback-commit), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys)

## Steps

GIVEN a chat model whose live reply lists two spaces as inline buttons
AND focus is on the button labelled "Family" with callback data `space?id=family1`
WHEN the `enter` key is pressed
THEN the live reply commits to scrollback with its buttons rendered inert
AND an echo of the press naming "Family" commits to scrollback after it
AND the reply returned by `PressButton` renders afterwards
AND no live reply from before the press remains focusable

GIVEN a chat model that has just pressed a space button
AND `PressButton` returned a reply carrying no keyboard
WHEN the resulting reply renders
THEN it commits to scrollback immediately
AND there is no live reply
AND focus is the input

## TODO

- [ ] Implement by driving `Update` with a `tea.KeyMsg` for `enter` while focus is the button block.
- [ ] This is the `/spaces` → press-a-space flow end to end; it is the scenario that pins the press half of REQ: scrollback-commit, which text submission alone does not exercise.
- [ ] Depends on [chat-messenger#req:active-space-selection](../../chat-messenger/README.md#req-active-space-selection) returning at least one reply — without that guarantee the live reply would never commit.

---
*This document follows the https://specscore.md/scenario-specification*
