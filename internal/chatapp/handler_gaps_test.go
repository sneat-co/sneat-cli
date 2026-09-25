package chatapp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bots-go-framework/bots-go-core/botkb"
	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/chatshell"
	"github.com/strongo/aichat/tui/grid"

	"github.com/sneat-co/sneat-cli/internal/aichat/controls"
	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// -- appendHistory ----------------------------------------------------------

// TestAppendHistory_EmptyUserIsNoOp covers the "" user guard.
func TestAppendHistory_EmptyUserIsNoOp(t *testing.T) {
	h, _ := testHandler(t)
	h.appendHistory("", "reply")
	if len(h.history) != 0 {
		t.Fatalf("history = %+v, want unchanged for an empty user text", h.history)
	}
}

// TestAppendHistory_TrimsToMax covers the over-cap trim branch.
func TestAppendHistory_TrimsToMax(t *testing.T) {
	h, _ := testHandler(t)
	for i := 0; i < pipeline.HistoryTurns+2; i++ {
		h.appendHistory("q", "a")
	}
	max := pipeline.HistoryTurns * 2
	if len(h.history) != max {
		t.Fatalf("len(history) = %d, want capped at %d", len(h.history), max)
	}
}

// -- Submit's FocusedRef/SelectionRefs branches ------------------------------

// TestSubmit_SnapshotsFocusedAndSelectionFromModel covers Submit's own
// h.model.FocusedRef()/SelectionRefs() branches: called directly while the
// transcript zone (not the composer) holds focus over a real block, so
// FocusedRef/SelectionRefs are non-nil -- the real condition under which a
// button/item activation could immediately be followed by a submit (e.g. a
// future key binding), even though typeAndEnter's own key routing always
// returns focus to the composer first.
func TestSubmit_SnapshotsFocusedAndSelectionFromModel(t *testing.T) {
	h, model := testHandler(t)
	m := typeAndEnter(t, model, "show my calendar today")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}) // focus the DayCalendar block; composer loses focus
	h.model = m.(*chatshell.Model)

	if h.model.FocusedRef() == nil {
		t.Fatal("test setup: expected the transcript's focused block to report a Current() ref")
	}
	cmd := h.Submit("it")
	if cmd == nil {
		t.Fatal("Submit returned a nil tea.Cmd")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want a tea.BatchMsg (work + busyCmd)", msg)
	}
	var got turnMsg
	found := false
	for _, c := range batch {
		if c == nil {
			continue
		}
		if tm, ok := c().(turnMsg); ok {
			got = tm
			found = true
		}
	}
	if !found {
		t.Fatal("no turnMsg produced by Submit's work closure")
	}
	if got.state.Focused == nil || got.state.Focused.Keys["happeningID"] != "h1" {
		t.Fatalf("snapshot.Focused = %+v, want the transcript's focused happening carried into the snapshot", got.state.Focused)
	}
}

// testLogger builds a Debug-level JSON logger writing into buf, for tests
// that assert on logTurn/OnStreamDone's diagnostics output.
func testLogger(buf *strings.Builder) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// -- cmdOrWork ----------------------------------------------------------------

// TestCmdOrWork covers both branches directly.
func TestCmdOrWork(t *testing.T) {
	work := func() tea.Msg { return "work" }
	if got := cmdOrWork(work, nil); got == nil {
		t.Fatal("expected work itself when busyCmd is nil")
	}
	busy := func() tea.Msg { return "busy" }
	if got := cmdOrWork(work, busy); got == nil {
		t.Fatal("expected a batched command when busyCmd is non-nil")
	}
}

// -- OnStreamEvent ------------------------------------------------------------

// newStreamHandler builds a handler with a fresh stream id's bookkeeping
// pre-populated the way startLLMStream would, for driving OnStreamEvent/
// OnStreamDone directly without a real ai.LLMProvider.
func newStreamHandler(t *testing.T) (*handler, string) {
	t.Helper()
	h, _ := testHandler(t)
	id := "turn-1"
	h.splitters = map[string]*pipeline.Splitter{}
	h.streamText = map[string]*strings.Builder{id: {}}
	h.streamUser = map[string]string{id: "hi"}
	h.streamStart = map[string]time.Time{id: time.Now()}
	h.streamUsage = map[string]*ai.Usage{}
	h.streamModel = map[string]string{}
	h.streamProvider = map[string]string{}
	h.streamDecided = map[string]bool{id: true}
	h.streamState = map[string]*session.State{id: {}}
	h.streamCtx = map[string]context.Context{}
	h.streamTurnSeq = map[string]int64{id: h.turnSeq}
	return h, id
}

// TestOnStreamEvent_CapturesProviderModelUsage covers the three capture
// branches directly.
func TestOnStreamEvent_CapturesProviderModelUsage(t *testing.T) {
	h, id := newStreamHandler(t)
	h.OnStreamEvent(id, ai.Event{Provider: "openai", Model: "gpt-5", Usage: &ai.Usage{}})
	if h.streamProvider[id] != "openai" || h.streamModel[id] != "gpt-5" || h.streamUsage[id] == nil {
		t.Fatalf("provider=%q model=%q usage=%v, want all captured", h.streamProvider[id], h.streamModel[id], h.streamUsage[id])
	}
}

// TestOnStreamEvent_NoSplitterForID covers OnStreamEvent's own "no splitter
// registered for this id" guard on EventCompleted (sp == nil).
func TestOnStreamEvent_NoSplitterForID(t *testing.T) {
	h, id := newStreamHandler(t)
	if cmd := h.OnStreamEvent(id, ai.Event{Type: ai.EventCompleted}); cmd != nil {
		t.Fatal("expected nil with no splitter registered for this stream id")
	}
}

// TestOnStreamEvent_DefensiveNilCtxFallback covers the "ctx == nil ->
// h.ctx" defensive fallback: h.streamCtx has no entry for id (startLLMStream
// always sets one via open(), but OnStreamEvent must not panic if that
// somehow hasn't happened yet).
func TestOnStreamEvent_DefensiveNilCtxFallback(t *testing.T) {
	h, id := newStreamHandler(t)
	h.splitters[id] = &pipeline.Splitter{}
	cmd := h.OnStreamEvent(id, ai.Event{Type: ai.EventCompleted})
	if cmd == nil {
		t.Fatal("expected a work command even with no streamCtx entry")
	}
	msg := cmd()
	if _, ok := msg.(actionMsg); !ok {
		if batch, ok := msg.(tea.BatchMsg); ok {
			found := false
			for _, c := range batch {
				if c == nil {
					continue
				}
				if _, ok := c().(actionMsg); ok {
					found = true
				}
			}
			if !found {
				t.Fatalf("msg = %+v, want an actionMsg somewhere in the batch", msg)
			}
		} else {
			t.Fatalf("msg = %T, want actionMsg or a batch containing one", msg)
		}
	}
}

// TestOnStreamEvent_FinishParseError covers the work closure's Finish()
// error branch: an unterminated <sneat-action> block.
func TestOnStreamEvent_FinishParseError(t *testing.T) {
	h, id := newStreamHandler(t)
	sp := &pipeline.Splitter{}
	sp.Feed("Hello <sneat-action>{\"kind\":\"x\"") // never closed
	h.splitters[id] = sp
	h.streamCtx = map[string]context.Context{id: context.Background()}
	cmd := h.OnStreamEvent(id, ai.Event{Type: ai.EventCompleted})
	am := extractActionMsg(t, cmd())
	if am.parseErr == nil {
		t.Fatal("expected a parseErr for an unterminated action block")
	}
}

// TestOnStreamEvent_PlainTextOnlyAction covers the "no <sneat-action> block
// at all" branch: Finish() returns action==nil, still an actionMsg.
func TestOnStreamEvent_PlainTextOnlyAction(t *testing.T) {
	h, id := newStreamHandler(t)
	sp := &pipeline.Splitter{}
	sp.Feed("just plain text, no action block")
	h.splitters[id] = sp
	h.streamCtx = map[string]context.Context{id: context.Background()}
	cmd := h.OnStreamEvent(id, ai.Event{Type: ai.EventCompleted})
	msg := cmd()
	am := extractActionMsg(t, msg)
	if am.output.Text != "" || am.err != nil {
		t.Fatalf("am = %+v, want a plain (empty-output) actionMsg", am)
	}
}

func extractActionMsg(t *testing.T, msg tea.Msg) actionMsg {
	t.Helper()
	if am, ok := msg.(actionMsg); ok {
		return am
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if am, ok := c().(actionMsg); ok {
				return am
			}
		}
	}
	t.Fatalf("msg = %#v, want an actionMsg or a batch containing one", msg)
	return actionMsg{}
}

// -- OnStreamDone -------------------------------------------------------------

// TestOnStreamDone_ReassertsBusyForPendingAction covers the
// h.pendingAction[id] branch.
func TestOnStreamDone_ReassertsBusyForPendingAction(t *testing.T) {
	h, id := newStreamHandler(t)
	h.pendingAction = map[string]context.CancelFunc{id: func() {}}
	cmd := h.OnStreamDone(id, nil)
	if cmd == nil {
		t.Fatal("expected a busyCmd re-asserting busy for a still-pending action")
	}
}

// TestOnStreamDone_NoPendingAction covers the branch's absence (no
// busyCmd).
func TestOnStreamDone_NoPendingAction(t *testing.T) {
	h, id := newStreamHandler(t)
	if cmd := h.OnStreamDone(id, nil); cmd != nil {
		t.Fatal("expected nil with no pending action for this stream id")
	}
}

// TestOnStreamDone_RecordsHistoryOnSuccess covers the "err == nil" history
// branch, and its absence on a stream error.
func TestOnStreamDone_RecordsHistoryOnSuccess(t *testing.T) {
	h, id := newStreamHandler(t)
	h.streamText[id].WriteString("the reply")
	h.OnStreamDone(id, nil)
	if len(h.history) != 2 {
		t.Fatalf("history = %+v, want the user+assistant exchange recorded", h.history)
	}
}

func TestOnStreamDone_NoHistoryOnError(t *testing.T) {
	h, id := newStreamHandler(t)
	h.streamText[id].WriteString("partial")
	h.OnStreamDone(id, errors.New("boom"))
	if len(h.history) != 0 {
		t.Fatalf("history = %+v, want nothing recorded on a stream error", h.history)
	}
}

// TestOnStreamDone_LogsDecisionAndLLMFallbackPaths cover both S11 path
// branches (streamDecided true/false) plus the latency/error sub-branches,
// through a real logger so logTurn/aidiag actually run.
func TestOnStreamDone_LogsDecisionAndLLMFallbackPaths(t *testing.T) {
	var buf strings.Builder
	h, id := newStreamHandler(t)
	h.logger = testLogger(&buf)
	h.streamDecided[id] = true
	h.OnStreamDone(id, errors.New("boom"))
	if !strings.Contains(buf.String(), "\"path\":\"decision\"") {
		t.Fatalf("log = %s, want path=decision", buf.String())
	}

	buf.Reset()
	h2, id2 := newStreamHandler(t)
	h2.logger = testLogger(&buf)
	h2.streamDecided[id2] = false
	h2.OnStreamDone(id2, nil)
	if !strings.Contains(buf.String(), "\"path\":\"llm-fallback\"") {
		t.Fatalf("log = %s, want path=llm-fallback", buf.String())
	}
}

// -- OnMsg --------------------------------------------------------------------

// TestOnMsg_LLMRequestReadyMsg_StaleSeqDropped covers the stale-seq guard
// on llmRequestReadyMsg directly.
func TestOnMsg_LLMRequestReadyMsg_StaleSeqDropped(t *testing.T) {
	h, _ := testHandler(t)
	h.turnSeq = 5
	if cmd := h.OnMsg(llmRequestReadyMsg{seq: 1}); cmd != nil {
		t.Fatal("expected nil for a stale llmRequestReadyMsg")
	}
}

// TestOnMsg_ActionMsg_ParseErrTrailingAndNotTrailing cover both parseErr
// sub-branches: a trailing-text flush, and the ErrActionNotTrailing note
// vs. the generic "couldn't parse" note.
func TestOnMsg_ActionMsg_ParseErrTrailingAndNotTrailing(t *testing.T) {
	h, model := testHandler(t)
	h.model = model

	h.OnMsg(actionMsg{parseErr: errors.New("malformed"), trailing: "some text", seq: h.turnSeq})
	view := h.model.View().Content
	if !strings.Contains(view, "some text") || !strings.Contains(view, "couldn't parse action") {
		t.Fatalf("view = %q, want the trailing text and the generic parse-error note", view)
	}

	h.OnMsg(actionMsg{parseErr: pipeline.ErrActionNotTrailing, seq: h.turnSeq})
	view = h.model.View().Content
	if !strings.Contains(view, "no action taken") {
		t.Fatalf("view = %q, want the ErrActionNotTrailing-specific note", view)
	}
}

// TestOnMsg_ActionMsg_ParseErr_LogsDiagnostics covers the h.logger != nil
// branch inside the parseErr case.
func TestOnMsg_ActionMsg_ParseErr_LogsDiagnostics(t *testing.T) {
	var buf strings.Builder
	h, model := testHandler(t)
	h.model = model
	h.logger = testLogger(&buf)
	h.OnMsg(actionMsg{parseErr: errors.New("boom"), seq: h.turnSeq})
	if !strings.Contains(buf.String(), "\"path\":\"llm-fallback\"") {
		t.Fatalf("log = %s, want a PathLLMFallback diagnostics line", buf.String())
	}
}

// TestOnMsg_GridRowActivated covers grid.RowActivatedMsg's own two branches:
// a nil Ref (no-op) and a real one (focuses it).
func TestOnMsg_GridRowActivated(t *testing.T) {
	h, _ := testHandler(t)
	if cmd := h.OnMsg(grid.RowActivatedMsg{Row: grid.Row{Ref: nil}}); cmd != nil {
		t.Fatal("expected nil")
	}
	if h.state.Focused != nil {
		t.Fatal("a nil Ref must not focus anything")
	}
	ref := session.EntityRef{Type: sneatdomain.EntityContact, Keys: map[string]string{"contactID": "c1"}}
	h.OnMsg(grid.RowActivatedMsg{Row: grid.Row{Ref: &ref}})
	if h.state.Focused == nil || h.state.Focused.Keys["contactID"] != "c1" {
		t.Fatalf("Focused = %+v, want the activated row's ref", h.state.Focused)
	}
}

// TestOnMsg_ControlsItemActivated covers controls.ItemActivatedMsg.
func TestOnMsg_ControlsItemActivated(t *testing.T) {
	h, _ := testHandler(t)
	ref := session.EntityRef{Type: sneatdomain.EntityHappening, Keys: map[string]string{"happeningID": "h1"}}
	h.OnMsg(controls.ItemActivatedMsg{Ref: ref})
	if h.state.Focused == nil || h.state.Focused.Keys["happeningID"] != "h1" {
		t.Fatalf("Focused = %+v, want the activated item's ref", h.state.Focused)
	}
}

// TestOnMsg_UnhandledMsgType covers the final default "return nil" for a
// tea.Msg type OnMsg's switch does not recognize.
func TestOnMsg_UnhandledMsgType(t *testing.T) {
	h, _ := testHandler(t)
	if cmd := h.OnMsg(struct{ tea.Msg }{}); cmd != nil {
		t.Fatal("expected nil for an unrecognized message type")
	}
}

// -- handleSlash / appendReplies ---------------------------------------------

// TestHandleSlash_Error covers the m.err branch.
func TestHandleSlash_Error(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	h.OnMsg(slashMsg{err: errors.New("boom")})
	if !strings.Contains(h.model.View().Content, "error: boom") {
		t.Fatalf("view = %q, want the error line", h.model.View().Content)
	}
}

// TestAppendReplies_KeyboardNilAndBlock cover both branches: a reply with
// no Keyboard (skipped) and one whose Keyboard builds a real buttonsBlock.
func TestAppendReplies_KeyboardNilAndBlock(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline, []botkb.Button{botkb.NewDataButton("Home", "space?id=sp1")})
	h.appendReplies([]chat.Reply{
		{Text: "no keyboard here"},
		{Text: "pick one", Keyboard: kb},
	})
	view := h.model.View().Content
	if !strings.Contains(view, "no keyboard here") || !strings.Contains(view, "pick one") || !strings.Contains(view, "Home") {
		t.Fatalf("view = %q, want both replies and the keyboard's button rendered", view)
	}
}

// -- pressButton --------------------------------------------------------------

// TestPressButton_BusyIsNoOp covers the busy-gate guard.
func TestPressButton_BusyIsNoOp(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	h.model.SetBusy(true)
	if cmd := h.pressButton(botkb.NewDataButton("x", "y")); cmd != nil {
		t.Fatal("expected nil while busy")
	}
}

// TestPressButton_DataButton covers the DataButton case's own tea.Cmd.
func TestPressButton_DataButton(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	cmd := h.pressButton(botkb.NewDataButton("x", "space?id=sp1"))
	if cmd == nil {
		t.Fatal("expected a command for a DataButton")
	}
	msg, ok := cmd().(slashMsg)
	if !ok {
		t.Fatalf("msg = %T, want slashMsg", msg)
	}
}

// TestPressButton_TextButton covers the TextButton case's own tea.Cmd.
func TestPressButton_TextButton(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	cmd := h.pressButton(botkb.NewTextButton("hi"))
	if cmd == nil {
		t.Fatal("expected a command for a TextButton")
	}
	if _, ok := cmd().(slashMsg); !ok {
		t.Fatal("expected a slashMsg")
	}
}

// TestPressButton_UrlButton covers the UrlButton case (echoed as a system
// line, no command).
func TestPressButton_UrlButton(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	if cmd := h.pressButton(botkb.NewUrlButton("Open", "https://example.com")); cmd != nil {
		t.Fatal("expected nil for a URL button")
	}
	if !strings.Contains(h.model.View().Content, "https://example.com") {
		t.Fatalf("view = %q, want the URL echoed", h.model.View().Content)
	}
}

// TestPressButton_UnknownType covers the default case.
func TestPressButton_UnknownType(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	if cmd := h.pressButton(unknownButton{}); cmd != nil {
		t.Fatal("expected nil for an unrecognized button type")
	}
}

type unknownButton struct{ botkb.Button }

// -- handleTurn -----------------------------------------------------------

// TestHandleTurn_StaleSeqDropped covers the guard directly.
func TestHandleTurn_StaleSeqDropped(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	h.turnSeq = 5
	if cmd := h.handleTurn(turnMsg{seq: 1}); cmd != nil {
		t.Fatal("expected nil for a stale turnMsg")
	}
}

// TestHandleTurn_Error covers the m.err branch, including logTurn.
func TestHandleTurn_Error(t *testing.T) {
	var buf strings.Builder
	h, model := testHandler(t)
	h.model = model
	h.logger = testLogger(&buf)
	h.handleTurn(turnMsg{err: errors.New("boom"), seq: h.turnSeq})
	if !strings.Contains(h.model.View().Content, "error: boom") {
		t.Fatalf("view = %q, want the error line", h.model.View().Content)
	}
	if buf.Len() == 0 {
		t.Fatal("expected a logTurn diagnostics line")
	}
}

// TestStartLLMStream_CancelledCtx covers the "building the request" phase
// having already been cancelled (Esc/Ctrl+C) by the time llmRequestReadyMsg
// arrives.
func TestStartLLMStream_CancelledCtx(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if cmd := h.startLLMStream(llmRequestReadyMsg{ctx: ctx}); cmd != nil {
		t.Fatal("expected nil for an already-cancelled request-building phase")
	}
}

// -- render / blockFor / cardFor / headingFor / focusedScopes ---------------

// TestRender_Error covers render's own error branch.
func TestRender_Error(t *testing.T) {
	h, model := testHandler(t)
	h.model = model
	h.render(pipeline.Output{}, errors.New("boom"))
	if !strings.Contains(h.model.View().Content, "error: boom") {
		t.Fatalf("view = %q, want the error line", h.model.View().Content)
	}
}

// TestBlockFor covers every presentation branch directly: multi-contact
// grid, single happening card, day/week/happenings-list with rows, todo/buy
// list with rows, and the generic ListBlock fallback.
func TestBlockFor(t *testing.T) {
	contactRef := session.EntityRef{Type: sneatdomain.EntityContact, Title: "Alice", Keys: map[string]string{"contactID": "c1"}}
	contactRef2 := session.EntityRef{Type: sneatdomain.EntityContact, Title: "Bob", Keys: map[string]string{"contactID": "c2"}}

	if blk := blockFor(pipeline.Output{Presentation: sneatdomain.PresentationContactsGrid, Entities: []session.EntityRef{contactRef, contactRef2}}); blk == nil {
		t.Fatal("multi-contact: expected a grid block")
	}

	happeningRef := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Standup"}
	if blk := blockFor(pipeline.Output{
		Presentation:  sneatdomain.PresentationHappeningCard,
		HappeningRows: []controls.HappeningRow{{Ref: happeningRef, Title: "Standup"}},
	}); blk == nil {
		t.Fatal("single happening card: expected a block")
	}

	for _, presentation := range []string{sneatdomain.PresentationDayCalendar, sneatdomain.PresentationWeekCalendar, sneatdomain.PresentationHappeningsList} {
		out := pipeline.Output{Presentation: presentation, HappeningRows: []controls.HappeningRow{{Ref: happeningRef, Title: "Standup"}}}
		if blk := blockFor(out); blk == nil {
			t.Fatalf("%s: expected a block", presentation)
		}
	}

	todoRef := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Milk"}
	for _, presentation := range []string{sneatdomain.PresentationTodoList, sneatdomain.PresentationBuyList} {
		out := pipeline.Output{Presentation: presentation, TodoRows: []controls.TodoRow{{Ref: todoRef, Title: "Milk"}}}
		if blk := blockFor(out); blk == nil {
			t.Fatalf("%s: expected a block", presentation)
		}
	}

	// Generic fallback: entities only, no rows, an unrecognized presentation.
	if blk := blockFor(pipeline.Output{Presentation: "bogus", Entities: []session.EntityRef{happeningRef}}); blk == nil {
		t.Fatal("fallback: expected a ListBlock")
	}
}

// TestCardFor covers both branches: a matching row (enriched card) and no
// match (name-only fallback).
func TestCardFor(t *testing.T) {
	ref := session.EntityRef{Type: sneatdomain.EntityContact, Title: "Alice", Keys: map[string]string{"contactID": "c1"}}
	rows := []controls.ContactRow{{Ref: ref, Name: "Alice", RelatedAs: "spouse"}}
	if got := cardFor(ref, rows); got == nil {
		t.Fatal("expected a card built from the matching row")
	}
	other := session.EntityRef{Type: sneatdomain.EntityContact, Title: "Bob", Keys: map[string]string{"contactID": "c2"}}
	if got := cardFor(other, rows); got == nil {
		t.Fatal("expected a name-only fallback card when nothing matches")
	}
}

// TestHeadingFor covers every branch, including the default.
func TestHeadingFor(t *testing.T) {
	cases := map[string]string{
		sneatdomain.PresentationDayCalendar:    "Today",
		sneatdomain.PresentationWeekCalendar:   "This week",
		sneatdomain.PresentationHappeningsList: "Happenings",
		sneatdomain.PresentationTodoList:       "Todos",
		sneatdomain.PresentationBuyList:        "To buy",
		"bogus":                                "Results",
	}
	for in, want := range cases {
		if got := headingFor(in); got != want {
			t.Errorf("headingFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFocusedScopes_SidebarAndDedup covers focusedScopes' Sidebar loop and
// its dedup-skip branch, distinct from the package's existing
// TestFocusedScopes (empty state and a bare Focused only).
func TestFocusedScopes_SidebarAndDedup(t *testing.T) {
	st := &session.State{
		Focused: &session.EntityRef{Type: sneatdomain.EntityHappening},
		Sidebar: []session.EntityRef{
			{Type: sneatdomain.EntityHappening}, // same scope as Focused: deduped
			{Type: sneatdomain.EntityContact},
			{Type: "unknown"}, // moduleForEntityType("") -> skipped
		},
	}
	got := focusedScopes(st)
	want := map[string]bool{sneatdomain.ModuleCalendar: true, sneatdomain.ModuleContacts: true}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want exactly %v", got, want)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("unexpected scope %q in %v", s, got)
		}
	}
}

// TestTopBar covers chatshell.TopBarProvider content: title is always
// "Sneat"; the context string names the active space, or says none is
// selected -- content only (shared-look cutover, REQ:
// chatshell-product-bars), no menu items.
func TestTopBar(t *testing.T) {
	h, _ := testHandler(t)

	title, context, items := h.topBar(80)
	if title != "Sneat" {
		t.Errorf("title = %q, want %q", title, "Sneat")
	}
	if context != "space: sp1" {
		t.Errorf("context = %q, want the active space", context)
	}
	if items != nil {
		t.Errorf("items = %v, want none", items)
	}

	h.spaceID = ""
	_, context, _ = h.topBar(80)
	if context != "no space selected" {
		t.Errorf("context = %q, want the no-space message", context)
	}
}

// TestHints covers chatshell.HintsProvider content: the idle hint set, and
// the busy set (only a spinner note plus quit) once SetBusy(true) is in
// effect -- content only, no styling.
func TestHints(t *testing.T) {
	h, _ := testHandler(t)

	hints, segments := h.hints(80)
	if len(hints) == 0 || segments != nil {
		t.Fatalf("hints = %v, segments = %v, want a non-empty idle hint set and no segments", hints, segments)
	}
	foundEnter := false
	for _, hint := range hints {
		if hint.Key == "Enter" && hint.Label == "send" {
			foundEnter = true
		}
	}
	if !foundEnter {
		t.Fatalf("hints = %v, want an Enter/send hint", hints)
	}

	h.model.SetBusy(true)
	hints, segments = h.hints(80)
	if len(hints) != 1 || hints[0].Key != "Ctrl+C" {
		t.Fatalf("busy hints = %v, want only Ctrl+C/quit", hints)
	}
	if len(segments) != 1 || segments[0] != "Thinking…" {
		t.Fatalf("busy segments = %v, want the thinking note", segments)
	}
}
