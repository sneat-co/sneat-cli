// Package data declares the read-only views the aichat pipeline needs of a
// space's happenings, todos and contacts, and the entity resolver searches
// against. Reads may go through the existing internal/firestoredb patterns or
// through sneat-go query endpoints; this package only fixes the shape callers
// depend on, so the pipeline and its tests never see a concrete backend
// (⛔ writes never go through these readers -- see internal/aichat/pipeline
// actions.go, which mutates only through internal/sneatapi HTTP calls).
package data

import (
	"context"
	"errors"
	"time"

	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
)

// Happening is the sliver of a calendarius happening the pipeline needs to
// list, render and resolve: enough to build a DayCalendar/WeekCalendar/
// HappeningsList row and an EntityRef, not the full domain model.
//
// Only single (non-recurring) happenings currently populate Start/End
// reliably -- see HappeningsReader's doc comment for the known limitation.
type Happening struct {
	ID      string
	SpaceID string
	Title   string
	Start   time.Time
	End     time.Time
	// SlotID is the calendarius slot this Start/End came from -- required to
	// target a mutation (update_slot/adjust_slot/cancel_happening's slotID)
	// at the right slot of a multi-slot happening. Empty when the happening
	// has no slots.
	SlotID string
	// Recurring is true when the source happening repeats; such happenings are
	// still returned (for listing) but Start/End are its stored template slot,
	// not a resolved next-occurrence -- callers must not treat it as "the next
	// occurrence" for scheduling decisions.
	Recurring bool
	// Slot is the full calendarius slot Start/End/SlotID/Recurring were
	// derived from (B2): a mutation that changes only the time MUST send back
	// every other slot field (Weekdays/Locations/pricing/participants/TZ)
	// unchanged -- calendarius's update_slot and adjust_slot both replace the
	// slot wholesale, so an executor that reconstructs a bare
	// HappeningSlotTiming silently drops everything else. Nil when the
	// happening has no slots (see SlotID).
	Slot *calendariusdbo.HappeningSlot
}

// Todo is one listus list item, on either the "do" (todo) or "buy"
// (to-buy/shopping) list.
type Todo struct {
	ID      string
	SpaceID string
	List    string // ListKindDo or ListKindBuy
	Title   string
	Done    bool
}

// List kinds a Todo belongs to.
const (
	ListKindDo  = "do"
	ListKindBuy = "buy"
)

// Contact is the sliver of a contactus contact the pipeline needs to list,
// render and resolve.
type Contact struct {
	ID      string
	SpaceID string
	Name    string
}

// HappeningsReader reads a space's happenings for calendar presentations and
// resolution.
//
// KNOWN MVP LIMITATION: recurring happenings are returned with their stored
// template slot rather than a computed next-occurrence, because
// dbo4calendarius's recurrence model (weekday/week-of-month rules plus
// per-date Adjustments) needs its own occurrence-expansion pass that this
// MVP slice does not implement. Window(...) still includes them so they are
// not silently dropped from a day/week view, but a date-window read may
// include or exclude a recurring happening's rendered date incorrectly.
// find-by-title matching is unaffected.
type HappeningsReader interface {
	// Window returns a space's happenings whose (single-occurrence) start
	// falls in [from, to), plus any recurring happenings (whose Start is their
	// template slot, not a resolved occurrence -- see the limitation above).
	Window(ctx context.Context, spaceID string, from, to time.Time) ([]Happening, error)
	// FindByTitle returns happenings whose title contains query
	// (case-insensitive), most relevant first.
	FindByTitle(ctx context.Context, spaceID, query string) ([]Happening, error)
	// Get reads a single happening by ID.
	Get(ctx context.Context, spaceID, happeningID string) (Happening, error)
}

// TodosReader reads a space's listus items.
type TodosReader interface {
	// List returns a space's items on the given list kind (ListKindDo or
	// ListKindBuy), not-done first.
	List(ctx context.Context, spaceID, list string) ([]Todo, error)
	// FindByTitle returns items (any list) whose title contains query.
	FindByTitle(ctx context.Context, spaceID, query string) ([]Todo, error)
}

// ContactsReader reads a space's contacts.
type ContactsReader interface {
	List(ctx context.Context, spaceID string) ([]Contact, error)
	FindByName(ctx context.Context, spaceID, query string) ([]Contact, error)
}

// Readers bundles the three module readers the pipeline depends on.
type Readers struct {
	Happenings HappeningsReader
	Todos      TodosReader
	Contacts   ContactsReader
}

// Close releases every reader's resources that support it (m4: the
// Firestore-backed readers each hold one lazily-opened, reused client for
// their whole lifetime -- see firestoredb.Session -- rather than one per
// call; the session that wires a Readers is responsible for closing it on
// shutdown). Readers that don't need closing (fakes, a future non-Firestore
// backend) are silently skipped via an io.Closer type assertion rather than
// growing every interface with an optional Close.
func (r Readers) Close() error {
	var errs []error
	for _, closer := range []any{r.Happenings, r.Todos, r.Contacts} {
		if c, ok := closer.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
