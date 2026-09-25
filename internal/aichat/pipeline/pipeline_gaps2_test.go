package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// TestTurnFromDecision_NotDeterministic covers the NeedsLLM fallback branch
// directly: a decision with no Interaction and CanHandleDeterministically
// false.
func TestTurnFromDecision_NotDeterministic(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	out, err := p.turnFromDecision(context.Background(), decision.Decision{}, &session.State{}, "sp1")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !out.NeedsLLM {
		t.Fatal("expected NeedsLLM=true for a non-deterministic decision")
	}
}

// TestHandleCommand_BuyListAndContactsGrid cover handleCommand's own
// dispatch for PresentationBuyList and PresentationContactsGrid, and the
// help/fallback-to-resolveAndAct/NeedsLLM tail branches.
func TestHandleCommand_BuyListAndContactsGrid(t *testing.T) {
	readers := data.Readers{
		Todos:    &data.FakeTodos{Items: []data.Todo{{ID: "t1", SpaceID: "sp1", List: data.ListKindBuy, Title: "Milk"}}},
		Contacts: &data.FakeContacts{Items: []data.Contact{{ID: "c1", SpaceID: "sp1", Name: "Alice"}}},
	}
	p := Pipeline{Readers: readers, Now: fixedNow}

	out, err := p.handleCommand(context.Background(), decision.Decision{Presentation: sneatdomain.PresentationBuyList}, &session.State{}, "sp1")
	if err != nil || out.Presentation != sneatdomain.PresentationBuyList {
		t.Fatalf("BuyList: out=%+v err=%v", out, err)
	}

	out, err = p.handleCommand(context.Background(), decision.Decision{Presentation: sneatdomain.PresentationContactsGrid}, &session.State{}, "sp1")
	if err != nil || out.Presentation != sneatdomain.PresentationContactsGrid {
		t.Fatalf("ContactsGrid: out=%+v err=%v", out, err)
	}
}

// TestHandleCommand_Help covers the general.help special case reached past
// the Presentation switch.
func TestHandleCommand_Help(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	d := decision.Decision{
		Module: decision.Scored{Value: sneatdomain.ModuleGeneral},
		Intent: decision.Scored{Value: sneatdomain.IntentHelp},
	}
	out, err := p.handleCommand(context.Background(), d, &session.State{}, "sp1")
	if err != nil || out.Text != helpText {
		t.Fatalf("out=%+v err=%v, want the help text", out, err)
	}
}

// TestHandleCommand_ReferenceFallsThroughToResolveAndAct covers
// handleCommand's own d.Reference != nil tail branch (no Presentation
// match, not help).
func TestHandleCommand_ReferenceFallsThroughToResolveAndAct(t *testing.T) {
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Standup"},
	}}}
	p := Pipeline{Readers: readers, Resolver: Resolver{Readers: readers}, Executor: &FakeExecutor{}, Now: fixedNow}
	d := decision.Decision{
		Module:    decision.Scored{Value: sneatdomain.ModuleCalendar},
		Intent:    decision.Scored{Value: sneatdomain.IntentFindHappening},
		Reference: &decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "standup"},
	}
	out, err := p.handleCommand(context.Background(), d, &session.State{}, "sp1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected the reference to resolve deterministically, not fall to the LLM")
	}
}

// TestHandleCommand_NeedsLLM covers the final "nothing matched" fallback.
func TestHandleCommand_NeedsLLM(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	d := decision.Decision{Module: decision.Scored{Value: "x"}, Intent: decision.Scored{Value: "y"}}
	out, err := p.handleCommand(context.Background(), d, &session.State{}, "sp1")
	if err != nil || !out.NeedsLLM {
		t.Fatalf("out=%+v err=%v, want NeedsLLM=true", out, err)
	}
}

// TestShowWeek_SundayUsesLastWeekOffset covers showWeek's Sunday-specific
// -6 offset branch (every other weekday takes the -(weekday-1) branch,
// already covered by TestTurn_ShowWeek).
func TestShowWeek_SundayUsesLastWeekOffset(t *testing.T) {
	sunday := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) // a Sunday
	p := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{}}, Now: func() time.Time { return sunday }}
	out, err := p.showWeek(context.Background(), &session.State{}, "sp1", "this_week")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	wantMonday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if !out.WeekStart.Equal(wantMonday) {
		t.Fatalf("WeekStart = %v, want the PRECEDING Monday %v", out.WeekStart, wantMonday)
	}
}

// TestShowWindow_NoReaderAndReadError cover showWindow's own two guard
// branches directly.
func TestShowWindow_NoReaderAndReadError(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	if _, err := p.showWindow(context.Background(), &session.State{}, "sp1", fixedNow(), fixedNow(), "", "", false); err == nil {
		t.Fatal("expected an error with no Happenings reader configured")
	}

	wantErr := errors.New("boom")
	p2 := Pipeline{Readers: data.Readers{Happenings: errFindByTitleHappenings{err: wantErr}}, Now: fixedNow}
	if _, err := p2.showWindow(context.Background(), &session.State{}, "sp1", fixedNow(), fixedNow(), "", "", false); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// TestListTodos_NoReaderAndReadError cover listTodos' own two guard
// branches, plus a non-empty result to exercise the row-building loop
// (existing tests may only cover the empty-list branch).
func TestListTodos_NoReaderAndReadError(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	if _, err := p.listTodos(context.Background(), &session.State{}, "sp1", data.ListKindDo); err == nil {
		t.Fatal("expected an error with no Todos reader configured")
	}

	wantErr := errors.New("boom")
	p2 := Pipeline{Readers: data.Readers{Todos: errTodos{err: wantErr}}, Now: fixedNow}
	if _, err := p2.listTodos(context.Background(), &session.State{}, "sp1", data.ListKindDo); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}

	p3 := Pipeline{Readers: data.Readers{Todos: &data.FakeTodos{Items: []data.Todo{{ID: "t1", SpaceID: "sp1", List: data.ListKindDo, Title: "Milk"}}}}, Now: fixedNow}
	st := &session.State{}
	out, err := p3.listTodos(context.Background(), st, "sp1", data.ListKindDo)
	if err != nil || len(out.TodoRows) != 1 || len(st.LastShown) != 1 {
		t.Fatalf("out=%+v err=%v st.LastShown=%v", out, err, st.LastShown)
	}
}

// TestListContacts_NoReaderAndReadError cover listContacts' own two guard
// branches and the empty-result branch.
func TestListContacts_NoReaderAndReadError(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	if _, err := p.listContacts(context.Background(), &session.State{}, "sp1"); err == nil {
		t.Fatal("expected an error with no Contacts reader configured")
	}

	wantErr := errors.New("boom")
	p2 := Pipeline{Readers: data.Readers{Contacts: errContactsReader{err: wantErr}}, Now: fixedNow}
	if _, err := p2.listContacts(context.Background(), &session.State{}, "sp1"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}

	p3 := Pipeline{Readers: data.Readers{Contacts: &data.FakeContacts{}}, Now: fixedNow}
	st := &session.State{LastShown: []session.EntityRef{{}}}
	out, err := p3.listContacts(context.Background(), st, "sp1")
	if err != nil || out.Text != "That space has no contacts." || st.LastShown != nil {
		t.Fatalf("out=%+v err=%v st.LastShown=%v", out, err, st.LastShown)
	}
}

// TestResolveAndAct_ResolveError covers resolveAndAct's own Resolver.Resolve
// error-propagation branch.
func TestResolveAndAct_ResolveError(t *testing.T) {
	wantErr := errors.New("boom")
	p := Pipeline{Resolver: Resolver{Readers: data.Readers{Happenings: errFindByTitleHappenings{err: wantErr}}}, Now: fixedNow}
	ref := decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "x"}
	_, err := p.resolveAndAct(context.Background(), sneatdomain.ModuleCalendar+"."+sneatdomain.IntentCancelHappening, ref, nil, &session.State{}, "sp1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// TestResolveAndAct_OtherSpaceTarget covers the B3 "belongs to another
// space" refusal.
func TestResolveAndAct_OtherSpaceTarget(t *testing.T) {
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp2", Title: "Standup"},
	}}}
	st := &session.State{Focused: &session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Standup", Keys: map[string]string{"spaceID": "sp2", "happeningID": "h1"}}}
	p := Pipeline{Resolver: Resolver{Readers: readers}, Now: fixedNow}
	ref := decision.Reference{Kind: sneatdomain.EntityHappening, Pronoun: true}
	out, err := p.resolveAndAct(context.Background(), sneatdomain.ModuleCalendar+"."+sneatdomain.IntentCancelHappening, ref, nil, st, "sp1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Text == "" {
		t.Fatal("expected a \"belongs to another space\" refusal text")
	}
}

// TestResolveAndAct_CancelSummaryOk covers the cancel-kind confirmRow
// success branch (resolveAndAct's own switch case for cancel_happening,
// distinct from cancelSummary's own direct-call tests).
func TestResolveAndAct_CancelSummaryOk(t *testing.T) {
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist", Start: fixedNow()},
	}}}
	p := Pipeline{Resolver: Resolver{Readers: readers}, Readers: readers, Now: fixedNow}
	ref := decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist"}
	out, err := p.resolveAndAct(context.Background(), sneatdomain.ModuleCalendar+"."+sneatdomain.IntentCancelHappening, ref, nil, &session.State{}, "sp1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(out.HappeningRows) != 1 {
		t.Fatalf("out = %+v, want a confirmRow-backed HappeningRows entry", out)
	}
}

// TestHandleCommand_HappeningsListDispatchesShowUpcoming covers the
// PresentationHappeningsList case in handleCommand's switch, distinct from
// showUpcoming's own existing direct-call tests.
func TestHandleCommand_HappeningsListDispatchesShowUpcoming(t *testing.T) {
	p := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{}}, Now: fixedNow}
	out, err := p.handleCommand(context.Background(), decision.Decision{Presentation: sneatdomain.PresentationHappeningsList}, &session.State{}, "sp1")
	if err != nil || out.Presentation != sneatdomain.PresentationHappeningsList {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// TestProjectOccurrences_NonQualifyingSlot covers the defensive
// non-weekly/no-weekdays early return (filterRecurringToWindow should
// already have dropped these, but projectOccurrences must not panic if
// called directly on one).
func TestProjectOccurrences_NonQualifyingSlot(t *testing.T) {
	h := data.Happening{Recurring: true, Slot: &calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Repeats: calendariusdbo.RepeatPeriodOnce, // not weekly
	}}}
	got := projectOccurrences(h, fixedNow(), fixedNow().AddDate(0, 0, 7))
	if got != nil {
		t.Fatalf("got = %+v, want nil for a non-qualifying recurrence rule", got)
	}
}

// TestRecurrenceHitsWindow_NonQualifyingSlot covers its own analogous
// early-return branch.
func TestRecurrenceHitsWindow_NonQualifyingSlot(t *testing.T) {
	h := data.Happening{Slot: &calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Repeats: calendariusdbo.RepeatPeriodOnce,
	}}}
	if recurrenceHitsWindow(h, fixedNow(), fixedNow().AddDate(0, 0, 7)) {
		t.Fatal("expected false for a non-weekly recurrence rule")
	}
}

// TestListTodos_EmptyList covers listTodos' own empty-result branch
// (distinct from the reader-error and non-empty-result cases already
// covered).
func TestListTodos_EmptyList(t *testing.T) {
	p := Pipeline{Readers: data.Readers{Todos: &data.FakeTodos{}}, Now: fixedNow}
	st := &session.State{LastShown: []session.EntityRef{{}}}
	out, err := p.listTodos(context.Background(), st, "sp1", data.ListKindDo)
	if err != nil || out.Text != "Nothing on that list." || st.LastShown != nil {
		t.Fatalf("out=%+v err=%v st.LastShown=%v", out, err, st.LastShown)
	}
}

// TestResolveAndAct_OutcomeNone_GenericText covers the generic
// "couldn't find" fallback (no Refusal set) -- distinct from
// TestPipeline_CancelWednesdaysYoga_RefusesWithSpecificReason's Refusal
// branch.
func TestResolveAndAct_OutcomeNone_GenericText(t *testing.T) {
	p := Pipeline{Resolver: Resolver{Readers: data.Readers{Happenings: &data.FakeHappenings{}}}, Now: fixedNow}
	ref := decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "nonexistent"}
	out, err := p.resolveAndAct(context.Background(), sneatdomain.ModuleCalendar+"."+sneatdomain.IntentCancelHappening, ref, nil, &session.State{}, "sp1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Text == "" {
		t.Fatal("expected the generic \"couldn't find\" text")
	}
}

// TestResolveAndAct_RescheduleRefusal covers resolveAndAct's own
// reschedule-kind refusal branch (a recurring happening's cross-day move,
// refused BEFORE staging any Pending confirmation).
func TestResolveAndAct_RescheduleRefusal(t *testing.T) {
	slot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Timing:   calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-18", Time: "09:00"}},
		Repeats:  calendariusdbo.RepeatPeriodWeekly,
		Weekdays: []calendariusdbo.WeekdayCode{calendariusdbo.Friday2},
	}}
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Yoga", SlotID: "s1", Recurring: true, Slot: &slot, Start: fixedNow().AddDate(0, 0, -7)},
	}}}
	p := Pipeline{Resolver: Resolver{Readers: readers}, Readers: readers, Now: fixedNow}
	ref := decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "yoga"}
	st := &session.State{}
	out, err := p.resolveAndAct(context.Background(), sneatdomain.ModuleCalendar+"."+sneatdomain.IntentRescheduleHappening, ref, map[string]string{"when": "Monday 16:00"}, st, "sp1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Text == "" || st.Pending != nil {
		t.Fatalf("out=%+v st.Pending=%v, want a refusal with nothing staged", out, st.Pending)
	}
}

// TestCancelSummary covers every branch directly: no reader, Get error,
// empty-title fallback, and the non-recurring success path.
func TestCancelSummary(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	if _, _, ok := p.cancelSummary(context.Background(), "sp1", session.EntityRef{}, ""); ok {
		t.Fatal("expected ok=false with no Happenings reader")
	}

	p2 := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{}}, Now: fixedNow}
	if _, _, ok := p2.cancelSummary(context.Background(), "sp1", session.EntityRef{Keys: map[string]string{"happeningID": "missing"}}, ""); ok {
		t.Fatal("expected ok=false when the happening cannot be read")
	}

	p3 := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Start: fixedNow()},
	}}}, Now: fixedNow}
	text, row, ok := p3.cancelSummary(context.Background(), "sp1", session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}, "")
	if !ok || text != `Cancel "it"?` || row.Title != "it" {
		t.Fatalf("text=%q row=%+v ok=%v, want the empty-title \"it\" fallback", text, row, ok)
	}
}

// TestPickFromLastShown covers every branch: no LastShown, an unparseable
// pick, a valid ordinal that doesn't resolve, an out-of-range pick, and the
// empty-title "that one" fallback on success.
func TestPickFromLastShown(t *testing.T) {
	p := Pipeline{Now: fixedNow}

	if _, handled := p.pickFromLastShown("2", &session.State{}); handled {
		t.Fatal("expected handled=false with no LastShown")
	}

	shown := []session.EntityRef{{Type: sneatdomain.EntityHappening, Keys: map[string]string{"happeningID": "h1"}}}
	if _, handled := p.pickFromLastShown("not a number", &session.State{LastShown: shown}); handled {
		t.Fatal("expected handled=false for unparseable text")
	}

	if out, handled := p.pickFromLastShown("5", &session.State{LastShown: shown}); !handled || out.Text == "" {
		t.Fatalf("out-of-range pick: out=%+v handled=%v, want a \"no such option\" text", out, handled)
	}

	out, handled := p.pickFromLastShown("1", &session.State{LastShown: shown})
	if !handled || out.Text != `Got it: "that one".` {
		t.Fatalf("out=%+v handled=%v, want the empty-title \"that one\" fallback", out, handled)
	}
}

// TestRescheduleSummary covers every guard/branch: no reader/empty when, a
// happening with no slot, an unparseable when, the cross-day refusal, an
// explicit slot TimeZone override, and the empty-title fallback.
func TestRescheduleSummary(t *testing.T) {
	p := Pipeline{Now: fixedNow}
	if _, _, ok, _ := p.rescheduleSummary(context.Background(), "sp1", session.EntityRef{}, ""); ok {
		t.Fatal("expected ok=false with no Happenings reader or empty when")
	}

	p2 := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{{ID: "h1", SpaceID: "sp1"}}}}, Now: fixedNow}
	if _, _, ok, _ := p2.rescheduleSummary(context.Background(), "sp1", session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}, "16:00"); ok {
		t.Fatal("expected ok=false for a happening with no slot")
	}

	slot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Timing: calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-24", Time: "10:00"}, TimeZone: "America/New_York"},
	}}
	p3 := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Slot: &slot, Start: fixedNow()},
	}}}, Now: fixedNow}
	if _, _, ok, _ := p3.rescheduleSummary(context.Background(), "sp1", session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}, "!!!nonsense!!!"); ok {
		t.Fatal("expected ok=false for an unparseable when")
	}

	recSlot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Timing:   calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-18", Time: "09:00"}},
		Repeats:  calendariusdbo.RepeatPeriodWeekly,
		Weekdays: []calendariusdbo.WeekdayCode{calendariusdbo.Friday2},
	}}
	p4 := Pipeline{Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Recurring: true, Slot: &recSlot, Start: fixedNow().AddDate(0, 0, -7)},
	}}}, Now: fixedNow}
	if _, _, ok, refusal := p4.rescheduleSummary(context.Background(), "sp1", session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}, "Monday 16:00"); ok || refusal == "" {
		t.Fatalf("ok=%v refusal=%q, want a cross-day refusal", ok, refusal)
	}

	text, row, ok, refusal := p3.rescheduleSummary(context.Background(), "sp1", session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}, "Friday 16:00")
	if !ok || refusal != "" || text == "" || row.Title != "it" {
		t.Fatalf("text=%q row=%+v ok=%v refusal=%q, want the empty-title \"it\" fallback with the slot's own TimeZone honoured", text, row, ok, refusal)
	}
}

// TestSummaryFor_DeleteTodo covers the delete_todo case.
func TestSummaryFor_DeleteTodo(t *testing.T) {
	got := summaryFor(sneatdomain.ModuleTodo+"."+sneatdomain.IntentDeleteTodo, session.EntityRef{Title: "Milk"}, nil)
	if got != `Delete "Milk"?` {
		t.Fatalf("got = %q", got)
	}
}

// TestRunAction_ExecutorError covers runAction's own Executor.Execute
// error-propagation branch, reached via resolveAndAct's non-destructive
// path (find_happening is not in isDestructive's list).
func TestRunAction_ExecutorError(t *testing.T) {
	wantErr := errors.New("boom")
	p := Pipeline{Executor: &FakeExecutor{Err: map[string]error{"x": wantErr}}, Now: fixedNow}
	_, err := p.runAction(context.Background(), "sp1", session.Action{Kind: "x"}, &session.State{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
