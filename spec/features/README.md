---
format: https://specscore.md/features-index-specification
---

# Features

Feature specifications for this project.

## Index

| Feature | Status | Description |
|---------|--------|-------------|
| [Chat Messenger](chat-messenger/README.md) | Stable | Platform-agnostic conversational contract for Sneat chat: the Processor seam, botkb inline-button vocabulary, URL-shaped callback data, and the slash commands that operate on the signed-in user's real data. Rendered by chat-tui today; reusable by a future web messenger. |
| [Chat TUI](chat-tui/README.md) | Stable | First renderer of the Sneat chat messenger: the `sneat chat` command and its inline (non-alt-screen) Bubble Tea terminal UI, with a bottom-pinned input and arrow-navigable inline buttons. |
| [Action Protocol CLI](action-protocol-cli/README.md) | Draft | sneat action/context/query commands: a thin CLI over the sneat-ai-backend Action Protocol |
| [Chat AI Pipeline](chat-ai/README.md) | Draft | `internal/aichat` is the sneat-chat MVP's AI processing pipeline: Sneat's decision taxonomy and deterministic rules on top of `strongo/aichat`'s shared `LLMProvider`/`DecisionProvider` contracts, entity resolution against real Firestore/API data, pending-confirmation and undo bookkeeping, the `<sneat-action>` stream-splitter convention for a main-LLM turn, and flag/env/file configuration for Sneat AI Cloud and BYOK. It is consumed by `cmd/sneat/commands/chat.go` today only for flag plumbing; running it against the interactive shell is out of scope for this artifact (see Out of Scope). |

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/features-index-specification*
