---
format: https://specscore.md/scenario-specification
---

# Scenario: dual projections have semantic parity

**Status:** pending
**Validates:** [extension-api-command-parity#ac:dual-projection-operations-have-semantic-parity](../README.md#ac-dual-projection-operations-have-semantic-parity)

## Steps

GIVEN a dual-projection operation, authenticated callers, shared fixtures, and one controlled persisted-data backend
WHEN the fixture invokes the actual HTTP endpoint and actual CLI command for success and defined failure cases
THEN normalized identities, values, pagination state, error classification, and persisted effects agree
AND any mismatch is attributed to the owning extension or CLI projection

## Automation

Bind this scenario to the real CLI binary and Sneat-Go test host using emulator-backed persisted data; mock HTTP coverage alone does not satisfy it.

---
*This document follows the https://specscore.md/scenario-specification*
