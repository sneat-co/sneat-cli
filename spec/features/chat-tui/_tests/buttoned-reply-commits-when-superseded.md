---
format: https://specscore.md/scenario-specification
---

# Scenario: a buttoned reply stays live until superseded, then commits to scrollback

**Validates:** [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:scrollback-commit](../README.md#req-scrollback-commit), [chat-tui#req:inline-rendering](../README.md#req-inline-rendering)

## Steps

GIVEN a chat model wired to a fake processor that returns a reply carrying a keyboard
WHEN the user submits `/spaces`
THEN the submitted user line is committed to scrollback
AND the reply becomes the live reply
AND the live region renders the reply text and its buttons

GIVEN a chat model whose live reply carries buttons
WHEN the user submits a further message
THEN the previously live reply is committed to scrollback before the new turn renders
AND its buttons are rendered inert in the committed text

GIVEN a chat model wired to a fake processor that returns a reply with no keyboard
WHEN the user submits `/help`
THEN the reply is committed to scrollback immediately
AND there is no live reply

## TODO

- [ ] Implement by driving the Bubble Tea model's `Update` directly and asserting on model state plus `View()` output; no real terminal.
- [ ] Assert the program is constructed without the alt-screen option.

---
*This document follows the https://specscore.md/scenario-specification*
