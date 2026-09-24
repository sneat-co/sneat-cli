package controls

import "github.com/strongo/aichat/ai/session"

// NewContactCard builds a single contact's card: just its display name, no
// raw entity keys (S8/S9: "human-readable fields, no raw IDs" -- unlike
// chatapp's generic cardFor fallback for other entity kinds, which dumps
// EntityRef.Keys as visible label/value pairs). internal/aichat/data.Contact
// exposes only a display name for this MVP slice; a richer card (email,
// phone, roles) is a follow-up once the reader carries them.
func NewContactCard(name string, ref session.EntityRef) *CardBlock {
	title := name
	if title == "" {
		title = "Contact"
	}
	return NewCardBlock(title, ref)
}
