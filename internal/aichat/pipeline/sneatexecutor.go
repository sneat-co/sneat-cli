package pipeline

import (
	"context"
	"fmt"
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
	CancelHappening(ctx context.Context, req dto4calendarius.CancelHappeningRequest) error
	RevokeHappeningCancellation(ctx context.Context, req dto4calendarius.HappeningRequest) error
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

// Execute implements Executor.
func (e SneatExecutor) Execute(ctx context.Context, action session.Action) (*session.Action, error) {
	switch action.Kind {
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentRescheduleHappening:
		return e.rescheduleHappening(ctx, action)
	case sneatdomain.ModuleCalendar + "." + sneatdomain.IntentCancelHappening:
		return e.cancelHappening(ctx, action)
	case calendarRevokeCancellationKind:
		return nil, e.revokeCancellation(ctx, action)
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentCompleteTodo:
		return e.setTodoDone(ctx, action, true)
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentReopenTodo:
		return e.setTodoDone(ctx, action, false)
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo:
		return nil, e.deleteTodo(ctx, action)
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddTodo:
		return e.addListItem(ctx, action, data.ListKindDo)
	case sneatdomain.ModuleTodo + "." + sneatdomain.IntentAddToBuy:
		return e.addListItem(ctx, action, data.ListKindBuy)
	default:
		return nil, fmt.Errorf("pipeline: no executor case for action kind %q", action.Kind)
	}
}

// calendarRevokeCancellationKind is the undo action kind cancelHappening
// stages -- not a taxonomy intent (it is never something a decision or the
// main LLM asks for directly), just the reverse of cancel_happening.
const calendarRevokeCancellationKind = sneatdomain.ModuleCalendar + ".revoke_cancellation"

func (e SneatExecutor) rescheduleHappening(ctx context.Context, action session.Action) (*session.Action, error) {
	if e.Calendar == nil || e.Happenings == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: reschedule_happening requires Calendar API, a happenings reader and a resolved target")
	}
	spaceID := action.Target.Keys["spaceID"]
	happeningID := action.Target.Keys["happeningID"]
	current, err := e.Happenings.Get(ctx, spaceID, happeningID)
	if err != nil {
		return nil, fmt.Errorf("pipeline: reading current happening before reschedule: %w", err)
	}
	if current.SlotID == "" {
		return nil, fmt.Errorf("pipeline: happening %q has no slot to reschedule", happeningID)
	}
	when := action.Args["when"]
	newStart, ok := parseWhen(e.now(), current.Start, when)
	if !ok {
		return nil, fmt.Errorf("pipeline: could not understand the new time %q", when)
	}
	duration := time.Hour
	if !current.Start.IsZero() && !current.End.IsZero() && current.End.After(current.Start) {
		duration = current.End.Sub(current.Start)
	}
	newEnd := newStart.Add(duration)

	req := dto4calendarius.HappeningSlotRequest{
		HappeningRequest: dto4calendarius.HappeningRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			HappeningID:  happeningID,
		},
		Slot: dto4calendarius.HappeningSlotWithID{
			ID: current.SlotID,
			HappeningSlot: dbo4calendarius.HappeningSlot{
				HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
					Timing:  dateTimeOf(newStart, newEnd),
					Repeats: dbo4calendarius.RepeatPeriodOnce,
				},
			},
		},
	}
	if err := e.Calendar.UpdateSlot(ctx, req); err != nil {
		return nil, err
	}
	undo := &session.Action{
		Kind:    action.Kind,
		Target:  action.Target,
		Args:    map[string]string{"when": current.Start.Format("2006-01-02 15:04")},
		Summary: fmt.Sprintf("Move %q back to %s?", action.Target.Title, current.Start.Format("Mon 15:04")),
	}
	return undo, nil
}

// parseWhen resolves a slot's free-text "when" ("Friday 16:00", "Friday",
// "4", "tomorrow 4pm") to an absolute time.Time, reusing sneat-ai-backend's
// temporal package (per the reuse-existing-sneat-code rule) rather than a
// bespoke parser: temporal.ParseText resolves the date component,
// temporal.NormalizeTime the time component. A "when" naming only a date
// keeps current's time-of-day; one naming only a time keeps current's date.
func parseWhen(now, current time.Time, text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	fields := strings.Fields(text)
	if len(fields) >= 2 {
		timePart, datePart := fields[len(fields)-1], strings.Join(fields[:len(fields)-1], " ")
		if tr, err := temporal.NormalizeTime(timePart); err == nil {
			if d, ok := temporal.ParseText(now, datePart); ok {
				return atTime(d, tr.Time, now.Location()), true
			}
		}
	}
	if d, ok := temporal.ParseText(now, text); ok {
		return atTime(d, current.Format("15:04"), now.Location()), true
	}
	if tr, err := temporal.NormalizeTime(text); err == nil {
		return atTime(current, tr.Time, now.Location()), true
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

func (e SneatExecutor) cancelHappening(ctx context.Context, action session.Action) (*session.Action, error) {
	if e.Calendar == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: cancel_happening requires Calendar API and a resolved target")
	}
	spaceID := action.Target.Keys["spaceID"]
	happeningID := action.Target.Keys["happeningID"]
	req := dto4calendarius.CancelHappeningRequest{
		HappeningRequest: dto4calendarius.HappeningRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			HappeningID:  happeningID,
		},
	}
	if err := e.Calendar.CancelHappening(ctx, req); err != nil {
		return nil, err
	}
	undo := &session.Action{Kind: calendarRevokeCancellationKind, Target: action.Target,
		Summary: fmt.Sprintf("Un-cancel %q?", action.Target.Title)}
	return undo, nil
}

func (e SneatExecutor) revokeCancellation(ctx context.Context, action session.Action) error {
	if e.Calendar == nil || action.Target == nil {
		return fmt.Errorf("pipeline: revoke_cancellation requires Calendar API and a resolved target")
	}
	spaceID := action.Target.Keys["spaceID"]
	happeningID := action.Target.Keys["happeningID"]
	return e.Calendar.RevokeHappeningCancellation(ctx, dto4calendarius.HappeningRequest{
		SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
		HappeningID:  happeningID,
	})
}

func (e SneatExecutor) setTodoDone(ctx context.Context, action session.Action, done bool) (*session.Action, error) {
	if e.Todo == nil || action.Target == nil {
		return nil, fmt.Errorf("pipeline: complete/reopen todo requires Todo API and a resolved target")
	}
	spaceID := action.Target.Keys["spaceID"]
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

func (e SneatExecutor) deleteTodo(ctx context.Context, action session.Action) error {
	if e.Todo == nil || action.Target == nil {
		return fmt.Errorf("pipeline: delete_todo requires Todo API and a resolved target")
	}
	spaceID := action.Target.Keys["spaceID"]
	list := action.Target.Keys["list"]
	itemID := action.Target.Keys["itemID"]
	return e.Todo.DeleteListItems(ctx, dto4listus.ListItemIDsRequest{
		ListRequest: dto4listus.ListRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			ListID:       dbo4listus.ListKey(listKeyFor(list)),
		},
		ItemIDs: []string{itemID},
	})
}

func (e SneatExecutor) addListItem(ctx context.Context, action session.Action, list string) (*session.Action, error) {
	if e.Todo == nil {
		return nil, fmt.Errorf("pipeline: add todo/to-buy requires Todo API")
	}
	spaceID := action.Args["spaceID"]
	title := action.Args["title"]
	if title == "" {
		return nil, fmt.Errorf("pipeline: add %s: a title is required", list)
	}
	req := dto4listus.CreateListItemsRequest{
		ListRequest: dto4listus.ListRequest{
			SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
			ListID:       dbo4listus.ListKey(listKeyFor(list)),
		},
		Items: []dto4listus.CreateListItemRequest{{ListItemBase: dbo4listus.ListItemBase{Title: title}}},
	}
	resp, err := e.Todo.CreateListItems(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.CreatedItems) == 0 {
		return nil, nil
	}
	created := resp.CreatedItems[0]
	target := session.EntityRef{Type: sneatdomain.EntityTodo, Title: title,
		Keys: map[string]string{"spaceID": spaceID, "list": list, "itemID": created.ID}}
	undo := &session.Action{Kind: sneatdomain.ModuleTodo + "." + sneatdomain.IntentDeleteTodo, Target: &target}
	return undo, nil
}
