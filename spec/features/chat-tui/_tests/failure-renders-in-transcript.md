---
format: https://specscore.md/scenario-specification
---

# Scenario: a backend failure is reported in the transcript and the session continues

**Validates:** [chat-tui#ac:transcript-is-durable-terminal-text](../README.md#ac-transcript-is-durable-terminal-text), [chat-tui#req:errors-render-in-transcript](../README.md#req-errors-render-in-transcript)

## Steps

GIVEN a chat model wired to a fake processor that returns an error
AND a transcript already holding at least one committed turn
WHEN the user submits `/spaces`
THEN the error is rendered as a bot message in the transcript, naming the failure
AND the program does not quit
AND the previously committed transcript is retained
AND the input accepts a further message

## TODO

- [ ] Implement by driving `Update` against a fake processor returning a canned error.
- [ ] Pair with [chat-messenger#req:errors-are-returned-not-formatted](../../chat-messenger/README.md#req-errors-are-returned-not-formatted) — the processor hands back a bare error and this renderer decides how it reads.

---
*This document follows the https://specscore.md/scenario-specification*
