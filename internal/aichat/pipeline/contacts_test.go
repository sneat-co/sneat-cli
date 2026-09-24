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

// TestTurn_FindContact_RelationshipWord_MatchesViaRelatedAsLabel is m11: once
// data.Contact carries RelatedAs/Gender, resolve.Resolve's RelatedAs-label
// fallback (no RelationsOf graph needed) can actually resolve a relationship
// word like "my wife" -- previously documented as a known MVP limitation
// (data.Contact had no roles/gender at all) that always fell through to a
// plain, unmatched name search instead.
func TestTurn_FindContact_RelationshipWord_MatchesViaRelatedAsLabel(t *testing.T) {
	readers := data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Dana Lee", FirstName: "Dana", Gender: "female", RelatedAs: "spouse"},
		{ID: "c2", SpaceID: "sp1", Name: "Sam Lee", FirstName: "Sam", Gender: "male", RelatedAs: "child"},
	}}}
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}
	out, err := p.Turn(context.Background(), "find contact my wife", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(out.Entities) != 1 || out.Entities[0].Keys["contactID"] != "c1" {
		t.Fatalf("Entities = %+v, want exactly Dana Lee (c1), resolved via RelatedAs=\"spouse\"+Gender=\"female\"", out.Entities)
	}
	if len(out.ContactRows) != 1 || out.ContactRows[0].RelatedAs != "spouse" {
		t.Fatalf("ContactRows = %+v, want the resolved contact's RelatedAs carried through for card rendering", out.ContactRows)
	}
}

// TestTurn_FindContact_OneMatch_ContactRowsCarriesFields is m11: the single-
// match path also populates Output.ContactRows (relationship/DoB/emails/
// phones), not just Entities, so cardFor can render more than a bare name.
func TestTurn_FindContact_OneMatch_ContactRowsCarriesFields(t *testing.T) {
	readers := data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Bob", DoB: "1990-01-02", Emails: []string{"bob@example.com"}},
	}}}
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
	if len(out.ContactRows) != 1 || out.ContactRows[0].DoB != "1990-01-02" || len(out.ContactRows[0].Emails) != 1 {
		t.Fatalf("ContactRows = %+v, want Bob's DoB/Emails carried through", out.ContactRows)
	}
}

// TestTurn_ListContacts_ContactRowsCarriesFields covers listContacts's own
// producer path (m11), separate from find_contact's resolve.Resolve path.
func TestTurn_ListContacts_ContactRowsCarriesFields(t *testing.T) {
	readers := data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice", RelatedAs: "parent"},
	}}}
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}},
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}
	out, err := p.Turn(context.Background(), "list contacts", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(out.ContactRows) != 1 || out.ContactRows[0].RelatedAs != "parent" {
		t.Fatalf("ContactRows = %+v, want Alice's RelatedAs carried through", out.ContactRows)
	}
}

// TestCandidateContactRows_EnrichesEachCandidateViaGet covers
// candidateContactRows directly (m11's counterpart to
// candidateHappeningRows, for an ambiguous CONTACTS choice list -- reached
// via ambiguousChoiceOutput's EntityContact case, an edge the current
// taxonomy only exercises for an unusual pronoun/reference combination on a
// non-find/show contacts action; testing the method directly is more
// direct than contriving that routing). One ContactsReader.Get per
// candidate enriches it with real fields.
func TestCandidateContactRows_EnrichesEachCandidateViaGet(t *testing.T) {
	readers := data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice", RelatedAs: "parent", DoB: "1980-01-01"},
		{ID: "c2", SpaceID: "sp1", Name: "Bob", RelatedAs: "spouse"},
	}}}
	p := Pipeline{Readers: readers}
	candidates := []session.EntityRef{
		{Type: sneatdomain.EntityContact, Title: "Alice", Keys: map[string]string{"spaceID": "sp1", "contactID": "c1"}},
		{Type: sneatdomain.EntityContact, Title: "Bob", Keys: map[string]string{"spaceID": "sp1", "contactID": "c2"}},
	}
	rows := p.candidateContactRows(context.Background(), "sp1", candidates)
	if len(rows) != 2 || rows[0].RelatedAs != "parent" || rows[0].DoB != "1980-01-01" || rows[1].RelatedAs != "spouse" {
		t.Fatalf("rows = %+v, want both candidates enriched via Get", rows)
	}
}

// TestCandidateContactRows_NilOnGetError degrades to nil (name-only
// rendering upstream) rather than a partial/erroring result when any
// candidate's Get fails -- matching candidateHappeningRows's own contract.
func TestCandidateContactRows_NilOnGetError(t *testing.T) {
	readers := data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{
		{ID: "c1", SpaceID: "sp1", Name: "Alice"},
	}}}
	p := Pipeline{Readers: readers}
	candidates := []session.EntityRef{
		{Type: sneatdomain.EntityContact, Title: "Alice", Keys: map[string]string{"spaceID": "sp1", "contactID": "c1"}},
		{Type: sneatdomain.EntityContact, Title: "Missing", Keys: map[string]string{"spaceID": "sp1", "contactID": "gone"}},
	}
	if rows := p.candidateContactRows(context.Background(), "sp1", candidates); rows != nil {
		t.Fatalf("rows = %+v, want nil when a candidate's Get fails", rows)
	}
}

// TestCandidateContactRows_NilWhenNoContactsReader covers the missing-
// reader guard.
func TestCandidateContactRows_NilWhenNoContactsReader(t *testing.T) {
	p := Pipeline{}
	if rows := p.candidateContactRows(context.Background(), "sp1", nil); rows != nil {
		t.Fatalf("rows = %+v, want nil with no Contacts reader configured", rows)
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
