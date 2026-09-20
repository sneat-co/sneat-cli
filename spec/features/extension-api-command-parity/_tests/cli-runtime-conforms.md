---
format: https://specscore.md/scenario-specification
---

# Scenario: CLI runtime conforms

**Status:** pending
**Validates:** [extension-api-command-parity#ac:cli-runtime-must-implement-the-cli-projection](../README.md#ac-cli-runtime-must-implement-the-cli-projection)

## Steps

GIVEN an emitted capability manifest and the actual Sneat command tree
WHEN the conformance runner inspects and executes every required CLI operation and injected mismatch fixtures
THEN conforming commands pass
AND every command-path, alias, input, format, exit, or envelope mismatch fails with its operation ID and manifest field
AND an undeclared advertised extension command and a required operation with no conformance result each fail inventory reconciliation

## Automation

Bind this scenario to Cobra tree inspection plus command execution through injected API clients and captured streams.

---
*This document follows the https://specscore.md/scenario-specification*
