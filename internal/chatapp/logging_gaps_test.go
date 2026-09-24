package chatapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	aidiag "github.com/strongo/aichat/ai/diag"

	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

func loggingHandler(buf *bytes.Buffer) *handler {
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &handler{ctx: context.Background(), logger: logger}
}

// TestHandler_LogTurn_NilLogger covers the h.logger==nil no-op guard: no
// panic, and (implicitly) no output.
func TestHandler_LogTurn_NilLogger(t *testing.T) {
	h := &handler{ctx: context.Background()}
	h.logTurn(aidiag.PathDeterministic, pipeline.Output{}, nil)
}

// TestHandler_LogTurn covers logTurn's full body: with a Decision set, with
// an error (ErrorCode branch), and its call-through to logDecisionTrace.
func TestHandler_LogTurn(t *testing.T) {
	var buf bytes.Buffer
	h := loggingHandler(&buf)
	out := pipeline.Output{
		Decision: &decision.Decision{
			Module: decision.Scored{Value: sneatdomain.ModuleCalendar},
			Intent: decision.Scored{Value: sneatdomain.IntentShowDay},
		},
		Trace: decision.Trace{Attempts: []decision.Attempt{{Provider: "sneat-rules", Outcome: "decided"}}},
	}
	h.logTurn(aidiag.PathDeterministic, out, errors.New("boom"))

	if !strings.Contains(buf.String(), "aichat.decision.attempt") {
		t.Fatalf("log output missing the decision-attempt line: %s", buf.String())
	}
}

// TestHandler_LogDecisionTrace_NilLoggerAndEmptyTrace cover both guard
// branches: a nil logger, and a trace with no Attempts.
func TestHandler_LogDecisionTrace_NilLoggerAndEmptyTrace(t *testing.T) {
	h := &handler{ctx: context.Background()}
	h.logDecisionTrace(decision.Trace{Attempts: []decision.Attempt{{Provider: "x"}}}) // nil logger: no-op, no panic

	var buf bytes.Buffer
	h2 := loggingHandler(&buf)
	h2.logDecisionTrace(decision.Trace{})
	if buf.Len() != 0 {
		t.Fatalf("expected no output for an empty trace, got %s", buf.String())
	}
}

// TestHandler_LogStreamRequest covers both branches: a nil *decision.Decision
// (PathLLMFallback) and a real one (PathDecision), plus the nil-logger
// no-op.
func TestHandler_LogStreamRequest(t *testing.T) {
	h := &handler{ctx: context.Background()}
	h.logStreamRequest(nil, ctxmgr.Report{}) // nil logger: no-op

	var buf bytes.Buffer
	h2 := loggingHandler(&buf)
	h2.logStreamRequest(nil, ctxmgr.Report{})
	var line map[string]any
	dec := json.NewDecoder(&buf)
	if err := dec.Decode(&line); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if line["path"] != "llm-fallback" {
		t.Fatalf("path = %v, want llm-fallback for a nil decision", line["path"])
	}

	buf.Reset()
	d := &decision.Decision{Module: decision.Scored{Value: sneatdomain.ModuleCalendar}, Intent: decision.Scored{Value: sneatdomain.IntentShowDay}}
	h3 := loggingHandler(&buf)
	h3.logStreamRequest(d, ctxmgr.Report{})
	line = nil
	if err := json.NewDecoder(&buf).Decode(&line); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if line["path"] != "decision" || line["module"] != sneatdomain.ModuleCalendar {
		t.Fatalf("line = %v, want path=decision module=%s", line, sneatdomain.ModuleCalendar)
	}
}

// TestModuleForEntityType covers every branch: each entity type and the
// default (unknown type) case.
func TestModuleForEntityType(t *testing.T) {
	cases := map[string]string{
		sneatdomain.EntityHappening: sneatdomain.ModuleCalendar,
		sneatdomain.EntityTodo:      sneatdomain.ModuleTodo,
		sneatdomain.EntityContact:   sneatdomain.ModuleContacts,
		"unknown":                   "",
	}
	for in, want := range cases {
		if got := moduleForEntityType(in); got != want {
			t.Errorf("moduleForEntityType(%q) = %q, want %q", in, got, want)
		}
	}
}
