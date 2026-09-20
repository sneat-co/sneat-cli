---
format: https://specscore.md/scenario-specification
---

# Scenario: dual projections have semantic parity

**Status:** pending
**Validates:** [extension-api-command-parity#ac:dual-projection-operations-have-semantic-parity](../README.md#ac-dual-projection-operations-have-semantic-parity)

## Steps

GIVEN a dual-projection operation, authenticated callers, and isolated backends initialized from equivalent fixture snapshots
WHEN the fixture invokes the actual HTTP endpoint and actual CLI command separately for success and defined failure cases and verifies each mutation through its own public read
THEN client-stable IDs compare exactly and server-generated IDs compare through fixture-local placeholders
AND normalized values, pagination state, error classification, and persisted effects agree
AND any mismatch is attributed to the owning extension or CLI projection
AND shared-state replay occurs only in that projection's explicit idempotency scenario

## Automation

Bind this scenario to the real CLI binary and Sneat-Go test host using resettable emulator-backed namespaces; mock HTTP coverage alone does not satisfy it.

---
*This document follows the https://specscore.md/scenario-specification*
