---
format: https://specscore.md/scenario-specification
---

# Scenario: a card edit re-renders the live card in place and the three button kinds each act distinctly

**Validates:** [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:card-edit-in-place](../README.md#req-card-edit-in-place), [chat-tui#req:button-kinds](../README.md#req-button-kinds), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys)

## Steps

GIVEN a chat model whose live card carries inline buttons
AND focus is on a callback button whose press will be answered by a single reply with its `Edit` flag set
WHEN the `enter` key is pressed and the edit reply returns
THEN nothing commits to scrollback and nothing is echoed
AND the live card is replaced in place by the reply's text and buttons
AND focus stays in the button block, on the new card's first button, so the user chains presses without returning to the input

GIVEN the model showing the edited card
WHEN a further button on it is pressed and answered by another `Edit` reply
THEN again nothing commits, and the card is replaced in place a second time

GIVEN a chat model showing a card and focus on a **send** button (`botkb.TextButton`)
WHEN the `enter` key is pressed
THEN the button's own text is sent through `SendText`, exactly as if the user had typed and submitted it
AND the card commits with an echo of the sent text, then the reply renders — the shape a submit has

GIVEN a chat model showing a card and focus on a **URL** button (`botkb.UrlButton`)
AND an injected browser opener stands in for the real one
WHEN the `enter` key is pressed
THEN the opener is asked to open the button's URL
AND no chat turn is made: nothing reaches the processor, nothing commits, and the card stays live
AND a browser that fails to open surfaces as a message in the transcript rather than crashing the session

GIVEN a chat model rendering a card that mixes all three button kinds
WHEN its live region is drawn
THEN each kind is told apart by a glyph in the button's label rather than by colour alone — a URL button marked as leaving for the browser, a send button as putting its text into the conversation, and a callback button unmarked

## TODO

- [ ] Implement by driving `Update` with a `tea.KeyMsg` for `enter` while focus is the button block, with a `fakeProcessor` returning `Edit` replies and an injected browser opener that records URLs.
- [ ] The card-edit focus rule is a deliberate exception to REQ: focus-and-keys — a normal reply leaves focus on the input, but an edited card keeps focus on its first button so a menu can be navigated by chained presses.
- [ ] The glyph, not colour, carries the kind: lipgloss emits no colour off a terminal, so a copied or piped transcript and a monochrome terminal both keep the glyph and lose the colour.

---
*This document follows the https://specscore.md/scenario-specification*
