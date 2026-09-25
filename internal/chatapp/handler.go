package chatapp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bots-go-framework/bots-go-core/botkb"
	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/clientctx"
	"github.com/strongo/aichat/ai/cloud"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	aidiag "github.com/strongo/aichat/ai/diag"
	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/chatshell"
	"github.com/strongo/aichat/tui/grid"
	"github.com/strongo/aichat/tui/theme"
	"github.com/strongo/aichat/tui/transcript"

	"github.com/sneat-co/sneat-cli/internal/aichat/controls"
	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	sneatrules "github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// handler implements chatshell.Handler, chatshell.MsgHandler,
// chatshell.StreamObserver and chatshell.SidebarObserver: it is the one
// place that turns the aichat pipeline's Output into chatshell calls.
//
// Concurrency (S1 coordinator ruling): every mutation of state (the live
// *session.State a chatshell key handler can read at any moment) happens on
// the UI loop -- inside Submit itself (called synchronously from
// chatshell's Update), OnSidebarChange/OnStreamEvent (also called
// synchronously from Update), or OnMsg (dispatched from Update). A
// background tea.Cmd closure -- Submit's own, and OnStreamEvent's
// EventCompleted branch, whose eventual HandleAction call used to run
// straight against the live h.state/h.ctx from a worker goroutine, a real
// bug -- runs pipeline.Turn/HandleAction on a COPY of state and a per-turn
// cancellable ctx captured on the UI loop BEFORE the closure is built, never
// the live pointer/h.ctx, and hands the resulting copy back in a message
// tagged with the turn's seq (h.turnSeq); OnMsg applies only that message's
// turn-owned fields (applyStateDelta) onto live state, and only when seq is
// still current -- a stale result (a newer turn already started) is dropped
// outright. See Submit, applyStateDelta, handleTurn, and OnStreamEvent's own
// doc comments.
type handler struct {
	ctx         context.Context
	pipeline    pipeline.Pipeline
	processor   chat.Processor
	state       *session.State
	spaceID     string
	logger      *slog.Logger
	reporter    interactionReporter
	turnReports map[int64]*turnReport

	model *chatshell.Model

	streamSeq int64
	// turnSeq is S1's monotonically increasing turn counter (coordinator
	// ruling S1): Submit assigns each turn the next value; every background
	// result that turn eventually produces (turnMsg, llmRequestReadyMsg,
	// actionMsg) carries it, and the UI loop drops (no render, no state
	// write, no SetBusy(false)) any result whose seq no longer matches --
	// meaning a newer turn already started (e.g. Esc cancelled this one's
	// background work and the user typed a new one before it returned).
	// Written only from the UI loop (Submit); read with atomic.LoadInt64
	// from both the UI loop and a background tea.Cmd closure comparing its
	// captured seq.
	turnSeq int64
	// splitters maps an in-flight stream's id to its Splitter, so
	// OnStreamEvent's EventCompleted can retrieve the parsed <sneat-action>
	// once the stream that produced it is known to have finished.
	splitters map[string]*pipeline.Splitter
	// streamState/streamCtx/streamTurnSeq are set once per stream (in
	// startLLMStream, before any event can fire) so OnStreamEvent's
	// EventCompleted branch -- whose own returned tea.Cmd runs HandleAction
	// on a worker goroutine -- operates on a private state snapshot and a
	// per-turn cancellable ctx (S1: "never the live h.state/h.ctx") rather
	// than the handler's own live fields, and can tag its result with the
	// turn it belongs to.
	streamState   map[string]*session.State
	streamCtx     map[string]context.Context
	streamTurnSeq map[string]int64
	// streamText/streamUser accumulate an in-flight stream's visible text and
	// the user turn that started it, so OnStreamDone can record the exchange
	// into history once the stream finishes (brief §7/S7 coordinator ruling:
	// "last N (8) turns of history" -- see pipeline.HistoryTurns).
	streamText map[string]*strings.Builder
	streamUser map[string]string
	// streamStart/streamUsage/streamModel/streamProvider record what
	// OnStreamDone needs for its S11 diagnostics line (LLM latency, token/
	// cache/allowance usage, provider/model) -- an ai.Event carries these only
	// on its own EventStarted/EventCompleted, not on OnStreamDone(id, err), so
	// they are captured as they arrive and looked up by id at the end.
	streamStart    map[string]time.Time
	streamUsage    map[string]*ai.Usage
	streamModel    map[string]string
	streamProvider map[string]string
	// streamDecided records whether the turn that started stream id had a
	// decision.Decision at all (S11: distinguishes PathDecision from
	// PathLLMFallback at OnStreamDone).
	streamDecided map[string]bool
	// history is the bounded exchange log StreamRequest sends ahead of the
	// current turn. Pipeline itself is a stateless value (rebuilt per call),
	// so this is where the session's history actually lives.
	history []ai.Message
	// pendingAction maps a stream id to its action phase's own cancel func,
	// for exactly as long as OnStreamEvent's EventCompleted background work
	// (Splitter.Finish + HandleAction) has been dispatched but its result
	// has not yet reached OnMsg (fix round r5 review, M2). chatshell's own
	// handleStreamDone unconditionally clears busy (and streamCancel) the
	// instant a stream's DoneMsg arrives -- which fires right after
	// EventCompleted, typically before the action work has even started,
	// let alone finished a network call -- so OnStreamDone must check this
	// map and RE-ASSERT busy (with THIS action's own cancel, not the
	// finished stream's) whenever an entry is still present. OnMsg's
	// actionMsg case deletes the entry once the result lands, whether
	// current or stale.
	pendingAction map[string]context.CancelFunc
}

// appendHistory records one user/assistant exchange and trims history to
// pipeline.HistoryTurns exchanges, oldest first. An empty assistant text
// (e.g. a deterministic turn that only rendered a structured control) still
// records the user's side, so a later pronoun/continuation still has it in
// view even though there was no prose reply.
// cmdOrWork combines a background work command with the SetBusy(true)
// command chatshell's own SetBusy returns, shared by Submit/OnStreamEvent/
// beginLLMStream: real chatshell.Model.SetBusy(true) always returns a
// non-nil spinner-tick command today, but this stays correct if a future
// chatshell version (or a product-supplied Model) ever returns nil for it.
func cmdOrWork(work, busyCmd tea.Cmd) tea.Cmd {
	if busyCmd == nil {
		return work
	}
	return tea.Batch(work, busyCmd)
}

func (h *handler) appendHistory(user, assistant string) {
	if user == "" {
		return
	}
	h.history = append(h.history, ai.Message{Role: ai.RoleUser, Text: user})
	if assistant != "" {
		h.history = append(h.history, ai.Message{Role: ai.RoleAssistant, Text: assistant})
	}
	max := pipeline.HistoryTurns * 2
	if len(h.history) > max {
		h.history = h.history[len(h.history)-max:]
	}
}

// --- chatshell.TopBarProvider / HintsProvider -------------------------------
//
// Shared-look cutover (strongo/aichat#chat-shared-look, REQ:
// chatshell-product-bars): both methods return CONTENT ONLY -- title/
// context/menu items, and a hint/segment list -- never a colour, padding or
// border. chatshell renders them through the shared theme.TopBar/
// theme.RenderHints chrome on every render.

// topBar is chatshell.TopBarProvider.
func (h *handler) topBar(int) (title, context string, items []theme.MenuItem) {
	space := h.spaceID
	if space == "" {
		space = "no space selected"
	} else {
		space = "space: " + space
	}
	return "Sneat", space, nil
}

// hints is chatshell.HintsProvider. While busy (a live stream or a bare
// SetBusy(true) phase), only the spinner note and quit hint apply -- the
// composer/chip/focus hints below are all disabled while busy anyway (see
// chatshell's own REQ: chatshell-composer-chips).
func (h *handler) hints(int) (hints []theme.Hint, segments []string) {
	if h.model.Busy() {
		return []theme.Hint{{Key: "Ctrl+C", Label: "quit"}}, []string{"Thinking…"}
	}
	return []theme.Hint{
		{Key: "Enter", Label: "send"},
		{Key: "Shift+↑↓", Label: "navigate"},
		{Key: "F6/Shift+→", Label: "pinned"},
		{Key: "Esc", Label: "cancel/clear"},
		{Key: "Ctrl+C", Label: "quit"},
	}, nil
}

// --- chatshell.Handler -----------------------------------------------------

// Submit runs on the UI loop (chatshell's handleInputKey calls it
// synchronously from Update), so it is where FOCUS is read (coordinator
// ruling: FocusedRef()/SelectionRefs() at Submit time, on the UI loop) and
// where the state snapshot handed to the background pipeline call is taken.
// The returned tea.Cmd's closure runs on a worker goroutine and must not
// touch h.state -- it works only on the snapshot, and its result message is
// applied back to h.state by OnMsg, which does run on the UI loop.
func (h *handler) Submit(text string) tea.Cmd {
	trimmed := strings.TrimSpace(text)
	interactionID, _ := clientctx.NewUUID()

	// Sync the pipeline's space to whatever the slash-command Processor's
	// active space now is (coordinator ruling SPACE: /space is the single
	// source of truth once the Processor itself changes it -- see
	// chat.Processor.ActiveSpace).
	h.applySpaceChange(h.processor.ActiveSpace())

	if strings.HasPrefix(trimmed, "/") {
		return func() tea.Msg {
			replies, err := h.processor.SendText(h.ctx, trimmed)
			return slashMsg{replies: replies, err: err, text: trimmed, interactionID: interactionID}
		}
	}

	// S1 coordinator ruling: this turn's own seq, assigned on the UI loop
	// before any background work starts. handleTurn (and, if this turn
	// proceeds into an LLM stream, startLLMStream/OnStreamEvent's
	// EventCompleted branch) carry it forward on every message this turn
	// produces, and drop a result whose seq no longer matches h.turnSeq --
	// see the field's own doc comment for why that can happen even though
	// chatshell's composer is disabled while busy (Esc cancels the busy
	// phase without necessarily killing the background goroutine).
	seq := atomic.AddInt64(&h.turnSeq, 1)
	if h.turnReports == nil {
		h.turnReports = make(map[int64]*turnReport)
	}
	h.turnReports[seq] = &turnReport{id: interactionID, text: trimmed}

	// Snapshot state ON THE UI LOOP: a shallow copy isolates the background
	// call's Pending/Previous/LastShown/Selection/Focused reassignments
	// (pipeline never mutates a slice/struct in place, only reassigns a
	// State field) from the live h.state until OnMsg applies the result
	// back. Sidebar is deliberately excluded from what gets applied back
	// (applyStateDelta) -- see its own doc comment -- so a pin added WHILE
	// this turn's background work is still running is never lost.
	snapshot := *h.state
	if ref := h.model.FocusedRef(); ref != nil {
		snapshot.Focus(ref)
	}
	if sel := h.model.SelectionRefs(); len(sel) > 0 {
		snapshot.Selection = sel
	}

	ctx, cancel := context.WithCancel(h.ctx)
	ctx = cloud.WithInteractionID(ctx, interactionID)
	busyCmd := h.model.SetBusy(true)
	h.model.SetBusyCancel(cancel)

	// m2 (fix round r4 review): capture spaceID and pl HERE, on the UI loop,
	// same as ctx/snapshot/seq above -- work's closure must never read
	// h.spaceID/h.pipeline live, or a /space switch (applySpaceChange,
	// synchronous on the UI loop) that lands AFTER Submit returns but
	// BEFORE this closure actually runs on its worker goroutine would run
	// this turn against a DIFFERENT space than the one active when the user
	// submitted it -- the same class of race S1's ctx/snapshot capture
	// already guards against, just for these two fields.
	spaceID := h.spaceID
	pl := h.pipeline
	work := func() tea.Msg {
		out, err := pl.Turn(ctx, trimmed, &snapshot, spaceID)
		return turnMsg{text: trimmed, output: out, err: err, state: snapshot, seq: seq}
	}
	return cmdOrWork(work, busyCmd)
}

// applyStateDelta copies the turn-owned fields of from onto the live
// h.state: Focused/Selection/LastShown/Pending/Previous/PreviousAt (S1
// coordinator ruling: "field-level deltas ... applied on the UI loop").
// Sidebar is deliberately NOT among them -- it is owned by OnSidebarChange,
// which writes it directly on the UI loop the moment a pin/unpin happens,
// and a turn's snapshot (taken before its OWN background work started) must
// never overwrite a pin the user added while that work was still running.
// Always call this on the UI loop, and only once a seq check has confirmed
// from is not stale.
func (h *handler) applyStateDelta(from session.State) {
	h.state.Focused = from.Focused
	h.state.Selection = from.Selection
	h.state.LastShown = from.LastShown
	h.state.Pending = from.Pending
	h.state.Previous = from.Previous
	h.state.PreviousAt = from.PreviousAt
}

// applyUndoDelta applies only Previous/PreviousAt from a STALE actionMsg
// result whose action nonetheless actually ran (fix round r5 review): a
// real HandleAction call already happened -- possibly a destructive
// calendar/todo mutation -- by the time this message lands, even if a
// newer turn has since started. That is not something a "stale, drop it"
// rule may ever silently discard, or "undo" stops working for a turn that
// genuinely executed. Focused/Selection/LastShown/Pending are deliberately
// NOT applied here (unlike applyStateDelta) since a newer, current turn may
// already hold its own fresher values for those.
func (h *handler) applyUndoDelta(from session.State) {
	h.state.Previous = from.Previous
	h.state.PreviousAt = from.PreviousAt
}

// applySpaceChange is B3's session-side half (coordinator ruling B3): when
// the Processor's active space differs from h.spaceID -- i.e. the user
// pressed a space button or ran /space -- every piece of working context
// that names an entity is invalidated, because Focused/Selection/Pending/
// Previous/LastShown may point at an entity from the OLD space, and a bare
// pronoun ("it", "that") or a bare "undo" must never silently resolve
// against it under the new one.
//
// Sidebar pins are the one exception: they are left in place rather than
// dropped, so switching back to a space doesn't lose what was pinned there,
// and so the sidebar keeps showing them (coordinator: "keep pins but mark
// them other-space and never act on them"). "Never act on them" is enforced
// on the acting side, not by deleting them here: Resolver/SneatExecutor
// already refuse a resolved target whose EntityRef.Keys["spaceID"] doesn't
// match the space a turn is running against (verified against the
// reviewer's own B3 probe tests, adapted into
// internal/aichat/pipeline/pipeline_test.go's
// TestHandleAction_AddTodo_ViaReference_IgnoresModelSuppliedSpaceID and
// TestHandleAction_Pronoun_RejectsFocusedEntityFromAnotherSpace, both of
// which pass), so a pin
// from another space can be a Candidates() fallback without ever being
// actable; explicitly excluding cross-space pins from the sidebar's own
// display is a UI nicety (sidebarRender/rendering), not a safety
// requirement, and is left out of this MVP round.
//
// active == "" means the Processor has no opinion yet (e.g. before the
// first ListSpaces resolves) -- never treated as a change.
func (h *handler) applySpaceChange(active string) {
	if active == "" || active == h.spaceID {
		return
	}
	h.spaceID = active
	h.state.Focused = nil
	h.state.Selection = nil
	h.state.LastShown = nil
	h.state.Pending = nil
	h.state.Previous = nil
	h.state.PreviousAt = time.Time{}
}

// --- chatshell.SidebarObserver ---------------------------------------------

// OnSidebarChange fires from chatshell's Update (UI loop), so writing
// straight to h.state is safe here.
func (h *handler) OnSidebarChange(refs []session.EntityRef) {
	h.state.Sidebar = refs
}

// --- chatshell.StreamObserver ------------------------------------------------

func (h *handler) OnStreamEvent(id string, ev ai.Event) tea.Cmd {
	// S11: capture provider/model as soon as the stream announces them, and
	// usage the moment it is known (EventCompleted) -- OnStreamDone(id, err)
	// itself carries neither, only the terminal error, so this is the only
	// place they are ever observed.
	if ev.Provider != "" {
		h.streamProvider[id] = ev.Provider
	}
	if ev.Model != "" {
		h.streamModel[id] = ev.Model
	}
	if ev.Usage != nil {
		h.streamUsage[id] = ev.Usage
	}
	if ev.Type == ai.EventTextDelta {
		// Accumulate the visible (splitter-filtered) reply text for history
		// (S7): OnStreamDone records it as the assistant side of this turn's
		// exchange once the stream finishes.
		if b := h.streamText[id]; b != nil {
			b.WriteString(ev.Text)
		}
		return nil
	}
	if ev.Type != ai.EventCompleted {
		return nil
	}
	sp := h.splitters[id]
	if sp == nil {
		return nil
	}
	// S1 coordinator ruling: everything the closure below needs is captured
	// HERE, on the UI loop (OnStreamEvent always runs on it) -- the closure
	// itself runs on a worker goroutine and must never read h.state/h.ctx/
	// h.spaceID directly, only these local copies. snapshot is a value copy
	// (not the shared *session.State startLLMStream stored), so HandleAction
	// mutating it in place can never race with anything else touching that
	// stored pointer or the live h.state.
	turnSeq := h.streamTurnSeq[id]
	ctx := h.streamCtx[id]
	if ctx == nil {
		ctx = h.ctx // defensive: should always be set by startLLMStream's open
	}
	var snapshot session.State
	if sp2 := h.streamState[id]; sp2 != nil {
		snapshot = *sp2
	}
	spaceID := h.spaceID
	pl := h.pipeline // m2: captured here too, not read live inside work below
	logger := h.logger
	// M2 (fix round r4/r5 review): the stream itself has already finished by
	// the time this tea.Cmd runs (that is what EventCompleted means), and
	// chatshell's tui/stream driver delivers this id's DoneMsg right after
	// EventCompleted -- its own handleStreamDone unconditionally clears busy
	// (and streamCancel) the instant that DoneMsg is handled, typically
	// BEFORE the work below has even started, let alone finished
	// HandleAction's network I/O. SetBusy(true) here is necessary but not
	// sufficient by itself: OnStreamDone (below) checks h.pendingAction[id],
	// set here, and RE-ASSERTS busy+SetBusyCancel for as long as this
	// action's result hasn't landed yet -- see its own doc comment. Same
	// convention as Submit's own background work otherwise: derive a
	// cancellable ctx from the turn's own (so Esc during the action phase
	// still works, whichever of these two call sites re-armed it last), and
	// clear h.pendingAction[id] in OnMsg's actionMsg case once the result
	// lands (current or stale).
	actionCtx, cancel := context.WithCancel(ctx)
	if h.pendingAction == nil {
		h.pendingAction = map[string]context.CancelFunc{}
	}
	h.pendingAction[id] = cancel
	busyCmd := h.model.SetBusy(true)
	h.model.SetBusyCancel(cancel)
	work := func() tea.Msg {
		trailing, action, err := sp.Finish()
		if err != nil {
			// Splitter contract: a malformed/unterminated block is surfaced,
			// never silently dropped, and whatever text was already held
			// back is still shown (coordinator ruling SPLITTER).
			return actionMsg{parseErr: err, trailing: trailing, seq: turnSeq, id: id}
		}
		if action == nil {
			// A plain text-only answer: still an actionMsg (not a bare nil)
			// so OnMsg's stale-seq-checked actionMsg case runs and clears
			// busy -- a bare nil never reaches OnMsg at all (bubbletea drops
			// a nil Msg outright), which would leave busy stuck true.
			return actionMsg{seq: turnSeq, id: id}
		}
		// prevBefore (fix round r6 review): snapshot.Previous exactly as it
		// stood before HandleAction runs -- HandleAction mutates snapshot
		// (a pointer) in place, reassigning Previous to a NEW *session.Action
		// only when it actually executes something (pipeline.runAction).
		// Comparing pointers after the call is how executed below tells
		// "really executed" apart from "errored/cancelled/no-op, Previous
		// is still whatever it already was".
		prevBefore := snapshot.Previous
		// UNSUPPORTED-KIND ruling (founder bug, buy-add-prompt.md): HandleAction
		// answers an unsupported/aliased kind with plain Output text, not an
		// error (see its own doc comment), so the fact that the model emitted
		// one at all would otherwise leave no trace -- log the raw kind here,
		// the one place this session still has it, for diagnosing a model that
		// keeps guessing at kinds.
		if logger != nil && !pipeline.IsSupportedActionKind(action.Kind) {
			logger.WarnContext(actionCtx, "aichat.action.unsupported_kind", "kind", action.Kind)
		}
		out, err := pl.HandleAction(actionCtx, *action, &snapshot, spaceID)
		executed := err == nil && snapshot.Previous != prevBefore
		return actionMsg{output: out, err: err, state: snapshot, seq: turnSeq, id: id, kind: action.Kind, executed: executed}
	}
	return cmdOrWork(work, busyCmd)
}

// OnStreamDone fires exactly once per StartStream call (success, fatal
// error, or cancellation) -- this is where the splitter entry for id is
// retired and the LLM-leg diagnostics are logged, regardless of outcome.
//
// M2 (fix round r5 review): chatshell's own handleStreamDone (tui/chatshell)
// already ran by the time this method is called -- it set m.busy=false
// UNCONDITIONALLY for the current stream, before ever invoking this
// StreamObserver callback. If OnStreamEvent's EventCompleted branch
// dispatched this id's action work and it has not resolved yet
// (h.pendingAction[id] still set), that busy=false is wrong: the action
// (possibly a destructive calendar/todo mutation, still doing network I/O)
// is still running. Re-assert SetBusy(true) + SetBusyCancel(the ACTION's
// own cancel, not the finished stream's) right here, synchronously, before
// returning -- chatshell's Update dispatches messages one at a time, so
// this runs in the same UI-loop turn as handleStreamDone's own busy=false,
// leaving busy correctly true by the time Update returns.
func (h *handler) OnStreamDone(id string, err error) tea.Cmd {
	var busyCmd tea.Cmd
	_, actionPending := h.pendingAction[id]
	if cancel, ok := h.pendingAction[id]; ok {
		busyCmd = h.model.SetBusy(true)
		h.model.SetBusyCancel(cancel)
	}
	delete(h.splitters, id)
	if b := h.streamText[id]; b != nil && err == nil {
		h.appendHistory(h.streamUser[id], b.String())
	}
	delete(h.streamText, id)
	delete(h.streamUser, id)
	delete(h.streamState, id)
	delete(h.streamCtx, id)
	turnSeq := h.streamTurnSeq[id]
	delete(h.streamTurnSeq, id)
	if h.logger != nil {
		// S11: PathLLMFallback when nothing decided this turn at all (the
		// StreamRequest that started it used SelectAll); PathDecision when a
		// decision chose LLM handling (StreamRequest used Select) -- see
		// startLLMStream, which records which one via streamDecided.
		path := aidiag.PathLLMFallback
		if h.streamDecided[id] {
			path = aidiag.PathDecision
		}
		t := aidiag.Turn{Path: path, Provider: h.streamProvider[id], Model: h.streamModel[id], Usage: h.streamUsage[id]}
		if start, ok := h.streamStart[id]; ok {
			t.LLMLatency = time.Since(start)
		}
		if err != nil {
			t.Errors = []string{aidiag.ErrorCode(err)}
		}
		aidiag.Log(h.ctx, h.logger, t)
	}
	delete(h.streamStart, id)
	delete(h.streamUsage, id)
	delete(h.streamModel, id)
	delete(h.streamProvider, id)
	delete(h.streamDecided, id)
	if actionPending {
		return busyCmd
	}
	turn := h.turnReports[turnSeq]
	delete(h.turnReports, turnSeq)
	status, outcome := "completed", "answer_presented"
	if err != nil {
		status, outcome = "failed", "provider_failed"
		if errors.Is(err, context.Canceled) {
			status, outcome = "cancelled", "cancelled"
		}
	}
	return cmdOrWork(h.reportCommand(turn, status, outcome, "", ""), busyCmd)
}

// --- chatshell.MsgHandler ----------------------------------------------------

// OnMsg runs on the UI loop (chatshell's dispatchUnhandled calls it from
// Update): every write to h.state in this file happens from here or from
// Submit itself (before the async work is dispatched), never from inside a
// tea.Cmd closure.
func (h *handler) OnMsg(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case slashMsg:
		return h.handleSlash(m)
	case turnMsg:
		return h.handleTurn(m)
	case llmRequestReadyMsg:
		if atomic.LoadInt64(&h.turnSeq) != m.seq {
			// S1: a newer turn already started (superseding an Esc-cancelled
			// one) before this "building the request" phase finished --
			// drop it, and leave busy alone: the newer turn owns it.
			turn := h.turnReports[m.seq]
			delete(h.turnReports, m.seq)
			return h.reportCommand(turn, "cancelled", "superseded", "", "")
		}
		h.model.SetBusy(false) // clears the "building the request" phase; StartStream sets busy again
		return h.startLLMStream(m)
	case actionMsg:
		turn := h.turnReports[m.seq]
		delete(h.turnReports, m.seq)
		// M2 (fix round r5 review): this stream's action has now resolved
		// (current or stale) -- OnStreamDone must stop re-asserting busy for
		// it.
		delete(h.pendingAction, m.id)
		if atomic.LoadInt64(&h.turnSeq) != m.seq {
			// S1/M2 (fix round r5, corrected in r6 review): a stale result --
			// the turn it belongs to is no longer current -- must still
			// apply/render when the action actually EXECUTED (m.executed,
			// not merely m.parseErr == nil): a real mutation already
			// happened and must never be silently dropped, or "undo" stops
			// working for a turn that genuinely ran. m.parseErr == nil alone
			// is NOT sufficient -- r5's regression: a stale plain text-only
			// answer (action == nil, m.state is the session.State zero
			// value) would set Previous=nil, WIPING the current turn's real
			// undo; a stale action that reached HandleAction but errored or
			// was cancelled (m.err != nil) carries m.state.Previous exactly
			// as it stood BEFORE that call -- an unrelated, possibly older
			// value that would overwrite a NEWER Previous a current turn
			// already recorded. m.executed (set in OnStreamEvent's work
			// closure by comparing state.Previous's pointer before/after the
			// HandleAction call) is false in both cases, so this correctly
			// drops them. When true, apply only Previous/undo
			// (applyUndoDelta), never Focused/Selection/LastShown/Pending (a
			// newer, current turn may already hold its own fresher values
			// there) or busy (the newer turn already owns it).
			if m.executed {
				h.applyUndoDelta(m.state)
				h.render(m.output, m.err)
			}
			if m.executed {
				return h.reportCommand(turn, "completed", "action_succeeded", m.kind, "succeeded")
			}
			return h.reportCommand(turn, "cancelled", "superseded", m.kind, "")
		}
		// M2 (fix round r4 review): the action phase (OnStreamEvent's
		// EventCompleted branch, re-armed if needed by OnStreamDone) owns
		// busy for its own duration -- clear it here, the same way
		// handleTurn/llmRequestReadyMsg clear the phase THEY own, now that
		// this result is confirmed current (not stale).
		h.model.SetBusy(false)
		if m.parseErr != nil {
			if m.trailing != "" {
				h.model.AppendAssistant(m.trailing)
			}
			// S6: a block that was not trailing (non-whitespace prose
			// followed it) gets its own note, distinct from a genuinely
			// malformed/unterminated one -- the model's answer still
			// rendered fine, it just did not act.
			note := "(couldn't parse action)"
			if errors.Is(m.parseErr, pipeline.ErrActionNotTrailing) {
				note = "(no action taken -- more text followed the action block)"
			}
			h.model.AppendSystem(note)
			if h.logger != nil {
				aidiag.Log(h.ctx, h.logger, aidiag.Turn{Path: aidiag.PathLLMFallback, Errors: []string{aidiag.ErrorCode(m.parseErr)}})
			}
			return h.reportCommand(turn, "completed", "action_not_executed", m.kind, "")
		}
		// S1: apply only the turn-owned fields (see applyStateDelta) so a
		// Sidebar pin added while HandleAction was running in the
		// background is never overwritten by this stale-relative-to-Sidebar
		// (but current-relative-to-turnSeq) snapshot.
		h.applyStateDelta(m.state)
		h.render(m.output, m.err)
		status, outcome, actionStatus := "completed", "answer_presented", ""
		if m.err != nil {
			status, outcome, actionStatus = "failed", "action_failed", "failed"
			if errors.Is(m.err, context.Canceled) {
				status, outcome, actionStatus = "cancelled", "cancelled", "cancelled"
			}
		}
		if m.executed {
			outcome, actionStatus = "action_succeeded", "succeeded"
		}
		return h.reportCommand(turn, status, outcome, m.kind, actionStatus)
	case grid.RowActivatedMsg:
		if m.Row.Ref != nil {
			h.state.Focus(m.Row.Ref)
		}
		return nil
	case controls.ItemActivatedMsg:
		ref := m.Ref
		h.state.Focus(&ref)
		return nil
	case buttonActivatedMsg:
		return h.pressButton(m.Button)
	}
	return nil
}

func (h *handler) handleSlash(m slashMsg) tea.Cmd {
	h.applySpaceChange(h.processor.ActiveSpace())
	status, outcome := "completed", "answer_presented"
	if m.err != nil {
		h.model.AppendSystem(h.chatSafeError("slash", m.err))
		status, outcome = "failed", "action_failed"
		if errors.Is(m.err, context.Canceled) {
			status, outcome = "cancelled", "cancelled"
		}
	} else {
		h.appendReplies(m.replies)
	}
	action, known := slashCommandAction(m.text, h.processor.Commands())
	actionStatus := status
	if !known {
		outcome, actionStatus = "unrecognized_command", ""
	} else if status == "completed" {
		outcome, actionStatus = "action_succeeded", "succeeded"
	}
	return h.reportCommand(&turnReport{id: m.interactionID, text: m.text}, status, outcome, action, actionStatus)
}

// appendReplies renders a []chat.Reply the same way for a typed slash
// command and a button press: text first, then a buttonsBlock (S10) when the
// reply carries a Keyboard. It always APPENDS -- Reply.Edit ("re-render the
// pressed message in place", e.g. a space card's own Keyboard replacing
// itself) has no chatshell equivalent this MVP round (no in-place transcript
// entry replacement API), a known limitation noted in the final report.
func (h *handler) appendReplies(replies []chat.Reply) {
	for _, r := range replies {
		if r.Text != "" {
			h.model.AppendAssistant(r.Text)
		}
		if r.Keyboard == nil {
			continue
		}
		if blk := newButtonsBlock(r.Keyboard); blk != nil {
			h.model.AppendBlock(blk)
		}
	}
}

// pressButton dispatches an activated button (S10): a data button goes
// through chat.Processor.PressButton exactly like the messenger surface
// (see internal/chat/processor.go's own PressButton doc); a text button
// resubmits its text through SendText, the same effect as the user typing
// it; a URL button cannot open a browser from inside the TUI, so it is
// echoed as a system line instead of silently doing nothing.
// pressButton dispatches a focused block's activated button. S1 coordinator
// ruling ("slash commands busy-gated"): chatshell's own composer is already
// disabled while busy, but a focused TRANSCRIPT block's Enter key is NOT --
// ZoneTranscript key events reach the block's own Update regardless of
// m.busy (see tui/chatshell.Model.handleKey) -- so a button press could
// otherwise fire chat.Processor.PressButton/SendText while a turn's
// background work is still touching shared state. Busy-gate it here too.
func (h *handler) pressButton(btn botkb.Button) tea.Cmd {
	if h.model.Busy() {
		return nil
	}
	switch b := btn.(type) {
	case *botkb.DataButton:
		return func() tea.Msg {
			replies, err := h.processor.PressButton(h.ctx, b.Data)
			return slashMsg{replies: replies, err: err}
		}
	case *botkb.TextButton:
		return func() tea.Msg {
			replies, err := h.processor.SendText(h.ctx, b.Text)
			return slashMsg{replies: replies, err: err}
		}
	case *botkb.UrlButton:
		h.model.AppendSystem("Open in browser: " + b.URL)
		return nil
	default:
		return nil
	}
}

func (h *handler) handleTurn(m turnMsg) tea.Cmd {
	if atomic.LoadInt64(&h.turnSeq) != m.seq {
		// S1: a newer turn already started before this one's background
		// pipeline.Turn call returned (Esc cancelled it and the user typed
		// again) -- drop it outright, and leave busy/render to the newer
		// turn.
		turn := h.turnReports[m.seq]
		delete(h.turnReports, m.seq)
		return h.reportCommand(turn, "cancelled", "superseded", "", "")
	}
	turn := h.turnReports[m.seq]
	if turn != nil {
		turn.output = m.output
	}
	previous := h.state.Previous
	h.model.SetBusy(false)
	// S1: apply only the turn-owned fields, never Sidebar (applyStateDelta's
	// own doc comment) -- a plain `*h.state = m.state` here would silently
	// undo a pin the user added while this turn's background work was still
	// running.
	h.applyStateDelta(m.state)
	if m.err != nil {
		h.model.AppendSystem(h.chatSafeError("turn", m.err))
		h.logTurn(pathFor(m.output), m.output, m.err)
		delete(h.turnReports, m.seq)
		if errors.Is(m.err, context.Canceled) {
			return h.reportCommand(turn, "cancelled", "cancelled", reportAction(m.output), "cancelled")
		}
		return h.reportCommand(turn, "failed", "action_failed", reportAction(m.output), "failed")
	}
	if !m.output.NeedsLLM {
		h.render(m.output, nil)
		h.logTurn(pathFor(m.output), m.output, nil)
		h.appendHistory(m.text, m.output.Text)
		delete(h.turnReports, m.seq)
		actionStatus, outcome := "", "answer_presented"
		if reportAction(m.output) != "" && (m.state.Previous != previous || m.output.Presentation != "") {
			actionStatus, outcome = "succeeded", "action_succeeded"
		}
		return h.reportCommand(turn, "completed", outcome, reportAction(m.output), actionStatus)
	}
	return h.beginLLMStream(m.text, m.output.Decision, m.seq)
}

// beginLLMStream is S2's fix: chatshell.Model.StartStream calls its `open`
// func SYNCHRONOUSLY, on the UI loop, to obtain the stream -- only the
// resulting iter.Seq2's own consumption is asynchronous (see
// tui/chatshell's StartStream: `seq := open(ctx); return
// tea.Batch(stream.Start(...), ...)`). The previous version built req
// (h.pipeline.StreamRequest, whose DynamicBlocks does real Firestore reads
// for calendar/todo/contacts context) INSIDE open, so every LLM turn
// blocked the UI loop on those reads before the stream even started -- a
// real freeze, not just a misleading comment. This version does that I/O in
// a background tea.Cmd FIRST; only once llmRequestReadyMsg arrives (back on
// the UI loop) does it call StartStream, whose own `open` now does no I/O
// at all (just Pipeline.Stream, which only asks the already-selected
// ai.LLMProvider to start streaming).
func (h *handler) beginLLMStream(text string, d *decision.Decision, seq int64) tea.Cmd {
	focused := focusedScopes(h.state)
	history := append([]ai.Message(nil), h.history...)
	stateSnapshot := *h.state
	ctx, cancel := context.WithCancel(h.ctx)
	// A bare SetBusy(true) phase (no stream yet) is exactly what chatshell's
	// own cancelBusy doc comment describes: Esc/Ctrl+C during this
	// "building the request" phase calls the registered SetBusyCancel func
	// and reports "(stopped)" itself, since there is no stream DoneMsg yet
	// to do it asynchronously.
	busyCmd := h.model.SetBusy(true)
	h.model.SetBusyCancel(cancel)
	// m2 (fix round r4 review): capture spaceID and pl here, on the UI loop,
	// same as ctx/stateSnapshot/seq above -- see Submit's own doc comment on
	// its matching capture for why a live h.spaceID/h.pipeline read inside a
	// background closure is a race against a concurrent /space switch.
	spaceID := h.spaceID
	pl := h.pipeline
	work := func() tea.Msg {
		req, report := pl.StreamRequest(ctx, text, &stateSnapshot, spaceID, d, focused, history)
		return llmRequestReadyMsg{ctx: ctx, text: text, decision: d, req: req, report: report, state: stateSnapshot, seq: seq}
	}
	return cmdOrWork(work, busyCmd)
}

// startLLMStream is beginLLMStream's second half, run once the request is
// built: it registers this stream's bookkeeping and calls the real
// chatshell.StartStream, whose `open` now only calls Pipeline.Stream (no
// I/O) -- safe to run synchronously on the UI loop.
func (h *handler) startLLMStream(m llmRequestReadyMsg) tea.Cmd {
	if m.ctx.Err() != nil {
		// The "building the request" phase was cancelled (Esc/Ctrl+C) before
		// it finished -- cancelBusy already reported "(stopped)"; starting a
		// stream now would be a new turn the user never asked to continue.
		turn := h.turnReports[m.seq]
		delete(h.turnReports, m.seq)
		return h.reportCommand(turn, "cancelled", "cancelled", "", "")
	}
	if turn := h.turnReports[m.seq]; turn != nil {
		m.req.InteractionID = turn.id
	}
	id := h.nextStreamID()
	if h.splitters == nil {
		h.splitters = map[string]*pipeline.Splitter{}
	}
	if h.streamText == nil {
		h.streamText = map[string]*strings.Builder{}
		h.streamUser = map[string]string{}
		h.streamStart = map[string]time.Time{}
		h.streamUsage = map[string]*ai.Usage{}
		h.streamModel = map[string]string{}
		h.streamProvider = map[string]string{}
		h.streamDecided = map[string]bool{}
		h.streamState = map[string]*session.State{}
		h.streamCtx = map[string]context.Context{}
		h.streamTurnSeq = map[string]int64{}
	}
	h.streamText[id] = &strings.Builder{}
	h.streamUser[id] = m.text
	h.streamStart[id] = time.Now()
	h.streamDecided[id] = m.decision != nil
	// S1: the snapshot and turn seq OnStreamEvent's EventCompleted branch
	// will later hand its background HandleAction closure -- stateSnapshot
	// takes its own copy so a second write into h.streamState[id] (there
	// won't be one; one entry per id) could never alias it.
	stateSnapshot := m.state
	h.streamState[id] = &stateSnapshot
	h.streamTurnSeq[id] = m.seq
	h.logStreamRequest(m.decision, m.report)
	open := func(ctx context.Context) iter.Seq2[ai.Event, error] {
		if turn := h.turnReports[m.seq]; turn != nil {
			turn.llmStarted = true
		}
		seq, splitter := h.pipeline.Stream(ctx, m.req)
		h.splitters[id] = splitter
		// The REAL per-turn cancellable ctx (S1: "Esc cancels via
		// SetBusyCancel" -- during an active stream, Esc/Ctrl+C actually
		// cancels via chatshell's own streamCancel, which is exactly this
		// ctx: see tui/chatshell.Model.StartStream/cancelStream). Stored so
		// OnStreamEvent's EventCompleted branch hands HandleAction this same
		// cancellable ctx instead of the session-lifetime h.ctx.
		h.streamCtx[id] = ctx
		return seq
	}
	// StartStreamMarkdown (not StartStream): the LLM's own streamed reply is
	// prose that may carry real markdown (lists, emphasis, code) and is the
	// one path this product renders through the shared tui/mdrender look
	// (chatapp.go's WithMarkdownRenderer) -- deterministic pipeline output
	// (h.render, appendReplies) stays plain AppendAssistant/AppendSystem,
	// unchanged, since it is already plain text chosen by this codebase, not
	// model prose.
	return h.model.StartStreamMarkdown(id, open)
}

func (h *handler) nextStreamID() string {
	return "turn-" + strconv.FormatInt(atomic.AddInt64(&h.streamSeq, 1), 10)
}

// chatSafeError turns a pipeline/reader/processor err into text safe to
// show a chat user, and separately logs the RAW error to the debug logger
// when one is configured -- ai/diag's own Turn.Errors deliberately never
// carries a raw error message (diag.ErrorCode's doc comment: "NEVER a raw
// error message ... which can carry arbitrary user content or
// provider-internal detail"), so without this the founder-reported
// PermissionDenied's full detail would be lost entirely once replaced by
// the friendly text below, rather than merely hidden from the transcript.
//
// PERMISSION-DENIED ruling (founder bug: "What's on today?" -> "system:
// error: rpc error: code = PermissionDenied desc = Missing or insufficient
// permissions."): a Firestore reader error surfacing its raw gRPC status
// text told the user nothing actionable and leaked transport detail. Now
// it renders as a plain, actionable message naming the space and the fix
// (/spaces, /space <id>); every other error's message passes through
// unchanged, same as before this fix.
func (h *handler) chatSafeError(source string, err error) string {
	if err == nil {
		return ""
	}
	if h.logger != nil {
		h.logger.ErrorContext(h.ctx, "aichat.error", "source", source, "error", err.Error())
	}
	if isPermissionDeniedErr(err) {
		return fmt.Sprintf("Couldn't read your data -- you may not have access to space %q. Try /spaces and /space <id>.", h.spaceID)
	}
	return "error: " + err.Error()
}

// isPermissionDeniedErr reports whether err is (or wraps, however deeply) a
// Firestore/gRPC PermissionDenied.
//
// PRIMARY check: status.FromError (coordinator ruling, PR #56 review round
// 2) uses errors.As internally (google.golang.org/grpc/status@v1.83.2's own
// FromError), so it sees through every fmt.Errorf("...: %w", err) layer
// this codebase's own reader/pipeline code adds, AND through
// dalgo2firestore's own wrapping (github.com/dal-go/dalgo2firestore's
// getter.go/inserter.go use github.com/pkg/errors.Wrapf, which implements
// Unwrap() since pkg/errors v0.9.1 -- verified in this module's vendored
// copy) -- so the real Firestore client's underlying *status.Status (or an
// apierror.APIError, which also implements GRPCStatus()) is reachable from
// here in production, not just from a directly-constructed status error.
//
// FALLBACK: a plain substring match on the standard "rpc error: code =
// PermissionDenied desc = ..." text, kept for defense in depth -- a test
// double or a future error path that stringifies before this point (losing
// the GRPCStatus() interface) still gets the friendly message instead of
// silently falling through to the raw-error branch.
func isPermissionDeniedErr(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
		return true
	}
	return strings.Contains(err.Error(), "PermissionDenied")
}

// render turns a deterministic/action Output into chatshell calls: a
// structured control (when Presentation+Entities are set) and/or plain text.
func (h *handler) render(out pipeline.Output, err error) {
	if err != nil {
		h.model.AppendSystem(h.chatSafeError("action", err))
		return
	}
	if len(out.Entities) > 0 {
		h.model.AppendBlock(blockFor(out))
	}
	if out.Text != "" {
		h.model.AppendAssistant(out.Text)
	}
}

// blockFor builds the transcript.Block for a pipeline.Output. ContactsGrid
// reuses tui/grid directly; a calendar/todo presentation with rows (S9
// follow-up: pipeline.Output.HappeningRows/TodoRows carry real Start/End/
// Recurring/Done, which a bare session.EntityRef cannot) renders through the
// matching controls constructor (NewDayCalendar/NewWeekCalendar/
// NewHappeningsList/NewTodoList/NewBuyList) instead of a generic,
// time-less ListBlock -- that generic fallback still covers presentations
// that only ever carry Entities (an ambiguous-reference choice list, a
// contacts.find_contact grid).
func blockFor(out pipeline.Output) transcript.Block {
	presentation, entities := out.Presentation, out.Entities
	// A single contact renders as a card (brief §3/§18 scenario 7: "single
	// contact -> contact card"), a real search result as the grid -- the
	// same distinction a card/grid pair always makes.
	if presentation == sneatdomain.PresentationContactsGrid && len(entities) == 1 {
		return cardFor(entities[0], out.ContactRows)
	}
	if presentation == sneatdomain.PresentationContactsGrid {
		contacts := make([]controls.Contact, 0, len(entities))
		for _, e := range entities {
			contacts = append(contacts, controls.Contact{Name: e.Title, Ref: e})
		}
		return controls.NewContactsGrid("Contacts", contacts)
	}
	// S5 coordinator ruling: a single happening -- a find_happening result or
	// a reschedule/cancel confirmation (pipeline.resolveAndAct's confirmRow)
	// -- renders as a card showing its real (resolved) time, not a one-row
	// list or a bare title.
	if presentation == sneatdomain.PresentationHappeningCard && len(out.HappeningRows) == 1 {
		return controls.NewHappeningCard(out.HappeningRows[0])
	}
	if len(out.HappeningRows) > 0 {
		switch presentation {
		case sneatdomain.PresentationDayCalendar:
			return controls.NewDayCalendar(headingFor(presentation), out.HappeningRows)
		case sneatdomain.PresentationWeekCalendar:
			return controls.NewWeekCalendar(headingFor(presentation), out.WeekStart, out.HappeningRows)
		case sneatdomain.PresentationHappeningsList:
			return controls.NewHappeningsList(headingFor(presentation), out.HappeningRows)
		}
	}
	if len(out.TodoRows) > 0 {
		switch presentation {
		case sneatdomain.PresentationTodoList:
			return controls.NewTodoList(headingFor(presentation), out.TodoRows)
		case sneatdomain.PresentationBuyList:
			return controls.NewBuyList(headingFor(presentation), out.TodoRows)
		}
	}
	items := make([]controls.Item, 0, len(entities))
	for _, e := range entities {
		items = append(items, controls.Item{Title: e.Title, Ref: e})
	}
	return controls.NewListBlock(headingFor(presentation), items)
}

// cardFor builds the single-contact card for a ContactsGrid presentation
// narrowed to exactly one match (blockFor's only caller). S8/S9: it shows
// only human-readable fields -- no raw entity keys (controls.NewContactCard's
// own doc comment). m11: rows is out.ContactRows, the producer's optional
// per-ref enrichment (relationship/DoB/emails/phones); when it doesn't cover
// ref (nil, or a producer that couldn't build it), the card falls back to
// name-only, same as before m11. A happening's single-result/confirmation
// card is a separate path (blockFor's PresentationHappeningCard case,
// controls.NewHappeningCard) since it needs the richer HappeningRow (Start/
// End/Recurring), which a bare session.EntityRef never carries -- there is
// no longer a generic raw-key fallback card for other entity kinds (S5
// coordinator ruling: it leaked internal keys like happeningID/contactID
// straight into the transcript).
func cardFor(ref session.EntityRef, rows []controls.ContactRow) *controls.CardBlock {
	for _, row := range rows {
		if row.Ref.Same(ref) {
			return controls.NewContactCard(row)
		}
	}
	return controls.NewContactCard(controls.ContactRow{Ref: ref, Name: ref.Title})
}

func headingFor(presentation string) string {
	switch presentation {
	case sneatdomain.PresentationDayCalendar:
		return "Today"
	case sneatdomain.PresentationWeekCalendar:
		return "This week"
	case sneatdomain.PresentationHappeningsList:
		return "Happenings"
	case sneatdomain.PresentationTodoList:
		return "Todos"
	case sneatdomain.PresentationBuyList:
		return "To buy"
	default:
		return "Results"
	}
}

// focusedScopes maps the session's focused/sidebar entity types to their
// owning module scope, for StreamRequest's pinnedScopes (brief §4/§7: the
// Context Manager includes dynamic data for focused/sidebar entities even
// when not required by a decision).
func focusedScopes(st *session.State) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		scope := moduleForEntityType(t)
		if scope == "" || seen[scope] {
			return
		}
		seen[scope] = true
		out = append(out, scope)
	}
	if st.Focused != nil {
		add(st.Focused.Type)
	}
	for _, r := range st.Sidebar {
		add(r.Type)
	}
	return out
}

func moduleForEntityType(t string) string {
	switch t {
	case sneatdomain.EntityHappening:
		return sneatdomain.ModuleCalendar
	case sneatdomain.EntityTodo:
		return sneatdomain.ModuleTodo
	case sneatdomain.EntityContact:
		return sneatdomain.ModuleContacts
	default:
		return ""
	}
}

// pathFor labels a deterministically-answered turn's diagnostics path (S11
// coordinator ruling DIAGNOSTICS): the sneat-rules provider deciding is
// "deterministic"; some OTHER decider (Jev/cloud-decision, or any future
// provider the chain gains) deciding is "decision" (the product's own logic
// still ran, but a real inference produced the decision); no provider
// deciding at all -- reached here only via the LastShown numeric-pick
// shortcut, since every other no-decision case sets NeedsLLM -- is
// "llm-fallback"'s deterministic sibling in spirit, so it is logged as
// PathDeterministic too (it never touches the main LLM), while the Trace
// itself (attached below) still shows DecidedBy=="" for anyone reading the
// full record.
func pathFor(out pipeline.Output) aidiag.Path {
	switch out.Trace.DecidedBy {
	case "":
		return aidiag.PathDeterministic
	case sneatrules.Name:
		return aidiag.PathDeterministic
	default:
		return aidiag.PathDecision
	}
}

// logTurn/logStreamRequest emit ai/diag.Turn records at Debug -- never user
// text. logTurn also emits the FULL decision.Trace (S11: every provider's
// outcome/latency, not just aidiag.Log's own decidedBy+count summary) as a
// separate structured line, since aidiag.Turn/aidiag.Log (a shared
// strongo/aichat contract) does not itself expand Attempts.
func (h *handler) logTurn(path aidiag.Path, out pipeline.Output, err error) {
	if h.logger == nil {
		return
	}
	t := aidiag.Turn{Path: path, LLMSkipped: true, Decision: &out.Trace}
	if out.Decision != nil {
		t.Module, t.Intent = out.Decision.Module.Value, out.Decision.Intent.Value
	}
	if err != nil {
		t.Errors = []string{aidiag.ErrorCode(err)}
	}
	aidiag.Log(h.ctx, h.logger, t)
	h.logDecisionTrace(out.Trace)
}

// logDecisionTrace logs every decision.Attempt in trace -- provider,
// outcome, latency -- never req.Text/user content (a decision.Attempt never
// carries it in the first place).
func (h *handler) logDecisionTrace(trace decision.Trace) {
	if h.logger == nil || len(trace.Attempts) == 0 {
		return
	}
	for _, a := range trace.Attempts {
		h.logger.DebugContext(h.ctx, "aichat.decision.attempt",
			"provider", a.Provider, "outcome", a.Outcome, "latency", a.Latency)
	}
}

func (h *handler) logStreamRequest(d *decision.Decision, report ctxmgr.Report) {
	if h.logger == nil {
		return
	}
	path := aidiag.PathLLMFallback
	var module, intent string
	if d != nil {
		path, module, intent = aidiag.PathDecision, d.Module.Value, d.Intent.Value
	}
	h.logger.DebugContext(h.ctx, "aichat.turn.llm",
		"path", string(path), "module", module, "intent", intent,
		"requiredScopes", fmt.Sprint(report.Required), "actualScopes", fmt.Sprint(append(report.Retained, report.Added...)))
}

// --- message types the pipeline hands back through tea.Cmd -----------------

type slashMsg struct {
	replies       []chat.Reply
	err           error
	text          string
	interactionID string
}

// turnMsg carries the state SNAPSHOT the background pipeline.Turn call
// produced (see Submit's doc comment); handleTurn is the only place that
// writes it onto the live h.state, and it does so on the UI loop.
type turnMsg struct {
	text   string
	output pipeline.Output
	err    error
	state  session.State
	// seq is the turn this message belongs to (S1: Submit's atomic.AddInt64
	// result) -- handleTurn drops the message if h.turnSeq has since moved
	// on.
	seq int64
}

// llmRequestReadyMsg carries beginLLMStream's background-built
// ai.ChatRequest back to the UI loop (S2): ctx is the "building the
// request" phase's own cancellable context (not the stream's own, which
// chatshell.StartStream creates separately) -- startLLMStream checks it to
// drop a request that finished building after the user already cancelled.
type llmRequestReadyMsg struct {
	ctx      context.Context
	text     string
	decision *decision.Decision
	req      ai.ChatRequest
	report   ctxmgr.Report
	// state is the snapshot beginLLMStream took (S1); startLLMStream stores
	// it per-stream-id so OnStreamEvent's EventCompleted branch can hand it
	// to a background HandleAction call without ever touching live h.state.
	state session.State
	// seq is the turn this request belongs to (S1) -- carried forward onto
	// every message the resulting stream itself produces (actionMsg).
	seq int64
}

type actionMsg struct {
	output   pipeline.Output
	err      error
	parseErr error
	trailing string
	// state is HandleAction's resulting snapshot (S1); OnMsg applies only
	// its turn-owned fields (applyStateDelta) onto live h.state, and only
	// when seq is still current.
	state session.State
	// seq is the turn this action belongs to (S1: the value startLLMStream
	// recorded in h.streamTurnSeq when this stream started) -- OnMsg drops
	// the message outright (no render, no state write) if h.turnSeq has
	// since moved on to a newer turn.
	seq int64
	// id is the stream this action's background work was dispatched for
	// (fix round r5, M2): OnMsg uses it to release h.pendingAction[id], so
	// OnStreamDone stops re-asserting busy for a stream whose action has
	// already resolved.
	id   string
	kind string
	// executed is true only when HandleAction was called AND it actually
	// ran an action (err == nil and state.Previous now differs from the
	// pointer it held right before the call) -- fix round r6 review
	// correction to r5's applyUndoDelta fix: parseErr == nil is NOT enough
	// to know something real happened. executed is FALSE for a plain
	// text-only answer (action == nil, HandleAction never even called --
	// state is the zero value, so its nil Previous would otherwise wipe a
	// real one) and for an action that reached HandleAction but errored or
	// was cancelled before executing (state.Previous is whatever stale
	// value the ORIGINAL pre-action snapshot already held, not a fresh
	// one, so applying it would overwrite a NEWER Previous with an old
	// one). OnMsg's stale branch uses this, not parseErr, to decide
	// whether a stale result may still apply/render.
	executed bool
}
