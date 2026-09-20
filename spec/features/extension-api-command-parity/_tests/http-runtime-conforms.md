---
format: https://specscore.md/scenario-specification
---

# Scenario: HTTP runtime conforms

**Status:** pending
**Validates:** [extension-api-command-parity#ac:http-runtime-must-implement-the-http-projection](../README.md#ac-http-runtime-must-implement-the-http-projection)

## Steps

GIVEN an emitted HTTP contract and the actual composed Sneat-Go test router
WHEN the conformance runner exercises every required HTTP operation and injected mismatch fixtures
THEN conforming handlers pass
AND every route, method, schema, error, pagination, or retry mismatch fails with its operation ID and contract element

## Automation

Bind this scenario to generated HTTP cases executed through the real test router rather than a generated-client compile check alone.

---
*This document follows the https://specscore.md/scenario-specification*
