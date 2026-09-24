package data

import (
	"context"
	"reflect"
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
	cfg config.Config
	ts  oauth2.TokenSource
}

// NewFirestoreHappenings builds a HappeningsReader over Firestore.
func NewFirestoreHappenings(cfg config.Config, ts oauth2.TokenSource) HappeningsReader {
	return &firestoreHappenings{cfg: cfg, ts: ts}
}

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
	db, err := firestoredb.Open(ctx, r.cfg, r.ts)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = db.Close() }()

	q := dal.NewQueryBuilder(dal.From(happeningsCollectionRef(spaceID))).
		WhereField("status", dal.Equal, "active").
		SelectIntoRecord(func() record.Record {
			return record.NewRecordWithIncompleteKey("happenings", reflect.String, &calendariusdbo.HappeningDbo{})
		})

	var records []record.Record
	err = db.DAL().RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
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
	db, err := firestoredb.Open(ctx, r.cfg, r.ts)
	if err != nil {
		return Happening{}, err
	}
	defer func() { _ = db.Close() }()
	var dbo calendariusdbo.HappeningDbo
	key := calendariusdbo.NewHappeningKey(coretypes.SpaceID(spaceID), happeningID)
	rec := record.NewRecordWithData(key, &dbo)
	err = db.DAL().RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
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
	cfg config.Config
	ts  oauth2.TokenSource
}

// NewFirestoreTodos builds a TodosReader over Firestore.
func NewFirestoreTodos(cfg config.Config, ts oauth2.TokenSource) TodosReader {
	return &firestoreTodos{cfg: cfg, ts: ts}
}

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
	db, err := firestoredb.Open(ctx, r.cfg, r.ts)
	if err != nil {
		return listusdbo.ListDbo{}, err
	}
	defer func() { _ = db.Close() }()

	// m2: reuse listus's own key helper (dal4listus.NewListKey) rather than a
	// hand-rolled spaces/{id}/ext/listus/lists/{key} path -- the same fix B1
	// applied to calendarius's happening key.
	var l listusdbo.ListDbo
	key := dal4listus.NewListKey(coretypes.SpaceID(spaceID), listusdbo.ListKey(listKey))
	rec := record.NewRecordWithData(key, &l)
	err = db.DAL().RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
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
	return out, nil
}

func (r *firestoreTodos) FindByTitle(ctx context.Context, spaceID, query string) ([]Todo, error) {
	q := normalizeForSearch(query)
	var out []Todo
	for _, list := range []string{ListKindDo, ListKindBuy} {
		items, err := r.List(ctx, spaceID, list)
		if err != nil {
			continue // one list missing/unreadable must not fail the whole search
		}
		for _, it := range items {
			if containsFold(it.Title, q) {
				out = append(out, it)
			}
		}
	}
	return out, nil
}
