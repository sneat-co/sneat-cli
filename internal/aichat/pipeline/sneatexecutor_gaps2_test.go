package pipeline

import (
	"context"
	"testing"
	"time"

	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// TestSneatExecutor_Execute_ReopenTodoAndAddToBuy covers Execute's own
// switch dispatch for the reopen_todo and add_to_buy cases directly (both
// underlying methods are exercised elsewhere, but never THROUGH Execute's
// dispatch for these two specific kinds).
func TestSneatExecutor_Execute_ReopenTodoAndAddToBuy(t *testing.T) {
	api, _ := newTestSneatAPI(t, nil)
	e := SneatExecutor{Todo: api}
	target := &session.EntityRef{Keys: map[string]string{"list": data.ListKindDo, "itemID": "i1"}}

	if _, err := e.Execute(context.Background(), "sp1", session.Action{
		Kind: sneatdomain.ModuleTodo + "." + sneatdomain.IntentReopenTodo, Target: target,
	}); err != nil {
		t.Fatalf("reopen_todo via Execute: %v", err)
	}
	if _, err := e.Execute(context.Background(), "sp1", session.Action{
		Kind: sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy, Args: map[string]string{"title": "Milk"},
	}); err != nil {
		t.Fatalf("add_to_buy via Execute: %v", err)
	}
}

// TestSneatExecutor_RescheduleHappening_NoAPIConfigured covers the guard
// branch directly (no Calendar/Happenings/target).
func TestSneatExecutor_RescheduleHappening_NoAPIConfigured(t *testing.T) {
	e := SneatExecutor{}
	if _, err := e.rescheduleHappening(context.Background(), "sp1", session.Action{}); err == nil {
		t.Fatal("expected an error with no Calendar API/Happenings reader/target")
	}
}

// TestSneatExecutor_RescheduleHappening_GetError covers the reader error
// branch, distinct from the no-slot case.
func TestSneatExecutor_RescheduleHappening_GetError(t *testing.T) {
	e := SneatExecutor{Calendar: mustClient(t), Happenings: &data.FakeHappenings{}, Now: fixedNow}
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "missing"}}
	_, err := e.rescheduleHappening(context.Background(), "sp1", session.Action{Target: target})
	if err == nil {
		t.Fatal("expected an error when the happening cannot be read")
	}
}

// TestSneatExecutor_RescheduleHappening_UnparseableWhen covers parseWhen
// failing inside rescheduleHappening.
func TestSneatExecutor_RescheduleHappening_UnparseableWhen(t *testing.T) {
	slot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Timing: calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-24", Time: "10:00"}},
	}}
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", SlotID: "s1", Slot: &slot, Start: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)},
	}}
	e := SneatExecutor{Calendar: mustClient(t), Happenings: happenings, Now: fixedNow}
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	_, err := e.rescheduleHappening(context.Background(), "sp1", session.Action{Target: target, Args: map[string]string{"when": "??? nonsense ???"}})
	if err == nil {
		t.Fatal("expected an error for an unparseable \"when\"")
	}
}

// TestSneatExecutor_RescheduleHappening_AdjustSlotError covers the
// recurring-branch API-error propagation (AdjustSlot).
func TestSneatExecutor_RescheduleHappening_AdjustSlotError(t *testing.T) {
	slot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Timing:   calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-18", Time: "09:00"}},
		Repeats:  calendariusdbo.RepeatPeriodWeekly,
		Weekdays: []calendariusdbo.WeekdayCode{calendariusdbo.Friday2},
	}}
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", SlotID: "s1", Recurring: true, Slot: &slot, Start: fixedNow().AddDate(0, 0, -7)},
	}}
	e, _ := errAPIServer(t)
	e.Happenings = happenings
	e.Now = fixedNow
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	_, err := e.rescheduleHappening(context.Background(), "sp1", session.Action{Target: target, Args: map[string]string{"when": "16:00"}})
	if err == nil {
		t.Fatal("expected AdjustSlot's error to propagate")
	}
}

// TestSneatExecutor_RescheduleHappening_UpdateSlotError covers the
// non-recurring-branch API-error propagation (UpdateSlot).
func TestSneatExecutor_RescheduleHappening_UpdateSlotError(t *testing.T) {
	slot := calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Timing: calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-24", Time: "10:00"}},
	}}
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", SlotID: "s1", Slot: &slot, Start: fixedNow()},
	}}
	e, _ := errAPIServer(t)
	e.Happenings = happenings
	e.Now = fixedNow
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	_, err := e.rescheduleHappening(context.Background(), "sp1", session.Action{Target: target, Args: map[string]string{"when": "16:00"}})
	if err == nil {
		t.Fatal("expected UpdateSlot's error to propagate")
	}
}

// TestUTCOffsetString_NegativeOffset covers the west-of-UTC sign branch.
func TestUTCOffsetString_NegativeOffset(t *testing.T) {
	loc := time.FixedZone("neg", -5*3600-30*60) // -05:30
	got := utcOffsetString(time.Date(2026, 1, 1, 0, 0, 0, 0, loc))
	if got != "-05:30" {
		t.Fatalf("utcOffsetString = %q, want -05:30", got)
	}
}

// TestRecurringAnchor_NoMatchingWeekday_FallsBackToStart covers the final
// fallback after the 8-day search finds no matching weekday (an
// unrecognized/empty weekday code that weekdayCodeMatches never matches).
func TestRecurringAnchor_NoMatchingWeekday_FallsBackToStart(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	h := data.Happening{Recurring: true, Start: start, Slot: &calendariusdbo.HappeningSlot{HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
		Repeats:  calendariusdbo.RepeatPeriodWeekly,
		Weekdays: []calendariusdbo.WeekdayCode{"zz"}, // never matches a real weekday
	}}}
	got := recurringAnchor(fixedNow(), h)
	if !got.Equal(start) {
		t.Fatalf("recurringAnchor = %v, want the fallback start %v", got, start)
	}
}

// TestParseWhen covers every branch: empty text, a date-only phrase
// (single-field ParseText match), a bare time (NormalizeTime-only match),
// and fully unparseable text.
func TestParseWhen(t *testing.T) {
	now := fixedNow()
	current := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	if _, ok := parseWhen(now, current, "  "); ok {
		t.Error("empty/blank text should not parse")
	}
	if _, ok := parseWhen(now, current, "tomorrow"); !ok {
		t.Error("a bare date phrase should parse (keeping current's time-of-day)")
	}
	if _, ok := parseWhen(now, current, "16:00"); !ok {
		t.Error("a bare time should parse (keeping current's date)")
	}
	if _, ok := parseWhen(now, current, "!!!not a time or date!!!"); ok {
		t.Error("nonsense text should not parse")
	}
}

// TestAtTime_InvalidClockFallsBackToDay covers atTime's parse-failure
// branch.
func TestAtTime_InvalidClockFallsBackToDay(t *testing.T) {
	day := time.Date(2026, 9, 20, 10, 30, 0, 0, time.UTC)
	got := atTime(day, "not-a-clock", time.UTC)
	if !got.Equal(day) {
		t.Fatalf("atTime with an invalid clock = %v, want the unchanged day %v", got, day)
	}
}

// TestOccurrenceAnchor_EmptyRefDate covers the "no override" branch.
func TestOccurrenceAnchor_EmptyRefDate(t *testing.T) {
	h := data.Happening{Start: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)}
	got := occurrenceAnchor(fixedNow(), h, "")
	if !got.Equal(h.Start) {
		t.Fatalf("occurrenceAnchor(refDate=\"\") = %v, want the plain recurringAnchor result %v", got, h.Start)
	}
}

// TestOccurrenceAnchor_UnparseableRefDate covers refDate being set but not
// a valid "2006-01-02" date -- the plain recurringAnchor guess is kept
// rather than erroring.
func TestOccurrenceAnchor_UnparseableRefDate(t *testing.T) {
	h := data.Happening{Start: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)}
	got := occurrenceAnchor(fixedNow(), h, "not-a-date")
	if !got.Equal(h.Start) {
		t.Fatalf("occurrenceAnchor(refDate=garbage) = %v, want the plain recurringAnchor result %v", got, h.Start)
	}
}

// TestParseTemporalPhrase covers the direct-match branch (no possessive
// stripping needed) and the total-failure branch (garbage text).
func TestParseTemporalPhrase(t *testing.T) {
	if _, ok := parseTemporalPhrase(fixedNow(), "tomorrow"); !ok {
		t.Error("\"tomorrow\" should parse directly, no possessive stripping needed")
	}
	if _, ok := parseTemporalPhrase(fixedNow(), "!!!gibberish!!!"); ok {
		t.Error("gibberish should not parse, with or without possessive stripping")
	}
}

// TestSneatExecutor_CancelHappening_NoAPIConfigured covers the guard
// branch directly.
func TestSneatExecutor_CancelHappening_NoAPIConfigured(t *testing.T) {
	e := SneatExecutor{}
	if _, err := e.cancelHappening(context.Background(), "sp1", session.Action{}); err == nil {
		t.Fatal("expected an error with no Calendar API/Happenings reader/target")
	}
}

// TestSneatExecutor_CancelHappening_GetError covers the reader error
// branch.
func TestSneatExecutor_CancelHappening_GetError(t *testing.T) {
	e := SneatExecutor{Calendar: mustClient(t), Happenings: &data.FakeHappenings{}, Now: fixedNow}
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "missing"}}
	if _, err := e.cancelHappening(context.Background(), "sp1", session.Action{Target: target}); err == nil {
		t.Fatal("expected an error when the happening cannot be read")
	}
}

// TestSneatExecutor_CancelHappening_APIError covers the
// Calendar.CancelHappening error-propagation branch.
func TestSneatExecutor_CancelHappening_APIError(t *testing.T) {
	happenings := &data.FakeHappenings{Items: []data.Happening{{ID: "h1", SpaceID: "sp1"}}}
	e, _ := errAPIServer(t)
	e.Happenings = happenings
	e.Now = fixedNow
	target := &session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	if _, err := e.cancelHappening(context.Background(), "sp1", session.Action{Target: target}); err == nil {
		t.Fatal("expected CancelHappening's error to propagate")
	}
}

// TestSneatExecutor_RevokeCancellation_NoAPIConfigured covers the guard
// branch directly.
func TestSneatExecutor_RevokeCancellation_NoAPIConfigured(t *testing.T) {
	e := SneatExecutor{}
	if err := e.revokeCancellation(context.Background(), "sp1", session.Action{}); err == nil {
		t.Fatal("expected an error with no Calendar API/target")
	}
}

// TestSneatExecutor_CancelAdjustment_NoAPIConfigured covers the guard
// branch directly.
func TestSneatExecutor_CancelAdjustment_NoAPIConfigured(t *testing.T) {
	e := SneatExecutor{}
	if err := e.cancelAdjustment(context.Background(), "sp1", session.Action{}); err == nil {
		t.Fatal("expected an error with no Calendar API/target")
	}
}

// TestSneatExecutor_SetTodoDone_Reopen covers the done=false (reopen)
// branch's own undo-kind selection -- existing tests only exercise
// done=true (complete).
func TestSneatExecutor_SetTodoDone_Reopen(t *testing.T) {
	api, _ := newTestSneatAPI(t, nil)
	e := SneatExecutor{Todo: api}
	target := &session.EntityRef{Keys: map[string]string{"list": data.ListKindDo, "itemID": "i1"}}
	undo, err := e.setTodoDone(context.Background(), "sp1", session.Action{Target: target}, false)
	if err != nil {
		t.Fatalf("setTodoDone(false): %v", err)
	}
	if undo == nil || undo.Kind != sneatdomain.ModuleTodo+"."+sneatdomain.IntentCompleteTodo {
		t.Fatalf("undo = %+v, want a complete_todo undo action", undo)
	}
}

// mustClient builds a real (never actually dialed for these tests, since
// the reader error fires first) CalendarAPI, for cases that need a non-nil
// Calendar to get past Execute's/the method's own nil-API guard.
func mustClient(t *testing.T) CalendarAPI {
	t.Helper()
	api, _ := newTestSneatAPI(t, nil)
	return api
}
