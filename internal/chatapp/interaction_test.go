// Copyright 2026 Sneat.app

package chatapp

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	"github.com/strongo/aichat/ai/cloudproto"
	"github.com/strongo/aichat/ai/decision"
)

type fakeInteractionReporter struct {
	reports []cloudproto.InteractionReport
	err     error
}

func (f *fakeInteractionReporter) ReportInteraction(_ context.Context, report cloudproto.InteractionReport) error {
	f.reports = append(f.reports, report)
	return f.err
}

func TestReportCommandCapturesDetectionAndExecutedAction(t *testing.T) {
	reporter := &fakeInteractionReporter{}
	h := &handler{ctx: context.Background(), reporter: reporter}
	turn := &turnReport{id: "test-id", text: "Show this week's calendar", output: pipeline.Output{
		Trace: decision.Trace{Attempts: []decision.Attempt{
			{Provider: "sneat-rules", Outcome: "abstained"},
			{Provider: "cloud-decision", Outcome: "decided"},
		}},
	}}
	cmd := h.reportCommand(turn, "completed", "action_succeeded", "calendar.show", "succeeded")
	if cmd == nil {
		t.Fatal("expected report command")
	}
	_ = cmd()
	if len(reporter.reports) != 1 {
		t.Fatalf("reports=%d", len(reporter.reports))
	}
	r := reporter.reports[0]
	if r.Product != Product || r.UserMessageWords != 4 || r.UserMessageChars != len([]rune(turn.text)) || len(r.DetectionSteps) != 2 ||
		r.DetectionSteps[0].Method != "deterministic" || r.DetectionSteps[1].Method != "jev" ||
		len(r.ActionExecutions) != 1 || r.ActionExecutions[0].Status != "succeeded" {
		t.Fatalf("report=%+v", r)
	}
}

func TestReportFailureNeverChangesChatResult(t *testing.T) {
	reporter := &fakeInteractionReporter{err: errors.New("telemetry offline")}
	h := &handler{ctx: context.Background(), reporter: reporter}
	cmd := h.reportCommand(&turnReport{id: "test-id", text: "Hi"}, "completed", "answer_presented", "", "")
	if got := cmd(); got != nil {
		t.Fatalf("report command returned user-visible message %T", got)
	}
	if len(reporter.reports) != 1 {
		t.Fatal("report was not attempted")
	}
}

func TestSneatClientContextUsesActualVersionAndInstallation(t *testing.T) {
	a := newClientContext("v1.2.3", "550e8400-e29b-41d4-a716-446655440000")
	b := newClientContext("v1.2.3", a.InstallationID)
	if a.Client.Type != "cli" || a.Client.Name != "sneat" || a.Client.Version != "v1.2.3" ||
		a.InstallationID != b.InstallationID || a.SessionID == "" || a.SessionID == b.SessionID ||
		a.Platform.OS != runtime.GOOS || a.Platform.Arch != runtime.GOARCH || a.Feature != "chat" {
		t.Fatalf("context=%+v subsequent=%+v", a, b)
	}
}

func TestReportPreservesDetectedIntentWithoutExecutionAndLLMFallback(t *testing.T) {
	reporter := &fakeInteractionReporter{}
	h := &handler{ctx: context.Background(), reporter: reporter}
	turn := &turnReport{id: "test-id", text: "create a reminder", output: pipeline.Output{
		NeedsLLM: true,
		Decision: &decision.Decision{Module: decision.Scored{Value: "reminder"}, Intent: decision.Scored{Value: "create"}},
		Trace:    decision.Trace{Attempts: []decision.Attempt{{Provider: "sneat-rules", Outcome: "abstained"}, {Provider: "cloud-decision", Outcome: "decided"}}},
	}}
	_ = h.reportCommand(turn, "completed", "answer_presented", "", "")()
	r := reporter.reports[0]
	if len(r.DetectionSteps) != 3 || r.DetectionSteps[1].Actions[0] != "reminder.create" ||
		r.DetectionSteps[1].Domains[0] != "reminder" || r.DetectionSteps[2].Method != "llm" ||
		len(r.ActionExecutions) != 0 {
		t.Fatalf("report=%+v", r)
	}
}

func TestCancelledDeterministicTurnReportsCancellation(t *testing.T) {
	h, _ := testHandler(t)
	reporter := &fakeInteractionReporter{}
	h.reporter = reporter
	h.turnSeq = 1
	h.turnReports = map[int64]*turnReport{1: {id: "test-id", text: "cancel this"}}
	cmd := h.handleTurn(turnMsg{seq: 1, text: "cancel this", state: *h.state, err: context.Canceled})
	if cmd == nil {
		t.Fatal("expected telemetry command")
	}
	_ = cmd()
	if len(reporter.reports) != 1 || reporter.reports[0].Status != "cancelled" || !reporter.reports[0].WasCancelled {
		t.Fatalf("reports=%+v", reporter.reports)
	}
}
