---
format: https://specscore.md/scenario-specification
---

# Scenario: the command palette opens on a typed / and holds focus

**Validates:** [chat-tui#ac:interaction-is-unambiguous](../README.md#ac-interaction-is-unambiguous), [chat-tui#req:command-palette](../README.md#req-command-palette), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys)

## Steps

GIVEN a chat model whose processor lists commands /spaces, /space, /contacts, /help
WHEN the input holds "/"
THEN the live region shows a palette listing all four commands
AND each row shows the command's summary
AND /contacts shows its argument hint

GIVEN a palette open on the input "/"
WHEN the input holds "/sp"
THEN the palette lists only /spaces and /space
AND does not list /contacts

GIVEN a palette open with the first command highlighted
WHEN the `down` key is pressed
THEN the highlight moves to the second command
AND the button block is not entered

GIVEN a palette open with the last command highlighted
WHEN the `down` key is pressed
THEN the highlight stays on the last command

GIVEN a palette open with /spaces highlighted
WHEN the `enter` key is pressed
THEN /spaces is run
AND the palette closes

GIVEN a palette open
WHEN the `esc` key is pressed
THEN the palette closes without running any command
AND the program does not quit
AND the palette stays closed while the typed text is unchanged

GIVEN a chat model whose live reply carries buttons and whose input holds "/sp"
WHEN the `up` key is pressed
THEN the palette highlight moves, not the button-block focus
AND the button block is unreachable while the palette is open

GIVEN a chat model whose input holds "/contacts family" (a space typed after the command)
WHEN the live region renders
THEN no palette is shown
AND `enter` submits the line as written

## TODO

- [ ] Implement by driving `Update` with `tea.KeyMsg` values and a fake processor that returns a known command list from `Commands()`.
- [ ] "Runs the command" is observed the way a submit is: the fake processor records the text it was asked to handle.

---
*This document follows the https://specscore.md/scenario-specification*
