package pipeline

import (
	"context"
	"testing"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// contactsTestReaders is a real (non-slicing) data.FakeContacts fixture with
// enough contacts to exercise a resolved match, an ambiguous match and an
// unknown one -- the shapes S8's find_contact/show_contact must all handle.
func contactsTestReaders() data.Readers {
	return data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice Smith"},
		{ID: "c2", SpaceID: "sp1", Name: "Alicia Jones"},
		{ID: "c3", SpaceID: "sp1", Name: "Bob"},
	}}}
}

// TestTurn_FindContact_OneMatch_RendersContactCard covers S8's real,
// end-to-end path: the deterministic "find contact ..." rule -> pipeline.Turn
// -> findOrShowContact -> resolve.Resolve, through the actual decision
// chain (rules.New()), not a direct findOrShowContact call.
func TestTurn_FindContact_OneMatch_RendersContactCard(t *testing.T) {
	readers := contactsTestReaders()
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}
	out, err := p.Turn(context.Background(), "find contact bob", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected the deterministic find_contact rule to answer, not fall back to the LLM")
	}
	if len(out.Entities) != 1 || out.Entities[0].Keys["contactID"] != "c3" {
		t.Fatalf("Entities = %+v, want exactly Bob (c3)", out.Entities)
	}
	if out.Presentation != sneatdomain.PresentationContactsGrid {
		t.Fatalf("Presentation = %q", out.Presentation)
	}
	if len(st.LastShown) != 1 || st.LastShown[0].Keys["contactID"] != "c3" {
		t.Fatalf("LastShown = %+v, want it set for a later pronoun/pick", st.LastShown)
	}
}

// TestTurn_FindContact_AmbiguousName_RendersChoices covers the ambiguous
// case: "alic" plausibly matches both Alice Smith and Alicia Jones via
// resolve.Resolve's scored matching, so both come back as candidates rather
// than an arbitrary pick.
func TestTurn_FindContact_AmbiguousName_RendersChoices(t *testing.T) {
	readers := contactsTestReaders()
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}
	out, err := p.Turn(context.Background(), "find contact alic", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(out.Entities) < 2 {
		t.Fatalf("Entities = %+v, want both Alice and Alicia as candidates", out.Entities)
	}
}

// TestTurn_FindContact_UnknownName_FallsBackToTitleSearch covers
// resolve.Resolve's OutcomeUnknown falling through to the ordinary
// Resolver's title search rather than reporting a hard failure -- here it
// still finds nothing (no contact named "zzz"), a plain "couldn't find" text
// answer, not an error.
func TestTurn_FindContact_UnknownName_FallsBackToTitleSearch(t *testing.T) {
	readers := contactsTestReaders()
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}
	out, err := p.Turn(context.Background(), "find contact zzz", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(out.Entities) != 0 {
		t.Fatalf("Entities = %+v, want none for an unmatched name", out.Entities)
	}
	if out.Text == "" {
		t.Fatal("expected a \"couldn't find\" text answer, not a silent empty Output")
	}
}

// TestHandleAction_FindContact_ViaLLM covers the LLM-action-block path
// (HandleAction), the equivalent of the main LLM emitting
// <sneat-action>{"kind":"contacts.find_contact","reference":"bob"}</sneat-action>.
func TestHandleAction_FindContact_ViaLLM(t *testing.T) {
	readers := contactsTestReaders()
	p := Pipeline{Resolver: Resolver{Readers: readers}, Readers: readers}
	st := &session.State{}
	out, err := p.HandleAction(context.Background(), Action{Kind: "contacts.find_contact", Reference: "bob"}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if len(out.Entities) != 1 || out.Entities[0].Keys["contactID"] != "c3" {
		t.Fatalf("Entities = %+v", out.Entities)
	}
}
