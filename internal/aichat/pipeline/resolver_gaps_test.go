package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// TestResolver_EmptyExpression covers Resolve's own "" expression guard
// (not a pronoun, not an ordinal, nothing to search for).
func TestResolver_EmptyExpression(t *testing.T) {
	r := Resolver{Readers: testReaders()}
	res, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: ""}, session.State{}, "sp1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNone {
		t.Fatalf("Outcome = %v, want OutcomeNone", res.Outcome)
	}
}

// TestResolver_NoReaderConfigured covers each kind's own "no reader
// configured" guard.
func TestResolver_NoReaderConfigured(t *testing.T) {
	r := Resolver{}
	for _, kind := range []string{sneatdomain.EntityHappening, sneatdomain.EntityTodo, sneatdomain.EntityContact} {
		_, err := r.Resolve(context.Background(), decision.Reference{Kind: kind, Expression: "x"}, session.State{}, "sp1")
		if err == nil {
			t.Errorf("kind %q: expected an error with no reader configured", kind)
		}
	}
}

// TestResolver_UnknownKind covers the default branch.
func TestResolver_UnknownKind(t *testing.T) {
	r := Resolver{Readers: testReaders()}
	if _, err := r.Resolve(context.Background(), decision.Reference{Kind: "bogus", Expression: "x"}, session.State{}, "sp1"); err == nil {
		t.Fatal("expected an error for an unknown reference kind")
	}
}

// errFindByTitleHappenings fails FindByTitle, for Resolve's happening-kind
// error branch.
type errFindByTitleHappenings struct{ err error }

func (e errFindByTitleHappenings) Window(ctx context.Context, spaceID string, from, to time.Time) ([]data.Happening, error) {
	return nil, e.err
}
func (e errFindByTitleHappenings) FindByTitle(ctx context.Context, spaceID, query string) ([]data.Happening, error) {
	return nil, e.err
}
func (e errFindByTitleHappenings) Get(ctx context.Context, spaceID, happeningID string) (data.Happening, error) {
	return data.Happening{}, e.err
}

// errTodos fails FindByTitle, for Resolve's todo-kind error branch.
type errTodos struct{ err error }

func (e errTodos) List(ctx context.Context, spaceID, list string) ([]data.Todo, error) {
	return nil, e.err
}
func (e errTodos) FindByTitle(ctx context.Context, spaceID, query string) ([]data.Todo, error) {
	return nil, e.err
}

// errContactsReader fails List/FindByName, for Resolve's contact-kind error
// branch (a data.ContactsReader double, distinct from pipeline's own
// errContacts in contacts_test.go which implements the same interface but
// with per-field configurability this test doesn't need).
type errContactsReader struct{ err error }

func (e errContactsReader) List(ctx context.Context, spaceID string) ([]data.Contact, error) {
	return nil, e.err
}
func (e errContactsReader) FindByName(ctx context.Context, spaceID, query string) ([]data.Contact, error) {
	return nil, e.err
}
func (e errContactsReader) Get(ctx context.Context, spaceID, contactID string) (data.Contact, error) {
	return data.Contact{}, e.err
}

// TestResolver_ReaderErrors covers each kind's FindByTitle/FindByName
// error-propagation branch.
func TestResolver_ReaderErrors(t *testing.T) {
	wantErr := errors.New("boom")

	t.Run("happening", func(t *testing.T) {
		r := Resolver{Readers: data.Readers{Happenings: errFindByTitleHappenings{err: wantErr}}}
		_, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityHappening, Expression: "x"}, session.State{}, "sp1")
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})
	t.Run("todo", func(t *testing.T) {
		r := Resolver{Readers: data.Readers{Todos: errTodos{err: wantErr}}}
		_, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityTodo, Expression: "x"}, session.State{}, "sp1")
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})
	t.Run("contact", func(t *testing.T) {
		r := Resolver{Readers: data.Readers{Contacts: errContactsReader{err: wantErr}}}
		_, err := r.Resolve(context.Background(), decision.Reference{Kind: sneatdomain.EntityContact, Expression: "x"}, session.State{}, "sp1")
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})
}

// TestResolvePronoun_LastShownAndPrevious cover the two rank-order
// fallbacks resolvePronoun's own tests don't isolate directly: a sole
// LastShown entity, and Previous.Target, each with no Focused/Selection set.
func TestResolvePronoun_LastShownAndPrevious(t *testing.T) {
	ref := session.EntityRef{Type: sneatdomain.EntityHappening, Keys: map[string]string{"happeningID": "h1"}}

	t.Run("sole LastShown", func(t *testing.T) {
		st := session.State{LastShown: []session.EntityRef{ref}}
		res := resolvePronoun(sneatdomain.EntityHappening, st, "sp1")
		if res.Outcome != OutcomeOne {
			t.Fatalf("Outcome = %v, want OutcomeOne", res.Outcome)
		}
	})

	t.Run("Previous.Target", func(t *testing.T) {
		st := session.State{Previous: &session.Action{Target: &ref}}
		res := resolvePronoun(sneatdomain.EntityHappening, st, "sp1")
		if res.Outcome != OutcomeOne {
			t.Fatalf("Outcome = %v, want OutcomeOne", res.Outcome)
		}
	})
}

// TestParseOrdinal_NumericSuffix covers the "2nd"-style suffix-stripping
// success branch.
func TestParseOrdinal_NumericSuffix(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"1st", 1}, {"2nd", 2}, {"3rd", 3}, {"4th", 4},
	} {
		if got, ok := parseOrdinal(tc.text); !ok || got != tc.want {
			t.Errorf("parseOrdinal(%q) = %d,%v, want %d,true", tc.text, got, ok, tc.want)
		}
	}
}

// TestResolveByPosition_NoLastShown covers its own standalone guard
// (pickFromLastShown already checks this before calling, but m2's
// kind-filtering callers reach resolveByPosition directly too).
func TestResolveByPosition_NoLastShown(t *testing.T) {
	_, handled := resolveByPosition(1, "", session.State{})
	if handled {
		t.Fatal("expected handled=false with an empty LastShown")
	}
}

// TestFilterByWordSet_EmptyTerms covers the "no terms -> keep everything"
// early return.
func TestFilterByWordSet_EmptyTerms(t *testing.T) {
	hs := []data.Happening{{ID: "h1", Title: "Anything"}}
	got := filterByWordSet(hs, nil)
	if len(got) != 1 {
		t.Fatalf("got = %+v, want the input unchanged", got)
	}
}
