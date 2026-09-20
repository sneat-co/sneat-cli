---
format: https://specscore.md/feature-specification
status: Approved
---

# Feature: Extension API-Command Parity

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/extension-api-command-parity?op=explore) | [Edit](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/extension-api-command-parity?op=edit) | [Ask question](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/extension-api-command-parity?op=ask) | [Request change](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/extension-api-command-parity?op=request-change) |
**Status:** Approved
**Supersedes:** —
**Source Ideas:** extension-api-command-parity
**Grade:** B

## Summary

Use TypeSpec to define extension operations and generate independently selectable HTTP and CLI projections with explicit exclusions and implementation conformance checks.

## Problem

Sneat extension APIs and CLI commands currently evolve through separate Go
registrations and implementations. An HTTP route can exist without an intentional
CLI decision, a CLI command can bypass the public API, and either side can change
its inputs, outputs, errors, pagination, or retry semantics without the other side
failing CI. Route inference alone cannot express product command names, local-only
commands, safety requirements, or deliberate exclusions.

## Behavior

### Canonical operations

#### REQ: typespec-is-canonical-operation-model

Each participating extension MUST define its public semantic operations, input
and output models, errors, constraints, and versioning in TypeSpec. Generated
OpenAPI, generated CLI capability manifests, and generated clients are derived
artifacts and MUST NOT become independently edited sources of truth.

#### REQ: stable-operation-identity

Every operation MUST have a stable, globally unambiguous operation ID that remains
identical across the TypeSpec model, OpenAPI projection, CLI capability manifest,
generated client, conformance reports, and runtime telemetry. Renaming an ID is a
contract change even when the HTTP path or CLI spelling remains unchanged.

### Projection selection

#### REQ: explicit-projection-disposition

Every operation MUST declare a disposition for both `http` and `cli`: `required`
or `excluded`. An excluded projection MUST carry a non-empty reason. Omission,
unknown disposition values, an exclusion without a reason, and an operation with
both projections excluded MUST fail contract compilation.

#### REQ: http-projection

For an operation whose HTTP disposition is `required`, TypeSpec MUST define the
method, route, parameter locations, request and response models, error responses,
and stable operation ID needed to emit its OpenAPI contract. HTTP-only operations
remain visible to parity reporting even when CLI is explicitly excluded.

#### REQ: cli-projection

For an operation whose CLI disposition is `required`, Sneat TypeSpec metadata
MUST define its command path, aliases, input-to-argument mapping, supported output
representations, collection semantics, mutation safety, and idempotency or retry
classification. CLI-only local operations MUST live outside the emitted HTTP
service while remaining first-class entries in the CLI manifest.

### Generated contracts

#### REQ: deterministic-projection-artifacts

The same TypeSpec sources and pinned compiler/emitter versions MUST produce
byte-stable OpenAPI and CLI capability artifacts. CI MUST regenerate them and fail
when committed artifacts differ, contain duplicate operation IDs or command paths,
or contain references that cannot be resolved.

#### REQ: machine-readable-capability-manifest

The CLI projection MUST emit a versioned machine-readable manifest containing
each command's operation ID, path, aliases, inputs, result schema, supported
formats, pagination and ordering contract, mutation classification, idempotency
requirements, and HTTP mapping when present. Sneat CLI help and schema discovery
MUST consume this manifest rather than scrape prose or infer routes.

### Implementation conformance

#### REQ: exhaustive-implementation-ledger

CI MUST account for every TypeSpec operation and both projection dispositions.
Every required projection MUST have an implementation-conformance result; every
excluded projection MUST report its exclusion reason. Silent omission, an
implemented but unspecified extension operation, or an advertised command without
a TypeSpec operation MUST fail the parity gate.

#### REQ: http-implementation-conformance

For each required HTTP projection, automated tests MUST verify that the composed
Sneat-Go router exposes the specified method and path and that requests, responses,
status codes, error bodies, pagination, and retry behavior conform to the emitted
contract. A generated client compiling successfully is supporting evidence, not a
substitute for exercising the actual handler.

#### REQ: cli-implementation-conformance

For each required CLI projection, automated tests MUST inspect and execute the
real command tree to verify command paths, aliases, required arguments, flag types,
supported formats, exit categories, and result/error envelopes against the
capability manifest. Help text alone is not implementation evidence.

#### REQ: cli-uses-public-api-boundary

An extension command whose operation has a required HTTP projection MUST invoke
the generated or contract-bound public API client and MUST NOT import or call
Firestore, extension persistence adapters, or extension storage models directly.
CI MUST enforce the dependency boundary statically, and a persisted-data journey
MUST verify each mutation through an independent public read. CLI-only local
operations MAY access their declared local configuration store but MUST NOT use
that exception to read or mutate extension data.

#### REQ: cross-projection-behavioral-parity

When an operation requires both projections, shared contract fixtures MUST invoke
the actual HTTP endpoint and the actual CLI command from equivalent, independently
isolated backend snapshots and compare their normalized semantic results. Each
projection's mutation MUST be verified by a follow-up public read against its own
isolated state. Client-stable identities compare exactly; server-generated
identities are mapped to fixture-local placeholders before comparison. Wire
formatting may differ; values, pagination state, error classification, and
persisted effects MUST agree. Shared-state replay is reserved for an operation's
explicit idempotency scenario and MUST NOT be used as the ordinary parity setup.

### Ownership boundary

#### REQ: extension-owned-contracts

Each extension repository owns its TypeSpec operations, HTTP projection, and
extension conformance fixtures. Sneat CLI owns the reusable CLI decorators,
capability emitter, command-tree conformance runner, and CLI-only operations.
Sneat-Go owns only composition and mounting of approved extension routes; it MUST
NOT become the owner of extension domain schemas or CLI metadata.

## Architecture

```text
extension-owned TypeSpec
       |-- OpenAPI emitter ------> HTTP contract/client/conformance
       `-- Sneat CLI emitter ----> capability manifest/command conformance
                                         |
                                         `--> YAML | JSON | Markdown | CSV*

* CSV is available only for collection results declared CSV-compatible.
```

The OpenAPI and CLI emitters select their declared projection sets independently.
The parity gate joins them by stable operation ID and evaluates the implementation
ledger; it does not require every operation to appear in both projections.

## Acceptance Criteria

### AC: every-operation-has-an-explicit-projection-disposition

**Requirements:** extension-api-command-parity#req:typespec-is-canonical-operation-model, extension-api-command-parity#req:stable-operation-identity, extension-api-command-parity#req:explicit-projection-disposition

Scenario: projection coverage is exhaustive
Given TypeSpec operations representing an HTTP-and-CLI operation, an HTTP-only operation, and a CLI-only operation
When the projection compiler builds the parity ledger
Then each operation has one stable ID and an explicit included or reasoned-excluded result for both HTTP and CLI, while an unclassified operation or empty exclusion reason fails compilation.

### AC: generated-projections-are-reproducible-and-discoverable

**Requirements:** extension-api-command-parity#req:http-projection, extension-api-command-parity#req:cli-projection, extension-api-command-parity#req:deterministic-projection-artifacts, extension-api-command-parity#req:machine-readable-capability-manifest

Scenario: the same source produces the same projection contracts
Given pinned TypeSpec and emitter versions and unchanged extension sources
When CI regenerates OpenAPI and the CLI capability manifest twice
Then both runs are byte-identical, every required HTTP and CLI operation is discoverable by its stable ID, and no excluded projection leaks into its generated artifact.
And injected duplicate operation IDs, duplicate command paths, unresolved references, and both-projections-excluded operations fail deterministically.

### AC: http-runtime-must-implement-the-http-projection

**Requirements:** extension-api-command-parity#req:exhaustive-implementation-ledger, extension-api-command-parity#req:http-implementation-conformance

Scenario: an HTTP implementation drift blocks parity
Given an operation whose HTTP projection is required and a composed Sneat-Go test router
When its route, method, request, response, error, pagination, or retry behavior differs from the emitted contract
Then the HTTP conformance suite fails with the operation ID and exact mismatched contract element, while a correctly excluded CLI projection requires no CLI implementation result.

### AC: cli-runtime-must-implement-the-cli-projection

**Requirements:** extension-api-command-parity#req:exhaustive-implementation-ledger, extension-api-command-parity#req:cli-implementation-conformance

Scenario: a CLI implementation drift blocks parity
Given an operation whose CLI projection is required and the real Sneat command tree
When its command path, alias, input mapping, supported format, exit category, or result/error envelope differs from the capability manifest
Then the CLI conformance suite fails with the operation ID and exact mismatch, while a correctly excluded HTTP projection requires no HTTP implementation result.

### AC: extension-commands-use-the-public-api-boundary

**Requirements:** extension-api-command-parity#req:cli-uses-public-api-boundary, extension-api-command-parity#req:extension-owned-contracts

Scenario: an extension command cannot bypass its public API
Given an extension command whose TypeSpec operation requires an HTTP projection
When CI checks its dependency graph and runs a mutation followed by an independent public read
Then the command uses the generated or contract-bound API client, no CLI package imports or invokes extension persistence, Firestore, or storage models, and the public read observes the persisted mutation.

### AC: dual-projection-operations-have-semantic-parity

**Requirements:** extension-api-command-parity#req:cross-projection-behavioral-parity, extension-api-command-parity#req:extension-owned-contracts

Scenario: HTTP and CLI agree through a persisted journey
Given a dual-projection operation, authenticated access, and independently isolated backends initialized from equivalent fixture snapshots
When the fixture invokes the actual HTTP endpoint and actual CLI command separately for success, invalid input, forbidden access, conflict, and retry cases and verifies each mutation through its own public read
Then client-stable identities compare exactly, server-generated identities compare through fixture-local placeholders, normalized values, pagination state, error classifications, and persisted effects agree, and the report attributes failures to the owning extension or CLI projection.
And any shared-state retry is evaluated only by that projection's explicit idempotency scenario.

## Rehearse Integration

All six acceptance criteria have observable compiler, artifact, HTTP, CLI, or
persisted-data surfaces. Pending Rehearse scenarios are provided under `_tests/`;
implementation will bind them to the TypeSpec compiler, router, command tree, and
controlled-backend harnesses.

## Out of Scope

- Selecting the user-facing command vocabulary for every extension operation;
  extension command Features own those decisions.
- Defining YAML, JSON, Markdown, and CSV rendering semantics; the sibling
  `extension-command-output` Feature owns the renderers.
- Authentication mechanics; this Feature assumes authenticated API access.
- Generating Go HTTP handler implementations. Existing handlers conform to the
  emitted contract; TypeSpec does not own extension business logic or authorization.
- Migrating extensions beyond Contactus, Calendarius, Assetus, Debtus, and
  Splitus during the MVP.

## Assumption Carryover

The source Idea's Must-be-true assumptions are made observable by the operation
ledger, the three-way projection pilot, required runtime conformance suites, and
shared semantic fixtures. Whether TypeSpec reduces maintenance cost and agent
prompt size remains a post-MVP measurement, not a prerequisite for correctness.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
