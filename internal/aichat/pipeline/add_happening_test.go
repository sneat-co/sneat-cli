package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/calendarius/backend/dto4calendarius"
	"github.com/sneat-co/sneat-core-modules/spaceus/dto4spaceus"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// addHappeningNow is a fixed Friday, matching pipeline_test.go's fixedNow
// (2026-09-25), used directly (not via Pipeline) by the resolver unit tests
// below.
func addHappeningNow() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }

// TestResolveAddHappeningSlots covers the founder brief's explicit test
// matrix: one-off, recurring multi-weekday, missing end -> default 1h,
// invalid time, past date allowed, cross-midnight end rejected.
func TestResolveAddHappeningSlots(t *testing.T) {
	now := addHappeningNow()
	cases := []struct {
		name    string
		args    map[string]string
		wantErr string // substring, "" means no error
		check   func(t *testing.T, s addHappeningSlots)
	}{
		{
			name: "one-off absolute date",
			args: map[string]string{"title": "Dentist", "date": "2026-09-26", "start": "10:30"},
			check: func(t *testing.T, s addHappeningSlots) {
				if s.Recurring {
					t.Fatal("want one-off, got recurring")
				}
				if got := s.Date.Format("2006-01-02"); got != "2026-09-26" {
					t.Fatalf("date = %s, want 2026-09-26", got)
				}
				if s.StartTime != "10:30" {
					t.Fatalf("start = %s, want 10:30", s.StartTime)
				}
				// missing end -> default 1h duration (founder ask).
				if s.EndTime != "11:30" {
					t.Fatalf("end = %s, want default 11:30 (start+1h)", s.EndTime)
				}
			},
		},
		{
			name: "one-off relative date (tomorrow)",
			args: map[string]string{"title": "Lunch with Bob", "date": "tomorrow", "start": "13:00", "end": "14:00"},
			check: func(t *testing.T, s addHappeningSlots) {
				if got := s.Date.Format("2006-01-02"); got != "2026-09-26" {
					t.Fatalf("date = %s, want 2026-09-26 (tomorrow, resolved deterministically)", got)
				}
				if s.StartTime != "13:00" || s.EndTime != "14:00" {
					t.Fatalf("start/end = %s/%s, want 13:00/14:00", s.StartTime, s.EndTime)
				}
			},
		},
		{
			name: "recurring multi-weekday",
			// "7am" (explicit meridiem), not bare "07:00" -- temporal.NormalizeTime
			// reads an unqualified hour 1-7 as PM (its own documented
			// "at 6"-means-18:00 heuristic), matching the founder's own
			// example wording ("Yoga Tuesdays and Thursdays 7am").
			args: map[string]string{"title": "Yoga", "weekdays": "th,tu", "start": "7am"},
			check: func(t *testing.T, s addHappeningSlots) {
				if !s.Recurring {
					t.Fatal("want recurring")
				}
				// Calendar order regardless of input order ("th,tu" -> tu,th).
				if len(s.Weekdays) != 2 || s.Weekdays[0] != dbo4calendarius.Tuesday2 || s.Weekdays[1] != dbo4calendarius.Thursday2 {
					t.Fatalf("weekdays = %v, want [tu th] in calendar order", s.Weekdays)
				}
				if s.StartTime != "07:00" {
					t.Fatalf("start = %s, want 07:00", s.StartTime)
				}
				if s.EndTime != "08:00" {
					t.Fatalf("end = %s, want default 08:00", s.EndTime)
				}
			},
		},
		{
			name: "duration slot instead of end",
			args: map[string]string{"title": "Standup", "date": "2026-09-26", "start": "09:00", "duration": "15"},
			check: func(t *testing.T, s addHappeningSlots) {
				if s.EndTime != "09:15" {
					t.Fatalf("end = %s, want 09:15 (start+15min duration)", s.EndTime)
				}
			},
		},
		{
			name:    "invalid start time",
			args:    map[string]string{"title": "X", "date": "2026-09-26", "start": "25:99"},
			wantErr: "start time",
		},
		{
			name:    "invalid end time",
			args:    map[string]string{"title": "X", "date": "2026-09-26", "start": "10:00", "end": "not-a-time"},
			wantErr: "end time",
		},
		{
			name:    "cross-midnight explicit end rejected",
			args:    map[string]string{"title": "Party", "date": "2026-09-26", "start": "23:00", "end": "01:00"},
			wantErr: "crossing midnight",
		},
		{
			name:    "cross-midnight default duration rejected",
			args:    map[string]string{"title": "Late thing", "date": "2026-09-26", "start": "23:45"},
			wantErr: "crossing midnight",
		},
		{
			name: "past date allowed (logging a past event)",
			args: map[string]string{"title": "Old meeting", "date": "2020-01-01", "start": "10:00"},
			check: func(t *testing.T, s addHappeningSlots) {
				if got := s.Date.Format("2006-01-02"); got != "2020-01-01" {
					t.Fatalf("date = %s, want 2020-01-01 (past dates are allowed)", got)
				}
			},
		},
		{
			name:    "missing title",
			args:    map[string]string{"date": "2026-09-26", "start": "10:00"},
			wantErr: "title",
		},
		{
			name:    "missing date and weekdays",
			args:    map[string]string{"title": "X", "start": "10:00"},
			wantErr: "date",
		},
		{
			name:    "both date and weekdays given",
			args:    map[string]string{"title": "X", "date": "2026-09-26", "weekdays": "mo", "start": "10:00"},
			wantErr: "not both",
		},
		{
			name:    "missing start",
			args:    map[string]string{"title": "X", "date": "2026-09-26"},
			wantErr: "start",
		},
		{
			name:    "invalid weekday code",
			args:    map[string]string{"title": "X", "weekdays": "xx", "start": "10:00"},
			wantErr: "weekday",
		},
		{
			name:    "invalid date",
			args:    map[string]string{"title": "X", "date": "not a date", "start": "10:00"},
			wantErr: "date",
		},
		{
			name:    "invalid duration",
			args:    map[string]string{"title": "X", "date": "2026-09-26", "start": "10:00", "duration": "not-a-number"},
			wantErr: "duration",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveAddHappeningSlots(now, "", c.args)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.check != nil {
				c.check(t, got)
			}
		})
	}
}

// TestMinutesOfDay covers minutesOfDay directly, including its defensive
// "not HH:MM" false branch -- unreachable from resolveAddHappeningSlots'
// call sites in production (temporal.NormalizeTime's own output is always
// well-formed, see the doc comments there), but the function itself keeps
// the check rather than assume its argument, so a direct test exercises it.
func TestMinutesOfDay(t *testing.T) {
	if got, ok := minutesOfDay("07:30"); !ok || got != 450 {
		t.Fatalf("minutesOfDay(07:30) = %d,%v, want 450,true", got, ok)
	}
	if _, ok := minutesOfDay("not-a-time"); ok {
		t.Fatal("minutesOfDay(not-a-time) = true, want false")
	}
}

// TestResolveAddHappeningSlots_TZFallback covers the session-clock TZ
// fallback: an explicit "tz" slot wins, an absent one falls back to the
// session's own TZ.
func TestResolveAddHappeningSlots_TZFallback(t *testing.T) {
	now := addHappeningNow()
	got, err := resolveAddHappeningSlots(now, "America/New_York", map[string]string{
		"title": "X", "date": "2026-09-26", "start": "10:00",
	})
	if err != nil {
		t.Fatalf("resolveAddHappeningSlots: %v", err)
	}
	if got.TZ != "America/New_York" {
		t.Fatalf("TZ = %q, want session TZ fallback", got.TZ)
	}
	got2, err := resolveAddHappeningSlots(now, "America/New_York", map[string]string{
		"title": "X", "date": "2026-09-26", "start": "10:00", "tz": "Europe/London",
	})
	if err != nil {
		t.Fatalf("resolveAddHappeningSlots: %v", err)
	}
	if got2.TZ != "Europe/London" {
		t.Fatalf("TZ = %q, want explicit slot to win", got2.TZ)
	}
}

// TestAddHappeningSlots_SummaryAndToArgs covers the confirmation preview
// text and the canonical Args round-trip for both shapes.
func TestAddHappeningSlots_SummaryAndToArgs(t *testing.T) {
	oneOff := addHappeningSlots{Title: "Dentist", Date: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), StartTime: "10:30", EndTime: "11:30"}
	if got := oneOff.summary(); !strings.Contains(got, "Dentist") || !strings.Contains(got, "10:30-11:30") {
		t.Fatalf("summary = %q", got)
	}
	args := oneOff.toArgs("sp1")
	if args["spaceID"] != "sp1" || args["date"] != "2026-09-26" || args["weekdays"] != "" {
		t.Fatalf("toArgs = %v", args)
	}

	recurring := addHappeningSlots{Title: "Yoga", Recurring: true, Weekdays: []dbo4calendarius.WeekdayCode{dbo4calendarius.Tuesday2, dbo4calendarius.Thursday2}, StartTime: "07:00", EndTime: "08:00", TZ: "UTC"}
	if got := recurring.summary(); !strings.Contains(got, "Tuesday, Thursday") {
		t.Fatalf("summary = %q, want weekday list", got)
	}
	rargs := recurring.toArgs("sp1")
	if rargs["weekdays"] != "tu,th" || rargs["date"] != "" || rargs["tz"] != "UTC" {
		t.Fatalf("toArgs = %v", rargs)
	}
}

// TestBuildHappeningBrief_MissingFields covers buildHappeningBrief's own
// defensive checks (reached only if Execute is ever called with malformed
// Args, since stageAddHappening's resolver already validates everything
// upstream in the normal path).
func TestBuildHappeningBrief_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		args map[string]string
	}{
		{"no title", map[string]string{"start": "10:00", "end": "11:00", "date": "2026-09-26"}},
		{"no start/end", map[string]string{"title": "X", "date": "2026-09-26"}},
		{"no date or weekdays", map[string]string{"title": "X", "start": "10:00", "end": "11:00"}},
		{"bad weekday", map[string]string{"title": "X", "start": "10:00", "end": "11:00", "weekdays": "zz"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := buildHappeningBrief(c.args); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

// TestBuildHappeningBrief_PayloadValidates decodes the REAL request shape
// SneatExecutor.addHappening builds through buildHappeningBrief and runs it
// through the real dto4calendarius DTO's own NormalizeTags+Validate --
// exactly the founder's "payload correctness" testing requirement -- for
// both the one-off and recurring shapes.
func TestBuildHappeningBrief_PayloadValidates(t *testing.T) {
	cases := []struct {
		name string
		args map[string]string
	}{
		{"one-off", map[string]string{"title": "Dentist", "date": "2026-09-26", "start": "10:30", "end": "11:30"}},
		{"recurring", map[string]string{"title": "Yoga", "weekdays": "tu,th", "start": "07:00", "end": "08:00"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			brief, err := buildHappeningBrief(c.args)
			if err != nil {
				t.Fatalf("buildHappeningBrief: %v", err)
			}
			req := dto4calendarius.CreateHappeningRequest{
				SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID("sp1")},
				Happening:    brief,
			}
			// Round-trip through JSON exactly like the real HTTP call does,
			// decoding into the REAL request type, then call its own
			// NormalizeTags+Validate -- a payload the backend would 400
			// must fail here too.
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded dto4calendarius.CreateHappeningRequest
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			decoded.NormalizeTags()
			if err := decoded.Validate(); err != nil {
				t.Fatalf("Validate: %v (payload: %s)", err, raw)
			}
		})
	}
}

// TestPipeline_StageAddHappening_OneOff covers the confirm-then-execute
// happy path end-to-end through HandleAction: a one-off add_happening action
// is staged as Pending (never executed immediately), "yes" confirms it, and
// "undo" reverses it via the delete-happening undo kind.
func TestPipeline_StageAddHappening_OneOff(t *testing.T) {
	exec := &FakeExecutor{Undo: map[string]*session.Action{
		sneatdomain.ModuleCalendar + "." + sneatdomain.IntentAddHappening: {
			Kind: calendarDeleteHappeningKind,
			Target: &session.EntityRef{Type: sneatdomain.EntityHappening, Title: "Dentist",
				Keys: map[string]string{"spaceID": "sp1", "happeningID": "h9"}},
		},
	}}
	p, st := newTestPipeline(exec)
	out, err := p.HandleAction(context.Background(), Action{
		Kind:  "calendar.add_happening",
		Slots: map[string]string{"title": "Dentist", "date": "2026-09-26", "start": "10:30"},
	}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if st.Pending == nil {
		t.Fatal("want a Pending confirmation staged, got none")
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("must not execute before confirmation: %+v", exec.Executed)
	}
	if !strings.Contains(out.Text, "Dentist") || !strings.Contains(out.Text, "yes/no") {
		t.Fatalf("confirmation text = %q", out.Text)
	}

	out, err = p.Turn(context.Background(), "yes", st, "sp1")
	if err != nil {
		t.Fatalf("Turn(yes): %v", err)
	}
	if len(exec.Executed) != 1 || exec.Executed[0].Kind != "calendar.add_happening" {
		t.Fatalf("executed = %+v, want exactly one calendar.add_happening", exec.Executed)
	}
	if exec.Executed[0].Args["date"] != "2026-09-26" || exec.Executed[0].Args["start"] != "10:30" {
		t.Fatalf("executed args = %v", exec.Executed[0].Args)
	}
	if out.Text == "" {
		t.Fatal("want a non-empty confirmation-of-success text")
	}
	if st.Previous == nil || st.Previous.Undo == nil {
		t.Fatal("want an undo staged for the created happening")
	}

	if _, err := p.Turn(context.Background(), "undo", st, "sp1"); err != nil {
		t.Fatalf("Turn(undo): %v", err)
	}
	if len(exec.Executed) != 2 || exec.Executed[1].Kind != calendarDeleteHappeningKind {
		t.Fatalf("executed = %+v, want undo to delete the happening", exec.Executed)
	}
}

// TestPipeline_StageAddHappening_Recurring covers the weekly-recurring shape
// end-to-end and asserts the resolved weekdays reach the staged action.
func TestPipeline_StageAddHappening_Recurring(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	out, err := p.HandleAction(context.Background(), Action{
		Kind:  "calendar.add_happening",
		Slots: map[string]string{"title": "Team sync", "weekdays": "mo", "start": "09:00"},
	}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if st.Pending == nil || st.Pending.Args["weekdays"] != "mo" {
		t.Fatalf("Pending = %+v", st.Pending)
	}
	if !strings.Contains(out.Text, "Monday") {
		t.Fatalf("confirmation text = %q, want it to name Monday", out.Text)
	}
}

// TestPipeline_StageAddHappening_ValidationFailure covers a slot the
// resolver refuses: plain text, no Pending staged, no error returned (the
// same "never a pipeline error" contract HandleAction's unsupported-kind
// branch uses).
func TestPipeline_StageAddHappening_ValidationFailure(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	out, err := p.HandleAction(context.Background(), Action{
		Kind:  "calendar.add_happening",
		Slots: map[string]string{"title": "Dentist", "start": "10:30"}, // no date/weekdays
	}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if st.Pending != nil {
		t.Fatal("a validation failure must not stage a Pending confirmation")
	}
	if len(exec.Executed) != 0 {
		t.Fatalf("must not execute: %+v", exec.Executed)
	}
	if out.Text == "" {
		t.Fatal("want an explanatory message")
	}
}

// TestPipeline_UpdateHappening_RenameRunsImmediately covers B: a rename is
// resolved by reference and runs immediately, no confirmation.
func TestPipeline_UpdateHappening_RenameRunsImmediately(t *testing.T) {
	exec := &FakeExecutor{}
	p, st := newTestPipeline(exec)
	out, err := p.HandleAction(context.Background(), Action{
		Kind:      "calendar.update_happening",
		Reference: "standup",
		Slots:     map[string]string{"title": "Weekly planning"},
	}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if st.Pending != nil {
		t.Fatal("rename is non-destructive -- must not stage a Pending confirmation")
	}
	if len(exec.Executed) != 1 || exec.Executed[0].Kind != "calendar.update_happening" {
		t.Fatalf("executed = %+v, want exactly one calendar.update_happening", exec.Executed)
	}
	if exec.Executed[0].Args["title"] != "Weekly planning" {
		t.Fatalf("args = %v", exec.Executed[0].Args)
	}
	if out.Text == "" {
		t.Fatal("want a non-empty confirmation-of-success text")
	}
}

// TestSummaryFor_UpdateHappening covers both the named-title and the
// empty-title-fallback branches for calendar.update_happening's confirmation
// preview text.
func TestSummaryFor_UpdateHappening(t *testing.T) {
	target := session.EntityRef{Title: "Team sync"}
	kind := sneatdomain.ModuleCalendar + "." + sneatdomain.IntentUpdateHappening
	if got := summaryFor(kind, target, map[string]string{"title": "Weekly planning"}); got != `Rename "Team sync" to Weekly planning?` {
		t.Fatalf("summaryFor = %q", got)
	}
	if got := summaryFor(kind, target, map[string]string{}); got != `Rename "Team sync" to a new title?` {
		t.Fatalf("summaryFor (no title slot) = %q", got)
	}
}

// TestCapitalizeFirst covers both branches: the normal uppercase-first-rune
// case and the empty-string no-op.
func TestCapitalizeFirst(t *testing.T) {
	if got := capitalizeFirst("didn't understand"); got != "Didn't understand" {
		t.Fatalf("capitalizeFirst = %q", got)
	}
	if got := capitalizeFirst(""); got != "" {
		t.Fatalf("capitalizeFirst(\"\") = %q, want \"\"", got)
	}
}
