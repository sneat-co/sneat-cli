---
format: https://specscore.md/scenario-specification
---

# Scenario: slash commands list real spaces and selection sets the active space

**Validates:** [chat-messenger#ac:conversation-input-is-handled-honestly](../README.md#ac-conversation-input-is-handled-honestly), [chat-messenger#req:spaces-command](../README.md#req-spaces-command), [chat-messenger#req:help-command](../README.md#req-help-command), [chat-messenger#req:slash-command-routing](../README.md#req-slash-command-routing), [chat-messenger#req:active-space-selection](../README.md#req-active-space-selection), [chat-messenger#req:unrecognized-callback-data](../README.md#req-unrecognized-callback-data)

## Steps

GIVEN a local processor backed by a fake spaces reader returning spaces `family1` titled "Family" and `personal1` titled "Personal"
WHEN `SendText` is called with `/spaces`
THEN the reply text states that the user has 2 spaces
AND the keyboard has exactly 2 rows
AND each row holds exactly one button, labelled "Family" and "Personal" respectively
AND the buttons are ordered by space ID, so `family1` precedes `personal1`

GIVEN a fake spaces reader returning a space `vaoyj` whose title is empty and whose type is `family`
WHEN `SendText` is called with `/spaces`
THEN the button for `vaoyj` is labelled `Family (vaoyj)`

GIVEN a fake spaces reader returning two spaces whose titles are empty and whose types are both `family`
WHEN `SendText` is called with `/spaces`
THEN the two buttons are distinguishable, each naming its own ID

GIVEN a fake spaces reader returning a space `solo1` whose title and type are both empty
WHEN `SendText` is called with `/spaces`
THEN the button for `solo1` is labelled `solo1`

GIVEN a fake spaces reader returning no spaces
WHEN `SendText` is called with `/spaces`
THEN the reply states the user has no spaces
AND the reply carries no keyboard

GIVEN that same processor with no active space set
WHEN `PressButton` is called with `space?id=family1`
THEN the processor's active space is `family1`
AND the reply confirms the active space is now "Family"

GIVEN the same processor
WHEN `SendText` is called with `/help`
THEN the reply text names the available commands, including `/spaces`

GIVEN the same processor
WHEN `SendText` is called with `/nope`
THEN the reply names the unknown command `/nope`
AND points the user at `/help`
AND no error is returned

GIVEN the same processor
WHEN `PressButton` is called with `nope?id=1`, with `%zz`, or with `space` carrying no `id`
THEN each call returns either a reply saying the action could not be handled, or an error
AND none panics
AND none silently returns nothing

GIVEN the same processor with an active space of `family1`
WHEN `PressButton` is called with `space?id=ghost1`, naming a space the reader does not return
THEN an error is returned
AND the active space remains `family1`

## TODO

- [ ] Implement as a unit test over the local processor, using the `fakeSpaces` pattern from `internal/tui/tui_test.go`.
- [ ] The button-order assertion guards against ranging over `ListSpaces`'s `map[string]any` directly — Go randomizes map iteration, so an unordered implementation passes intermittently. `internal/tui/items.go`'s `spaceItemsFrom` shows the established fix (`sortedKeys` before reading each title).

---
*This document follows the https://specscore.md/scenario-specification*
