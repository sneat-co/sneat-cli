---
format: https://specscore.md/scenario-specification
---

# Scenario: generated projections are reproducible

**Status:** pending
**Validates:** [extension-api-command-parity#ac:generated-projections-are-reproducible-and-discoverable](../README.md#ac-generated-projections-are-reproducible-and-discoverable)

## Steps

GIVEN pinned compiler and emitter versions and one unchanged extension TypeSpec fixture
WHEN OpenAPI and CLI capability artifacts are generated twice in clean output directories
THEN corresponding artifact bytes are identical
AND required operations are discoverable by stable ID only in their included projections
AND injected duplicate IDs, duplicate command paths, and unresolved references fail deterministically

## Automation

Bind this scenario to isolated TypeSpec compilation and byte-for-byte artifact comparison.

---
*This document follows the https://specscore.md/scenario-specification*
