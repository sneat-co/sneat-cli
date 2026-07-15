---
format: https://specscore.md/scenario-specification
---

# Scenario: the processor returns errors rather than formatting them

**Validates:** [chat-messenger#ac:processing-is-swappable](../README.md#ac-processing-is-swappable), [chat-messenger#req:errors-are-returned-not-formatted](../README.md#req-errors-are-returned-not-formatted), [chat-messenger#req:processor-seam](../README.md#req-processor-seam)

## Steps

GIVEN a local processor backed by a fake spaces reader that returns an error
WHEN `SendText` is called with `/spaces`
THEN a non-nil error is returned to the caller
AND no reply carrying user-facing error prose is returned

GIVEN the chat renderer package
WHEN its imports are inspected
THEN it depends on the `Processor` interface
AND it does not reference any concrete processor implementation

## TODO

- [ ] Implement the first case as a unit test using the `fakeSpaces{err: …}` pattern from `internal/tui/tui_test.go`.
- [ ] Implement the second as a structural check (import inspection), not a behavioural mock — the requirement is about coupling, which a mock cannot observe.

---
*This document follows the https://specscore.md/scenario-specification*
