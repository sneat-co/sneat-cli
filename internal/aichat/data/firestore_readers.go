package data

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	"github.com/sneat-co/calendarius/backend/const4calendarius"
	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/listus/backend/dal4listus"
	listusdbo "github.com/sneat-co/listus/backend/dbo4listus"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-core-modules/spaceus/dbo4spaceus"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"golang.org/x/oauth2"
)

// normalizeForSearch/containsFold back FindByTitle's case-insensitive
// substring search -- deliberately not a fuzzy matcher; that belongs to a
// future iteration, not this MVP slice.
func normalizeForSearch(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func containsFold(haystack, needleLower string) bool {
	return strings.Contains(strings.ToLower(haystack), needleLower)
}

// firestoreHappenings reads calendarius happenings directly from Firestore,
// following the same read-as-the-user pattern as
// internal/firestoredb.ContactsReader.
//
// UNVERIFIED AGAINST A LIVE SPACE: the collection path
// (spaces/{id}/ext/calendarius/happenings) mirrors contactus's own
// spaces/{id}/ext/contactus/contacts convention, but this MVP slice was not
// run against a real or emulated Firestore space to confirm it -- see the
// final report's "manual / not done" list.
type firestoreHappenings struct {
	session *firestoredb.Session
}

// NewFirestoreHappenings builds a HappeningsReader over ONE lazily-opened,
// reused Firestore client (m4) rather than a fresh client per call.
func NewFirestoreHappenings(cfg config.Config, ts oauth2.TokenSource) HappeningsReader {
	return &firestoreHappenings{session: firestoredb.NewSession(cfg, ts)}
}

// Close releases the reader's Firestore client, if one was ever opened.
// HappeningsReader does not declare Close (not every implementation needs
// one, e.g. FakeHappenings) -- a caller that wants to release it type-
// asserts for io.Closer, or a concrete *firestoreHappenings.
func (r *firestoreHappenings) Close() error { return r.session.Close() }

// happeningsCollectionRef builds the happenings collection ref the same way
// calendarius itself does (dbo4calendarius.NewHappeningKey's parent), rather
// than a hand-rolled spaces/{id}/ext/{module} path -- see B1: the earlier
// hand-rolled path here matched, but Get below used a bare top-level
// "happenings" collection, which does not.
func happeningsCollectionRef(spaceID string) dal.CollectionRef {
	moduleKey := dbo4spaceus.NewSpaceModuleKey(coretypes.SpaceID(spaceID), const4calendarius.ExtensionID)
	return dal.NewCollectionRef(const4calendarius.HappeningsCollection, "", moduleKey)
}

func (r *firestoreHappenings) list(ctx context.Context, spaceID string) ([]calendariusdbo.HappeningDbo, []string, error) {
	db, err := r.session.DB(ctx)
	if err != nil {
		return nil, nil, err
	}

	q := dal.NewQueryBuilder(dal.From(happeningsCollectionRef(spaceID))).
		WhereField("status", dal.Equal, "active").
		SelectIntoRecord(func() record.Record {
			return record.NewRecordWithIncompleteKey("happenings", reflect.String, &calendariusdbo.HappeningDbo{})
		})

	var records []record.Record
	err = db.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		records, err = dal.ExecuteQueryAndReadAllToRecords(ctx, q, tx)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	dbos := make([]calendariusdbo.HappeningDbo, 0, len(records))
	ids := make([]string, 0, len(records))
	for _, rec := range records {
		id, _ := rec.Key().ID.(string)
		dbo, _ := rec.Data().(*calendariusdbo.HappeningDbo)
		if dbo == nil {
			continue
		}
		dbos = append(dbos, *dbo)
		ids = append(ids, id)
	}
	return dbos, ids, nil
}

// firstSlotWindow returns the earliest slot's start/end as time.Time in the
// happening's own stated timezone-less local wall time (calendarius stores
// date+time as separate strings; this MVP slice treats them as UTC, which is
// wrong for a user in another timezone -- a follow-up, not silently ignored:
// see the final report).
func firstSlotWindow(h calendariusdbo.HappeningDbo) (start, end time.Time, slotID string, recurring bool, slot *calendariusdbo.HappeningSlot) {
	for id, s := range h.Slots {
		if s == nil {
			continue
		}
		recurring = recurring || s.Repeats != "" && s.Repeats != calendariusdbo.RepeatPeriodOnce
		st, sErr := time.Parse("2006-01-02 15:04", s.Start.Date+" "+s.Start.Time)
		if sErr != nil {
			continue
		}
		if start.IsZero() || st.Before(start) {
			start = st
			slotID = id
			// Copy, not the map's own pointer: a later mutation on Happening's
			// Slot (e.g. changing Timing for a reschedule) must never alias
			// h.Slots, which a caller may still hold/reuse.
			cp := *s
			slot = &cp
			if s.End.Time != "" {
				if et, eErr := time.Parse("2006-01-02 15:04", s.End.Date+" "+s.End.Time); eErr == nil {
					end = et
				}
			}
		}
	}
	return start, end, slotID, recurring, slot
}

func toHappening(spaceID, id string, h calendariusdbo.HappeningDbo) Happening {
	start, end, slotID, recurring, slot := firstSlotWindow(h)
	return Happening{ID: id, SpaceID: spaceID, Title: h.Title, Start: start, End: end, SlotID: slotID, Recurring: recurring, Slot: slot}
}

func (r *firestoreHappenings) Window(ctx context.Context, spaceID string, from, to time.Time) ([]Happening, error) {
	dbos, ids, err := r.list(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	var out []Happening
	for i, dbo := range dbos {
		h := toHappening(spaceID, ids[i], dbo)
		if h.Recurring || (!h.Start.IsZero() && !h.Start.Before(from) && h.Start.Before(to)) {
			out = append(out, h)
		}
	}
	return out, nil
}

func (r *firestoreHappenings) FindByTitle(ctx context.Context, spaceID, query string) ([]Happening, error) {
	dbos, ids, err := r.list(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	q := normalizeForSearch(query)
	var out []Happening
	for i, dbo := range dbos {
		if containsFold(dbo.Title, q) {
			out = append(out, toHappening(spaceID, ids[i], dbo))
		}
	}
	return out, nil
}

// Get reads one happening by its real path (spaces/{id}/ext/calendarius/
// happenings/{id}, via dbo4calendarius.NewHappeningKey) -- B1: a prior
// version read a bare top-level "happenings/{id}" via GetDoc, which only
// matched fakes in tests, never a real space.
func (r *firestoreHappenings) Get(ctx context.Context, spaceID, happeningID string) (Happening, error) {
	db, err := r.session.DB(ctx)
	if err != nil {
		return Happening{}, err
	}
	var dbo calendariusdbo.HappeningDbo
	key := calendariusdbo.NewHappeningKey(coretypes.SpaceID(spaceID), happeningID)
	rec := record.NewRecordWithData(key, &dbo)
	err = db.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		return tx.Get(ctx, rec)
	})
	if err != nil {
		return Happening{}, err
	}
	return toHappening(spaceID, happeningID, dbo), nil
}

// firestoreTodos reads listus list documents directly from Firestore. Like
// firestoreHappenings, the collection path is unverified against a live
// space.
type firestoreTodos struct {
	session *firestoredb.Session
}

// NewFirestoreTodos builds a TodosReader over ONE lazily-opened, reused
// Firestore client (m4) rather than a fresh client per call.
func NewFirestoreTodos(cfg config.Config, ts oauth2.TokenSource) TodosReader {
	return &firestoreTodos{session: firestoredb.NewSession(cfg, ts)}
}

// Close releases the reader's Firestore client, if one was ever opened (see
// firestoreHappenings.Close's doc comment on why this isn't in TodosReader).
func (r *firestoreTodos) Close() error { return r.session.Close() }

// listKeyFor maps this package's list-kind constant to listus's standard
// list key ("do!tasks", "buy!groceries").
func listKeyFor(list string) string {
	switch list {
	case ListKindBuy:
		return listusdbo.BuyGroceriesListID
	default:
		return listusdbo.DoTasksListID
	}
}

func (r *firestoreTodos) getList(ctx context.Context, spaceID, listKey string) (listusdbo.ListDbo, error) {
	db, err := r.session.DB(ctx)
	if err != nil {
		return listusdbo.ListDbo{}, err
	}

	// m2: reuse listus's own key helper (dal4listus.NewListKey) rather than a
	// hand-rolled spaces/{id}/ext/listus/lists/{key} path -- the same fix B1
	// applied to calendarius's happening key.
	var l listusdbo.ListDbo
	key := dal4listus.NewListKey(coretypes.SpaceID(spaceID), listusdbo.ListKey(listKey))
	rec := record.NewRecordWithData(key, &l)
	err = db.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
		return tx.Get(ctx, rec)
	})
	if err != nil {
		return listusdbo.ListDbo{}, err
	}
	return l, nil
}

func toTodo(spaceID, list string, it *listusdbo.ListItemBrief) Todo {
	if it == nil {
		return Todo{}
	}
	return Todo{ID: it.ID, SpaceID: spaceID, List: list, Title: it.Title, Done: it.IsDone()}
}

func (r *firestoreTodos) List(ctx context.Context, spaceID, list string) ([]Todo, error) {
	l, err := r.getList(ctx, spaceID, listKeyFor(list))
	if err != nil {
		return nil, err
	}
	out := make([]Todo, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, toTodo(spaceID, list, it))
	}
	sortNotDoneFirst(out)
	return out, nil
}

// sortNotDoneFirst orders not-done items before done ones (m5), preserving
// each group's original relative order (a stable sort) rather than
// re-sorting by title/date -- listus does not define an item ordering this
// MVP slice should second-guess beyond done-state grouping.
func sortNotDoneFirst(items []Todo) {
	sort.SliceStable(items, func(i, j int) bool { return !items[i].Done && items[j].Done })
}

// FindByTitle searches both list kinds. A list that does not exist YET in
// this space (record.IsNotFound) is not an error -- an empty/never-created
// to-buy list, say, must not sink the whole search. Any other read error
// (auth failure, network, a malformed document) is surfaced rather than
// silently swallowed (m6): a caller must be able to tell "no matches" from
// "the search could not run".
func (r *firestoreTodos) FindByTitle(ctx context.Context, spaceID, query string) ([]Todo, error) {
	q := normalizeForSearch(query)
	var out []Todo
	for _, list := range []string{ListKindDo, ListKindBuy} {
		items, err := r.List(ctx, spaceID, list)
		if err != nil {
			if record.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("data: searching %s list: %w", list, err)
		}
		for _, it := range items {
			if containsFold(it.Title, q) {
				out = append(out, it)
			}
		}
	}
	return out, nil
}
