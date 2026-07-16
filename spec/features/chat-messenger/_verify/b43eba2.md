```yaml
feature: chat-messenger
revision: b43eba2
verdicts:
  - ac: chat-messenger#ac:processing-is-swappable
    verdict: pass
    justification: "All four REQs hold at HEAD: chat exports only Processor/Reply/SpacesReader/NewProcessor(->interface); the concrete processor is unexported, so chattui cannot name it (compiler-enforced, probe confirmed); failures return as bare errors; keyboards are botkb.Keyboard; callback data is URL-shaped with a real round-trip guard."
  - ac: chat-messenger#ac:conversation-input-is-handled-honestly
    verdict: pass
    justification: "Both revised rules match the REQ at HEAD — label falls back to \"Family (fam1)\" then bare ID; order is custom-by-label, private, family last, ID tiebreak. All four skeptical claims survived mutation: removed sort, swapped ranks, dropped tiebreak, and a transitive sandbox route each fail the suite."
```

# Verify Report — chat-messenger @ b43eba2

Per-AC verdicts for [chat-messenger](../README.md), produced by `specstudio:verify`.

**Tally:** 2 passed, 0 failed, 0 unmapped, 0 errored.

Supersedes [c0d832b.md](c0d832b.md), which passed both ACs but predates two revisions to `REQ: spaces-command` — the label fallback and the ordering rule — and so describes rules that no longer apply.

## AC: processing-is-swappable

**Verdict:** pass

**Justification:** The seam is compiler-enforced rather than conventional, and that was probed rather than assumed: `go doc -all ./internal/chat` exports exactly `Processor`, `NewProcessor(...) Processor`, `Reply`, and `SpacesReader` — there is no exported concrete implementation to name. Adding `var _ = chat.processor{}` to `internal/chattui` fails to compile with `name processor not exported by package chat`. `NewProcessor` is called from exactly one place outside tests: `cmd/sneat/main.go:78`.

`encodeCallbackData`'s construct-then-verify check was probed and discriminates: `"space:sub"` renders as `./space:sub?id=x1` and re-parses with the command mangled — rejected; `"//space"` re-parses with `Host="space"`, `Path=""` — rejected by the host guard. Ordinary commands (`space`, `sp ace`, `a#b`) round-trip cleanly, so the check is not vacuous.

The two revisions since the last report (`6e02e47` labels, `247aee4` ordering) touched only `spaceLabel`/`spaceRank`/`orderSpaces` in `processor.go` — neither `chat.go` nor `callback.go`. Callback data is still built through `encodeCallbackData`, and each row is still `[]botkb.Button{botkb.NewDataButton(...)}`, so both requirements survive.

**Commits:** 61f9552, 396e53f, 1af179f

**Evidence:**
- `internal/chat/chat.go` — `Processor`, `Reply{Text, botkb.Keyboard}`; one commit in its history
- `go doc -all ./internal/chat` — no exported concrete implementation
- Probe: `var _ = chat.processor{}` in `internal/chattui` → `name processor not exported` (reverted; tree clean)
- `cmd/sneat/main.go:78` — the sole `NewProcessor` call site
- `internal/chat/callback.go` — construct-then-verify, probed against both mangling shapes
- `go.mod:7` — `bots-go-core v0.2.4` a direct require
- `go vet` clean; `go test ./internal/chat/... ./internal/chattui/... -count=1` passes

## AC: conversation-input-is-handled-honestly

**Verdict:** pass

**Justification:** Both revised rules match the requirement as it now reads. `spaceLabel` is title → `capitalize(type) + " (id)"` → bare ID. `orderSpaces` ranks family 2, private 1, everything else 0, then sorts by label, then by ID.

Five mutations were applied and each was caught:

| Mutation | Caught by |
|---|---|
| sort removed | 3 ordering tests |
| family/private ranks swapped | `RankFamilyLast`, and 2 others deterministically |
| ID tiebreak dropped | `OrderIsTotal` (fails on pass 0–1 across 3 runs) |
| an innocent-named package importing `convoruntime` | the transitive subtest fails while the direct one passes |
| label reverted to the old bare-ID rule | 4 `SpaceButtonLabelFallsBackToID` subtests |

The tiebreak result is worth recording. The commit that introduced this rule admits an earlier version of that test was **vacuous** — it compared labels that were equal by construction, so it could not detect a swap however the buttons were ordered. The current version asserts on `button.Data`, which carries the ID the tiebreak actually sorts on, and now dies to the mutation. This report confirms that independently rather than on the commit's word.

The transitive sandbox guard is genuinely load-bearing: an innocent-named package reaching `convoruntime` **passes** the direct-import check and is **caught** only by the `go list -deps` one.

**Commits:** 396e53f, 1af179f, 8a1088b, 6e02e47, 247aee4

**Evidence:**
- `internal/chat/processor.go` — `spaceLabel`, `spaceRank`, `orderSpaces` (rank → label → `cmp.Compare(a, b)`)
- `internal/chat/processor_test.go` — `RankFamilyLast`, `OrderIsTotal`, `SpaceButtonLabelFallsBackToID`, `TestPackage_DoesNotReachTheSandboxRuntime`
- Five mutations applied and reverted; tree restored byte-identically (shasum verified)
- `go test ./internal/chat/...` passes

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/index-specification*
