---
format: https://specscore.md/scenario-specification
---

# Scenario: free text is refused rather than routed to the sandbox runtime

**Validates:** [chat-messenger#ac:conversation-input-is-handled-honestly](../README.md#ac-conversation-input-is-handled-honestly), [chat-messenger#req:free-text-deferred](../README.md#req-free-text-deferred)

## Steps

GIVEN a local processor backed by a fake spaces reader
WHEN `SendText` is called with `hello` (no leading slash)
THEN the reply states that free-text chat is not yet available
AND names at least one working command
AND the reply carries no keyboard
AND no error is returned

GIVEN the local processor package
WHEN its imports are inspected
THEN it does not import `convoruntime`
AND it constructs no sandbox datastore

## TODO

- [ ] Implement the first case as a unit test over the local processor.
- [ ] The second step guards the design's core reason for deferring free text: `convoruntime` is wired only to the mock-LLM sandbox with a fake space and user, so reaching it from a real-data session would blend fixture actions with real listings in one transcript. Prefer the structural import check over a behavioural mock.
- [ ] Planning note: the import check is at Go *package* granularity, and `cmd/sneat/commands` already imports `convoruntime` (`convo.go`). The processor must therefore live in its own package — an `internal/chat`, alongside `internal/tui` — or this guard is unimplementable as written. This Feature deliberately does not name packages; the plan must pin it.

---
*This document follows the https://specscore.md/scenario-specification*
