package commands

import (
	"context"
	"fmt"
	"sync"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	"github.com/sneat-co/calendarius/backend/convoservice4calendarius"
	"github.com/sneat-co/contactus/backend/botservice4contactus"
	"github.com/sneat-co/contactus/backend/dal4contactus"
	"github.com/sneat-co/ext-contactus/backend/contract4contactus"
	"github.com/sneat-co/sneat-bots/extensions/anybot/convo/convosetup"
	"github.com/sneat-co/sneat-bots/extensions/contactus/convoactions"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/sneat-co/sneat-go-core/facade"
	"github.com/sneat-co/trackus/backend/facade4trackus"
)

// convoServices supplies the services the conversational catalogs call,
// mirroring what the bots host binds in production.
//
// The catalogs are controller code: they hold no persistence and reach their
// extension only through an injected service, so an unbound one fails the turn
// with "<extension> conversational service is not configured" rather than
// misbehaving quietly. Binding them here is what makes `sneat convo say`
// exercise the same code path a bot does.
//
// Calendarius takes its service through this struct rather than a global, and
// contacts are injected INTO it because Calendarius may depend on the Contactus
// contract but not on its implementation — binding the two is the host's job,
// which is what this composition is.
func convoServices() convosetup.Services {
	configureConvoServices()
	return convosetup.Services{
		Calendarius: convoservice4calendarius.New(botservice4contactus.New()),
	}
}

// configureConvoServices binds the ports that are package-level globals
// upstream and so cannot be passed through convosetup.Services. Idempotent,
// because every entry point binds defensively.
func configureConvoServices() {
	convoServicesOnce.Do(func() {
		actions4contactus.ConfigureService(botservice4contactus.New())
		facade4trackus.ConfigureContactResolver(contactusTrackerResolver{})
	})
}

var convoServicesOnce sync.Once

// contactusTrackerResolver binds Trackus's contact-resolution seam to the real
// Contactus module, the same shape the bots host binds.
//
// Trackus records a measurement against a CONTACT, not a user, so facade4trackus
// fails with "trackus contact resolver is not configured" until this is bound —
// which would make `sneat convo say "20 push-ups"` error out rather than record
// anything. The sandbox seeds the member contact briefs this reads, so a seeded
// space resolves correctly.
type contactusTrackerResolver struct{}

func (contactusTrackerResolver) ContactIDByUser(ctx context.Context, tx dal.ReadTransaction, spaceID coretypes.SpaceID, userID string) (string, error) {
	entry := dal4contactus.NewContactusSpaceEntry(spaceID)
	if err := tx.Get(ctx, entry.Record); err != nil && !record.IsNotFound(err) {
		return "", fmt.Errorf("get contactus space: %w", err)
	}
	contactID, _ := entry.Data.GetContactBriefByUserID(userID)
	return contactID, nil
}

func (contactusTrackerResolver) HasContact(ctx context.Context, tx dal.ReadTransaction, spaceID coretypes.SpaceID, contactID string) (bool, error) {
	entry := dal4contactus.NewContactusSpaceEntry(spaceID)
	if err := tx.Get(ctx, entry.Record); err != nil && !record.IsNotFound(err) {
		return false, fmt.Errorf("get contactus space: %w", err)
	}
	_, ok := entry.Data.Contacts[contactID]
	return ok, nil
}

func (contactusTrackerResolver) ContactBriefs(ctx context.Context, db dal.ReadSession, spaceID coretypes.SpaceID) (map[string]facade4trackus.ContactBrief, error) {
	entry := dal4contactus.NewContactusSpaceEntry(spaceID)
	if err := db.Get(ctx, entry.Record); err != nil {
		return nil, fmt.Errorf("get contactus space: %w", err)
	}
	briefs := make(map[string]facade4trackus.ContactBrief, len(entry.Data.Contacts))
	for contactID, brief := range entry.Data.Contacts {
		if brief == nil {
			continue
		}
		briefs[contactID] = facade4trackus.ContactBrief{UserID: brief.UserID, Title: brief.Title}
	}
	return briefs, nil
}

// seedSandboxContacts creates the demo contacts the dev scenarios reference
// ("meet Sarah tomorrow at 3pm"). Without them participant resolution correctly
// resolves nothing and the turn asks who is meant — accurate, but useless as a
// sandbox.
//
// Idempotent by name: the sandbox DB persists across runs under
// SNEAT_STORAGE=openvaultdb, so seeding unconditionally would add a duplicate
// "Sarah Connor" on every invocation — and duplicates make the name ambiguous,
// which is exactly the case the resolver refuses to guess at.
func seedSandboxContacts(ctx context.Context, userID, spaceID string) error {
	// The facades are membership-gated, and the sandbox context carries only the
	// DB; the user is attached per turn by the caller.
	userCtx := facade.NewContextWithUserID(ctx, userID)
	service := botservice4contactus.New()
	existing, err := service.ListContacts(userCtx, spaceID)
	if err != nil {
		return fmt.Errorf("failed to list sandbox contacts: %w", err)
	}
	present := make(map[string]bool, len(existing))
	for _, contact := range existing {
		present[contact.Name] = true
	}
	for _, name := range []string{"Sarah Connor", "John Smith"} {
		if present[name] {
			continue
		}
		if _, err = service.CreateContact(userCtx, contract4contactus.CreateContactRequest{
			SpaceID: spaceID, Name: name,
		}); err != nil {
			return fmt.Errorf("failed to seed sandbox contact %q: %w", name, err)
		}
	}
	return nil
}
