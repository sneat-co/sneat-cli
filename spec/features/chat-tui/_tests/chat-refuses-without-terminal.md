---
format: https://specscore.md/scenario-specification
---

# Scenario: chat refuses to start without a terminal or a session

**Validates:** [chat-tui#ac:chat-starts-only-when-usable](../README.md#ac-chat-starts-only-when-usable), [chat-tui#req:startup-preconditions](../README.md#req-startup-preconditions), [chat-tui#req:chat-command](../README.md#req-chat-command)

## Steps

GIVEN a `sneat` CLI whose injected `IsTerminal` reports false
AND a capturing `RunChat` that records whether it was called
WHEN `sneat chat` is executed
THEN the command returns an error stating that chat requires a terminal
AND `RunChat` was not called

GIVEN a `sneat` CLI whose injected `IsTerminal` reports true
AND a session store that returns an error when loaded
WHEN `sneat chat` is executed
THEN the command returns the session store's error
AND `RunChat` was not called

GIVEN a `sneat` CLI whose injected `IsTerminal` reports true
AND a session store holding a signed-in user with uid `u1`
WHEN `sneat chat` is executed
THEN `RunChat` is called exactly once with uid `u1`

## TODO

- [ ] Implement as `cmd/sneat/commands/chat_test.go`, mirroring the existing `ui_test.go` fixtures.

---
*This document follows the https://specscore.md/scenario-specification*
