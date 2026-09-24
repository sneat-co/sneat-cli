package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

func testReaders() data.Readers {
	return data.Readers{
		Happenings: &data.FakeHappenings{Items: []data.Happening{
			{ID: "h1", SpaceID: "sp1", Title: "Dentist appointment", Start: time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)},
			{ID: "h2", SpaceID: "sp1", Title: "Dentist follow-up", Start: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
			{ID: "h3", SpaceID: "sp1", Title: "Team standup", Start: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)},
		}},
		Todos: &data.FakeTodos{Items: []data.Todo{
			{ID: "t1", SpaceID: "sp1", List: data.ListKindDo, Title: "Buy milk"},
		}},
		Contacts: &data.FakeContacts{Items: []data.Contact{
			{ID: "c1", SpaceID: "sp1", Name: "Alice"},
		}},
	}
}

func TestResolver_ExpressionOneMatch(t *testing.T) {
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "standup"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne {
		t.Fatalf("Outcome = %v, want OutcomeOne", res.Outcome)
	}
	if res.Candidates[0].Keys["happeningID"] != "h3" {
		t.Fatalf("candidate = %+v", res.Candidates[0])
	}
}

func TestResolver_ExpressionManyMatches(t *testing.T) {
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeMany || len(res.Candidates) != 2 {
		t.Fatalf("res = %+v, want 2 ambiguous candidates", res)
	}
}

func TestResolver_ExpressionNoMatch(t *testing.T) {
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "nonexistent thing"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("Outcome = %v, want OutcomeNone", res.Outcome)
	}
}

// TestResolver_PronounUsesFocused covers scenario 5: focus a happening, then
// "move it to Friday" resolves the pronoun from session state, not text
// search.
func TestResolver_PronounUsesFocused(t *testing.T) {
	focused := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Dentist appointment",
		Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Pronoun: true},
		session.State{Focused: &focused}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || !res.Candidates[0].Same(focused) {
		t.Fatalf("res = %+v, want the focused happening", res)
	}
}

func TestResolver_PronounFiltersByKind(t *testing.T) {
	focusedContact := session.EntityRef{Type: sneatdomain.EntityContact, Keys: map[string]string{"contactID": "c1"}}
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Pronoun: true},
		session.State{Focused: &focusedContact}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("res = %+v, want no happening candidate when only a contact is focused", res)
	}
}

func TestResolver_PronounFallsBackToSidebar(t *testing.T) {
	pinned := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Buy milk", Keys: map[string]string{"itemID": "t1"}}
	st := session.State{}
	st.Pin(pinned)
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityTodo, Pronoun: true}, st, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || !res.Candidates[0].Same(pinned) {
		t.Fatalf("res = %+v, want the sidebar-pinned todo", res)
	}
}

// TestResolver_PronounExcludesOtherSpaceSidebarPin is m3 (fix round r4
// review): applySpaceChange deliberately leaves Sidebar pins in place
// across a space switch (so switching back doesn't lose them) -- a pin from
// a space the session has since left must never become "it" just because
// nothing else in the tiered fallback matched.
func TestResolver_PronounExcludesOtherSpaceSidebarPin(t *testing.T) {
	otherSpacePin := session.EntityRef{Type: sneatdomain.EntityTodo, Title: "Old space item", Keys: map[string]string{"spaceID": "spOLD", "itemID": "t1"}}
	st := session.State{}
	st.Pin(otherSpacePin)
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityTodo, Pronoun: true}, st, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("res = %+v, want OutcomeNone -- the only sidebar pin belongs to another space (spOLD, not sp1)", res)
	}
}

// TestResolver_PronounFocusedWinsOverSidebarAmbiguity is the S3 regression:
// a flat merge of session.State.Candidates() would make this ambiguous
// (focused happening + a DIFFERENT happening pinned to the sidebar, same
// kind). The focused entity must win outright -- a session with a focused
// happening and "move it" should never ask "which one?" just because the
// sidebar also holds a happening.
func TestResolver_PronounFocusedWinsOverSidebarAmbiguity(t *testing.T) {
	focused := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Dentist appointment",
		Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	sidebarHappening := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Team standup",
		Keys: map[string]string{"spaceID": "sp1", "happeningID": "h3"}}
	st := session.State{Focused: &focused}
	st.Pin(sidebarHappening)
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Pronoun: true}, st, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || !res.Candidates[0].Same(focused) {
		t.Fatalf("res = %+v, want ONLY the focused happening (no sidebar ambiguity)", res)
	}
}

// TestResolver_PronounSelectionWinsOverSidebar mirrors the above for the
// selection tier: a multi-select of happenings must win outright over an
// unrelated sidebar-pinned happening, even though a flat Candidates() merge
// would combine them into one ambiguous set.
func TestResolver_PronounSelectionWinsOverSidebar(t *testing.T) {
	selected := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Dentist appointment",
		Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	sidebarHappening := session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Team standup",
		Keys: map[string]string{"spaceID": "sp1", "happeningID": "h3"}}
	st := session.State{Selection: []session.EntityRef{selected}}
	st.Pin(sidebarHappening)
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Pronoun: true}, st, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || !res.Candidates[0].Same(selected) {
		t.Fatalf("res = %+v, want ONLY the selected happening (no sidebar ambiguity)", res)
	}
}

// TestResolver_OrdinalPicksFromLastShown covers S3's choice-list picking:
// "2" (and "the second one") selects LastShown[1] regardless of ref.Kind.
func TestResolver_OrdinalPicksFromLastShown(t *testing.T) {
	shown := []session.EntityRef{
		{Type: sneatdomain.EntityHappening, Title: "Dentist appointment", Keys: map[string]string{"happeningID": "h1"}},
		{Type: sneatdomain.EntityHappening, Title: "Dentist follow-up", Keys: map[string]string{"happeningID": "h2"}},
	}
	st := session.State{LastShown: shown}
	r := Resolver{Readers: testReaders()}

	for _, expr := range []string{"2", "the second one", "second", "#2"} {
		res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: expr}, st, "sp1")
		if err != nil {
			t.Fatalf("Resolve(%q): %v", expr, err)
		}
		if res.Outcome != OutcomeOne || !res.Candidates[0].Same(shown[1]) {
			t.Errorf("Resolve(%q) = %+v, want the second shown happening", expr, res)
		}
	}
}

// TestResolver_ExpressionNarrowedByTemporalWord covers S4: "dentist
// appointment tomorrow" strips the stopword ("my" style words) and the
// temporal word ("tomorrow"), searches by the remaining significant words,
// then narrows to happenings starting that day -- disambiguating the two
// otherwise title-matching "dentist" happenings by date.
func TestResolver_ExpressionNarrowedByTemporalWord(t *testing.T) {
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) // h1 (Sep 26) is "tomorrow"
	r := Resolver{Readers: testReaders(), Now: func() time.Time { return now }}
	res, err := r.Resolve(context.Background(),
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "my dentist appointment tomorrow"},
		session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || res.Candidates[0].Keys["happeningID"] != "h1" {
		t.Fatalf("res = %+v, want ONLY h1 (tomorrow), not h2 (next week) or h3 (no title match)", res)
	}
}

// TestResolver_ExpressionTemporalWord_NoMatchInWindow reports OutcomeNone
// (not a stale match from a different day) when the significant words match
// a title but not within the named window.
func TestResolver_ExpressionTemporalWord_NoMatchInWindow(t *testing.T) {
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	r := Resolver{Readers: testReaders(), Now: func() time.Time { return now }}
	// "today" -- neither dentist happening starts today (Sep 25); h3
	// (standup) starts today but doesn't match "dentist" at all.
	res, err := r.Resolve(context.Background(),
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist appointment today"},
		session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("res = %+v, want OutcomeNone (title matches exist, but none today)", res)
	}
}

// TestResolver_WordSetMatchesAnyOrder covers m1: a happening titled with
// words in one order still matches a reference expression naming them in
// the OTHER order -- a joined-substring query would miss this.
func TestResolver_WordSetMatchesAnyOrder(t *testing.T) {
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Appointment: Dentist"},
	}}}
	r := Resolver{Readers: readers, Now: func() time.Time { return time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) }}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "dentist appointment"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || res.Candidates[0].Keys["happeningID"] != "h1" {
		t.Fatalf("res = %+v, want h1 matched despite reversed word order", res)
	}
}

// TestResolver_PossessiveWeekday covers m1: "Friday's yoga" recognises
// "Friday's" as the weekday "Friday", not a literal title word.
func TestResolver_PossessiveWeekday(t *testing.T) {
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) // Friday
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{
		{ID: "fri", SpaceID: "sp1", Title: "Yoga", Start: time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)},
		{ID: "mon", SpaceID: "sp1", Title: "Yoga", Start: time.Date(2026, 9, 21, 18, 0, 0, 0, time.UTC)},
	}}}
	r := Resolver{Readers: readers, Now: func() time.Time { return now }}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "Friday's yoga"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || res.Candidates[0].Keys["happeningID"] != "fri" {
		t.Fatalf("res = %+v, want ONLY the Friday yoga (possessive weekday narrowed the window)", res)
	}
	if res.Candidates[0].Keys["date"] != "2026-09-25" {
		t.Fatalf("Keys[date] = %q, want 2026-09-25 -- M1: the occurrence the reference itself named must be carried", res.Candidates[0].Keys["date"])
	}
}

// TestResolver_ExpressionNarrowedByTemporalWord_CarriesOccurrenceDate is M1
// (fix round r3b review): a reference's temporal word doesn't just narrow
// the search window, it names WHICH occurrence the user means -- resolved
// on Keys["date"] so cancel/reschedule use THIS occurrence via
// occurrenceAnchor, not recurringAnchor's own "next from now" guess.
func TestResolver_ExpressionNarrowedByTemporalWord_CarriesOccurrenceDate(t *testing.T) {
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) // h1 (Sep 26) is "tomorrow"
	r := Resolver{Readers: testReaders(), Now: func() time.Time { return now }}
	res, err := r.Resolve(context.Background(),
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "my dentist appointment tomorrow"},
		session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne {
		t.Fatalf("res = %+v", res)
	}
	if res.Candidates[0].Keys["date"] != "2026-09-26" {
		t.Fatalf("Keys[date] = %q, want 2026-09-26 (\"tomorrow\")", res.Candidates[0].Keys["date"])
	}
}

// TestResolver_ExpressionNoTemporalWord_NoDateKey covers the other half: a
// reference naming no day at all ("the dentist appointment") carries no
// Keys["date"] -- occurrenceAnchor must fall back to recurringAnchor's own
// guess for it, not a spuriously-set empty/wrong date.
func TestResolver_ExpressionNoTemporalWord_NoDateKey(t *testing.T) {
	r := Resolver{Readers: testReaders(), Now: func() time.Time { return time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) }}
	res, err := r.Resolve(context.Background(),
		decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "standup"},
		session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne {
		t.Fatalf("res = %+v", res)
	}
	if _, ok := res.Candidates[0].Keys["date"]; ok {
		t.Fatalf("Keys = %+v, want no \"date\" key -- the reference named no day", res.Candidates[0].Keys)
	}
}

// TestResolver_ReferenceDay_NoOccurrenceThatDay_Refuses is a minor (fix
// round r5 review): "cancel Wednesday's yoga" against a happening that only
// recurs Mon/Fri must not resolve to a phantom Wednesday occurrence, or
// fall back to the generic "couldn't find" text as if no yoga existed at
// all -- it must refuse with the SPECIFIC reason.
func TestResolver_ReferenceDay_NoOccurrenceThatDay_Refuses(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t) // Monday 2026-09-21; Wednesday of that week is 2026-09-23
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}}
	r := Resolver{Readers: readers, Now: func() time.Time { return now }}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "Wednesday's yoga"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("res = %+v, want OutcomeNone -- yoga does not recur on Wednesday", res)
	}
	if res.Refusal != "Yoga doesn't happen on Wednesday." {
		t.Fatalf("Refusal = %q, want the specific \"Yoga doesn't happen on Wednesday.\" reason", res.Refusal)
	}
}

// TestResolver_ReferenceDay_OccurrenceExists_StillResolves is the same
// fixture's positive control: "cancel Friday's yoga" (a real occurrence)
// must resolve normally, not be swept up by the new wrong-day exclusion.
func TestResolver_ReferenceDay_OccurrenceExists_StillResolves(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}}
	r := Resolver{Readers: readers, Now: func() time.Time { return now }}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "Friday's yoga"}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeOne || res.Refusal != "" {
		t.Fatalf("res = %+v, want a clean OutcomeOne match, no refusal", res)
	}
}

// TestPipeline_CancelWednesdaysYoga_RefusesWithSpecificReason is the M1
// resolver fix's end-to-end counterpart: the specific refusal text must
// reach the user through resolveAndAct, not the generic "couldn't find"
// fallback.
func TestPipeline_CancelWednesdaysYoga_RefusesWithSpecificReason(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}}
	p := Pipeline{
		Resolver: Resolver{Readers: readers, Now: func() time.Time { return now }},
		Readers:  readers,
		Now:      func() time.Time { return now },
	}
	st := &session.State{}
	out, err := p.HandleAction(context.Background(),
		Action{Kind: "calendar.cancel_happening", Reference: "Wednesday's yoga"}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if out.Text != "Yoga doesn't happen on Wednesday." {
		t.Fatalf("out.Text = %q, want the specific refusal", out.Text)
	}
}

// TestResolver_OrdinalFiltersByKind covers m2: "2" after a happenings
// choice list must not resolve to a todo reference.
func TestResolver_OrdinalFiltersByKind(t *testing.T) {
	shown := []session.EntityRef{
		{Type: sneatdomain.EntityHappening, Title: "A", Keys: map[string]string{"happeningID": "h1"}},
		{Type: sneatdomain.EntityHappening, Title: "B", Keys: map[string]string{"happeningID": "h2"}},
	}
	st := session.State{LastShown: shown}
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityTodo, Expression: "2"}, st, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("res = %+v, want OutcomeNone -- LastShown[1] is a happening, not a todo", res)
	}
}

// TestResolver_OrdinalOutOfRange_ReportsNone ensures a number beyond the
// shown list's length is a clean "no such option", not a panic or a
// fall-through to some unrelated candidate.
func TestResolver_OrdinalOutOfRange_ReportsNone(t *testing.T) {
	st := session.State{LastShown: []session.EntityRef{
		{Type: sneatdomain.EntityHappening, Title: "Only one", Keys: map[string]string{"happeningID": "h1"}},
	}}
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "5"}, st, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("res = %+v, want OutcomeNone for an out-of-range pick", res)
	}
}
