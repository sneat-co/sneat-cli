---
format: https://specscore.md/plan-specification
status: Executing
---
# Plan: Extension API-Command Parity

**Status:** Executing
**Source Feature:** extension-api-command-parity
**Date:** 2026-09-20
**Owner:** alex
**Supersedes:** —

## Summary

Build the contract and conformance foundation that projects one extension-owned
TypeSpec operation model into HTTP/OpenAPI and CLI capability contracts, then
proves that the Sneat-Go and Sneat CLI runtimes implement those contracts without
the CLI bypassing the public API.

This Plan does not select the commands, flags, or output layouts for Contactius,
Calendarius, Assetus, Debtus, or Splitus. Those remain sibling Features that use
this foundation. Authentication is treated as an available dependency.

## Approach

The end-to-end journey is extension-author first:

1. An extension author declares an operation once, gives it a stable identity,
   and explicitly includes or reasonedly excludes its HTTP and CLI projections.
   **Observable good result:** the compiler produces an exhaustive ledger and
   rejects missing dispositions, empty reasons, duplicate identities, and an
   operation excluded from both projections.
2. The author generates artifacts without manually editing either projection.
   **Observable good result:** pinned tooling emits byte-identical OpenAPI and
   CLI capability manifests on repeat runs, with the same operation identities
   and no excluded operations leaking into either artifact.
3. The author promotes the generated ledger, OpenAPI, and CLI manifest as one
   immutable contract-only module release, then does nothing to copy or patch it
   in a consumer repository. **Observable good result:** the release carries its
   source revision, tooling versions, module checksum, and bundle digest; a
   separately checked-out consumer resolves the exact version and rejects
   incomplete or altered bytes.
4. HTTP and CLI implementers bind their runtimes to that released contract.
   **Observable good result:** each runtime conformance suite either passes or
   reports the operation identity and exact mismatched contract element; the CLI
   reaches extension data only through its contract-bound public API client.
5. CI invokes a dual-projection mutation through independently isolated,
   equivalently seeded backends and then does nothing outside the declared
   public follow-up reads. **Observable good result:** both persisted states and
   normalized results agree, while failures are attributed to the owning
   extension or projection.

Implementation follows that dependency chain. The canonical model and ledger
come first, deterministic emitters consume that ledger next, and an immutable
release boundary makes the exact artifacts consumable across repositories before
the two runtime conformance adapters follow. The final task composes those pieces
into the persisted parity journey. A controlled fixture extension supplies
HTTP-and-CLI, HTTP-only, and CLI-only operations so this Feature can prove the
framework without prematurely defining the five product command vocabularies.

All seven acceptance criteria are covered; none are deferred. Each task includes
its compiler, artifact, runtime, or controlled-backend checks so testing is not
postponed to a separate final phase.

## Tasks

### Task 1: Compile the canonical operation ledger

**Id:** task-1
**Verifies:** extension-api-command-parity#ac:every-operation-has-an-explicit-projection-disposition
**Depends-On:** —
**Status:** complete

Define the extension-owned TypeSpec operation metadata for stable identities and
independent HTTP/CLI inclusion or reasoned exclusion, then compile it into one
exhaustive implementation ledger. Add positive fixtures for all three supported
projection combinations and deterministic failures for missing dispositions,
empty reasons, duplicate operation IDs, unresolved references, and operations
excluded from both projections.

### Task 2: Emit reproducible HTTP and CLI contracts

**Id:** task-2
**Verifies:** extension-api-command-parity#ac:generated-projections-are-reproducible-and-discoverable
**Depends-On:** 1
**Status:** complete

Implement the pinned projection pipeline that emits OpenAPI and the
machine-readable CLI capability manifest from the ledger, including stable
ordering and duplicate command-path validation, and make the manifest the input
for CLI capability discovery, schema inspection, and generated help metadata.
Add checked generation and CI tests that run twice, compare bytes, resolve every
required operation by stable ID, and prove that excluded projections never
appear in their artifact. This completed task established deterministic emission
and discovery for the then-approved manifest fields; the conformance-completeness
fields added by the amended Feature are assigned to Task 3.

### Task 3: Publish a complete versioned projection bundle

**Id:** task-3
**Verifies:** extension-api-command-parity#ac:projection-bundle-is-complete-versioned-and-consumable
**Depends-On:** 2
**Status:** planning

Extend the controlled TypeSpec operation so its emitted OpenAPI carries typed
success and coded error responses, request constraints, explicit pagination and
ordering disposition, and retry or idempotency metadata; extend the CLI manifest
schema and generated Go representation with error-envelope schemas and exit
categories, and fail publication when either projection omits any required
semantic or provenance field. Atomically promote the ledger, OpenAPI, and CLI
manifest—with source revision, pinned tooling versions, and content digest over
the artifacts and non-digest provenance fields—into a contract-only module under
`sneat-ext-contracts`. Release and consume an exact module version from a
separate checkout, verifying its module checksum and bundle digest; prove that
no copied fixture, local path, or dependency replacement is accepted and that
byte substitution fails verification.

### Task 4: Enforce the emitted HTTP contract in Sneat-Go

**Id:** task-4
**Verifies:** extension-api-command-parity#ac:http-runtime-must-implement-the-http-projection
**Depends-On:** 3
**Status:** planning

Build the HTTP conformance adapter over the composed Sneat-Go test router and
the ledger's HTTP implementation entries. Exercise method, route, request,
response, coded error, pagination, and retry semantics with the controlled
fixture extension, and make every mismatch identify the stable operation ID and
contract element while honoring reasoned HTTP exclusions. Reconcile the runtime
inventory in both directions so a missing required handler and an implemented
but unspecified HTTP operation each fail with an attributable diagnostic, and
reconcile the result ledger so an implemented required operation with no
conformance result fails independently of route presence or contract-shape
mismatches.

### Task 5: Enforce the CLI contract and public API boundary

**Id:** task-5
**Verifies:** extension-api-command-parity#ac:cli-runtime-must-implement-the-cli-projection, extension-api-command-parity#ac:extension-commands-use-the-public-api-boundary
**Depends-On:** 4
**Status:** planning

Build the CLI conformance adapter against the real Sneat command tree, checking
paths, aliases, input mappings, formats, exit categories, and result/error
envelopes from the capability manifest. Add dependency-boundary checks that
reject extension persistence, Firestore, or storage-model access from CLI
packages, plus a fixture mutation through the contract-bound API client that is
verified by an independent public read. Reconcile the command inventory in both
directions so missing required commands and implemented-but-unspecified commands
fail independently of ordinary contract-shape mismatches, and reconcile the
result ledger so an implemented required command with no conformance result is a
separate attributable failure.

### Task 6: Prove persisted semantic parity across both projections

**Id:** task-6
**Verifies:** extension-api-command-parity#ac:dual-projection-operations-have-semantic-parity
**Depends-On:** 5
**Status:** planning

Create the controlled-backend journey that invokes the actual HTTP endpoint and
actual CLI command against independently isolated backends seeded from equivalent
snapshots. Compare success, invalid-input, forbidden, conflict, and retry cases,
including exact client-stable identities, fixture-local placeholders for
server-generated identities, normalized values, pagination, coded errors, and
persisted effects; keep idempotency cases projection-specific and attribute each
failure to its owning extension or projection.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/plan-specification*
