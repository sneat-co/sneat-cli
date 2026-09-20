---
format: https://specscore.md/scenario-specification
---

# Scenario: projection bundle is complete and versioned

**Status:** pending
**Validates:** [extension-api-command-parity#ac:projection-bundle-is-complete-versioned-and-consumable](../README.md#ac-projection-bundle-is-complete-versioned-and-consumable)

## Steps

GIVEN a controlled operation with complete response, error, constraint, pagination, ordering, and retry metadata
WHEN the ledger, OpenAPI, and CLI manifest are promoted into a released contract-only module
THEN a separate checkout resolves the exact version and verifies its module checksum plus a bundle digest over all three artifacts and the non-digest provenance fields
AND missing required semantics, local dependency replacement, copied fixtures, and substituted bytes fail deterministically

## Automation

Bind this scenario to projection publication, released-module resolution and checksum verification from an isolated consumer checkout, and negative provenance and digest fixtures.

---
*This document follows the https://specscore.md/scenario-specification*
