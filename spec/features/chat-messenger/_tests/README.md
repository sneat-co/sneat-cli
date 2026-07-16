---
format: https://specscore.md/scenarios-index-specification
---

# Scenarios: Chat Messenger

Test scenarios for the [Chat Messenger](../README.md) specification.

| Scenario | Validates |
|---|---|
| [buttons-use-botkb-and-url-callback-data](buttons-use-botkb-and-url-callback-data.md) | [chat-messenger#ac:processing-is-swappable](../README.md#ac-processing-is-swappable), [chat-messenger#req:botkb-vocabulary](../README.md#req-botkb-vocabulary), [chat-messenger#req:callback-data-url](../README.md#req-callback-data-url) |
| [processor-returns-errors-unformatted](processor-returns-errors-unformatted.md) | [chat-messenger#ac:processing-is-swappable](../README.md#ac-processing-is-swappable), [chat-messenger#req:errors-are-returned-not-formatted](../README.md#req-errors-are-returned-not-formatted), [chat-messenger#req:processor-seam](../README.md#req-processor-seam) |
| [slash-commands-act-on-real-spaces](slash-commands-act-on-real-spaces.md) | [chat-messenger#ac:conversation-input-is-handled-honestly](../README.md#ac-conversation-input-is-handled-honestly), [chat-messenger#req:spaces-command](../README.md#req-spaces-command), [chat-messenger#req:help-command](../README.md#req-help-command), [chat-messenger#req:slash-command-routing](../README.md#req-slash-command-routing), [chat-messenger#req:active-space-selection](../README.md#req-active-space-selection), [chat-messenger#req:unrecognized-callback-data](../README.md#req-unrecognized-callback-data) |
| [free-text-returns-deferred-notice](free-text-returns-deferred-notice.md) | [chat-messenger#ac:conversation-input-is-handled-honestly](../README.md#ac-conversation-input-is-handled-honestly), [chat-messenger#req:free-text-deferred](../README.md#req-free-text-deferred) |

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/scenarios-index-specification*
