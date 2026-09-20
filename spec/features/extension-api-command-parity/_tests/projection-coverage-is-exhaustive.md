---
format: https://specscore.md/scenario-specification
---

# Scenario: projection coverage is exhaustive

**Status:** pending
**Validates:** [extension-api-command-parity#ac:every-operation-has-an-explicit-projection-disposition](../README.md#ac-every-operation-has-an-explicit-projection-disposition)

## Steps

GIVEN TypeSpec fixtures containing dual-projection, HTTP-only, CLI-only, unclassified, reasonless-exclusion, and both-projections-excluded operations
WHEN the parity compiler builds the operation ledger
THEN the first three fixtures produce complete HTTP and CLI dispositions
AND the unclassified, reasonless-exclusion, and both-projections-excluded fixtures fail with their stable operation IDs

## Automation

Bind this scenario to compiler fixtures for the Sneat TypeSpec decorators and parity-ledger validation.

---
*This document follows the https://specscore.md/scenario-specification*
