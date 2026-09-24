package pipeline

import (
	"context"
	"fmt"

	"github.com/sneat-co/sneat-ai-backend/ports"
	"github.com/sneat-co/sneat-ai-backend/resolve"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/controls"
	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// isContactLookup reports whether kind ("<module>.<intent>") is a read-only
// contacts lookup (find_contact/show_contact), rather than a mutating
// action -- S8 coordinator ruling: these never reach Executor (there is
// nothing to execute; the whole point is rendering a ContactCard/
// ContactsGrid).
func isContactLookup(kind string) bool {
	return kind == sneatdomain.ModuleContacts+"."+sneatdomain.IntentFindContact ||
		kind == sneatdomain.ModuleContacts+"."+sneatdomain.IntentShowContact
}

// findOrShowContact resolves ref against the space's contacts and renders
// the result -- S8: find_contact/show_contact via sneat-ai-backend's
// resolve.Resolve (relationship words -- "mum", "my wife" -- plus scored
// name/nick/full matching), falling back to the ordinary Resolver (title
// search, and the pronoun/ordinal/session-state handling every other kind
// already gets) when resolve.Resolve finds nothing. One match renders a
// ContactCard; several render a ContactsGrid (both reuse blockFor's existing
// len(Entities)==1 special case, the same distinction scenario 7 already
// makes for a plain contacts listing); none reports it plainly.
//
// m11: internal/aichat/data.Contact now carries RelatedAs/Gender (from
// dbo4contactus), so a relationship word ("my wife") CAN match here --
// resolve.Resolve's RelatedAs-label fallback (used when its deeper
// RelationsOf graph is nil, which resolve.Space.RelationsOf is left here,
// per resolve/contacts.go: "Fall back to the legacy RelatedAs label on
// briefs") only needs Contact.RelatedAs to equal the relationship's role
// ("spouse", "parent", "child", case-insensitively) and Contact.Gender to
// agree. STILL OUT OF SCOPE: the deeper cross-contact Related graph
// (RelationsOf) -- resolve.Space.RelationsOf stays nil, so a space that
// only links contacts via that graph (never sets the flat RelatedAs label)
// still falls through to a plain name search, same as before. See
// spec/features/chat-ai/README.md's Out of Scope note.
func (p Pipeline) findOrShowContact(ctx context.Context, ref decision.Reference, st *session.State, spaceID string) (Output, error) {
	if p.Readers.Contacts == nil {
		return Output{}, fmt.Errorf("pipeline: no contacts reader configured")
	}

	if !ref.Pronoun && ref.Expression != "" {
		cs, err := p.Readers.Contacts.List(ctx, spaceID)
		if err != nil {
			return Output{}, err
		}
		byID := make(map[string]data.Contact, len(cs))
		for _, c := range cs {
			byID[c.ID] = c
		}
		res := resolve.Resolve(resolve.Space{Contacts: toPortsContacts(cs)}, ref.Expression, "")
		switch res.Outcome {
		case resolve.OutcomeResolved:
			ref0 := contactRef(spaceID, *res.Contact)
			return p.contactLookupOutput(st, []session.EntityRef{ref0}, []controls.ContactRow{contactRow(ref0, byID[res.Contact.ID])}), nil
		case resolve.OutcomeAmbiguous:
			refs := make([]session.EntityRef, 0, len(res.Candidates))
			rows := make([]controls.ContactRow, 0, len(res.Candidates))
			for _, c := range res.Candidates {
				r := contactRef(spaceID, c.Contact)
				refs = append(refs, r)
				rows = append(rows, contactRow(r, byID[c.Contact.ID]))
			}
			return p.contactLookupOutput(st, refs, rows), nil
		}
		// OutcomeUnknown: fall through to the ordinary Resolver below.
	}

	result, err := p.Resolver.Resolve(ctx, ref, *st, spaceID)
	if err != nil {
		return Output{}, err
	}
	if result.Outcome == OutcomeNone {
		return Output{Text: fmt.Sprintf("I couldn't find a contact matching %q.", ref.Expression)}, nil
	}
	// The ordinary Resolver only returns session.EntityRef (title search,
	// shared with happenings/todos) -- no rich data.Contact behind each
	// candidate to build a ContactRow from, so this path's card/grid shows
	// name only, same as before m11.
	return p.contactLookupOutput(st, result.Candidates, nil), nil
}

// contactLookupOutput renders refs as a ContactsGrid presentation -- a
// single ref, per blockFor's existing rule, still ends up a ContactCard.
// rows is m11's optional per-ref enrichment (relationship/DoB/emails/
// phones); nil (or a length mismatch) leaves Output.ContactRows unset and
// the card/grid falls back to name-only rendering.
func (p Pipeline) contactLookupOutput(st *session.State, refs []session.EntityRef, rows []controls.ContactRow) Output {
	st.LastShown = refs
	text := fmt.Sprintf("%d contact(s).", len(refs))
	if len(refs) == 1 {
		text = refs[0].Title
	} else if len(refs) > 1 {
		text = fmt.Sprintf("I found %d contacts -- which one did you mean?", len(refs))
	}
	out := Output{Text: text, Presentation: sneatdomain.PresentationContactsGrid, Entities: refs}
	if len(rows) == len(refs) {
		out.ContactRows = rows
	}
	return out
}

// contactRow builds a controls.ContactRow for ref from c -- a zero-value c
// (e.g. byID missed) still renders a valid, name-only card via ref.Title.
func contactRow(ref session.EntityRef, c data.Contact) controls.ContactRow {
	return controls.ContactRow{Ref: ref, Name: ref.Title, RelatedAs: c.RelatedAs, DoB: c.DoB, Emails: c.Emails, Phones: c.Phones}
}

// toPortsContacts adapts data.Contact to resolve.Resolve's ports.Contact
// (m11: now maps the real name/gender/DoB/relationship split dbo4contactus
// carries, not just a flat display name in Title/Full).
func toPortsContacts(cs []data.Contact) []ports.Contact {
	out := make([]ports.Contact, 0, len(cs))
	for _, c := range cs {
		out = append(out, ports.Contact{
			ID: c.ID, Type: "person",
			Title: c.Name, First: c.FirstName, Last: c.LastName, Nick: c.NickName, Full: c.FullName,
			Gender: c.Gender, DoB: c.DoB, RelatedAs: c.RelatedAs,
		})
	}
	return out
}

func contactRef(spaceID string, c ports.Contact) session.EntityRef {
	title := c.Full
	if title == "" {
		title = c.Title
	}
	return session.EntityRef{Type: sneatdomain.EntityContact, Title: title,
		Keys: map[string]string{"spaceID": spaceID, "contactID": c.ID}}
}
