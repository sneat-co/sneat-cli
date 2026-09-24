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
