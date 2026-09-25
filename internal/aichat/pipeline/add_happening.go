package pipeline

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/sneat-ai-backend/temporal"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// calendarDeleteHappeningKind is the undo action kind addHappening
// (sneatexecutor.go) stages -- not a taxonomy intent (never something a
// decision or the main LLM asks for directly), just the reverse of
// create_happening. Same shape as calendarRevokeCancellationKind/
// calendarCancelAdjustmentKind: an undo-only executor case,
// deliberately excluded from SupportedActionKinds (kinds.go) so it is
// never offered to the model.
const calendarDeleteHappeningKind = sneatdomain.ModuleCalendar + ".delete_happening"

// defaultEventDurationMinutes is calendar.add_happening's fallback duration
// when no "end" or "duration" slot is given (founder ask,
// add-event-prompt.md: "default duration 1h if no end").
const defaultEventDurationMinutes = 60

// addHappeningSlots is calendar.add_happening's Args, resolved ONCE against
// the session clock/TZ when the action is staged for confirmation
// (Pipeline.stageAddHappening) -- founder ask: a relative date word
// ("tomorrow", "Friday") is resolved deterministically in code, never
// trusted from the model's own arithmetic; an absolute date the model DID
// supply is validated the same way, not trusted blindly either. The
// confirmation preview (summary) and the eventual create_happening request
// (via toArgs, then SneatExecutor.addHappening's buildHappeningBrief) both
// read these same resolved values, so what the user confirms and what gets
// created can never differ.
type addHappeningSlots struct {
	Title     string
	Recurring bool
	Date      time.Time                     // one-off only
	Weekdays  []dbo4calendarius.WeekdayCode // recurring only, calendar order
	StartTime string                        // "HH:MM"
	EndTime   string                        // "HH:MM"
	TZ        string                        // IANA zone name, may be ""
}

// weekdayNames renders a WeekdayCode for the confirmation preview.
var weekdayNames = map[dbo4calendarius.WeekdayCode]string{
	dbo4calendarius.Monday2:    "Monday",
	dbo4calendarius.Tuesday2:   "Tuesday",
	dbo4calendarius.Wednesday2: "Wednesday",
	dbo4calendarius.Thursday2:  "Thursday",
	dbo4calendarius.Friday2:    "Friday",
	dbo4calendarius.Saturday2:  "Saturday",
	dbo4calendarius.Sunday2:    "Sunday",
}

// weekdayCodeOrder fixes parseWeekdayList's output to calendar order
// (mo..su) regardless of the order the model listed them in, so "Thursdays
// and Mondays" and "Mondays and Thursdays" produce identical confirmation
// text and an identical request.
var weekdayCodeOrder = []dbo4calendarius.WeekdayCode{
	dbo4calendarius.Monday2, dbo4calendarius.Tuesday2, dbo4calendarius.Wednesday2,
	dbo4calendarius.Thursday2, dbo4calendarius.Friday2, dbo4calendarius.Saturday2, dbo4calendarius.Sunday2,
}

// weekdayCodeNames maps a lowercase 2-letter code to its WeekdayCode -- the
// slot contract's closed vocabulary (kinds.go's instruction text: "mo, tu,
// we, th, fr, sa, su").
var weekdayCodeNames = map[string]dbo4calendarius.WeekdayCode{
	"mo": dbo4calendarius.Monday2, "tu": dbo4calendarius.Tuesday2, "we": dbo4calendarius.Wednesday2,
	"th": dbo4calendarius.Thursday2, "fr": dbo4calendarius.Friday2, "sa": dbo4calendarius.Saturday2,
	"su": dbo4calendarius.Sunday2,
}

// parseWeekdayList parses a comma-separated weekday-code list ("mo,we",
// "tu, th") into calendar order, deduplicated. Returns a plain, user-showable
// error naming the unrecognised token for anything else.
func parseWeekdayList(s string) ([]dbo4calendarius.WeekdayCode, error) {
	seen := map[dbo4calendarius.WeekdayCode]bool{}
	for _, p := range strings.Split(s, ",") {
		code := strings.ToLower(strings.TrimSpace(p))
		wd, ok := weekdayCodeNames[code]
		if !ok {
			return nil, fmt.Errorf("didn't understand the weekday %q -- use mo, tu, we, th, fr, sa, or su", p)
		}
		seen[wd] = true
	}
	var out []dbo4calendarius.WeekdayCode
	for _, wd := range weekdayCodeOrder {
		if seen[wd] {
			out = append(out, wd)
		}
	}
	return out, nil
}

// weekdayListLabel renders codes (already in calendar order) for the
// confirmation preview, e.g. "Tuesday, Thursday".
func weekdayListLabel(codes []dbo4calendarius.WeekdayCode) string {
	names := make([]string, 0, len(codes))
	for _, c := range codes {
		names = append(names, weekdayNames[c])
	}
	return strings.Join(names, ", ")
}

// minutesOfDay parses an "HH:MM" clock time (already normalized by
// temporal.NormalizeTime) into minutes since midnight.
func minutesOfDay(hhmm string) (int, bool) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// resolveAddHappeningDate resolves the "date" slot for a one-off event: an
// absolute YYYY-MM-DD (parsed, not trusted blindly) or, failing that, a
// relative phrase ("tomorrow", "Friday", "next Friday") resolved
// deterministically via sneat-ai-backend's temporal package -- the SAME
// package reschedule's parseWhen (resolver.go) reuses, per the
// reuse-existing-sneat-code rule -- rather than trusting the model's own
// date arithmetic (founder ask, add-event-prompt.md).
//
// startMin (the event's OWN already-resolved start time, minutes since
// midnight) anchors two rules temporal.ParseText's own weekday resolution
// (NextWeekday, "today included") does not implement on its own, and that a
// bare temporal.ParseText call got wrong for creating a NEW event
// (coordinator review, PR #58): a reschedule's "when" has an existing
// occurrence to anchor against, but add_happening has none, so "Friday
// 10:30" asked on a Friday afternoon must not silently resolve to a start
// time already in the past.
//
//  1. "next <weekday>" NEVER resolves to today, even when asked before that
//     weekday's start time would pass: "next Friday" said on Friday morning
//     means the COMING Friday (a week out), the common reading of "next" --
//     not "next" meaning nothing because delta-0 already satisfies "the next
//     Friday on/after today" (temporal.NextWeekday's own semantics, meant for
//     an unqualified bare weekday, not one explicitly marked "next").
//  2. A BARE weekday ("Friday", "this Friday" -- "this" carries no special
//     meaning here beyond temporal.ParseText's own stripping) that names
//     TODAY, whose start time has ALREADY PASSED in the session clock, rolls
//     to the same weekday next week instead of creating an event in the
//     past nobody asked to log.
//
// The literal word "today" is a DELIBERATE exception to rule 2: it always
// stays today even when startMin has already passed -- an explicit "today"
// is read as "log this for today" (past events are allowed elsewhere in
// this resolver too, e.g. an explicit YYYY-MM-DD in the past), not as "the
// next day that happens to be named today".
func resolveAddHappeningDate(now time.Time, dateRaw string, startMin int) (time.Time, bool) {
	if d, err := time.ParseInLocation("2006-01-02", dateRaw, now.Location()); err == nil {
		return d, true
	}
	lower := strings.ToLower(strings.TrimSpace(dateRaw))
	if lower == "today" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), true
	}
	hasNext := strings.Contains(lower, "next")
	// Strip the same "next "/"this " prefixes temporal.ParseText itself
	// strips internally, so ParseWeekday sees a bare weekday name either way
	// -- this is our OWN weekday resolution (not a call into ParseText),
	// because only here can rules 1/2 above be applied before returning.
	stripped := strings.TrimSpace(strings.NewReplacer("next ", "", "this ", "").Replace(lower))
	if code, ok := temporal.ParseWeekday(stripped); ok {
		d, _ := temporal.NextWeekday(now, code)
		if sameCalendarDay(d, now) {
			nowMin := now.Hour()*60 + now.Minute()
			if hasNext || startMin <= nowMin {
				d = d.AddDate(0, 0, 7)
			}
		}
		return d, true
	}
	if d, ok := temporal.ParseText(now, dateRaw); ok {
		return d, true
	}
	return time.Time{}, false
}

// resolveAddHappeningSlots parses and validates calendar.add_happening's raw
// Args (title/date/weekdays/start/end/duration/tz -- kinds.go's slot
// contract) into addHappeningSlots. now/sessionTZ anchor a relative date
// word -- see the type's own doc comment. Every returned error is plain,
// user-showable text (never a raw internal error), since
// Pipeline.stageAddHappening shows it to the chat user as-is.
func resolveAddHappeningSlots(now time.Time, sessionTZ string, args map[string]string) (addHappeningSlots, error) {
	title := strings.TrimSpace(args["title"])
	if title == "" {
		return addHappeningSlots{}, fmt.Errorf("what should I title the event?")
	}

	dateRaw := strings.TrimSpace(args["date"])
	weekdaysRaw := strings.TrimSpace(args["weekdays"])
	switch {
	case dateRaw != "" && weekdaysRaw != "":
		return addHappeningSlots{}, fmt.Errorf("tell me either a date or which weekdays %q repeats on, not both", title)
	case dateRaw == "" && weekdaysRaw == "":
		return addHappeningSlots{}, fmt.Errorf("what date (or which weekdays) is %q on?", title)
	}

	out := addHappeningSlots{Title: title, TZ: strings.TrimSpace(args["tz"])}
	if out.TZ == "" {
		out.TZ = sessionTZ
	}

	// The start time is resolved BEFORE the one-off date, not after: a bare
	// weekday that names TODAY needs to compare against the event's OWN
	// start time to decide whether that occurrence has already passed (see
	// resolveAddHappeningDate's doc comment, rule 2) -- recurring events
	// need no such comparison (a weekly slot has no single "today").
	startRaw := strings.TrimSpace(args["start"])
	if startRaw == "" {
		return addHappeningSlots{}, fmt.Errorf("what time does %q start?", title)
	}
	startNorm, err := temporal.NormalizeTime(startRaw)
	if err != nil {
		return addHappeningSlots{}, fmt.Errorf("didn't understand the start time %q", startRaw)
	}
	// minutesOfDay's own "not HH:MM" branch is intentionally not re-checked
	// here (see its doc comment): temporal.NormalizeTime's Time field is
	// ALWAYS "%02d:%02d" (hour 0-23, minute already range-checked), so
	// minutesOfDay can never actually fail on its output -- a genuine parse
	// failure here would mean the temporal package itself changed its output
	// format, not a user-input problem this function should try to explain.
	startMin, _ := minutesOfDay(startNorm.Time)
	out.StartTime = startNorm.Time

	if weekdaysRaw != "" {
		codes, err := parseWeekdayList(weekdaysRaw)
		if err != nil {
			return addHappeningSlots{}, err
		}
		out.Recurring = true
		out.Weekdays = codes
	} else {
		d, ok := resolveAddHappeningDate(now, dateRaw, startMin)
		if !ok {
			return addHappeningSlots{}, fmt.Errorf("didn't understand the date %q", dateRaw)
		}
		out.Date = d
	}

	endMin := startMin + defaultEventDurationMinutes
	switch {
	case strings.TrimSpace(args["end"]) != "":
		endRaw := strings.TrimSpace(args["end"])
		endNorm, err := temporal.NormalizeTime(endRaw)
		if err != nil {
			return addHappeningSlots{}, fmt.Errorf("didn't understand the end time %q", endRaw)
		}
		// See the identical note on the start-time branch above: minutesOfDay
		// cannot fail on temporal.NormalizeTime's own output.
		m, _ := minutesOfDay(endNorm.Time)
		endMin = m
	case strings.TrimSpace(args["duration"]) != "":
		durRaw := strings.TrimSpace(args["duration"])
		n, err := strconv.Atoi(durRaw)
		if err != nil || n <= 0 {
			return addHappeningSlots{}, fmt.Errorf("didn't understand the duration %q", durRaw)
		}
		endMin = startMin + n
	}
	// BLOCKER-shaped ruling, same spirit as sneatexecutor.go's
	// crossDayMoveRefusal: a cross-midnight event (explicit end before/equal
	// to start, or a duration/default that pushes past 24:00) is refused
	// outright with a clear message rather than silently wrapping or
	// truncating, per the founder brief's explicit test requirement.
	if endMin <= startMin || endMin > 24*60 {
		return addHappeningSlots{}, fmt.Errorf("%q can't end before or when it starts, and crossing midnight isn't supported yet", title)
	}
	out.EndTime = fmt.Sprintf("%02d:%02d", endMin/60, endMin%60)
	return out, nil
}

// capitalizeFirst upper-cases s's first rune, leaving the rest unchanged --
// used ONLY to turn a Go-idiomatic lowercase error string into user-facing
// chat text (stageAddHappening), never applied to an error before it is
// wrapped/logged internally.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// toArgs reconstructs the canonical, already-resolved Args a Pending
// calendar.add_happening action carries -- spaceID plus every field
// SneatExecutor.addHappening's buildHappeningBrief needs, with no free text
// left for Execute to re-interpret (resolution happens ONCE, in
// resolveAddHappeningSlots, not again at confirm time).
func (s addHappeningSlots) toArgs(spaceID string) map[string]string {
	args := map[string]string{
		"spaceID": spaceID,
		"title":   s.Title,
		"start":   s.StartTime,
		"end":     s.EndTime,
	}
	if s.TZ != "" {
		args["tz"] = s.TZ
	}
	if s.Recurring {
		codes := make([]string, len(s.Weekdays))
		for i, c := range s.Weekdays {
			codes[i] = string(c)
		}
		args["weekdays"] = strings.Join(codes, ",")
	} else {
		args["date"] = s.Date.Format("2006-01-02")
	}
	return args
}

// summary builds the "yes/no" confirmation preview (founder ask: "a preview
// (title, date or weekly days, start-end)"). The one-off date is rendered
// WITH its weekday name and year ("Fri 2 Oct 2026", not just "Oct 2") --
// coordinator review (PR #58): resolveAddHappeningDate's weekday rules
// (today vs. next week, "next" handling) are subtle enough that a
// mis-resolution must be visually obvious in the confirmation, before "yes"
// commits to it, not discoverable only after the happening is created.
func (s addHappeningSlots) summary() string {
	if s.Recurring {
		return fmt.Sprintf("Add %q every %s, %s-%s?", s.Title, weekdayListLabel(s.Weekdays), s.StartTime, s.EndTime)
	}
	return fmt.Sprintf("Add %q on %s, %s-%s?", s.Title, s.Date.Format("Mon 2 Jan 2006"), s.StartTime, s.EndTime)
}

// stageAddHappening resolves and validates calendar.add_happening's slots
// (once, deterministically -- resolveAddHappeningSlots) and stages a Pending
// confirmation, the same "yes/no" mechanism resolveAndAct uses for the
// destructive calendar kinds (reschedule/cancel) -- founder ask: "creating
// an event is shown as a proposal ... and executes only on 'yes'". Unlike
// resolveAndAct's kinds, add_happening never resolves a reference (it always
// creates a new happening, like add_todo), so it is staged directly here
// rather than through resolveAndAct's resolve-then-confirm path. A
// validation failure answers with the specific reason as plain Output text
// (err == nil) and stages nothing, the same contract HandleAction's
// unsupported-kind branch already promises the caller.
func (p Pipeline) stageAddHappening(rawSlots map[string]string, st *session.State, spaceID string) (Output, error) {
	args := spaceScopedArgs(rawSlots, spaceID)
	resolved, err := resolveAddHappeningSlots(p.now(), p.TZ, args)
	if err != nil {
		// resolveAddHappeningSlots' messages are lowercase Go-error-style
		// strings (errcheck/staticcheck ST1005) but are shown to the chat
		// user verbatim otherwise, so capitalize the first letter here, at
		// the one place they cross from internal error to user-facing text.
		return Output{Text: capitalizeFirst(err.Error())}, nil
	}
	action := session.Action{
		Kind:    sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening,
		Args:    resolved.toArgs(spaceID),
		Summary: resolved.summary(),
	}
	st.Pending = &action
	return Output{Text: action.Summary + " (yes/no)"}, nil
}

// buildHappeningBrief builds calendarius's create_happening payload from an
// ALREADY-RESOLVED add_happening Args map (addHappeningSlots.toArgs) --
// SneatExecutor.addHappening calls this at confirm time; it never re-parses
// free text, since resolveAddHappeningSlots already did that once,
// deterministically, when the action was staged. Kind is left "" (event kind
// unset -- a plain happening, not calendarius's separate eventus-style
// HappeningKindEvent shape), matching every other happening this MVP slice
// creates/reads.
func buildHappeningBrief(args map[string]string) (*dbo4calendarius.HappeningBrief, error) {
	title := args["title"]
	if title == "" {
		return nil, fmt.Errorf("pipeline: add_happening requires a title")
	}
	start, end := args["start"], args["end"]
	if start == "" || end == "" {
		return nil, fmt.Errorf("pipeline: add_happening requires start and end times")
	}
	slot := dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing: dbo4calendarius.Timing{
				Start:    dbo4calendarius.DateTime{Time: start},
				End:      dbo4calendarius.DateTime{Time: end},
				TimeZone: args["tz"],
			},
		},
	}
	happeningType := dbo4calendarius.HappeningTypeSingle
	if weekdaysRaw := args["weekdays"]; weekdaysRaw != "" {
		codes, err := parseWeekdayList(weekdaysRaw)
		if err != nil {
			return nil, err
		}
		slot.Repeats = dbo4calendarius.RepeatPeriodWeekly
		slot.Weekdays = codes
		happeningType = dbo4calendarius.HappeningTypeRecurring
	} else {
		date := args["date"]
		if date == "" {
			return nil, fmt.Errorf("pipeline: add_happening requires a date or weekdays")
		}
		slot.Start.Date = date
		slot.Repeats = dbo4calendarius.RepeatPeriodOnce
	}
	return &dbo4calendarius.HappeningBrief{
		HappeningBase: dbo4calendarius.HappeningBase{
			Type:   happeningType,
			Status: dbo4calendarius.HappeningStatusActive,
			Title:  title,
			Slots:  map[string]*dbo4calendarius.HappeningSlot{"s1": &slot},
		},
	}, nil
}
