---
format: https://specscore.md/idea-specification
status: Draft
---

# Idea: Extension API-Command Parity

**Status:** Draft
**Date:** 2026-09-19
**Owner:** alex
**Promotes To:** —
**Supersedes:** —
**Related Ideas:** —

## Problem Statement

How might we keep Sneat extension APIs and CLI commands bidirectionally mapped so humans and AI agents can discover and execute the same supported operations without storage-specific knowledge or silent drift?

## Context

Sneat CLI is growing extension-specific command groups for Contactius, Calendarius, Assetus, Debtus, and Splitus. Current coverage is uneven: some reads bypass Sneat-Go through Firestore, some APIs expose writes without canonical reads, and manually maintained commands can silently drift from routes, DTOs, errors, pagination, and idempotency semantics. The command system must serve both humans and AI agents with deterministic YAML, JSON, Markdown, and list-only CSV renderings.

## Recommended Direction

Define a bidirectional parity contract between extension-owned public API operations and intentional CLI capabilities. Each extension owns versioned API schemas and a small capability mapping that declares command paths, arguments, safety semantics, and exclusions; generated typed clients and CI checks enforce the relationship, while Sneat CLI owns command composition, current-space resolution, machine-readable discovery, and deterministic rendering from one typed result model.

## Alternatives Considered

- **Generate commands directly from Go routes and DTOs.** This can inventory
  transport shapes, but it cannot reliably choose product nouns, distinguish a
  safe partial update from replacement, describe idempotency, or explain why an
  operation is deliberately unavailable to the CLI.
- **Maintain only a hand-written Sneat CLI manifest.** This expresses the desired
  command experience, but duplicates API facts without an authoritative link and
  therefore cannot prevent route, request, response, or enum drift.
- **Treat the existing CLI implementation as the contract.** This preserves
  direct Firestore reads and other storage knowledge, making API evolution and
  authorization parity impossible to verify.

## MVP Scope

Cover Contactius/contact, Calendarius/calendar, Assetus/asset, Debtus/debt, and Splitus/bill. Specify their supported API-to-command mappings and explicit exclusions; add current-space semantics; define YAML as the fixed default plus JSON and Markdown for all results and CSV for list results; define stable errors, pagination, mutation identities, idempotency metadata, schema discovery, generated-client boundaries, and CI drift checks. Produce specifications and plans before implementation.

## Not Doing (and Why)

- Authentication implementation — a separate active lane owns CLI authentication and this work assumes it is ready
- Extensions beyond Contactius, Calendarius, Assetus, Debtus, and Splitus — deferred until the mapping system is proven
- Direct Firestore access as a supported extension command contract — extension commands should reflect public APIs
- Generating user-facing command names solely from HTTP routes — command UX remains an intentional product contract

## Key Assumptions to Validate

| Tier | Assumption | How to validate |
|------|------------|-----------------|
| Must-be-true | Each public extension operation can expose a stable operation identity and machine-readable request, response, error, and safety metadata. | Prototype the manifest against the five MVP extensions and require every mapped operation to resolve to a versioned API contract. |
| Must-be-true | Bidirectional parity permits explicit exclusions; it does not require every API route to become a CLI command. | Require each public operation to be mapped or carry a reviewed `cliExcluded` rationale, and require every advertised command to resolve back to an operation. |
| Must-be-true | Extension commands can use public APIs without reading Firestore or extension storage directly. | Add dependency and end-to-end tests that run the CLI against Sneat-Go and verify persisted results through an independent public read. |
| Should-be-true | Extension-owned schemas plus a small capability mapping are cheaper to maintain than duplicated Cobra and API metadata. | Measure manual metadata, generated output, and review effort for Contactius and Assetus before rolling through the remaining extensions. |
| Should-be-true | One typed result model can render deterministic YAML, JSON, Markdown, and list-only CSV without semantic loss. | Golden-test each renderer from the same fixtures, including nulls, nested values, IDs, errors, and pagination metadata. |
| Might-be-true | Agents will benefit from machine-readable command discovery enough to reduce extension-specific skill prompt size. | Compare agent task completion and prompt size with prose help versus capability-schema discovery after the MVP exists. |


## SpecScore Integration

- **New Features this would create:** `extension-command-contract`,
  `extension-command-output`, `current-space-context`, and one API-contract
  Feature in each of Contactus, Calendarius, Assetus, Debtus, and Splitus.
- **Existing Features affected:** `action-protocol-cli`; its JSON-only output and
  exit behavior must either adopt the shared result/error contract or document a
  compatibility exception.
- **Dependencies:** authenticated Sneat API access from the parallel CLI
  authentication initiative; versioned public extension contracts; Sneat-Go
  composition of the approved routes.

## Open Questions

None at this time.
