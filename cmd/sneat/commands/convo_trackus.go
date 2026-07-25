package commands

import (
	"context"
	"fmt"
	"sync"

	"github.com/sneat-co/calendarius/backend/convoservice4calendarius"
	"github.com/sneat-co/contactus/backend/botservice4contactus"
	"github.com/sneat-co/ext-contactus/backend/contract4contactus"
	"github.com/sneat-co/sneat-bots/extensions/anybot/convo/convobindings"
	"github.com/sneat-co/sneat-bots/extensions/anybot/convo/convosetup"
	"github.com/sneat-co/sneat-bots/extensions/contactus/convoactions"
	"github.com/sneat-co/sneat-go-core/facade"
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
//
// Trackus records a measurement against a CONTACT, not a user, so its seam must
// be bound or `sneat convo say "20 push-ups"` fails with "trackus contact
// resolver is not configured". The binding itself lives in sneat-bots so the
// CLI, the bots host and the tests all use one implementation.
func configureConvoServices() {
	convoServicesOnce.Do(func() {
		actions4contactus.ConfigureService(botservice4contactus.New())
		convobindings.ConfigureContactResolver()
	})
}

var convoServicesOnce sync.Once

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
