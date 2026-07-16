```yaml
feature: chat-tui
revision: b43eba2
verdicts:
  - ac: chat-tui#ac:chat-starts-only-when-usable
    verdict: pass
    justification: "runChat applies both gates in runSpaceUI's exact order (IsTerminal -> Store.Load -> NewSpacesReader -> RunChat); commands.Chat(env) is registered in main.go with a non-nil RunChat closure, so a session is reachable; mutating away either gate fails a distinct test."
  - ac: chat-tui#ac:transcript-is-durable-terminal-text
    verdict: pass
    justification: "All three REQs hold at HEAD and are mutation-tested: alt screen genuinely absent, commit ordered ahead of send on both paths via tea.Sequence (spinner nested inside, no race), keyboarded replies stay live and commit inert only when superseded, errors commit without tea.Quit."
  - ac: chat-tui#ac:interaction-is-unambiguous
    verdict: pass
    justification: "All three REQs hold against the revised text: up enters the block at its LAST row (row-0 mutation fails 12 assertions), down from the last row exits, up at row 0 stays; the pending lock carves out ctrl+c (precedent copied verbatim fails TestCtrlCAlwaysQuits); the indicator is a real spinner above the input (a frozen line fails the tick-advance assertion)."
```

# Verify Report — chat-tui @ b43eba2

Per-AC verdicts for [chat-tui](../README.md), produced by `specstudio:verify`.

**Tally:** 3 passed, 0 failed, 0 unmapped, 0 errored.

Supersedes [08963f6.md](08963f6.md). That report passed all three ACs but predates the focus-direction inversion, the new `REQ: pending-is-visible`, the placeholder fix, and the restyle — it describes a key model that has since been reversed.

## AC: chat-starts-only-when-usable

**Verdict:** pass

**Justification:** `runChat` applies `IsTerminal` → `Store.Load()` → `NewSpacesReader(cfg)`, each returning before any terminal program exists, matching `runSpaceUI` line for line. The caveat the previous report tracked is closed: `commands.Chat(env)` is registered and `RunChat` is assigned, so a session is genuinely reachable — confirmed by `go run ./cmd/sneat --help` listing `chat`, and end-to-end by the non-TTY refusal.

Neither gate is decorative: deleting the `IsTerminal` gate fails `TestChat_NonTerminalErrors`; swallowing the store error fails `TestChat_SessionStoreErrorAborts`.

**Commits:** 5a4aa43

**Evidence:**
- `cmd/sneat/commands/chat.go:22-36` — the three gates, each `return err`, then `env.RunChat(spaces, sess.UID)`
- `cmd/sneat/commands/spaces.go:107-132` — `runSpaceUI`, the mirrored precedent, confirmed line-by-line
- `cmd/sneat/main.go:74-92` — `commands.Chat(env)` registered; `RunChat` → `chattui.Run(chat.NewProcessor(spaces, uid))`
- `cmd/sneat/commands/chat_test.go:41-87` — three refusal branches, each asserting `RunChat` uncalled
- `go run ./cmd/sneat chat` with no TTY → `sneat: the interactive chat requires a terminal`, exit 1
- Two mutations applied and reverted, each failing its named test

## AC: transcript-is-durable-terminal-text

**Verdict:** pass

**Justification:** The main risk to this AC was the typing indicator added since the last report. `REQ: pending-is-visible` needed a spinner tick on submit and press, and batching it around the commit would have reintroduced exactly the race Task 3's `tea.Sequence` was chosen to avoid — a fast processor landing its reply in scrollback ahead of the line that prompted it.

It did not. Both paths are `tea.Sequence(commit(blocks), tea.Batch(m.spin.Tick, send/press))`: the tick is nested **inside** the sequence's second element, after the commit, not batched around it. Bubble Tea dispatches the first command's message before invoking the next, and a tick writes no scrollback, so it cannot interleave. Swapping either path's `Sequence` for `Batch` fails its dedicated ordering test.

The restyle did not break the commit rule either: `replyStyle`'s border is composed inside `renderReply`, which both live and committed paths share, and `renderCommittedReply` takes no focus argument at all — so no caller can carry a focus mark into scrollback.

Five mutations applied, five caught: alt-screen added; submit `Sequence`→`Batch`; press `Sequence`→`Batch`; a keyboarded reply committing immediately; `fail()` returning `tea.Quit`.

**Commits:** 0a5f777, ebb3909, 72e567e, 74306a7, 6e02e47

**Evidence:**
- `internal/chattui/run.go` — `newProgram`, no `tea.WithAltScreen()`
- `internal/chattui/chattui.go:365,490` — `tea.Sequence(commit, tea.Batch(spin.Tick, …))` on press and submit
- `internal/chattui/chattui.go:501-535` — `receive` live-slot rule; `fail` commits without `tea.Quit`
- `internal/chattui/chattui.go:670` — `renderCommittedReply` takes no focus argument
- `internal/chattui/chattui_test.go:121-134,344` — reflection helpers, each with a control test; `TestSequenceIsFoundThroughABatch` fails in both directions
- Five mutations applied and reverted; `go test ./internal/chattui/... -count=1` passes

## AC: interaction-is-unambiguous

**Verdict:** pass

**Justification:** This AC changed most, and the code matches the requirement as it now reads.

**Direction.** `handleInputKey`'s `keyUp` sets `m.row = len(rows)-1`, entering at the last row. `down` is absent from the input's table entirely, so it cannot enter the block. Mutating entry to row 0 fails 12 assertions across `TestFocusMovement`, `TestUpEntersTheButtonBlockAtTheLastRow`, and `TestEnterOnAFocusedButtonPressesIt` (which then presses the wrong button).

**The `ctrl+c` carve-out is real.** `handleKey` matches `ctrl+c` *ahead* of the pending guard. Copying `internal/tui/confirm_screen.go`'s precedent verbatim — the lock first — fails `TestCtrlCAlwaysQuits/while_a_reply_is_in_flight`. The REQ's warning about that precedent is accurate: it does ignore every key, `ctrl+c` included.

**The animation is real, not a word.** `TestPendingIsVisible` advances the tick and asserts the frame *changed*; a static indicator fails it. It also asserts absence when idle, position above the input, and clearing on both resolutions — replies *and* error.

**Exactly one focus.** `syncInputCursor` blurs the input when the block holds focus, asserted by `TestInputCursorFollowsFocus`.

This verification surfaced one genuine gap, since closed in `b43eba2`: **no test pinned that `down` from the input must not enter the block**. Every other focus test walks in with `up`, so a `down` that also entered would have gone unnoticed — which is the exact shape of the bug this Feature had before the inversion. `TestDownFromTheInputDoesNotEnterTheButtonBlock` now covers it and dies to that mutation. The same pass found four stale comments left by the inversion, also fixed.

**Commits:** 72e567e, 74306a7, f5a4d61

**Evidence:**
- `internal/chattui/chattui.go:237-264` — `handleInputKey` `keyUp` enters at `len(rows)-1`; `down` absent from the input table
- `internal/chattui/chattui.go:211-233` — `ctrl+c` matched ahead of the `m.pending` guard
- `internal/tui/confirm_screen.go:34-37` — the precedent that ignores every key; the divergence is deliberate
- `internal/chattui/chattui.go:167-175,580-595` — tick advances only while pending; `View()` renders spinner + label above the input; `receive`/`fail` both clear `pending`
- `internal/chattui/chattui_test.go:897-935` — `TestPendingIsVisible`, tick-advance assertion
- `internal/chattui/chattui_test.go:1163-1322` — `TestFocusMovement` (13 cases) + `TestUpEntersTheButtonBlockAtTheLastRow`
- `internal/chattui/chattui_test.go:1370-1385,1586-1618` — one-focus-indicator; pending lock over 8 keys
- `TestDownFromTheInputDoesNotEnterTheButtonBlock` (b43eba2) — closes the gap this verification found

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/index-specification*
