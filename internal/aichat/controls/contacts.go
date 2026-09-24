package controls

import (
	"strings"

	"github.com/strongo/aichat/ai/session"
)

// ContactRow is the sliver of a contact a ContactCard/enriched ContactsGrid
// row needs -- decoupled from internal/aichat/data.Contact for the same
// leaf-package reason as HappeningRow/TodoRow (m11 follow-up to S8/S9's
// "richer card... once the reader carries them"). Only human-readable
// fields -- nothing here is a raw entity key.
type ContactRow struct {
	Ref  session.EntityRef
	Name string
	// RelatedAs is the contact's relationship label (e.g. "spouse",
	// "parent", "child"), empty when not set.
	RelatedAs string
	// DoB is "date of birth", YYYY-MM-DD, empty when unknown.
	DoB string
	// Emails/Phones are display-only communication channels, primary first.
	Emails []string
	Phones []string
}

// NewContactCard builds a single contact's card: display name plus whatever
// human-readable fields row carries (relationship, birthday, emails,
// phones) -- no raw entity keys (S8/S9 ruling, m11 follow-up: unlike
// chatapp's now-removed generic cardFor fallback, which used to dump
// EntityRef.Keys as visible label/value pairs). A field row is only added
// when it actually has a value, so a contact with just a name renders
// exactly as before.
func NewContactCard(row ContactRow) *CardBlock {
	title := row.Name
	if title == "" {
		title = "Contact"
	}
	var fields [][2]string
	if row.RelatedAs != "" {
		fields = append(fields, [2]string{"Relationship", row.RelatedAs})
	}
	if row.DoB != "" {
		fields = append(fields, [2]string{"Birthday", row.DoB})
	}
	if len(row.Emails) > 0 {
		fields = append(fields, [2]string{"Email", strings.Join(row.Emails, ", ")})
	}
	if len(row.Phones) > 0 {
		fields = append(fields, [2]string{"Phone", strings.Join(row.Phones, ", ")})
	}
	return NewCardBlock(title, row.Ref, fields...)
}
