package chatapp

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/strongo/aichat/ai/decision"
	aidiag "github.com/strongo/aichat/ai/diag"
	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/chatshell"

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
// deterministic path never needs one).
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
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})   // Shift+Up: focus the DayCalendar block
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})                  // Enter: activate its (only) item
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
	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})                // press the (only) space button

	if !strings.Contains(m.View().Content, "Space: Home") {
		t.Fatalf("pressing the space button did not open its card, view:\n%s", m.View().Content)
	}
}

// TestChatshell_ContactSearch_GridThenCard is the scenario-7 chatshell-level
// test: contact search renders the reusable tui/grid; a single match renders
// a contact card instead.
func TestChatshell_ContactSearch_GridThenCard(t *testing.T) {
	h, model := testHandler(t)
	h.pipeline.Readers.Contacts = &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice"},
		{ID: "c2", SpaceID: "sp1", Name: "Alicia"},
	}}
	h.pipeline.Resolver = pipeline.Resolver{Readers: h.pipeline.Readers}

	m := typeAndEnter(t, model, "contacts")
	view := m.View().Content
	if !strings.Contains(view, "Alice") || !strings.Contains(view, "Alicia") {
		t.Fatalf("expected the contacts grid to list both contacts:\n%s", view)
	}

	// A resolver search narrowing to exactly one contact renders a card, not
	// a one-row grid (blockFor's len(entities)==1 special case).
	out, err := h.pipeline.Turn(h.ctx, "contacts", h.state, h.spaceID)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	out.Entities = out.Entities[:1] // simulate a search narrowed to one match
	h.render(out, nil)
	if !strings.Contains(h.model.View().Content, "Alice") {
		t.Fatalf("expected the single-contact card, view:\n%s", h.model.View().Content)
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
