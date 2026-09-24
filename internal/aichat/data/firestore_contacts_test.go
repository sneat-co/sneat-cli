package data

import (
	"testing"

	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/strongo/strongoapp/person"
	"github.com/strongo/strongoapp/with"

	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// TestToDataContact_MapsHumanReadableFields is m11: toDataContact must carry
// every human-readable field dbo4contactus.ContactDbo actually has (names,
// DoB, gender, the flat relatedAs relationship label, and email/phone
// channels with the primary one first) into data.Contact, for both
// ContactCard rendering and resolve.Resolve's relationship-word matching --
// never a raw ID.
func TestToDataContact_MapsHumanReadableFields(t *testing.T) {
	dbo := &dbo4contactus.ContactDbo{}
	dbo.Names = &person.NameFields{FirstName: "Dana", LastName: "Lee", NickName: "D", FullName: "Dana Lee"}
	dbo.Gender = "female"
	dbo.DoB = "1990-01-02"
	dbo.RelatedAs = "spouse"
	dbo.Emails = map[string]*with.CommunicationChannelProps{
		"work@example.com": {},
		"home@example.com": {IsPrimary: true},
	}
	dbo.Phones = map[string]*with.CommunicationChannelProps{
		"+1 555 0100": {},
	}

	got := toDataContact("sp1", firestoredb.Contact{ID: "c1", Contact: dbo})

	if got.ID != "c1" || got.SpaceID != "sp1" {
		t.Fatalf("ID/SpaceID = %q/%q, want c1/sp1", got.ID, got.SpaceID)
	}
	if got.Name != "Dana Lee" {
		t.Fatalf("Name = %q, want the full name (no title set)", got.Name)
	}
	if got.FirstName != "Dana" || got.LastName != "Lee" || got.NickName != "D" || got.FullName != "Dana Lee" {
		t.Fatalf("names = %+v, want Dana/Lee/D/\"Dana Lee\"", got)
	}
	if got.Gender != "female" || got.DoB != "1990-01-02" || got.RelatedAs != "spouse" {
		t.Fatalf("Gender/DoB/RelatedAs = %q/%q/%q", got.Gender, got.DoB, got.RelatedAs)
	}
	if len(got.Emails) != 2 || got.Emails[0] != "home@example.com" {
		t.Fatalf("Emails = %v, want the primary (home@example.com) first", got.Emails)
	}
	if len(got.Phones) != 1 || got.Phones[0] != "+1 555 0100" {
		t.Fatalf("Phones = %v", got.Phones)
	}
}

// TestToDataContact_TitleWinsOverFullName covers the same Title-first
// fallback cmd/sneat/main.go's contactDisplayName already uses.
func TestToDataContact_TitleWinsOverFullName(t *testing.T) {
	dbo := &dbo4contactus.ContactDbo{}
	dbo.Title = "Doc Bob"
	dbo.Names = &person.NameFields{FullName: "Robert Smith"}

	got := toDataContact("sp1", firestoredb.Contact{ID: "c1", Contact: dbo})
	if got.Name != "Doc Bob" {
		t.Fatalf("Name = %q, want the explicit title", got.Name)
	}
}

// TestToDataContact_NilContact_ReturnsIDOnly covers a defensively-nil
// *dbo4contactus.ContactDbo (should not happen from a real read, but
// toDataContact must not panic).
func TestToDataContact_NilContact_ReturnsIDOnly(t *testing.T) {
	got := toDataContact("sp1", firestoredb.Contact{ID: "c1"})
	if got.ID != "c1" || got.SpaceID != "sp1" || got.Name != "" {
		t.Fatalf("got = %+v, want ID/SpaceID only", got)
	}
}
