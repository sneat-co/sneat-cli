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
