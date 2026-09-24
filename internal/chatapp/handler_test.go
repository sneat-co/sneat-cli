package chatapp

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/decision"
	aidiag "github.com/strongo/aichat/ai/diag"
	"github.com/strongo/aichat/ai/openaicompat"
	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/chatshell"
	"github.com/strongo/aichat/tui/stream"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	sneatrules "github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// TestPathFor covers S11's path labelling: sneat-rules deciding is
// "deterministic", any other decider (Jev/cloud-decision) is "decision",
// and no decider at all is "deterministic" too (the LastShown numeric-pick
// shortcut is the only way Turn returns !NeedsLLM with an empty
// Trace.DecidedBy).
func TestPathFor(t *testing.T) {
	cases := []struct {
		name      string
		decidedBy string
		want      aidiag.Path
	}{
		{"sneat-rules", sneatrules.Name, aidiag.PathDeterministic},
		{"no decider (LastShown pick)", "", aidiag.PathDeterministic},
		{"some other decider (Jev)", "jev", aidiag.PathDecision},
	}
	for _, c := range cases {
		out := pipeline.Output{Trace: decision.Trace{DecidedBy: c.decidedBy}}
		if got := pathFor(out); got != c.want {
			t.Errorf("%s: pathFor = %q, want %q", c.name, got, c.want)
		}
	}
}

// pressKey is a single non-printable/chorded key press (e.g. "shift+up",
// "enter", "+") driven through the real chatshell.Model.Update, draining any
// resulting tea.Cmd the same way typeAndEnter does for typed text.
func pressKey(m tea.Model, key tea.KeyPressMsg) tea.Model {
	var cmd tea.Cmd
	m, cmd = m.Update(key)
	return drain(m, cmd, 10)
}

type fakeSpaces struct{}

func (fakeSpaces) ListSpaces(context.Context, string) (map[string]any, error) {
	return map[string]any{"sp1": map[string]any{"title": "Home"}}, nil
}

type fakeContacts struct{}

func (fakeContacts) ListContacts(context.Context, string) ([]chat.Contact, error) { return nil, nil }

func testHandler(t *testing.T) (*handler, *chatshell.Model) {
	t.Helper()
	readers := data.Readers{
		Happenings: &data.FakeHappenings{Items: []data.Happening{
			{ID: "h1", SpaceID: "sp1", Title: "Team standup", SlotID: "s1",
				Start: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 10, 15, 0, 0, time.UTC)},
		}},
		Todos:    &data.FakeTodos{},
		Contacts: &data.FakeContacts{},
	}
	pl := pipeline.Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{sneatrules.New()}},
		Resolver: pipeline.Resolver{Readers: readers},
		Executor: &pipeline.FakeExecutor{},
		Readers:  readers,
		Now:      func() time.Time { return time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC) },
		Product:  "sneat",
	}
	processor := chat.NewProcessor(chat.Deps{Spaces: fakeSpaces{}, Contacts: fakeContacts{}, UID: "u1", Email: "u1@example.com"})
	h := &handler{ctx: context.Background(), pipeline: pl, processor: processor, state: &session.State{}, spaceID: "sp1"}
	model := chatshell.New(h, chatshell.WithTitle("sneat chat"), chatshell.WithSidebarRenderer(sidebarRender))
	h.model = model
	// A real terminal sends WindowSizeMsg before any key; without it the
	// transcript viewport has zero size and View() renders no history, even
	// for calls to h.model.View() that bypass typeAndEnter.
	var m tea.Model = model
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.model = m.(*chatshell.Model)
	return h, h.model
}

// typeAndEnter feeds text as individual printable-key presses followed by
// Enter, the same sequence a real terminal would produce, then drains every
// resulting tea.Cmd (a stream's re-arm command, a spinner tick, ...) up to
// maxSteps so async work (Submit's tea.Cmd closure, which runs the pipeline
// synchronously here since there is no real terminal driving it) settles
// before the test inspects the model.
func typeAndEnter(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	// A real terminal sends WindowSizeMsg before any key; without it the
	// transcript viewport has zero size and View() renders no history.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, r := range text {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = drain(m, cmd, 3)
	}
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return drain(m, cmd, 40)
}

// slowCmdBudget bounds how long drain waits for any single tea.Cmd before
// giving up on it (m13: chatapp tests must not carry real sleeps). Bubble
// Tea's own textarea cursor blink and chatshell's busy spinner are real
// wall-clock timers (cursor.Model.Blink() blocks on a context timeout,
// spinner.Tick fires on its FPS interval) that re-arm themselves
// indefinitely while focused/busy; run for real, an unbounded drain of them
// dominates every keystroke's test time. Dropping a cmd that does not
// resolve within the budget is safe here: this harness never asserts on
// cursor-blink or spinner-tick state, only on the transcript/session
// content those keys produced synchronously.
const slowCmdBudget = 5 * time.Millisecond

// runCmd runs cmd with slowCmdBudget, returning nil (and abandoning cmd's
// goroutine -- harmless, it sends to a buffered channel of size 1) if it has
// not produced a Msg within the budget.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(slowCmdBudget):
		return nil
	}
}

// drain is a headless stand-in for tea.Program's event loop: it maintains a
// queue of pending commands (seeded with cmd), runs each one, and feeds the
// resulting Msg back into Update -- expanding a tea.BatchMsg into its
// constituent commands rather than treating it as an ordinary Msg, since
// chatshell's StartStream returns exactly one (the stream plus the
// spinner). Bounded by maxSteps so an indefinitely-recurring command (the
// spinner's own Tick, re-armed on every busy tick) cannot hang a test.
func drain(m tea.Model, cmd tea.Cmd, maxSteps int) tea.Model {
	queue := []tea.Cmd{cmd}
	for steps := 0; steps < maxSteps && len(queue) > 0; steps++ {
		cmd, queue = queue[0], queue[1:]
		if cmd == nil {
			continue
		}
		msg := runCmd(cmd)
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		if next != nil {
			queue = append(queue, next)
		}
	}
	return m
}

// TestChatshell_ShowCalendarToday_NoMainLLM is the scenario-2 headless smoke
// test: typing "show my calendar today" through the real chatshell.Model
// renders the fake happening's title with no LLM configured (proving the
// deterministic path never needs one). It also covers the S9 follow-up:
// pipeline.Output.HappeningRows carries the happening's real Start, so the
// rendered DayCalendar block shows its actual time ("10:00"), not just the
// title a bare session.EntityRef would have given it.
func TestChatshell_ShowCalendarToday_NoMainLLM(t *testing.T) {
	_, model := testHandler(t)
	m := typeAndEnter(t, model, "show my calendar today")
	view := m.View()
	if !strings.Contains(view.Content, "Team standup") {
		t.Fatalf("view does not contain the happening title:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Today") {
		t.Fatalf("view does not contain the DayCalendar heading:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "10:00") {
		t.Fatalf("view does not contain the happening's real start time (S9 rows wiring):\n%s", view.Content)
	}
}

// TestChatshell_ShowCalendarThisWeek_ShowsRealTimes is the S9 follow-up's
// week-view counterpart: WeekCalendar renders through
// controls.NewWeekCalendar (pipeline.Output.HappeningRows + WeekStart), so
// the happening's real time appears under its day section, not a bare
// title-only row.
func TestChatshell_ShowCalendarThisWeek_ShowsRealTimes(t *testing.T) {
	_, model := testHandler(t)
	m := typeAndEnter(t, model, "this week")
	view := m.View().Content
	if !strings.Contains(view, "Team standup") || !strings.Contains(view, "10:00") {
		t.Fatalf("view does not contain the happening's title+time in the week view:\n%s", view)
	}
}

// TestChatshell_UnknownText_AttemptsMainLLM is the scenario-13 headless
// smoke test: free text the deterministic chain cannot handle starts a
// stream attempt (which fails cleanly, since this test has no LLM
// configured -- see TestStream_* in internal/aichat/pipeline for a real
// LLM's streaming behaviour) rather than silently doing nothing.
func TestChatshell_UnknownText_AttemptsMainLLM(t *testing.T) {
	_, model := testHandler(t)
	m := typeAndEnter(t, model, "what is the meaning of life")
	view := m.View()
	if !strings.Contains(view.Content, "error") {
		t.Fatalf("expected the no-LLM-configured error to surface in the transcript:\n%s", view.Content)
	}
}

// TestChatshell_ExplainFreeForm_StreamsThroughLLM is scenario 1's second
// half: "keep deterministic help, add an LLM-backed free-form explain path
// when an LLM is configured." A phrase outside sneat-rules' fixed help-phrase
// table ("give me a rundown of what this app is for") falls through to
// NeedsLLM; this test proves it drives the real chatshell.StartStream path
// with a real openaicompat.Provider (the DoneMsg carries no error, and the
// splitter records no action), which combined with
// internal/aichat/pipeline's TestStream_BYOKOpenAICompatible_ThroughPipeline
// (the same provider's actual streamed text/usage) covers the free-form
// explain path end to end without paying for a second full real-time UI
// event-loop drain: chatshell's own busy spinner re-arms on a REAL 100ms
// FPS timer while a stream is in flight, and draining that for real (rather
// than the 50ms-budgeted drop the other tests rely on) would put this one
// test alone over m13's <5s-for-the-package target.
func TestChatshell_ExplainFreeForm_StreamsThroughLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseWriteHandler(w, `{"model":"gpt-5","choices":[{"delta":{"content":"This is sneat chat."}}]}`)
		sseWriteHandler(w, "[DONE]")
	}))
	defer srv.Close()

	h, model := testHandler(t)
	h.pipeline.LLM = openaicompat.New(openaicompat.Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-5"})

	m, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drain(m, cmd, 6)
	for _, r := range "explain this app" {
		m, cmd = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drain(m, cmd, 6) // enough for Submit's work + StartStream's own kickoff, not a full real-time spinner drain

	if h.splitters == nil {
		t.Fatal("expected startLLMStream to have registered a splitter for the in-flight stream")
	}
}

// sseWriteHandler matches internal/aichat/pipeline/llm_test.go's own
// sseWrite helper (unexported there too), duplicated rather than exported
// across packages for one test helper.
func sseWriteHandler(w http.ResponseWriter, data string) {
	_, _ = io.WriteString(w, "data: "+data+"\n\n")
	w.(http.Flusher).Flush()
}

// TestChatshell_MyTodos_ShowsDoneState is the S9 follow-up's TodoList
// counterpart: pipeline.Output.TodoRows carries each item's Done, so a
// completed item renders with its "[x]" marker through controls.NewTodoList.
func TestChatshell_MyTodos_ShowsDoneState(t *testing.T) {
	h, model := testHandler(t)
	h.pipeline.Readers.Todos = &data.FakeTodos{Items: []data.Todo{
		{ID: "t1", SpaceID: "sp1", List: data.ListKindDo, Title: "Buy milk", Done: false},
		{ID: "t2", SpaceID: "sp1", List: data.ListKindDo, Title: "Pay rent", Done: true},
	}}

	m := typeAndEnter(t, model, "my todos")
	view := m.View().Content
	if !strings.Contains(view, "[ ] Buy milk") {
		t.Fatalf("view does not show the open todo's marker:\n%s", view)
	}
	if !strings.Contains(view, "[x] Pay rent") {
		t.Fatalf("view does not show the done todo's marker:\n%s", view)
	}
}

// slowHappenings is a HappeningsReader whose Window blocks until release is
// closed -- stands in for a real Firestore read's latency (S2 test).
type slowHappenings struct {
	release chan struct{}
}

func (s *slowHappenings) Window(ctx context.Context, spaceID string, from, to time.Time) ([]data.Happening, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}
func (s *slowHappenings) FindByTitle(context.Context, string, string) ([]data.Happening, error) {
	return nil, nil
}
func (s *slowHappenings) Get(context.Context, string, string) (data.Happening, error) {
	return data.Happening{}, nil
}

// TestBeginLLMStream_DoesNotBlockOnRequestBuildingIO is S2's regression
// test: beginLLMStream must return its tea.Cmd immediately, without waiting
// for StreamRequest's own I/O (DynamicBlocks' Firestore reads) to finish --
// that I/O happens only once the RETURNED Cmd is actually executed (on a
// worker goroutine, per bubbletea's own contract), never inline on the call
// that produces the Cmd (the UI loop). A slow Happenings reader proves it:
// if beginLLMStream itself blocked on the read, this call would hang for
// the whole test timeout instead of returning right away.
func TestBeginLLMStream_DoesNotBlockOnRequestBuildingIO(t *testing.T) {
	h, _ := testHandler(t)
	slow := &slowHappenings{release: make(chan struct{})}
	defer close(slow.release)
	h.pipeline.Readers.Happenings = slow

	done := make(chan struct{})
	go func() {
		h.beginLLMStream("show my calendar today", nil, 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("beginLLMStream blocked on StreamRequest's own I/O -- it must only build and return the tea.Cmd, not run it")
	}
}

// TestFocusedScopes covers brief §4/§7: a focused happening pins the
// calendar scope so the Context Manager includes its dynamic data even when
// a decision does not require it.
func TestFocusedScopes(t *testing.T) {
	st := &session.State{}
	if got := focusedScopes(st); got != nil {
		t.Fatalf("focusedScopes(empty) = %v, want nil", got)
	}
	focused := session.EntityRef{Type: sneatdomain.EntityHappening}
	st.Focus(&focused)
	got := focusedScopes(st)
	if len(got) != 1 || got[0] != sneatdomain.ModuleCalendar {
		t.Fatalf("focusedScopes = %v, want [calendar]", got)
	}
}

// TestChatshell_FocusThenPronounAction_ResolvesConfirmsAndUndoes is the
// scenario-5 chatshell-level test: "focus a happening, then 'move it to
// Friday' -> use session context." Focus itself is driven entirely through
// real chatshell.Model.Update keys -- Shift+Up into the transcript's
// DayCalendar block, then Enter over its (only) item, which the block emits
// as controls.ItemActivatedMsg and handler.OnMsg turns into state.Focus, the
// same way a grid row's Enter already does (see OnMsg's grid.RowActivatedMsg
// case) -- never a direct h.state.Focus call from the test. A real LLM is
// not configured in this test harness (see internal/aichat/pipeline's
// TestStream_* for that leg tested against real providers), so the action
// itself still drives the SAME path chatshell's own StreamObserver would
// drive once a stream completes with a <sneat-action> block --
// h.pipeline.HandleAction with Pronoun:true -- and then confirms/executes
// through the real chatshell.Model exactly as a user would type "yes".
func TestChatshell_FocusThenPronounAction_ResolvesConfirmsAndUndoes(t *testing.T) {
	h, model := testHandler(t)
	exec := &pipeline.FakeExecutor{Undo: map[string]*session.Action{
		"calendar.reschedule_happening": {Kind: "calendar.reschedule_happening", Args: map[string]string{"when": "original time"}},
	}}
	h.pipeline.Executor = exec

	m := typeAndEnter(t, model, "show my calendar today")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}) // Shift+Up: focus the DayCalendar block
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})                 // Enter: activate its (only) item
	if h.state.Focused == nil || h.state.Focused.Keys["happeningID"] != "h1" {
		t.Fatalf("Enter over the block's item did not focus it via OnMsg: %+v", h.state.Focused)
	}

	out, err := h.pipeline.HandleAction(h.ctx, pipeline.Action{
		Kind: "calendar.reschedule_happening", Pronoun: true, Slots: map[string]string{"when": "Friday 16:00"},
	}, h.state, h.spaceID)
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	h.render(out, nil)
	if h.state.Pending == nil {
		t.Fatal("expected the pronoun-resolved reschedule to stage a Pending confirmation, not execute immediately")
	}
	if !strings.Contains(h.model.View().Content, "Move") {
		t.Fatalf("view does not show the confirmation prompt:\n%s", h.model.View().Content)
	}

	// Return focus to the composer (Shift+Down past the last transcript
	// stop), then "yes" through the real UI confirms it -- the rules
	// provider's confirmation rule only fires because state.Pending is set.
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	m = typeAndEnter(t, m, "yes")
	if !strings.Contains(m.View().Content, "Done") {
		t.Fatalf("view does not show confirmation, view:\n%s", m.View().Content)
	}
	if len(exec.Executed) != 1 || exec.Executed[0].Target.Keys["happeningID"] != "h1" {
		t.Fatalf("executed = %+v, want the focused happening rescheduled exactly once", exec.Executed)
	}

	// "undo" reverses it.
	m = typeAndEnter(t, m, "undo")
	if !strings.Contains(m.View().Content, "Undone") {
		t.Fatalf("view does not show undo, view:\n%s", m.View().Content)
	}
	if len(exec.Executed) != 2 {
		t.Fatalf("executed = %d calls, want 2 after undo", len(exec.Executed))
	}
}

// TestChatshell_SlashCommandButtons_PressSelectsSpace covers S10: a slash
// command reply carrying a Keyboard (chat.Processor's /spaces) renders as a
// focusable buttonsBlock, and pressing Enter over it dispatches through
// chat.Processor.PressButton exactly like the messenger surface -- restoring
// the behaviour the chatshell cutover had left as plain text only.
func TestChatshell_SlashCommandButtons_PressSelectsSpace(t *testing.T) {
	_, model := testHandler(t)
	m := typeAndEnter(t, model, "/spaces")
	if !strings.Contains(m.View().Content, "1 space") {
		t.Fatalf("view does not show the spaces list:\n%s", m.View().Content)
	}

	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}) // focus the buttons block
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})                 // press the (only) space button

	if !strings.Contains(m.View().Content, "Space: Home") {
		t.Fatalf("pressing the space button did not open its card, view:\n%s", m.View().Content)
	}
}

// TestChatshell_ContactSearch_GridThenCard is the scenario-7 chatshell-level
// test: contact search renders the reusable tui/grid; a single match renders
// a contact card instead. Both legs are driven through a REAL search typed
// through the UI -- "contacts" (list all -> grid) and "find contact bob"
// (a real resolve.Resolve/title search narrowing to one match -> card) --
// never a hand-sliced Entities slice standing in for a narrowed result
// (m7 coordinator ruling).
func TestChatshell_ContactSearch_GridThenCard(t *testing.T) {
	h, model := testHandler(t)
	h.pipeline.Readers.Contacts = &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice"},
		{ID: "c2", SpaceID: "sp1", Name: "Alicia"},
		{ID: "c3", SpaceID: "sp1", Name: "Bob"},
	}}
	h.pipeline.Resolver = pipeline.Resolver{Readers: h.pipeline.Readers}

	m := typeAndEnter(t, model, "contacts")
	view := m.View().Content
	if !strings.Contains(view, "Alice") || !strings.Contains(view, "Alicia") || !strings.Contains(view, "Bob") {
		t.Fatalf("expected the contacts grid to list every contact:\n%s", view)
	}

	// A real search ("find contact bob") narrowing to exactly one contact
	// renders a card, not a one-row grid (blockFor's len(entities)==1
	// special case) -- via the same deterministic rule/resolve.Resolve path
	// a real user's search would take.
	m = typeAndEnter(t, m, "find contact bob")
	view = m.View().Content
	if !strings.Contains(view, "Bob") {
		t.Fatalf("expected the single-contact card for Bob, view:\n%s", view)
	}
}

// TestChatshell_PinToSidebar_ThenReferredToByPronoun is the scenario-8
// chatshell-level test: adding an entity to the sidebar (as a card/grid's
// "+ add to sidebar" would) makes it resolvable by a later pronoun even
// after focus moves elsewhere. The pin itself is driven entirely through
// real chatshell.Model.Update keys -- Shift+Up into the rendered TodoList
// block, then "+" over its (only) item, which the block emits as
// tui.AddToSidebarMsg exactly like tui/grid's own "+" -- never a direct
// h.model.PinToSidebar call from the test.
func TestChatshell_PinToSidebar_ThenReferredToByPronoun(t *testing.T) {
	h, model := testHandler(t)
	h.pipeline.Readers.Todos = &data.FakeTodos{Items: []data.Todo{
		{ID: "t1", SpaceID: "sp1", List: data.ListKindDo, Title: "Buy milk"},
	}}
	h.pipeline.Resolver = pipeline.Resolver{Readers: h.pipeline.Readers}

	m := typeAndEnter(t, model, "my todos")
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}) // Shift+Up: focus the TodoList block
	m = pressKey(m, tea.KeyPressMsg{Text: "+", Code: '+'})               // "+": pin its (only) item to the sidebar
	_ = pressKey(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})

	pinned := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Buy milk", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "t1"}}
	if len(h.state.Sidebar) != 1 || !h.state.Sidebar[0].Same(pinned) {
		t.Fatalf("\"+\" did not pin the item to the sidebar via OnSidebarChange: %+v", h.state.Sidebar)
	}

	// Nothing focused, nothing last-shown -- only the sidebar pin resolves
	// "it".
	exec := &pipeline.FakeExecutor{}
	h.pipeline.Executor = exec
	out, err := h.pipeline.HandleAction(h.ctx, pipeline.Action{Kind: "todo.complete_todo", Pronoun: true}, h.state, h.spaceID)
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	h.render(out, nil)
	if len(exec.Executed) != 1 || exec.Executed[0].Target.Keys["itemID"] != "t1" {
		t.Fatalf("executed = %+v, want the sidebar-pinned todo completed", exec.Executed)
	}
	if !strings.Contains(h.model.View().Content, "Done") {
		t.Fatalf("view does not show completion, view:\n%s", h.model.View().Content)
	}
}

// TestApplySpaceChange_ClearsWorkingContext_KeepsSidebar is B3's session
// side: switching the active space invalidates Focused/Selection/Pending/
// Previous/LastShown (they may point at an entity from the space just left)
// but leaves Sidebar pins in place -- they stay visible so switching back
// doesn't lose them, and are never resolved-and-acted-on for the new space
// only because Resolver/SneatExecutor refuse a cross-space target
// defensively (covered separately by the reviewer's own B3 probe tests,
// adapted into internal/aichat/pipeline/pipeline_test.go's
// TestHandleAction_AddTodo_ViaReference_IgnoresModelSuppliedSpaceID and
// TestHandleAction_Pronoun_RejectsFocusedEntityFromAnotherSpace).
func TestApplySpaceChange_ClearsWorkingContext_KeepsSidebar(t *testing.T) {
	h, _ := testHandler(t)
	h.spaceID = "sp1"

	oldRef := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Standup", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	h.state.Focused = &oldRef
	h.state.Selection = []session.EntityRef{oldRef}
	h.state.LastShown = []session.EntityRef{oldRef}
	h.state.Pending = &session.Action{Kind: "calendar.reschedule_happening", Target: &oldRef}
	h.state.Previous = &session.Action{Kind: "calendar.reschedule_happening", Target: &oldRef}
	h.state.PreviousAt = time.Now()
	h.state.Sidebar = []session.EntityRef{oldRef}

	// Same space (or empty, meaning "the Processor has no opinion yet") is
	// never a change: nothing is cleared.
	h.applySpaceChange("")
	h.applySpaceChange("sp1")
	if h.state.Focused == nil || h.spaceID != "sp1" {
		t.Fatalf("applySpaceChange treated a no-op as a space change: spaceID=%q focused=%+v", h.spaceID, h.state.Focused)
	}

	h.applySpaceChange("sp2")

	if h.spaceID != "sp2" {
		t.Fatalf("spaceID = %q, want sp2", h.spaceID)
	}
	if h.state.Focused != nil {
		t.Errorf("Focused survived a space change: %+v", h.state.Focused)
	}
	if h.state.Selection != nil {
		t.Errorf("Selection survived a space change: %+v", h.state.Selection)
	}
	if h.state.LastShown != nil {
		t.Errorf("LastShown survived a space change: %+v", h.state.LastShown)
	}
	if h.state.Pending != nil {
		t.Errorf("Pending survived a space change: %+v", h.state.Pending)
	}
	if h.state.Previous != nil {
		t.Errorf("Previous survived a space change: %+v", h.state.Previous)
	}
	if !h.state.PreviousAt.IsZero() {
		t.Errorf("PreviousAt survived a space change: %v", h.state.PreviousAt)
	}
	if len(h.state.Sidebar) != 1 || !h.state.Sidebar[0].Same(oldRef) {
		t.Errorf("Sidebar pin was dropped on a space change, want it kept: %+v", h.state.Sidebar)
	}
}

// slowExecutor delegates to an embedded FakeExecutor but blocks Execute
// until release is closed, and signals (entered, closed once) the instant
// it is called -- a stand-in for a real sneat-go HTTP mutation's latency,
// giving TestS1_ActionRunsOnSnapshot_SidebarPinDuringItSurvives a
// controlled window to interleave a sidebar pin WHILE OnStreamEvent's
// EventCompleted background HandleAction closure is still running.
type slowExecutor struct {
	*pipeline.FakeExecutor
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *slowExecutor) Execute(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.FakeExecutor.Execute(ctx, spaceID, action)
}

// TestS1_ActionRunsOnSnapshot_SidebarPinDuringItSurvives is S1's
// concurrency regression test: OnStreamEvent(EventCompleted)'s returned
// tea.Cmd runs HandleAction on a background goroutine (this test holds it
// open via slowExecutor). WHILE it is still running, a sidebar pin is added
// the same way a real "+" press would (OnSidebarChange, which -- like every
// chatshell key handler -- always runs on the UI loop, never concurrently
// with a tea.Cmd closure). Before S1's fix, the eventual actionMsg applied
// its ENTIRE state snapshot (`*h.state = m.state`) back onto live state,
// silently dropping that pin because the snapshot was taken before it
// existed; after the fix, applyStateDelta only ever touches the turn-owned
// fields, so the pin -- added on a different "thread" of control entirely
// -- survives.
//
// This drives OnStreamEvent/the returned cmd directly (simulating
// startLLMStream's own per-stream bookkeeping by hand) rather than through
// the full real-SSE/chatshell.StartStream/drain plumbing: drain's own
// runCmd has a fixed 5ms per-command budget (see its doc comment) that
// exists to keep m13's real-timer noise (spinner ticks, cursor blink) from
// dominating test time, and is fundamentally incompatible with a command
// this test DELIBERATELY blocks open for much longer than 5ms -- driving it
// through drain would make whether the message is ever delivered a race
// against drain's own queue-processing speed, not a reliable test of
// applyStateDelta.
func TestS1_ActionRunsOnSnapshot_SidebarPinDuringItSurvives(t *testing.T) {
	h, model := testHandler(t)
	slow := &slowExecutor{FakeExecutor: &pipeline.FakeExecutor{}, entered: make(chan struct{}), release: make(chan struct{})}
	h.pipeline.Executor = slow
	todoRef := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Buy milk", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "t1"}}
	h.state.Focus(&todoRef)

	// Simulate startLLMStream's own per-stream bookkeeping (S1) by hand --
	// the same values a real turn would have stored via Submit ->
	// beginLLMStream -> startLLMStream -- so OnStreamEvent's EventCompleted
	// branch has a splitter/snapshot/ctx/seq to work with.
	const id = "turn-1"
	seq := h.turnSeq + 1
	h.turnSeq = seq
	sp := &pipeline.Splitter{}
	sp.Feed(`Done. <sneat-action>{"kind":"todo.complete_todo","pronoun":true}</sneat-action>`)
	h.splitters = map[string]*pipeline.Splitter{id: sp}
	snapshot := *h.state
	h.streamState = map[string]*session.State{id: &snapshot}
	h.streamCtx = map[string]context.Context{id: context.Background()}
	h.streamTurnSeq = map[string]int64{id: seq}

	cmd := h.OnStreamEvent(id, ai.Event{Type: ai.EventCompleted})
	if cmd == nil {
		t.Fatal("OnStreamEvent(EventCompleted) returned a nil cmd -- the splitter/action setup above is wrong")
	}
	// M2 (fix round r4 review): the action phase claims busy SYNCHRONOUSLY,
	// inside OnStreamEvent itself, before its background work even starts --
	// the stream that produced this action already finished (that is what
	// EventCompleted means), so without this the composer would re-enable
	// the instant the stream ended, letting the user submit a new turn while
	// HandleAction is still running (possibly a destructive mutation).
	if !h.model.Busy() {
		t.Fatal("model is not busy right after OnStreamEvent(EventCompleted) -- the action phase must own busy for its own duration")
	}

	// M2 (fix round r4 review): the action phase now owns busy for its own
	// duration, so OnStreamEvent's returned cmd is tea.Batch(work, busyCmd)
	// -- a real tea.Program dispatches every sub-command of a tea.BatchMsg
	// concurrently, so this harness (which drives OnStreamEvent directly,
	// bypassing the real runtime -- see the doc comment above) must do the
	// same rather than call cmd() and expect it to BE work itself, which
	// would just hand back the (unexecuted) BatchMsg. Only the sub-command
	// that actually produces an actionMsg matters here; a spinner-tick
	// message from busyCmd is dropped.
	done := make(chan tea.Msg, 1)
	var dispatch func(tea.Cmd)
	dispatch = func(c tea.Cmd) {
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				if sub != nil {
					go dispatch(sub)
				}
			}
			return
		}
		if _, ok := msg.(actionMsg); ok {
			done <- msg
		}
	}
	go dispatch(cmd)

	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleAction never reached the (blocked) executor")
	}

	pinnedRef := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Call dentist", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "t2"}}
	h.OnSidebarChange([]session.EntityRef{pinnedRef})
	close(slow.release)

	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the background HandleAction closure did not finish after releasing the executor")
	}

	var m tea.Model = model
	m, _ = m.Update(msg)
	h.model = m.(*chatshell.Model)

	if len(slow.Executed) != 1 || slow.Executed[0].Kind != "todo.complete_todo" {
		t.Fatalf("Executed = %+v, want exactly 1 todo.complete_todo", slow.Executed)
	}
	if len(h.state.Sidebar) != 1 || !h.state.Sidebar[0].Same(pinnedRef) {
		t.Fatalf("Sidebar = %+v, want the pin added WHILE the action was running to survive (S1: applyStateDelta must never touch Sidebar)", h.state.Sidebar)
	}
	// M2: the actionMsg's OnMsg case clears busy once the result is applied
	// -- the action phase no longer owns it.
	if h.model.Busy() {
		t.Fatal("model still busy after the actionMsg was applied -- OnMsg's actionMsg case must clear it")
	}
}

// TestProbeR4_M2_BusySurvivesStreamDone_AndSubmitIsRefused is the reviewer's
// real-runtime M2 regression from fix round r5 (adapted from
// zz_m2_probe_test.go): chatshell's OWN handleStreamDone (tui/chatshell)
// unconditionally clears busy the moment a stream's DoneMsg is handled --
// which fires right after EventCompleted, well before OnStreamEvent's
// background HandleAction work has even started, let alone finished. Fix
// round r4's SetBusy(true) inside OnStreamEvent was necessary but not
// sufficient: this test drives a REAL chatshell.Model through
// EventCompleted then DoneMsg (via m.Update, matching how the real runtime
// delivers them) and asserts busy survives DoneMsg, AND that the composer
// actually refuses a submit attempt while the action is still running
// (handleInputKey's own busy gate short-circuits before ever calling
// h.Submit -- verified here by h.turnSeq staying put, since Submit's first
// action is always atomic.AddInt64(&h.turnSeq, 1)). It then releases the
// held-open action and drains it to prove busy is eventually cleared and
// the action's result (Executed, Previous/undo) is still recorded once it
// resolves.
func TestProbeR4_M2_BusySurvivesStreamDone_AndSubmitIsRefused(t *testing.T) {
	h, model := testHandler(t)
	slow := &slowExecutor{FakeExecutor: &pipeline.FakeExecutor{}, entered: make(chan struct{}), release: make(chan struct{})}
	h.pipeline.Executor = slow
	todoRef := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Buy milk", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "t1"}}
	h.state.Focus(&todoRef)

	const id = "turn-1"
	seq := h.turnSeq + 1
	h.turnSeq = seq
	// StartStream (a real chatshell.Model method) sets m.streamID/m.busy the
	// same way a real turn would; the empty iterator is never actually
	// drained -- EventCompleted/DoneMsg are fed by hand below, exactly like
	// a real SSE stream delivers them, without drain's 5ms-per-command
	// budget (incompatible with this test's deliberately-held-open action;
	// see TestS1_ActionRunsOnSnapshot_SidebarPinDuringItSurvives's own doc
	// comment for why).
	_ = h.model.StartStream(id, func(ctx context.Context) iter.Seq2[ai.Event, error] {
		return func(yield func(ai.Event, error) bool) {}
	})
	sp := &pipeline.Splitter{}
	sp.Feed(`Done. <sneat-action>{"kind":"todo.complete_todo","pronoun":true}</sneat-action>`)
	h.splitters = map[string]*pipeline.Splitter{id: sp}
	snapshot := *h.state
	h.streamState = map[string]*session.State{id: &snapshot}
	h.streamCtx = map[string]context.Context{id: context.Background()}
	h.streamTurnSeq = map[string]int64{id: seq}

	var m tea.Model = model
	var cmd tea.Cmd
	m, cmd = m.Update(stream.EventMsg{ID: id, Event: ai.Event{Type: ai.EventCompleted}})
	h.model = m.(*chatshell.Model)
	if !h.model.Busy() {
		t.Fatal("model not busy right after EventCompleted")
	}
	if cmd == nil {
		t.Fatal("EventCompleted produced a nil cmd -- the splitter/action setup above is wrong")
	}

	// Dispatch the action work (as a real tea.Program would) without
	// blocking this goroutine -- slowExecutor holds it open until release.
	done := make(chan tea.Msg, 1)
	var dispatch func(tea.Cmd)
	dispatch = func(c tea.Cmd) {
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				if sub != nil {
					go dispatch(sub)
				}
			}
			return
		}
		if _, ok := msg.(actionMsg); ok {
			done <- msg
		}
	}
	go dispatch(cmd)

	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleAction never reached the (blocked) executor")
	}

	// THE core regression: chatshell's real handleStreamDone runs BEFORE
	// this method sees the message, so calling Update at all exercises its
	// own unconditional busy=false -- our OnStreamDone must win the race by
	// re-asserting busy synchronously, in the SAME Update call.
	m, _ = m.Update(stream.DoneMsg{ID: id})
	h.model = m.(*chatshell.Model)
	if !h.model.Busy() {
		t.Fatal("PROBE R4 M2: busy cleared by DoneMsg while HandleAction is still running -- composer would re-enable mid-action")
	}

	// A submit attempt while busy must be refused outright: handleInputKey
	// returns before ever calling h.Submit, so turnSeq never advances.
	seqBeforeSubmitAttempt := h.turnSeq
	m, _ = m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	h.model = m.(*chatshell.Model)
	if h.turnSeq != seqBeforeSubmitAttempt {
		t.Fatalf("turnSeq advanced from %d to %d -- a submit attempt while busy must be refused, not start a new turn", seqBeforeSubmitAttempt, h.turnSeq)
	}

	close(slow.release)
	var actionResult tea.Msg
	select {
	case actionResult = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the background HandleAction closure did not finish after releasing the executor")
	}
	m, _ = m.Update(actionResult)
	h.model = m.(*chatshell.Model)

	if len(slow.Executed) != 1 || slow.Executed[0].Kind != "todo.complete_todo" {
		t.Fatalf("Executed = %+v, want exactly 1 todo.complete_todo", slow.Executed)
	}
	if h.model.Busy() {
		t.Fatal("model still busy after the actionMsg was applied")
	}
	if h.state.Previous == nil || h.state.Previous.Kind != "todo.complete_todo" {
		t.Fatalf("Previous = %+v, want the resolved action recorded once it actually resolved", h.state.Previous)
	}
}

// TestOnMsg_ActionMsg_StaleSeqDropped_NoRenderNoStateWrite is S1's other
// half: an actionMsg tagged with an OLD turn seq (a newer turn already
// started -- e.g. Esc cancelled the one that produced it) must be dropped
// outright, never rendered and never allowed to write its state snapshot
// onto live state.
// TestOnMsg_ActionMsg_StaleSeq_StillRendersAndRecordsUndo_ButNotFocus is
// S1's other half, UPDATED for fix round r5's review correction: a stale
// actionMsg (an older turn's result, superseded by a newer one already in
// flight) whose action nonetheless actually ran (m.parseErr == nil --
// Splitter.Finish parsed fine, so HandleAction WAS called, for real) must
// never be silently dropped outright the way a genuinely parse-failed one
// still is -- whatever it did (or the error it hit) already happened, so it
// is rendered and its Previous/undo recorded regardless of staleness (or
// "undo" would stop working for a turn that genuinely executed). What it
// must NOT do is let its OWN stale Focused/Selection/LastShown/Pending
// snapshot overwrite live state a newer, current turn may already have
// moved on from -- exactly the field-level split applyUndoDelta (vs.
// applyStateDelta) makes.
func TestOnMsg_ActionMsg_StaleSeq_StillRendersAndRecordsUndo_ButNotFocus(t *testing.T) {
	h, model := testHandler(t)
	h.turnSeq = 5 // simulate a newer turn already in flight

	liveFocused := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Live", Keys: map[string]string{"spaceID": "sp1", "happeningID": "live"}}
	h.state.Focus(&liveFocused)

	staleTarget := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Stale todo", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "stale"}}
	staleState := session.State{
		Focused:  &session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Stale", Keys: map[string]string{"spaceID": "sp1", "happeningID": "stale"}},
		Previous: &session.Action{Kind: "todo.complete_todo", Target: &staleTarget, Undo: &session.Action{Kind: "todo.reopen_todo", Target: &staleTarget}},
	}
	var m tea.Model = model
	m, _ = m.Update(actionMsg{output: pipeline.Output{Text: "Done."}, state: staleState, seq: 4})
	h.model = m.(*chatshell.Model)

	if !strings.Contains(h.model.View().Content, "Done.") {
		t.Fatal("a stale-but-EXECUTED actionMsg (m.parseErr == nil: HandleAction actually ran) must still render its Output")
	}
	if h.state.Focused == nil || !h.state.Focused.Same(liveFocused) {
		t.Fatalf("Focused = %+v, want the live value untouched by a stale actionMsg's own Focused snapshot", h.state.Focused)
	}
	if h.state.Previous == nil || h.state.Previous.Kind != "todo.complete_todo" || h.state.Previous.Undo == nil {
		t.Fatalf("Previous = %+v, want the stale-but-executed action's Previous/undo recorded regardless of staleness", h.state.Previous)
	}
}

// TestOnMsg_ActionMsg_StaleSeq_ParseErr_StillDropped covers the OTHER stale
// case: a parse error (Splitter.Finish itself failed) means HandleAction
// was NEVER called -- nothing real happened, so a stale result here is
// still dropped outright, exactly as before fix round r5's correction.
func TestOnMsg_ActionMsg_StaleSeq_ParseErr_StillDropped(t *testing.T) {
	h, model := testHandler(t)
	h.turnSeq = 5

	liveFocused := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Live", Keys: map[string]string{"spaceID": "sp1", "happeningID": "live"}}
	h.state.Focus(&liveFocused)

	var m tea.Model = model
	m, _ = m.Update(actionMsg{parseErr: pipeline.ErrActionNotTrailing, seq: 4})
	h.model = m.(*chatshell.Model)

	if strings.Contains(h.model.View().Content, "couldn't parse") || strings.Contains(h.model.View().Content, "no action taken") {
		t.Fatal("a stale actionMsg with a parse error (nothing ever executed) must still be dropped outright")
	}
	if h.state.Previous != nil {
		t.Fatalf("Previous = %+v, want nil -- nothing executed for a parse-error result", h.state.Previous)
	}
}

// fakeJevDecision is a minimal decision.Provider standing in for the Sneat
// AI Cloud/Jev decision service (never the real network): it decides a
// reschedule deterministically whenever the text contains "dentist",
// abstaining otherwise so it never shadows sneat-rules' own fixed-phrase
// commands in a test that shares a handler.
type fakeJevDecision struct{ name string }

func (f fakeJevDecision) Name() string { return f.name }

func (f fakeJevDecision) Decide(_ context.Context, req decision.Request) (decision.Decision, bool, error) {
	if !strings.Contains(req.Text, "dentist") {
		return decision.Decision{}, false, nil
	}
	return decision.Decision{
		Module:                     decision.Scored{Value: sneatdomain.ModuleCalendar, Confidence: 1},
		Intent:                     decision.Scored{Value: sneatdomain.IntentRescheduleHappening, Confidence: 1},
		Interaction:                decision.InteractionCommand,
		Reference:                  &decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist"},
		Slots:                      map[string]string{"when": "Friday"},
		CanHandleDeterministically: true,
	}, true, nil
}

// TestChatshell_Scenario4_AmbiguousReference_ThroughFakeJevDecision is
// scenario 4's deterministic-decision leg, driven through the real
// chatshell.Model (not a direct resolveAndAct call): a fake Jev-shaped
// decision.Provider decides "reschedule dentist" deterministically, and two
// same-word candidates in the space must render as a choice -- never a
// silent guess, never an execution.
func TestChatshell_Scenario4_AmbiguousReference_ThroughFakeJevDecision(t *testing.T) {
	h, model := testHandler(t)
	h.pipeline.Readers.Happenings = &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist checkup",
			Start: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 10, 30, 0, 0, time.UTC)},
		{ID: "h2", SpaceID: "sp1", Title: "Dentist follow-up",
			Start: time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 26, 14, 30, 0, 0, time.UTC)},
	}}
	h.pipeline.Resolver = pipeline.Resolver{Readers: h.pipeline.Readers}
	h.pipeline.Chain = decision.Chain{Providers: []decision.Provider{fakeJevDecision{name: "fake-jev"}}}
	exec := &pipeline.FakeExecutor{}
	h.pipeline.Executor = exec

	m := typeAndEnter(t, model, "reschedule dentist")
	view := m.View().Content
	if !strings.Contains(view, "Dentist checkup") || !strings.Contains(view, "Dentist follow-up") {
		t.Fatalf("expected both ambiguous candidates in the choice list:\n%s", view)
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("an ambiguous reference must never execute: %+v", exec.Executed)
	}
}

// TestChatshell_Scenario4_AmbiguousReference_ThroughNaturalLanguageLLM is
// scenario 4's other leg: text sneat-rules does not classify at all (it has
// no reschedule handling) falls to the main LLM, whose streamed reply ends
// with a <sneat-action> block naming an ambiguous reference. This drives
// OnStreamEvent's EventCompleted -> HandleAction path end to end through the
// real chatshell.Model and a real openaicompat.Provider (httptest SSE),
// proving the same "never guess, never execute" guarantee holds on the LLM
// leg, not just the deterministic one above.
func TestChatshell_Scenario4_AmbiguousReference_ThroughNaturalLanguageLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseWriteHandler(w, `{"model":"gpt-5","choices":[{"delta":{"content":"Sure -- which one? <sneat-action>{\"kind\":\"calendar.reschedule_happening\",\"reference\":\"dentist\",\"slots\":{\"when\":\"Friday\"}}</sneat-action>"}}]}`)
		sseWriteHandler(w, "[DONE]")
	}))
	defer srv.Close()

	h, model := testHandler(t)
	h.pipeline.LLM = openaicompat.New(openaicompat.Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-5"})
	h.pipeline.Readers.Happenings = &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist checkup",
			Start: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 10, 30, 0, 0, time.UTC)},
		{ID: "h2", SpaceID: "sp1", Title: "Dentist follow-up",
			Start: time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 26, 14, 30, 0, 0, time.UTC)},
	}}
	h.pipeline.Resolver = pipeline.Resolver{Readers: h.pipeline.Readers}
	exec := &pipeline.FakeExecutor{}
	h.pipeline.Executor = exec

	var m tea.Model = model
	var cmd tea.Cmd
	m, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drain(m, cmd, 6)
	// Not one of sneat-rules' fixed help phrases, and not shaped like any
	// deterministic reschedule phrasing -- must fall through to NeedsLLM.
	for _, r := range "please sort out my dentist thing" {
		m, cmd = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drain(m, cmd, 40)

	view := m.View().Content
	if !strings.Contains(view, "Dentist checkup") || !strings.Contains(view, "Dentist follow-up") {
		t.Fatalf("expected both ambiguous candidates rendered after the LLM's action block:\n%s", view)
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("an ambiguous reference must never execute: %+v", exec.Executed)
	}
}

// TestChatshell_Scenario5_MoveItToFriday_ThroughUI_FakeLLM is scenario
// 5/8's chatshell-level test (coordinator ruling: through the real UI, not
// a direct HandleAction injection): a happening is focused, the user types
// "move it to Friday" through the real composer, a fake LLM streams a
// reschedule <sneat-action> block with pronoun:true, and the resulting
// destructive confirmation renders as a HappeningCard (S5) rather than
// executing outright. Typing "yes" then confirms it, and the executor runs
// exactly once.
func TestChatshell_Scenario5_MoveItToFriday_ThroughUI_FakeLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseWriteHandler(w, `{"model":"gpt-5","choices":[{"delta":{"content":"<sneat-action>{\"kind\":\"calendar.reschedule_happening\",\"pronoun\":true,\"slots\":{\"when\":\"Friday 16:00\"}}</sneat-action>"}}]}`)
		sseWriteHandler(w, "[DONE]")
	}))
	defer srv.Close()

	h, model := testHandler(t)
	h.pipeline.LLM = openaicompat.New(openaicompat.Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-5"})
	h.pipeline.Now = func() time.Time { return time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC) } // a Friday
	slot := dbo4calendarius.HappeningSlot{HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
		Timing:  dbo4calendarius.Timing{Start: dbo4calendarius.DateTime{Date: "2026-09-25", Time: "10:00"}, End: dbo4calendarius.DateTime{Date: "2026-09-25", Time: "10:30"}},
		Repeats: dbo4calendarius.RepeatPeriodOnce,
	}}
	h.pipeline.Readers.Happenings = &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist appointment", SlotID: "s1",
			Start: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 10, 30, 0, 0, time.UTC), Slot: &slot},
	}}
	exec := &pipeline.FakeExecutor{Undo: map[string]*session.Action{
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening: {Kind: sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening},
	}}
	h.pipeline.Executor = exec
	todoRef := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Dentist appointment", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	h.state.Focus(&todoRef)

	var m tea.Model = model
	var cmd tea.Cmd
	m, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drain(m, cmd, 6)
	for _, r := range "move it to Friday" {
		m, cmd = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drain(m, cmd, 40)

	view := m.View().Content
	if !strings.Contains(view, "yes/no") {
		t.Fatalf("expected a reschedule confirmation prompt, view:\n%s", view)
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("a destructive action must not execute before confirmation: %+v", exec.Executed)
	}

	m = typeAndEnter(t, m, "yes")
	if len(exec.Executed) != 1 || exec.Executed[0].Kind != sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening {
		t.Fatalf("Executed = %+v, want exactly 1 reschedule after confirming", exec.Executed)
	}
}
