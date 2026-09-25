package pipeline

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/calendarius/backend/dto4calendarius"
	"github.com/sneat-co/listus/backend/dbo4listus"
	"github.com/sneat-co/listus/backend/dto4listus"
	"github.com/sneat-co/sneat-ai-backend/temporal"
	"github.com/sneat-co/sneat-core-modules/spaceus/dto4spaceus"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// CalendarAPI is the subset of internal/sneatapi.Client this executor needs
// for calendar mutations. Declared here (rather than imported) so this
// package stays a leaf and a test can satisfy it with a fake or an
// httptest-backed *sneatapi.Client (⛔ mutations go through these HTTP calls
// only -- this executor never writes Firestore directly).
type CalendarAPI interface {
	UpdateSlot(ctx context.Context, req dto4calendarius.HappeningSlotRequest) error
	// AdjustSlot stages a per-date deviation of a recurring happening's slot
	// (calendarius's adjust_slot) -- used instead of UpdateSlot when the
	// target happening is recurring, so a "move this Friday's Yoga to 16:00"
	// changes only that one occurrence rather than the recurring template
	// every future occurrence inherits (B2 ruling; moving the whole series is
	// out of MVP scope).
	AdjustSlot(ctx context.Context, req dto4calendarius.HappeningSlotDateRequest) error
	// CancelAdjustment removes a per-date deviation entirely (calendarius's
	// cancel_adjustment), restoring that occurrence to whatever the
	// recurring template says -- used as AdjustSlot's undo (B2 ruling)
	// instead of re-running AdjustSlot with a reconstructed "old" slot,
	// which could drift from fields this executor never read back.
	CancelAdjustment(ctx context.Context, req dto4calendarius.HappeningDateSlotIDRequest) error
	CancelHappening(ctx context.Context, req dto4calendarius.CancelHappeningRequest) error
	// RevokeHappeningCancellation undoes CancelHappening. It takes the SAME
	// CancelHappeningRequest shape (Date/SlotID included) as CancelHappening
	// itself -- calendarius's own facade4calendarius.RevokeHappeningCancellation
	// does, since undoing a per-occurrence cancellation needs the same
	// Date/SlotID that cancelled it, not just the happening ID (S6: "Cancel of
	// recurring cancels the occurrence (date/slotID)").
	RevokeHappeningCancellation(ctx context.Context, req dto4calendarius.CancelHappeningRequest) error
	// CreateHappening creates a new happening (calendar.add_happening,
	// founder ask add-event-prompt.md). action.Args are already-resolved
	// canonical slots (see add_happening.go's addHappeningSlots) by the time
	// addHappening builds this request -- never free text.
	CreateHappening(ctx context.Context, req dto4calendarius.CreateHappeningRequest) (dto4calendarius.CreateHappeningResponse, error)
	// UpdateHappeningTexts renames a happening (calendar.update_happening,
	// rename-only slice B).
	UpdateHappeningTexts(ctx context.Context, req dto4calendarius.UpdateHappeningRequest) error
	// DeleteHappening permanently deletes a happening -- addHappening's undo
	// ONLY (never something a decision or the main LLM asks for directly).
	DeleteHappening(ctx context.Context, req dto4calendarius.HappeningRequest) error
}

// TodoAPI is the subset of internal/sneatapi.Client this executor needs for
// todo/to-buy mutations.
type TodoAPI interface {
	CreateListItems(ctx context.Context, req dto4listus.CreateListItemsRequest) (dto4listus.CreateListItemResponse, error)
	SetListItemsIsDone(ctx context.Context, req dto4listus.ListItemsSetIsDoneRequest) error
	DeleteListItems(ctx context.Context, req dto4listus.ListItemIDsRequest) error
}

// SneatExecutor is the real Executor: every case calls a sneat-go HTTP
// endpoint through CalendarAPI/TodoAPI (⛔ never a Firestore write). It reads
// the target's current state through data.Readers first when a mutation
// needs more than the resolved reference carries (e.g. a slot's current
// duration, or building an undo).
type SneatExecutor struct {
	Calendar   CalendarAPI
	Todo       TodoAPI
	Happenings data.HappeningsReader
	Now        func() time.Time
}

// listKeyFor maps this product's list-kind constant (data.ListKindDo/Buy) to
// listus's standard list key ("do!tasks", "buy!groceries") -- mirrors
// data's own unexported listKeyFor since that package cannot export it
// without also exporting listus's key format as a data-package concern.
func listKeyFor(list string) string {
	switch list {
	case data.ListKindBuy:
		return dbo4listus.BuyGroceriesListID
	default:
		return dbo4listus.DoTasksListID
	}
}

func (e SneatExecutor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Execute implements Executor. spaceID (the pipeline's own current space,
// B3 ruling) is the ONLY source of truth for which space every request
// below targets -- action.Args["spaceID"] is never read (a model-supplied
// value there is meaningless to this executor), and a resolved
// action.Target whose OWN Keys["spaceID"] disagrees with spaceID is refused
// outright rather than executed against whichever space it names: a
// lingering focused/pinned entity from a space the session has since left
// must never let a mutation leak into it.
func (e SneatExecutor) Execute(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
	if action.Target != nil {
		if targetSpace := action.Target.Keys["spaceID"]; targetSpace != "" && targetSpace != spaceID {
			return nil, fmt.Errorf("pipeline: %q belongs to another space", action.Target.Title)
		}
	}
	handler, ok := e.handlers()[action.Kind]
	if !ok {
		return nil, fmt.Errorf("pipeline: no executor case for action kind %q", action.Kind)
	}
	return handler(ctx, spaceID, action)
}

// handlers is the single dispatch table Execute runs through. Every key IS
// a real, reachable executor case -- kinds.go's
// TestSupportedActionKinds_MatchesExecutor (kinds_test.go) cross-checks
// this map's keys against SupportedActionKinds()'s executor-backed entries
// (plus the two undo-only kinds this map also carries, which are
// deliberately excluded from SupportedActionKinds -- see their own doc
// comments below), so the kinds this executor can run and the kinds
// sneatActionInstruction advertises to the model can never drift apart the
// way the old switch/prose pair did (founder bug, buy-add-prompt.md).
func (e SneatExecutor) handlers() map[string]func(context.Context, string, session.Action) (*session.Action, error) {
	return map[string]func(context.Context, string, session.Action) (*session.Action, error){
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening: e.rescheduleHappening,
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentCancelHappening:     e.cancelHappening,
		calendarRevokeCancellationKind: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return nil, e.revokeCancellation(ctx, spaceID, action)
		},
		calendarCancelAdjustmentKind: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return nil, e.cancelAdjustment(ctx, spaceID, action)
		},
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return e.setTodoDone(ctx, spaceID, action, true)
		},
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentReopenTodo: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return e.setTodoDone(ctx, spaceID, action, false)
		},
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return nil, e.deleteTodo(ctx, spaceID, action)
		},
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return e.addListItem(ctx, spaceID, action, data.ListKindDo)
		},
		sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return e.addListItem(ctx, spaceID, action, data.ListKindBuy)
		},
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening:    e.addHappening,
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentUpdateHappening: e.renameHappening,
		calendarDeleteHappeningKind: func(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
			return nil, e.deleteHappening(ctx, spaceID, action)
		},
	}
}

// addHappening creates a new happening via calendarius's create_happening
// (founder ask, add-event-prompt.md: "ability to add calendar events --
// it's core must have feature"). action.Args are the ALREADY-RESOLVED,
// canonical slots resolveAddHappeningSlots produced when this action was
// staged for confirmation (add_happening.go's stageAddHappening) -- this
// method never re-parses free text like "tomorrow"/"Friday". The undo it
// returns permanently deletes the happening just created
// (calendarDeleteHappeningKind), the same "undo of add is delete" shape
// addListItem's own undo uses for a todo/to-buy item -- a just-created
// happening has nothing to preserve, unlike cancelHappening's reversible
// mark.
func (e SneatExecutor) addHappening(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
	if e.Calendar == nil {
		return nil, fmt.Errorf("pipeline: add_happening requires Calendar API")
	}
	brief, err := buildHappeningBrief(action.Args)
	if err != nil {
		return nil, err
	}
	req := dto4calendarius.CreateHappeningRequest{
		SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
		Happening:    brief,
	}
	resp, err := e.Calendar.CreateHappening(ctx, req)
	if err != nil {
		return nil, err
	}
	target := session.EntityRef{Type: sneatdomain.EntityHappening, Title: brief.Title,
		Keys: map[string]string{"spaceID": spaceID, "happeningID": resp.ID}}
	undo := &session.Action{Kind: calendarDeleteHappeningKind, Target: &target,
		Summary: fmt.Sprintf("Delete %q?", brief.Title)}
	return undo, nil
}

// renameHappening implements calendar.update_happening's rename-only slice
// (B) via calendarius's update_happening_texts. Non-destructive -- isDestructive
// (pipeline.go) does not list update_happening, so a rename runs immediately
// once its reference resolves, exactly like a todo complete/reopen, with no
// "yes/no" confirmation (documented policy: a rename does not change WHEN or
// WHETHER something happens, unlike reschedule/cancel, so brief §17's
// destructive/ambiguous-only confirmation rule does not apply to it). Its
// own undo re-runs the same action kind with the original title, mirroring
// how a non-recurring reschedule's undo re-runs itself with the original
// "when" (sneatexecutor.go's rescheduleHappening).
func (e SneatExecutor) renameHappening(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
	if e.Calendar == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: update_happening requires Calendar API and a resolved target")
	}
	title := strings.TrimSpace(action.Args["title"])
	if title == "" {
		return nil, fmt.Errorf("pipeline: update_happening: a title is required")
	}
	happeningID := action.Target.Keys["happeningID"]
	oldTitle := action.Target.Title
	req := dto4calendarius.UpdateHappeningRequest{
		HappeningRequest: dto4calendarius.HappeningRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			HappeningID:  happeningID,
		},
		Title: &title,
	}
	if err := e.Calendar.UpdateHappeningTexts(ctx, req); err != nil {
		return nil, err
	}
	undoTarget := *action.Target
	undoTarget.Title = title
	undo := &session.Action{Kind: action.Kind, Target: &undoTarget,
		Args: map[string]string{"title": oldTitle}, Summary: fmt.Sprintf("Rename %q back to %q?", title, oldTitle)}
	return undo, nil
}

// deleteHappening permanently deletes a happening via calendarius's
// delete_happening -- addHappening's undo ONLY (never something a decision
// or the main LLM asks for directly, same shape as
// calendarRevokeCancellationKind/calendarCancelAdjustmentKind above).
func (e SneatExecutor) deleteHappening(ctx context.Context, spaceID string, action session.Action) error {
	if e.Calendar == nil || action.Target == nil {
		return fmt.Errorf("pipeline: delete_happening requires Calendar API and a resolved target")
	}
	return e.Calendar.DeleteHappening(ctx, dto4calendarius.HappeningRequest{
		SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
		HappeningID:  action.Target.Keys["happeningID"],
	})
}

// calendarRevokeCancellationKind is the undo action kind cancelHappening
// stages -- not a taxonomy intent (it is never something a decision or the
// main LLM asks for directly), just the reverse of cancel_happening.
const calendarRevokeCancellationKind = sneatdomain.ModuleCalendar + ".revoke_cancellation"

// calendarCancelAdjustmentKind is the undo action kind a RECURRING
// reschedule stages (B2 ruling): reversing an adjust_slot deviation means
// removing it (calendarius's cancel_adjustment), not re-adjusting back to a
// reconstructed "old" slot.
const calendarCancelAdjustmentKind = sneatdomain.ModuleCalendar + ".cancel_adjustment"

// rescheduleHappening moves ONE happening's slot to a new time. B2 ruling:
//   - a single (non-recurring) happening reads its full current slot and
//     changes ONLY the time fields, then sends the WHOLE slot back via
//     update_slot -- calendarius replaces the slot map entry wholesale
//     (facade4calendarius/happening_slot_update.go), so a request built from
//     a bare HappeningSlotTiming would silently drop Weekdays/Locations/
//     pricing/participants.
//   - a recurring happening's single occurrence moves via adjust_slot (also a
//     wholesale slot replacement) instead of update_slot, which would
//     rewrite the template every future occurrence inherits. adjust_slot's
//     Date identifies the calendar day the ORIGINAL occurrence being
//     deviated lives on (facade4calendarius/happening_slot_adjust.go stores
//     the adjustment keyed by that date) -- a SAME-DAY retime (a new time of
//     day, same calendar day) is exactly this: Date=that day, the Slot
//     payload's own Start/End carries the new time.
//
// BLOCKER ruling (fix round r3b review): a recurring occurrence CANNOT be
// moved to a DIFFERENT calendar day via adjust_slot, despite the mechanism
// LOOKING like it should support it (Date=original, Slot.Start=new day) --
// calendarius's real read paths never honour a cross-day Slot.Start:
// calendar-day.ts's joinRecurringsWithSinglesAndEmit attaches a deviation to
// its Date (the ORIGINAL day) regardless of what the deviation's own
// Slot.Start date says, and its timing badge reads only start/end TIME;
// dbo4calendarius.Occurrences ignores the Date field entirely too. The
// mutation would silently "succeed" while doing nothing a user can observe.
// So this executor now REFUSES a cross-day move outright (see
// crossDayMoveRefusal) rather than sending a request that looks correct and
// isn't -- same-day retime is unaffected. Moving the WHOLE series remains
// separately out of MVP scope.
//
// Both paths re-read the happening's current state. Undo for a single
// happening re-runs this same action with the original "when" (so it too
// re-reads current state via UpdateSlot). Undo for a recurring happening
// instead cancels the adjustment outright (cancelAdjustment) rather than
// reconstructing the old slot, which could drift from fields this executor
// never read back.
func (e SneatExecutor) rescheduleHappening(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
	if e.Calendar == nil || e.Happenings == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: reschedule_happening requires Calendar API, a happenings reader and a resolved target")
	}
	happeningID := action.Target.Keys["happeningID"]
	current, err := e.Happenings.Get(ctx, spaceID, happeningID)
	if err != nil {
		return nil, fmt.Errorf("pipeline: reading current happening before reschedule: %w", err)
	}
	if current.SlotID == "" || current.Slot == nil {
		return nil, fmt.Errorf("pipeline: happening %q has no slot to reschedule", happeningID)
	}
	when := action.Args["when"]
	// anchor is the ORIGINAL occurrence being modified: for a single
	// happening that is simply its stored date; for a recurring one,
	// current.Start is the stored TEMPLATE date (documented HappeningsReader
	// limitation), so occurrenceAnchor resolves the actual occurrence date --
	// M1: preferring action.Target.Keys["date"] (the occurrence the REFERENCE
	// itself named, e.g. "Friday's yoga") when set, else falling back to
	// recurringAnchor's "next/current occurrence from now" guess. parseWhen
	// resolves the NEW time relative to this same anchor (a bare "4" keeps
	// anchor's date -- now the REFERENCED occurrence's date, not whichever
	// one recurringAnchor happened to guess; an explicit "Wednesday 16:00"
	// still names its own day/time entirely, still anchored off the ORIGINAL
	// occurrence being moved for the cross-day-refusal comparison below).
	anchor := occurrenceAnchor(e.now(), current, action.Target.Keys["date"])
	newStart, ok := parseWhen(e.now(), anchor, when)
	if !ok {
		return nil, fmt.Errorf("pipeline: could not understand the new time %q", when)
	}
	if current.Recurring && !sameCalendarDay(anchor, newStart) {
		return nil, fmt.Errorf("pipeline: %s", crossDayMoveRefusal)
	}
	duration := time.Hour
	if !current.Start.IsZero() && !current.End.IsZero() && current.End.After(current.Start) {
		duration = current.End.Sub(current.Start)
	}
	newEnd := newStart.Add(duration)

	// Copy the FULL current slot and change only Start/End -- Repeats/
	// Weekdays/Weeks/Locations/pricing/participants all ride along unchanged
	// (B2). Replacing the whole Timing struct (rather than just its
	// Start/End) would silently drop TimeZone/UTCOffset/DurationInMinutes.
	slot := *current.Slot
	newTiming := dateTimeOf(newStart, newEnd)
	slot.Start = newTiming.Start
	slot.End = newTiming.End
	// m4: an UTCOffset (as opposed to a named TimeZone) is only valid for
	// the specific instant it was recorded at -- moving the slot to a new
	// time (a different day, possibly across a DST transition) makes a
	// stale copied-over offset wrong. Recompute it for the NEW instant in
	// the same zone parseWhen/newStart already resolved into (see its own
	// doc comment: newStart's Location is the slot's own TZ or the reader's
	// user-zone fallback), rather than leaving the old slot's offset
	// untouched. A named TimeZone needs no such fix -- it is recomputed by
	// the receiving system from the zone name plus the new date/time.
	if slot.UTCOffset != "" {
		slot.UTCOffset = utcOffsetString(newStart)
	}
	if slot.EndUTCOffset != "" || (slot.UTCOffset != "" && !newEnd.IsZero()) {
		slot.EndUTCOffset = utcOffsetString(newEnd)
	}

	happeningReq := dto4calendarius.HappeningRequest{
		SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
		HappeningID:  happeningID,
	}
	var undo *session.Action
	if current.Recurring {
		req := dto4calendarius.HappeningSlotDateRequest{
			HappeningRequest: happeningReq,
			// B2: Date is the ORIGINAL occurrence's date (the calendar day the
			// deviation attaches to), NOT newStart's date -- the slot payload's
			// own Start/End (possibly a different date) carries the move.
			Date: anchor.Format("2006-01-02"),
			Slot: dto4calendarius.HappeningSlotWithID{ID: current.SlotID, HappeningSlot: slot},
		}
		if err := e.Calendar.AdjustSlot(ctx, req); err != nil {
			return nil, err
		}
		undo = &session.Action{
			Kind:   calendarCancelAdjustmentKind,
			Target: action.Target,
			Args:   map[string]string{"date": req.Date, "slotID": current.SlotID},
			Summary: fmt.Sprintf("Move %q back to %s?", action.Target.Title,
				anchor.Format("Mon 15:04")),
		}
	} else {
		req := dto4calendarius.HappeningSlotRequest{
			HappeningRequest: happeningReq,
			Slot:             dto4calendarius.HappeningSlotWithID{ID: current.SlotID, HappeningSlot: slot},
		}
		if err := e.Calendar.UpdateSlot(ctx, req); err != nil {
			return nil, err
		}
		// Undo restores the exact previous time by re-running this same
		// action with anchor's original "when" -- update_slot always
		// re-reads current state, so this stays correct even if other
		// fields changed since.
		undo = &session.Action{
			Kind:    action.Kind,
			Target:  action.Target,
			Args:    map[string]string{"when": anchor.Format("2006-01-02 15:04")},
			Summary: fmt.Sprintf("Move %q back to %s?", action.Target.Title, anchor.Format("Mon 15:04")),
		}
	}
	return undo, nil
}

// utcOffsetString formats t's zone offset as calendarius's "+01:00"-style
// UTCOffset field.
func utcOffsetString(t time.Time) string {
	_, secs := t.Zone()
	sign := "+"
	if secs < 0 {
		sign = "-"
		secs = -secs
	}
	return fmt.Sprintf("%s%02d:%02d", sign, secs/3600, (secs%3600)/60)
}

// recurringAnchor resolves the date a recurring happening's bare
// time-of-day reschedule ("4", "16:00") should apply to: the next real
// occurrence matching the slot's weekly Weekdays rule, at the template's
// stored time-of-day -- today's occurrence counts ONLY when it hasn't
// happened yet (m5: an occurrence already passed today is skipped, the same
// way a user asking to move "today's" class after it already ended surely
// means next week's, not a class that's over). Non-recurring happenings,
// and recurring ones whose repeat rule isn't a weekly/weekdays pattern this
// MVP slice can expand (daily/monthly/yearly -- see HappeningsReader's
// documented limitation), fall back to the stored (possibly stale) Start.
func recurringAnchor(now time.Time, current data.Happening) time.Time {
	if !current.Recurring || current.Slot == nil || current.Slot.Repeats != dbo4calendarius.RepeatPeriodWeekly || len(current.Slot.Weekdays) == 0 {
		return current.Start
	}
	loc := current.Start.Location()
	hour, minute := 0, 0
	if !current.Start.IsZero() {
		hour, minute = current.Start.Hour(), current.Start.Minute()
	}
	// 8, not 7: day 0 (today) can be skipped for having already passed, and
	// the search must still be able to reach the SAME weekday next week.
	for i := 0; i < 8; i++ {
		d := now.AddDate(0, 0, i)
		if !weekdayCodeMatches(current.Slot.Weekdays, d.Weekday()) {
			continue
		}
		candidate := time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc)
		if i == 0 && !candidate.After(now) {
			continue // today's occurrence already happened
		}
		return candidate
	}
	return current.Start
}

// weekdayCodeMatches reports whether wd is named among codes ("mo".."su").
func weekdayCodeMatches(codes []dbo4calendarius.WeekdayCode, wd time.Weekday) bool {
	var code dbo4calendarius.WeekdayCode
	switch wd {
	case time.Monday:
		code = dbo4calendarius.Monday2
	case time.Tuesday:
		code = dbo4calendarius.Tuesday2
	case time.Wednesday:
		code = dbo4calendarius.Wednesday2
	case time.Thursday:
		code = dbo4calendarius.Thursday2
	case time.Friday:
		code = dbo4calendarius.Friday2
	case time.Saturday:
		code = dbo4calendarius.Saturday2
	case time.Sunday:
		code = dbo4calendarius.Sunday2
	}
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// crossDayMoveRefusal is the BLOCKER ruling's fixed refusal text (fix round
// r3b review): moving a recurring happening's single occurrence to a
// DIFFERENT calendar day via one adjust_slot call (Date=original occurrence,
// Slot.Start=new day) looks correct in this MVP's own data model, but
// calendarius's real read paths never honour it -- the web calendar's day
// grouping (calendar-day.ts's joinRecurringsWithSinglesAndEmit) attaches a
// deviation to the ORIGINAL day regardless of what the deviation's own
// Slot.Start date says, and its timing badge reads only the deviation's
// start/end TIME, not its date; dbo4calendarius.Occurrences ignores the
// Date field entirely too. A cross-day move via adjust_slot therefore
// silently does nothing a user can observe: the occurrence still renders on
// its original day, just at the (wrong, because never actually applied
// there) new time. Until calendarius itself supports moving one occurrence
// to another day, this MVP slice refuses the request outright -- both here
// (the executor, a hard backstop) and in rescheduleSummary (so the refusal
// is shown BEFORE asking to confirm, not after) -- rather than silently
// failing to move it. Same-day retime (a new time, same calendar day) is
// unaffected; moving the WHOLE series remains separately out of scope.
const crossDayMoveRefusal = "I can only retime this occurrence on the same day (moving one occurrence to another day isn't supported yet)."

// sameCalendarDay reports whether a and b fall on the same Y-M-D, comparing
// in a's own location: a recurring happening's anchor is already decoded in
// the slot's own zone, and b (a newly parsed "when") is converted into that
// same zone before comparing, so a same-day retime that merely crosses a
// DST boundary, or a bare time-of-day answer, is never mistaken for a
// cross-day move.
func sameCalendarDay(a, b time.Time) bool {
	if loc := a.Location(); loc != nil {
		b = b.In(loc)
	}
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// parseWhen resolves a slot's free-text "when" ("Friday 16:00", "Friday",
// "4", "tomorrow 4pm") to an absolute time.Time, reusing sneat-ai-backend's
// temporal package (per the reuse-existing-sneat-code rule) rather than a
// bespoke parser: temporal.ParseText resolves the date component,
// temporal.NormalizeTime the time component. A "when" naming only a date
// keeps current's time-of-day; one naming only a time keeps current's date.
//
// The resolved clock time is always read in current's OWN location (S5),
// not now's: current is the happening's anchor time, already decoded in the
// slot's own TimeZone (or the reader's user-zone fallback) by
// internal/aichat/data -- "move it to 4" means 16:00 in THAT zone, not
// whatever zone the caller's clock happens to be in (which, for a UTC
// server clock and a happening in America/New_York, is a different zone
// entirely and previously produced a silently wrong instant).
func parseWhen(now, current time.Time, text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	loc := current.Location()
	fields := strings.Fields(text)
	if len(fields) >= 2 {
		timePart, datePart := fields[len(fields)-1], strings.Join(fields[:len(fields)-1], " ")
		if tr, err := temporal.NormalizeTime(timePart); err == nil {
			if d, ok := temporal.ParseText(now, datePart); ok {
				return atTime(d, tr.Time, loc), true
			}
		}
	}
	if d, ok := temporal.ParseText(now, text); ok {
		return atTime(d, current.Format("15:04"), loc), true
	}
	if tr, err := temporal.NormalizeTime(text); err == nil {
		return atTime(current, tr.Time, loc), true
	}
	return time.Time{}, false
}

// atTime combines day's date with an "HH:MM" clock time.
func atTime(day time.Time, hhmm string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("15:04", hhmm, loc)
	if err != nil {
		return day
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, loc)
}

func dateTimeOf(start, end time.Time) dbo4calendarius.Timing {
	return dbo4calendarius.Timing{
		Start: dbo4calendarius.DateTime{Date: start.Format("2006-01-02"), Time: start.Format("15:04")},
		End:   dbo4calendarius.DateTime{Date: end.Format("2006-01-02"), Time: end.Format("15:04")},
	}
}

// occurrenceAnchor resolves the ORIGINAL-occurrence baseline cancel/
// reschedule both anchor off, overriding recurringAnchor's own "next/current
// occurrence from now" guess with refDate when one is given (M1, fix round
// r3b review): refDate is the occurrence date the RESOLVER already matched
// straight from the reference text itself ("cancel Friday's yoga", "move
// Friday's yoga to 16:00" -- significantTerms' day window, carried on
// session.EntityRef.Keys["date"] by Resolver.Resolve) or from a week-view
// row's own focused occurrence (pipeline.go's happeningRowsInWindow sets
// the same key on a recurring row's ref, per its own doc comment -- fix
// round r3b's M1 nit fixed here: it was previously misattributed to
// chatapp's blockFor, which never sets it). Without this, "move Friday's yoga to 16:00"
// silently retimed WHATEVER occurrence recurringAnchor happened to guess
// (typically the next one from "now", e.g. Monday's, not Friday's) because
// a bare time-only "when" carries no date of its own for parseWhen/
// resolveOccurrenceDate to fall back on. Only recurringAnchor's
// time-of-day is kept; refDate supplies the calendar day. An unparseable or
// empty refDate leaves recurringAnchor's own guess untouched.
func occurrenceAnchor(now time.Time, current data.Happening, refDate string) time.Time {
	anchor := recurringAnchor(now, current)
	if refDate == "" {
		return anchor
	}
	d, err := time.Parse("2006-01-02", refDate)
	if err != nil {
		return anchor
	}
	loc := anchor.Location()
	return time.Date(d.Year(), d.Month(), d.Day(), anchor.Hour(), anchor.Minute(), 0, 0, loc)
}

// resolveOccurrenceDate resolves WHICH occurrence of a recurring happening
// an action names, for cancelHappening (and available to the confirmation
// builder in pipeline.go, which must compute the identical date BEFORE
// asking -- B1 ruling). An explicit date/weekday word in when ("Friday",
// "Friday's yoga") wins over refDate (M1's reference-carried occurrence
// date), which in turn wins over occurrenceAnchor's own
// recurringAnchor-based default; a prior version ignored when (and refDate)
// entirely and always cancelled recurringAnchor's occurrence, silently
// cancelling the wrong day whenever the user named one explicitly.
func resolveOccurrenceDate(now time.Time, current data.Happening, when, refDate string) time.Time {
	anchor := occurrenceAnchor(now, current, refDate)
	d, ok := parseTemporalPhrase(now, when)
	if !ok {
		return anchor
	}
	loc := anchor.Location()
	return time.Date(d.Year(), d.Month(), d.Day(), anchor.Hour(), anchor.Minute(), 0, 0, loc)
}

// parseTemporalPhrase resolves a free-text date phrase via
// sneat-ai-backend/temporal, retrying with a possessive weekday
// ("Friday's") stripped (m1) when the phrase as given doesn't match --
// temporal.ParseText's own vocabulary has no possessive form.
func parseTemporalPhrase(now time.Time, text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	if d, ok := temporal.ParseText(now, text); ok {
		return d, true
	}
	if stripped := stripPossessive(strings.ToLower(text)); stripped != strings.ToLower(text) {
		return temporal.ParseText(now, stripped)
	}
	return time.Time{}, false
}

// cancelHappening cancels ONE happening. For a recurring happening it
// cancels just the current occurrence (Date+SlotID), never the whole
// series -- calendarius's own CancelHappeningRequest distinguishes the two
// by whether Date is set (S6: "Cancel of recurring cancels the occurrence
// (date/slotID) and says so"); a single (non-recurring) happening has no
// meaningful "occurrence" distinct from itself, so Date/SlotID stay empty.
// It re-reads the happening (like rescheduleHappening) both to know
// Recurring and, when recurring, to resolve which occurrence date and slot
// are being cancelled via resolveOccurrenceDate (B1: honours an explicit
// date the user named, e.g. "cancel Friday's yoga", rather than always
// defaulting to "today's/next occurrence").
func (e SneatExecutor) cancelHappening(ctx context.Context, spaceID string, action session.Action) (*session.Action, error) {
	if e.Calendar == nil || e.Happenings == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: cancel_happening requires Calendar API, a happenings reader and a resolved target")
	}
	happeningID := action.Target.Keys["happeningID"]
	current, err := e.Happenings.Get(ctx, spaceID, happeningID)
	if err != nil {
		return nil, fmt.Errorf("pipeline: reading current happening before cancel: %w", err)
	}
	req := dto4calendarius.CancelHappeningRequest{
		HappeningRequest: dto4calendarius.HappeningRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			HappeningID:  happeningID,
		},
	}
	undoArgs := map[string]string{}
	if current.Recurring {
		occ := resolveOccurrenceDate(e.now(), current, action.Args["when"], action.Target.Keys["date"])
		req.Date = occ.Format("2006-01-02")
		req.SlotID = current.SlotID
		undoArgs["date"] = req.Date
		undoArgs["slotID"] = req.SlotID
	}
	if err := e.Calendar.CancelHappening(ctx, req); err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("Un-cancel %q?", action.Target.Title)
	if current.Recurring {
		summary = fmt.Sprintf("Un-cancel %q on %s (this occurrence only)?", action.Target.Title, req.Date)
	}
	undo := &session.Action{Kind: calendarRevokeCancellationKind, Target: action.Target, Args: undoArgs, Summary: summary}
	return undo, nil
}

func (e SneatExecutor) revokeCancellation(ctx context.Context, spaceID string, action session.Action) error {
	if e.Calendar == nil || action.Target == nil {
		return fmt.Errorf("pipeline: revoke_cancellation requires Calendar API and a resolved target")
	}
	happeningID := action.Target.Keys["happeningID"]
	return e.Calendar.RevokeHappeningCancellation(ctx, dto4calendarius.CancelHappeningRequest{
		HappeningRequest: dto4calendarius.HappeningRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			HappeningID:  happeningID,
		},
		// Same Date/SlotID CancelHappening cancelled with -- empty for a
		// single happening's whole-happening cancellation, exactly matching
		// what un-cancels it.
		Date:   action.Args["date"],
		SlotID: action.Args["slotID"],
	})
}

// cancelAdjustment undoes a RECURRING reschedule's adjust_slot (B2 ruling):
// removes the per-date deviation outright (calendarius's cancel_adjustment)
// instead of re-adjusting back to a reconstructed "old" slot, restoring the
// occurrence to whatever the recurring template says.
func (e SneatExecutor) cancelAdjustment(ctx context.Context, spaceID string, action session.Action) error {
	if e.Calendar == nil || action.Target == nil {
		return fmt.Errorf("pipeline: cancel_adjustment requires Calendar API and a resolved target")
	}
	happeningID := action.Target.Keys["happeningID"]
	return e.Calendar.CancelAdjustment(ctx, dto4calendarius.HappeningDateSlotIDRequest{
		HappeningRequest: dto4calendarius.HappeningRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			HappeningID:  happeningID,
		},
		Date:   action.Args["date"],
		SlotID: action.Args["slotID"],
	})
}

func (e SneatExecutor) setTodoDone(ctx context.Context, spaceID string, action session.Action, done bool) (*session.Action, error) {
	if e.Todo == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: complete/reopen todo requires Todo API and a resolved target")
	}
	list := action.Target.Keys["list"]
	itemID := action.Target.Keys["itemID"]
	req := dto4listus.ListItemsSetIsDoneRequest{
		ListItemIDsRequest: dto4listus.ListItemIDsRequest{
			ListRequest: dto4listus.ListRequest{
				SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
				ListID:       dbo4listus.ListKey(listKeyFor(list)),
			},
			ItemIDs: []string{itemID},
		},
		IsDone: done,
	}
	if err := e.Todo.SetListItemsIsDone(ctx, req); err != nil {
		return nil, err
	}
	undoKind := sneatdomain.ModuleTodo + "." + sneatdomain.IntentReopenTodo
	if !done {
		undoKind = sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo
	}
	undo := &session.Action{Kind: undoKind, Target: action.Target}
	return undo, nil
}

// deleteTodo deletes one or more items in a single call. Target.Keys
// ["itemID"] is usually a single ID, but addListItem's own undo (below) can
// set it to several comma-joined IDs when the add it is undoing created
// more than one item -- DeleteListItems already accepts a []string, so a
// plain strings.Split (a no-op comma split for the single-ID case) covers
// both without a second code path.
func (e SneatExecutor) deleteTodo(ctx context.Context, spaceID string, action session.Action) error {
	if e.Todo == nil || action.Target == nil {
		return fmt.Errorf("pipeline: delete_todo requires Todo API and a resolved target")
	}
	list := action.Target.Keys["list"]
	itemID := action.Target.Keys["itemID"]
	return e.Todo.DeleteListItems(ctx, dto4listus.ListItemIDsRequest{
		ListRequest: dto4listus.ListRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			ListID:       dbo4listus.ListKey(listKeyFor(list)),
		},
		ItemIDs: strings.Split(itemID, ","),
	})
}

// splitListItemTitle splits a free-text title into one or more items (fix
// round: founder buy-add-prompt.md reported "buy milk and bread tomorrow"
// producing a single item titled "milk and bread" instead of two). It
// splits ONLY on a comma, newline, or semicolon -- deliberately NOT on the
// word "and" (coordinator ruling, PR #56 review round 2): "fish and
// chips", "salt and pepper", "Q and A" are single items whose title
// legitimately contains "and", and are indistinguishable from a two-item
// list by text alone. Splitting on "and" would silently corrupt those.
// Instead, sneatActionInstruction's slot contract (llm.go) tells the model
// directly to comma-separate multiple items in slots.title -- so "buy milk
// and bread" is expected to arrive here as title="milk, bread" already,
// the model having done the one-item-per-comma normalisation itself. This
// function's job is only to split what the model was told to comma-join,
// and to be a harmless no-op (one item) for an ordinary single-item title.
func splitListItemTitle(title string) []string {
	parts := listItemSeparator.Split(title, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// listItemSeparator matches a comma, semicolon, or newline between list
// items -- see splitListItemTitle's doc comment. "and" is deliberately not
// a separator.
var listItemSeparator = regexp.MustCompile(`[,;\n]`)

// addListItem creates one or more todo/to-buy items (splitListItemTitle) in
// the PIPELINE's spaceID (B3 ruling: "ignore/strip Args[\"spaceID\"]
// everywhere") -- action.Args["spaceID"] is never read, however this
// action was built (a model has no legitimate reason to name a space; even
// a genuine multi-space feature would route through a fresh pipeline call,
// not a client-supplied field). The undo it returns deletes every item
// this call created, not just the first, via deleteTodo's comma-joined
// itemID contract.
func (e SneatExecutor) addListItem(ctx context.Context, spaceID string, action session.Action, list string) (*session.Action, error) {
	if e.Todo == nil {
		return nil, fmt.Errorf("pipeline: add todo/to-buy requires Todo API")
	}
	titles := splitListItemTitle(action.Args["title"])
	if len(titles) == 0 {
		return nil, fmt.Errorf("pipeline: add %s: a title is required", list)
	}
	items := make([]dto4listus.CreateListItemRequest, 0, len(titles))
	for _, title := range titles {
		items = append(items, dto4listus.CreateListItemRequest{ListItemBase: dbo4listus.ListItemBase{Title: title}})
	}
	req := dto4listus.CreateListItemsRequest{
		ListRequest: dto4listus.ListRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			ListID:       dbo4listus.ListKey(listKeyFor(list)),
		},
		Items: items,
	}
	resp, err := e.Todo.CreateListItems(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.CreatedItems) == 0 {
		return nil, nil
	}
	createdTitles := make([]string, 0, len(resp.CreatedItems))
	createdIDs := make([]string, 0, len(resp.CreatedItems))
	for _, it := range resp.CreatedItems {
		createdTitles = append(createdTitles, it.Title)
		createdIDs = append(createdIDs, it.ID)
	}
	target := session.EntityRef{Type: sneatdomain.EntityTodo, Title: strings.Join(createdTitles, ", "),
		Keys: map[string]string{"spaceID": spaceID, "list": list, "itemID": strings.Join(createdIDs, ",")}}
	undo := &session.Action{Kind: sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo, Target: &target}
	return undo, nil
}
