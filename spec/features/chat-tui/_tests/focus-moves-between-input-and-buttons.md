---
format: https://specscore.md/scenario-specification
---

# Scenario: focus moves predictably and input is locked while a reply is in flight

**Validates:** [chat-tui#ac:interaction-is-unambiguous](../README.md#ac-interaction-is-unambiguous), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys), [chat-tui#req:input-locked-while-pending](../README.md#req-input-locked-while-pending), [chat-tui#req:pending-is-visible](../README.md#req-pending-is-visible)

## Steps

GIVEN a chat model whose live reply carries two button rows and whose focus is the input
WHEN the `up` key is pressed
THEN focus moves to the button block at its LAST row — the row nearest the input
AND not to row 0

GIVEN a chat model whose focus is the button block at its last row
WHEN the `down` key is pressed
THEN focus returns to the input

GIVEN a chat model whose focus is the button block at its last row
WHEN the `up` key is pressed
THEN focus moves one row up, toward the top of the block
AND focus stays in the button block

GIVEN a chat model whose focus is the button block at row 0
WHEN the `up` key is pressed
THEN focus stays at row 0
AND focus stays in the button block

GIVEN a chat model whose focus is the input and whose live reply carries no buttons
WHEN the `up` key is pressed
THEN focus stays on the input

GIVEN a chat model whose focus is the button block on the button with callback data `space?id=family1`
WHEN the `enter` key is pressed
THEN `PressButton` is called exactly once with `space?id=family1`

GIVEN a chat model whose focus is the button block
WHEN the `esc` key is pressed
THEN focus returns to the input
AND the program does not quit

GIVEN a chat model whose focus is the input
WHEN the `esc` key is pressed
THEN the program quits

GIVEN a chat model with a reply in flight
WHEN any key other than `ctrl+c` is pressed
THEN the key is ignored
AND no further processor call is made

GIVEN a chat model with a reply in flight
WHEN the live region renders
THEN it shows an indicator, above the input, saying the bot is composing a reply
AND the indicator animates rather than sitting frozen

GIVEN a chat model with a reply in flight
WHEN the turn resolves — into replies, or into an error
THEN the indicator is gone

## TODO

- [ ] Implement by driving `Update` with `tea.KeyMsg` values against a fake processor.
- [ ] Cover `left` / `right` within a row once a command emits a multi-button row (`/spaces` emits one button per row, so this Feature has no such case yet).
- [ ] "Animates rather than frozen" is asserted by advancing the spinner's tick and observing the rendered frame change — a static-text indicator would pass a mere "contains the word" check, which is what makes that check worth avoiding.

---
*This document follows the https://specscore.md/scenario-specification*
