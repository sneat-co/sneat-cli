---
format: https://specscore.md/scenario-specification
---

# Scenario: CLI uses the public API boundary

**Status:** pending
**Validates:** [extension-api-command-parity#ac:extension-commands-use-the-public-api-boundary](../README.md#ac-extension-commands-use-the-public-api-boundary)

## Steps

GIVEN an extension command whose operation requires an HTTP projection
WHEN CI checks its dependencies and runs a mutation followed by an independent public read
THEN the command uses the generated or contract-bound API client
AND no CLI package imports or invokes Firestore, extension persistence, or storage models
AND the public read observes the persisted mutation

## Automation

Bind this scenario to a forbidden-dependency check plus the emulator-backed CLI journey; a mock HTTP assertion alone does not satisfy the persisted observation.

---
*This document follows the https://specscore.md/scenario-specification*
