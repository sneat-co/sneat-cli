---
format: https://specscore.md/scenario-specification
---

# Scenario: slash commands list real spaces and selection sets the active space

**Validates:** [chat-messenger#ac:conversation-input-is-handled-honestly](../README.md#ac-conversation-input-is-handled-honestly), [chat-messenger#req:spaces-command](../README.md#req-spaces-command), [chat-messenger#req:help-command](../README.md#req-help-command), [chat-messenger#req:slash-command-routing](../README.md#req-slash-command-routing), [chat-messenger#req:active-space-selection](../README.md#req-active-space-selection), [chat-messenger#req:card-edit](../README.md#req-card-edit), [chat-messenger#req:button-kinds](../README.md#req-button-kinds), [chat-messenger#req:contacts-card](../README.md#req-contacts-card), [chat-messenger#req:unrecognized-callback-data](../README.md#req-unrecognized-callback-data)

## Steps

GIVEN a local processor backed by a fake spaces reader returning spaces `family1` titled "Family" and `personal1` titled "Personal"
WHEN `SendText` is called with `/spaces`
THEN the reply text states that the user has 2 spaces
AND the keyboard has exactly 2 rows
AND each row holds exactly one button, labelled "Family" and "Personal" respectively

GIVEN a fake spaces reader returning an untitled `family` space, an untitled `private` space, and two titled custom spaces "Z Space 2" and "Space 1"
WHEN `SendText` is called with `/spaces`
THEN the buttons read, top to bottom: "Space 1", "Z Space 2", "Private (…)", "Family (…)"
AND the family space is last, because the renderer's entry point is nearest the end

GIVEN a fake spaces reader returning two custom spaces sharing the title "Shared"
WHEN `SendText` is called with `/spaces`
THEN their relative order is stable across invocations, broken by space ID

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

GIVEN that same processor with no active space set, having just listed its spaces
WHEN `PressButton` is called with `space?id=family1`
THEN the processor's active space is `family1`
AND the reply is an `Edit` reply that re-renders the pressed message in place
AND its text names the "Family" space
AND its keyboard carries a callback button to the space's contacts, a URL button opening the space in the browser, a send button, and a callback button back to the spaces list

GIVEN the processor showing the "Family" space card, backed by a contacts reader returning "Alice" and "Bob" for `family1`
WHEN `PressButton` is called with `contacts?space=family1`
THEN the reply is an `Edit` reply listing "Alice" and "Bob"
AND its keyboard carries a callback button back to the space card

GIVEN the processor showing the contacts card
WHEN `PressButton` is called with `space?id=family1`
THEN the reply is an `Edit` reply re-rendering the space card

GIVEN the processor showing a space card
WHEN `PressButton` is called with `spaces`
THEN the reply is an `Edit` reply re-rendering the spaces list
AND its keyboard carries one button per listed space

GIVEN the same processor
WHEN `SendText` is called with `/help`
THEN the reply text names the available commands, including `/spaces`, `/space`, `/who-am-i`, `/contacts`, and `/version`

GIVEN a processor built with the signed-in user's email "user@example.com"
WHEN `SendText` is called with `/who-am-i`
THEN the reply names the email "user@example.com"

GIVEN a processor built with the CLI version "1.2.3"
WHEN `SendText` is called with `/version`
THEN the reply names "1.2.3"

GIVEN a processor with no active space selected
WHEN `SendText` is called with `/space`
THEN the reply states that no space is selected
AND points the user at `/spaces`

GIVEN a processor whose active space was set to `family1` by a prior press
WHEN `SendText` is called with `/space`
THEN the reply names the active space

GIVEN a processor whose active space is `family1`, backed by a contacts reader returning "Alice" and "Bob" for `family1`
WHEN `SendText` is called with `/contacts`
THEN the reply lists "Alice" and "Bob"

GIVEN a processor backed by a family-typed space `vaoyj` and a contacts reader returning "Carol" for `vaoyj`
WHEN `SendText` is called with `/contacts family`
THEN the space type resolves to `vaoyj`
AND the reply lists "Carol"

GIVEN a processor backed by a space `ao58m`
WHEN `SendText` is called with `/contacts ao58m`
THEN the argument matches the space ID directly

GIVEN a processor with no active space and a `/contacts` call carrying no argument
WHEN `SendText` is called with `/contacts`
THEN the reply says no space is selected
AND points the user at `/spaces`

GIVEN a processor whose spaces include two of type `club`
WHEN `SendText` is called with `/contacts club`
THEN the reply says the type is ambiguous, rather than picking one

GIVEN a processor
WHEN `SendText` is called with `/contacts nope`
THEN the reply says there is no such space, naming "nope"
AND no error is returned

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
