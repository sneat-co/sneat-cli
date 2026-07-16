---
format: https://specscore.md/scenarios-index-specification
---

# Scenarios: Chat TUI

Test scenarios for the [Chat TUI](../README.md) specification.

| Scenario | Validates |
|---|---|
| [chat-refuses-without-terminal](chat-refuses-without-terminal.md) | [chat-tui#ac:chat-starts-only-when-usable](../README.md#ac-chat-starts-only-when-usable), [chat-tui#req:startup-preconditions](../README.md#req-startup-preconditions), [chat-tui#req:chat-command](../README.md#req-chat-command) |
| [buttoned-reply-commits-when-superseded](buttoned-reply-commits-when-superseded.md) | [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:scrollback-commit](../README.md#req-scrollback-commit), [chat-tui#req:inline-rendering](../README.md#req-inline-rendering) |
| [press-commits-live-reply](press-commits-live-reply.md) | [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:scrollback-commit](../README.md#req-scrollback-commit), [chat-tui#req:card-edit-in-place](../README.md#req-card-edit-in-place), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys) |
| [card-edit-navigates-in-place](card-edit-navigates-in-place.md) | [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:card-edit-in-place](../README.md#req-card-edit-in-place), [chat-tui#req:button-kinds](../README.md#req-button-kinds), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys) |
| [failure-renders-in-transcript](failure-renders-in-transcript.md) | [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:errors-render-in-transcript](../README.md#req-errors-render-in-transcript) |
| [focus-moves-between-input-and-buttons](focus-moves-between-input-and-buttons.md) | [chat-tui#ac:interaction-is-unambiguous](../README.md#ac-interaction-is-unambiguous), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys), [chat-tui#req:input-locked-while-pending](../README.md#req-input-locked-while-pending) |
| [command-palette](command-palette.md) | [chat-tui#ac:interaction-is-unambiguous](../README.md#ac-interaction-is-unambiguous), [chat-tui#req:command-palette](../README.md#req-command-palette), [chat-tui#req:focus-and-keys](../README.md#req-focus-and-keys) |

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/scenarios-index-specification*
