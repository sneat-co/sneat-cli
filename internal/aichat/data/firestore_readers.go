package data

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	"github.com/sneat-co/calendarius/backend/const4calendarius"
	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/listus/backend/dal4listus"
	listusdbo "github.com/sneat-co/listus/backend/dbo4listus"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-core-modules/spaceus/dbo4spaceus"
	"github.com/sneat-co/sneat-go-core/coretypes"
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
	session sessionProvider
	// loc is the "user zone" a slot with no TimeZone/UTCOffset of its own
	// decodes in (S5/S4). Defaults to time.Local when the caller passes no
	// WithLocation option.
	loc *time.Location
}

// ReaderOption configures a Firestore-backed reader's optional behaviour.
type ReaderOption func(*readerOptions)

type readerOptions struct{ loc *time.Location }

// WithLocation sets the "user zone" (S4 ruling: "reader takes the user zone
// as a parameter (not hardcoded time.Local)") a slot with no TimeZone/
// UTCOffset of its own decodes in. Callers that know the signed-in user's
// actual configured zone (a future settings/flag/env source) pass it here;
// omitting the option keeps the previous default, time.Local.
func WithLocation(loc *time.Location) ReaderOption {
	return func(o *readerOptions) {
		if loc != nil {
			o.loc = loc
		}
	}
}

func newReaderOptions(opts []ReaderOption) readerOptions {
	o := readerOptions{loc: time.Local}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// NewFirestoreHappenings builds a HappeningsReader over session, an
// ALREADY-OWNED *firestoredb.Session (m6: "one Firestore client per chat
// session shared by all three readers") -- the caller (internal/chatapp)
// opens ONE Session and shares it across Happenings/Todos/Contacts, rather
// than each reader opening its own client. See WithLocation (S4) for the
// "user zone" a TimeZone/UTCOffset-less slot decodes in.
func NewFirestoreHappenings(session *firestoredb.Session, opts ...ReaderOption) HappeningsReader {
	o := newReaderOptions(opts)
	return &firestoreHappenings{session: firestoreSession{s: session}, loc: o.loc}
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

// newHappeningQueryRecord builds an empty, incomplete-key envelope for one
// query result row. Named (rather than an inline closure in list) so it has
// its own direct unit test: a real backend's query executor invokes it per
// decoded document, but this package's own fake QueryExecutor (used in unit
// tests) supplies already-built records and never calls it -- mirrors
// internal/firestoredb's newContactRecord.
func newHappeningQueryRecord() record.Record {
	return record.NewRecordWithIncompleteKey("happenings", reflect.String, &calendariusdbo.HappeningDbo{})
}

func (r *firestoreHappenings) list(ctx context.Context, spaceID string) ([]calendariusdbo.HappeningDbo, []string, error) {
	db, err := r.session.DB(ctx)
	if err != nil {
		return nil, nil, err
	}

	q := dal.NewQueryBuilder(dal.From(happeningsCollectionRef(spaceID))).
		WhereField("status", dal.Equal, "active").
		SelectIntoRecord(newHappeningQueryRecord)

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

// parseSlotDateTime interprets a calendarius DateTime in, in priority
// order: the slot's own TimeZone (an IANA name); else its UTCOffset (a
// fixed "+01:00"-style offset, honoured when TimeZone is empty -- S4);
// else fallback, the reader's configured "user zone". A prior version
// always used time.Parse's implicit UTC, which was silently wrong for a
// slot with an explicit TimeZone/UTCOffset and for a user whose local zone
// isn't UTC.
func parseSlotDateTime(dt calendariusdbo.DateTime, tz, utcOffset string, fallback *time.Location) (time.Time, bool) {
	loc := fallback
	if loc == nil {
		loc = time.UTC
	}
	switch {
	case tz != "":
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	case utcOffset != "":
		if l, ok := fixedZoneFromOffset(utcOffset); ok {
			loc = l
		}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", dt.Date+" "+dt.Time, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// fixedZoneFromOffset parses a "+01:00"/"-05:30"-style UTC offset into a
// fixed-offset time.Location.
func fixedZoneFromOffset(s string) (*time.Location, bool) {
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return nil, false
	}
	h, hErr := strconv.Atoi(s[1:3])
	m, mErr := strconv.Atoi(s[4:6])
	if hErr != nil || mErr != nil {
		return nil, false
	}
	secs := h*3600 + m*60
	if s[0] == '-' {
		secs = -secs
	}
	return time.FixedZone(s, secs), true
}

// firstSlotWindow returns the earliest slot's start/end as time.Time,
// interpreted per parseSlotDateTime's rule. fallback is the reader's
// configured "user zone" (see firestoreHappenings.loc), used only for a slot
// that has no TimeZone/UTCOffset of its own. End prefers the slot's own
// EndUTCOffset over its (start) UTCOffset -- they can legitimately differ
// across a DST transition -- falling back to UTCOffset when EndUTCOffset is
// unset (the common case: one offset for the whole slot).
func firstSlotWindow(h calendariusdbo.HappeningDbo, fallback *time.Location) (start, end time.Time, slotID string, recurring bool, slot *calendariusdbo.HappeningSlot) {
	for id, s := range h.Slots {
		if s == nil {
			continue
		}
		recurring = recurring || s.Repeats != "" && s.Repeats != calendariusdbo.RepeatPeriodOnce
		st, ok := parseSlotDateTime(s.Start, s.TimeZone, s.UTCOffset, fallback)
		if !ok {
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
			end = time.Time{}
			if s.End.Time != "" {
				endOffset := s.EndUTCOffset
				if endOffset == "" {
					endOffset = s.UTCOffset
				}
				if et, ok := parseSlotDateTime(s.End, s.TimeZone, endOffset, fallback); ok {
					end = et
				}
			}
		}
	}
	return start, end, slotID, recurring, slot
}

func toHappening(spaceID, id string, h calendariusdbo.HappeningDbo, fallback *time.Location) Happening {
	start, end, slotID, recurring, slot := firstSlotWindow(h, fallback)
	return Happening{ID: id, SpaceID: spaceID, Title: h.Title, Start: start, End: end, SlotID: slotID, Recurring: recurring, Slot: slot}
}

func (r *firestoreHappenings) Window(ctx context.Context, spaceID string, from, to time.Time) ([]Happening, error) {
	dbos, ids, err := r.list(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	var out []Happening
	for i, dbo := range dbos {
		h := toHappening(spaceID, ids[i], dbo, r.loc)
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
			out = append(out, toHappening(spaceID, ids[i], dbo, r.loc))
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
	return toHappening(spaceID, happeningID, dbo, r.loc), nil
}

// firestoreTodos reads listus list documents directly from Firestore. Like
// firestoreHappenings, the collection path is unverified against a live
// space.
type firestoreTodos struct {
	session sessionProvider
}

// NewFirestoreTodos builds a TodosReader over session, an ALREADY-OWNED
// *firestoredb.Session shared with the other readers (m6; see
// NewFirestoreHappenings's doc comment).
func NewFirestoreTodos(session *firestoredb.Session) TodosReader {
	return &firestoreTodos{session: firestoreSession{s: session}}
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
