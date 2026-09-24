package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/controls"
	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

func fixedNow() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) } // a Friday

func newTestPipeline(exec Executor) (Pipeline, *session.State) {
	readers := testReaders()
	return Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers, Now: fixedNow},
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

// TestTurn_ShowWeek_ProjectsBothRecurringOccurrences is S3: a Mon/Fri
// recurring happening in a week view produces TWO rows, each carrying its
// own projected occurrence date -- not the template's single stale date
// (adapted from the coordinator's probe TestProbe_WeekCalendarShowsRecurring).
func TestTurn_ShowWeek_ProjectsBothRecurringOccurrences(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t) // Monday 2026-09-21
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}}
	p := Pipeline{Readers: readers, Now: func() time.Time { return now }}
	st := &session.State{}
	out, err := p.showWeek(context.Background(), st, "sp1")
	if err != nil {
		t.Fatalf("showWeek: %v", err)
	}
	if len(out.HappeningRows) != 2 {
		t.Fatalf("HappeningRows = %+v, want 2 (Monday + Friday occurrences)", out.HappeningRows)
	}
	wantDates := []string{"2026-09-21", "2026-09-25"}
	for i, want := range wantDates {
		if got := out.HappeningRows[i].Start.Format("2006-01-02"); got != want {
			t.Errorf("HappeningRows[%d].Start = %s, want %s", i, got, want)
		}
	}
	view := controls.NewWeekCalendar("This week", out.WeekStart, out.HappeningRows).View(0, false)
	if !strings.Contains(view, "Yoga") {
		t.Errorf("WeekCalendar view = %q, want it to render Yoga", view)
	}
}

// TestTurn_ShowUpcoming_RecurringShowsOnlyNextOccurrence is S3's "upcoming
// shows next occurrence date": a recurring happening contributes ONE row
// (its next occurrence), not one per matching day across the whole month
// window.
func TestTurn_ShowUpcoming_RecurringShowsOnlyNextOccurrence(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}}
	p := Pipeline{Readers: readers, Now: func() time.Time { return now }}
	st := &session.State{}
	out, err := p.showUpcoming(context.Background(), st, "sp1")
	if err != nil {
		t.Fatalf("showUpcoming: %v", err)
	}
	if len(out.HappeningRows) != 1 {
		t.Fatalf("HappeningRows = %+v, want exactly 1 (next occurrence only)", out.HappeningRows)
	}
	if got := out.HappeningRows[0].Start.Format("2006-01-02"); got != "2026-09-21" {
		t.Errorf("HappeningRows[0].Start = %s, want 2026-09-21 (today, the NEXT occurrence)", got)
	}
}

// TestTurn_ShowDay_RecurringOnlyOnMatchingWeekday is S6: a weekly-recurring
// happening appears in a day view only on a day its Weekdays rule hits, not
// on every day just because it "is recurring".
func TestTurn_ShowDay_RecurringOnlyOnMatchingWeekday(t *testing.T) {
	fridayYoga := dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing:   dbo4calendarius.Timing{Start: dbo4calendarius.DateTime{Date: "2026-09-18", Time: "09:00"}},
			Repeats:  dbo4calendarius.RepeatPeriodWeekly,
			Weekdays: []dbo4calendarius.WeekdayCode{dbo4calendarius.Friday2},
		},
	}
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "yoga", SpaceID: "sp1", Title: "Yoga", SlotID: "s1", Recurring: true, Slot: &fridayYoga},
	}}}
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers, Now: fixedNow},
		Readers:  readers,
		Now:      fixedNow, // fixedNow is a Friday
	}

	out, err := p.Turn(context.Background(), "show my calendar today", &session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(out.Entities) != 1 || out.Entities[0].Keys["happeningID"] != "yoga" {
		t.Fatalf("Friday: Entities = %+v, want the Friday yoga class", out.Entities)
	}

	// Advance "now" to a Monday: the same recurring happening must NOT show.
	monday := func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }
	p.Now, p.Resolver.Now = monday, monday
	out, err = p.Turn(context.Background(), "show my calendar today", &session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(out.Entities) != 0 {
		t.Fatalf("Monday: Entities = %+v, want none (yoga only recurs on Fridays)", out.Entities)
	}
}

// TestShowDay_HonoursWhenSlot is m1: a decision's "when" slot (e.g. from a
// future rule/Jev/LLM decision for "show my calendar tomorrow") selects the
// day shown, not always today.
func TestShowDay_HonoursWhenSlot(t *testing.T) {
	p, st := newTestPipeline(nil) // fixedNow is Friday 2026-09-25; h1 is Sep 26 (tomorrow)
	out, err := p.showDay(context.Background(), st, "sp1", "tomorrow")
	if err != nil {
		t.Fatalf("showDay: %v", err)
	}
	if len(out.Entities) != 1 || out.Entities[0].Keys["happeningID"] != "h1" {
		t.Fatalf("Entities = %+v, want h1 (Sep 26, resolved from the \"tomorrow\" slot)", out.Entities)
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
	// m10: an undoable action's Output.Text carries a discoverable hint,
	// not just a bare "Done.".
	if !strings.HasPrefix(out.Text, "Done.") || !strings.Contains(out.Text, "undo") {
		t.Fatalf("out.Text = %q, want a \"Done.\" answer with an undo hint", out.Text)
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

// TestResolveAndAct_Reschedule_ConfirmationShowsResolvedDateTimeTZ is
// scenario 4 end to end with realistic data (S4/B2 ruling): resolving "my
// dentist appointment tomorrow" and rescheduling it "to 4" must show the
// CONFIRMATION with the fully resolved date, 16:00 (not "4"), and the
// slot's own timezone -- computed BEFORE the user answers yes/no -- then
// "yes" sends the exact resolved request and "undo" restores it exactly.
func TestResolveAndAct_Reschedule_ConfirmationShowsResolvedDateTimeTZ(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	slot := dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing: dbo4calendarius.Timing{
				Start: dbo4calendarius.DateTime{Date: "2026-09-26", Time: "10:00"},
				End:   dbo4calendarius.DateTime{Date: "2026-09-26", Time: "10:30"},
				// TimeZone deliberately differs from fixedNow's UTC location, so
				// the confirmation text is a real assertion, not a coincidence.
				TimeZone: "America/New_York",
			},
			Repeats: dbo4calendarius.RepeatPeriodOnce,
		},
	}
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist appointment", SlotID: "s1",
			Start: time.Date(2026, 9, 26, 10, 0, 0, 0, nyc), End: time.Date(2026, 9, 26, 10, 30, 0, 0, nyc),
			Slot: &slot},
	}}}
	undo := &session.Action{Kind: sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening}
	exec := &FakeExecutor{Undo: map[string]*session.Action{
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening: undo,
	}}
	// fixedNow is Friday 2026-09-25 12:00 UTC -- "tomorrow" is Sep 26.
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers, Now: fixedNow},
		Executor: exec,
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}

	out, err := p.resolveAndAct(context.Background(),
		sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening,
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "my dentist appointment tomorrow"},
		map[string]string{"when": "4"}, st, "sp1")
	if err != nil {
		t.Fatalf("resolveAndAct: %v", err)
	}
	if st.Pending == nil {
		t.Fatal("expected a Pending confirmation")
	}
	wantSubstrs := []string{"Sep 26", "16:00", "EDT"}
	for _, want := range wantSubstrs {
		if !strings.Contains(out.Text, want) {
			t.Errorf("confirmation text = %q, want it to contain %q (resolved date/time/TZ before asking)", out.Text, want)
		}
	}
	// S5 coordinator ruling: the confirmation also carries a HappeningCard
	// (PresentationHappeningCard + a single HappeningRow) showing the SAME
	// resolved new time as the text, not just prose.
	if out.Presentation != sneatdomain.PresentationHappeningCard {
		t.Errorf("Presentation = %q, want PresentationHappeningCard", out.Presentation)
	}
	if len(out.HappeningRows) != 1 {
		t.Fatalf("HappeningRows = %+v, want exactly 1 (the resolved new time)", out.HappeningRows)
	}
	if row := out.HappeningRows[0]; row.Start.Hour() != 16 || row.Start.Day() != 26 {
		t.Errorf("confirmation row Start = %v, want Sep 26 16:00", row.Start)
	}

	out, err = p.Turn(context.Background(), "yes", st, "sp1")
	if err != nil {
		t.Fatalf("Turn(yes): %v", err)
	}
	if !strings.HasPrefix(out.Text, "Done.") || len(exec.Executed) != 1 {
		t.Fatalf("out = %+v, executed = %+v", out, exec.Executed)
	}

	out, err = p.Turn(context.Background(), "undo", st, "sp1")
	if err != nil {
		t.Fatalf("Turn(undo): %v", err)
	}
	if out.Text != "Undone." || len(exec.Executed) != 2 {
		t.Fatalf("out = %+v, executed = %+v", out, exec.Executed)
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
	// S5/m3 coordinator ruling: an ambiguous HAPPENING reference's choice
	// list shows each candidate's real time (HappeningRows), not just a bare
	// title -- otherwise two "dentist" candidates would be indistinguishable
	// in the choice list itself.
	if len(out.HappeningRows) != 2 {
		t.Fatalf("HappeningRows = %+v, want a time-bearing row per candidate", out.HappeningRows)
	}
	for _, row := range out.HappeningRows {
		if row.Start.IsZero() {
			t.Errorf("candidate row %+v has a zero Start", row)
		}
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

// TestHandleAction_AddTodo_ViaReference_IgnoresModelSuppliedSpaceID is B3's
// other Args-stripping path: add_todo routed through resolveAndAct (a
// Reference is set, so HandleAction resolves it instead of taking the
// no-reference shortcut) must ALSO discard a model-supplied spaceID slot.
func TestHandleAction_AddTodo_ViaReference_IgnoresModelSuppliedSpaceID(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	kind := sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo
	_, err := p.HandleAction(context.Background(), Action{
		Kind: kind, Reference: "milk", // forces the resolveAndAct path
		Slots: map[string]string{"title": "eggs", "spaceID": "evil"},
	}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if len(exec.Executed) != 1 {
		t.Fatalf("executed = %+v, want 1", exec.Executed)
	}
	if got := exec.Executed[0].Args["spaceID"]; got != "sp1" {
		t.Fatalf("spaceID = %q, want sp1 (resolveAndAct must strip a model-supplied spaceID too)", got)
	}
}

// TestHandleAction_Pronoun_RejectsFocusedEntityFromAnotherSpace is B3: a
// focused entity left over from a space the session has since LEFT must
// not let a bare pronoun reference execute against that old space.
func TestHandleAction_Pronoun_RejectsFocusedEntityFromAnotherSpace(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	st.Focus(&session.EntityRef{Type: sneatdomain.EntityTodo, Title: "old",
		Keys: map[string]string{"spaceID": "spOLD", "list": "do", "itemID": "t9"}})
	out, err := p.HandleAction(context.Background(), Action{Kind: sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo, Pronoun: true}, st, "spNEW")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("executed = %+v, want NOTHING run against a cross-space focused entity (pipeline space is spNEW, target is spOLD)", exec.Executed)
	}
	if out.Text == "" {
		t.Fatal("expected an explanatory message, not a silent no-op")
	}
}
