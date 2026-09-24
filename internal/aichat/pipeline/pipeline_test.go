package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

func fixedNow() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) } // a Friday

func newTestPipeline(exec Executor) (Pipeline, *session.State) {
	readers := testReaders()
	return Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers},
		Executor: exec,
		Readers:  readers,
		Now:      fixedNow,
	}, &session.State{}
}

// TestTurn_ShowDay_NoMainLLM covers scenario 2: "show my calendar today" is
// answered deterministically, with Decision.NeedsLLM left false.
func TestTurn_ShowDay_NoMainLLM(t *testing.T) {
	p, st := newTestPipeline(nil)
	out, err := p.Turn(context.Background(), "show my calendar today", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected no main-LLM call for a deterministic show_day")
	}
	if out.Presentation != sneatdomain.PresentationDayCalendar {
		t.Fatalf("presentation = %q", out.Presentation)
	}
	if len(st.LastShown) != 1 || st.LastShown[0].Keys["happeningID"] != "h3" {
		t.Fatalf("LastShown = %+v, want just the standup (h1/h2 are not today)", st.LastShown)
	}
}

func TestTurn_ShowWeek(t *testing.T) {
	p, st := newTestPipeline(nil)
	out, err := p.Turn(context.Background(), "this week", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.Presentation != sneatdomain.PresentationWeekCalendar {
		t.Fatalf("presentation = %q", out.Presentation)
	}
	if len(out.Entities) != 2 {
		t.Fatalf("entities = %+v, want the 2 happenings in this week's window (h1, h3) -- h2 is next week", out.Entities)
	}
}

func TestTurn_UnknownText_NeedsLLM(t *testing.T) {
	p, st := newTestPipeline(nil)
	out, err := p.Turn(context.Background(), "what is the meaning of life", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if !out.NeedsLLM {
		t.Fatal("expected NeedsLLM for text the deterministic chain cannot handle")
	}
}

// TestTurn_PendingConfirmCancelUndo covers scenario 9 (deterministic yes/
// cancel) end to end: a destructive action is staged as Pending, "yes"
// executes it and records Previous with Undo, and a later "undo" reverses it.
func TestTurn_PendingConfirmCancelUndo(t *testing.T) {
	undo := &session.Action{Kind: sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening}
	exec := &FakeExecutor{Undo: map[string]*session.Action{
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening: undo,
	}}
	p, st := newTestPipeline(exec)

	// A destructive action goes to Pending, not straight to the executor.
	out, err := p.resolveAndAct(context.Background(),
		sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening,
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "standup"},
		map[string]string{"when": "Friday 16:00"}, st, "sp1")
	if err != nil {
		t.Fatalf("resolveAndAct: %v", err)
	}
	if st.Pending == nil {
		t.Fatal("expected a Pending action for a destructive kind")
	}
	if len(exec.Executed) != 0 {
		t.Fatal("must not execute before confirmation")
	}
	if out.Text == "" {
		t.Fatal("expected a confirmation prompt")
	}

	// "yes" runs it.
	out, err = p.Turn(context.Background(), "yes", st, "sp1")
	if err != nil {
		t.Fatalf("Turn(yes): %v", err)
	}
	if st.Pending != nil {
		t.Fatal("Pending must clear after confirmation")
	}
	if len(exec.Executed) != 1 {
		t.Fatalf("executed = %d, want 1", len(exec.Executed))
	}
	if st.Previous == nil || st.Previous.Undo == nil {
		t.Fatal("expected Previous with Undo set after execution")
	}
	if out.Text != "Done." {
		t.Fatalf("out.Text = %q", out.Text)
	}

	// "undo" reverses it.
	out, err = p.Turn(context.Background(), "undo", st, "sp1")
	if err != nil {
		t.Fatalf("Turn(undo): %v", err)
	}
	if len(exec.Executed) != 2 {
		t.Fatalf("executed = %d, want 2 after undo", len(exec.Executed))
	}
	if st.Previous != nil {
		t.Fatal("Previous must clear after undo")
	}
	if out.Text != "Undone." {
		t.Fatalf("out.Text = %q", out.Text)
	}
}

func TestTurn_CancelPending(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	st.Pending = &session.Action{Kind: "calendar.cancel_happening", Summary: "Cancel \"Standup\"?"}

	out, err := p.Turn(context.Background(), "cancel that", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if st.Pending != nil {
		t.Fatal("Pending must clear on cancellation")
	}
	if len(exec.Executed) != 0 {
		t.Fatal("a cancelled action must never execute")
	}
	if out.Text != "Cancelled." {
		t.Fatalf("out.Text = %q", out.Text)
	}
}

// TestTurn_AmbiguousReferenceOffersChoice covers scenario 4/5's ambiguous
// case: several matches must produce a choice, never a guess.
func TestTurn_AmbiguousReferenceOffersChoice(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	out, err := p.resolveAndAct(context.Background(),
		sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening,
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist"},
		nil, st, "sp1")
	if err != nil {
		t.Fatalf("resolveAndAct: %v", err)
	}
	if len(out.Entities) != 2 {
		t.Fatalf("expected 2 ambiguous candidates, got %+v", out.Entities)
	}
	if st.Pending != nil {
		t.Fatal("must not stage a Pending action when the reference is ambiguous")
	}
	if len(exec.Executed) != 0 {
		t.Fatal("must not execute an ambiguous reference")
	}
	if len(st.LastShown) != 2 {
		t.Fatalf("LastShown = %+v, want the 2 ambiguous candidates so a later \"2\" can pick one (S3)", st.LastShown)
	}
}

// TestTurn_PickByNumber_AfterAmbiguousChoice covers S3 end to end: an
// ambiguous reference presents a choice, and a bare "2" (nothing the
// deterministic rules chain classifies) focuses the second option instead
// of falling through to the main LLM.
func TestTurn_PickByNumber_AfterAmbiguousChoice(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	out, err := p.resolveAndAct(context.Background(),
		sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening,
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist"},
		nil, st, "sp1")
	if err != nil {
		t.Fatalf("resolveAndAct: %v", err)
	}
	second := out.Entities[1]

	out, err = p.Turn(context.Background(), "2", st, "sp1")
	if err != nil {
		t.Fatalf("Turn(2): %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected \"2\" to be handled deterministically, not routed to the main LLM")
	}
	if st.Focused == nil || !st.Focused.Same(second) {
		t.Fatalf("Focused = %+v, want the second shown candidate %+v", st.Focused, second)
	}
}

// TestNoJevParity runs a representative scenario set through a chain that
// contains ONLY the deterministic provider -- standing in for `--no-jev`,
// which the CLI wires as "do not add the cloud decision provider to the
// chain" (see internal/aichat/aiconfig). Every one of these must still work.
func TestNoJevParity(t *testing.T) {
	p, st := newTestPipeline(&FakeExecutor{})
	texts := []string{"show my calendar today", "this week", "my todos", "contacts", "help"}
	for _, text := range texts {
		out, err := p.Turn(context.Background(), text, st, "sp1")
		if err != nil {
			t.Fatalf("Turn(%q): %v", text, err)
		}
		if out.NeedsLLM {
			t.Errorf("Turn(%q): expected deterministic handling with no cloud decision provider", text)
		}
	}
}

// TestHandleAction_MainLLMAction covers the <sneat-action> convention: a
// main-LLM answer's action block is resolved and executed the same way a
// deterministic command is.
func TestHandleAction_MainLLMAction(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	kind := sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo
	out, err := p.HandleAction(context.Background(), Action{Kind: kind, Reference: "milk"}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if out.Text != "Done." {
		t.Fatalf("out.Text = %q", out.Text)
	}
	if len(exec.Executed) != 1 || exec.Executed[0].Target.Keys["itemID"] != "t1" {
		t.Fatalf("executed = %+v", exec.Executed)
	}
}

// TestHandleAction_AddTodo_IgnoresModelSuppliedSpaceID is the B3 regression:
// an add_todo/add_to_buy action has no resolved Target (nothing to look up),
// so the space it runs in came from action.Args["spaceID"] -- model-
// controlled. Every action must execute in the pipeline's OWN space; a
// model-forged spaceID (e.g. from a prompt-injected happening title) must
// never leak through.
func TestHandleAction_AddTodo_IgnoresModelSuppliedSpaceID(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	kind := sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo
	_, err := p.HandleAction(context.Background(), Action{
		Kind:  kind,
		Slots: map[string]string{"title": "Buy milk", "spaceID": "attacker-space"},
	}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if len(exec.Executed) != 1 {
		t.Fatalf("executed = %+v, want 1", exec.Executed)
	}
	if got := exec.Executed[0].Args["spaceID"]; got != "sp1" {
		t.Fatalf("spaceID = %q, want the pipeline's own space sp1 (model-supplied spaceID must be discarded)", got)
	}
}
