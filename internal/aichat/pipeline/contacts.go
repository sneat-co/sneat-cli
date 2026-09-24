package pipeline

import (
	"context"
	"fmt"

	"github.com/sneat-co/sneat-ai-backend/ports"
	"github.com/sneat-co/sneat-ai-backend/resolve"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

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
// KNOWN MVP LIMITATION: internal/aichat/data.Contact carries only ID/Name
// (no roles/gender), so a relationship word ("my mum") never actually
// matches here -- resolve.Resolve's relation lookup needs ports.Contact's
// Roles/RelationsOf, which this MVP slice's ContactsReader does not expose.
// It still degrades correctly: OutcomeUnknown falls through to the plain
// name search below, so "my mum" just behaves like an unmatched name search
// rather than erroring.
func (p Pipeline) findOrShowContact(ctx context.Context, ref decision.Reference, st *session.State, spaceID string) (Output, error) {
	if p.Readers.Contacts == nil {
		return Output{}, fmt.Errorf("pipeline: no contacts reader configured")
	}

	if !ref.Pronoun && ref.Expression != "" {
		cs, err := p.Readers.Contacts.List(ctx, spaceID)
		if err != nil {
			return Output{}, err
		}
		res := resolve.Resolve(resolve.Space{Contacts: toPortsContacts(cs)}, ref.Expression, "")
		switch res.Outcome {
		case resolve.OutcomeResolved:
			return p.contactLookupOutput(st, []session.EntityRef{contactRef(spaceID, *res.Contact)}), nil
		case resolve.OutcomeAmbiguous:
			refs := make([]session.EntityRef, 0, len(res.Candidates))
			for _, c := range res.Candidates {
				refs = append(refs, contactRef(spaceID, c.Contact))
			}
			return p.contactLookupOutput(st, refs), nil
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
	return p.contactLookupOutput(st, result.Candidates), nil
}

// contactLookupOutput renders refs as a ContactsGrid presentation -- a
// single ref, per blockFor's existing rule, still ends up a ContactCard.
func (p Pipeline) contactLookupOutput(st *session.State, refs []session.EntityRef) Output {
	st.LastShown = refs
	text := fmt.Sprintf("%d contact(s).", len(refs))
	if len(refs) == 1 {
		text = refs[0].Title
	} else if len(refs) > 1 {
		text = fmt.Sprintf("I found %d contacts -- which one did you mean?", len(refs))
	}
	return Output{Text: text, Presentation: sneatdomain.PresentationContactsGrid, Entities: refs}
}

// toPortsContacts adapts data.Contact (this MVP's minimal contacts shape) to
// resolve.Resolve's ports.Contact -- Title and Full both carry the display
// name, since data.Contact has no separate first/last/nick split.
func toPortsContacts(cs []data.Contact) []ports.Contact {
	out := make([]ports.Contact, 0, len(cs))
	for _, c := range cs {
		out = append(out, ports.Contact{ID: c.ID, Type: "person", Title: c.Name, Full: c.Name})
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
