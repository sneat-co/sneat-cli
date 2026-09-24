package diag

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLog_DisabledEmitsNothing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	Log(context.Background(), logger, false, Turn{Path: PathDeterministic, Module: "calendar"})
	if buf.Len() != 0 {
		t.Fatalf("expected no output when disabled, got %q", buf.String())
	}
}

func TestLog_EnabledEmitsFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	Log(context.Background(), logger, true, Turn{Path: PathLLMFallback, Module: "calendar", Intent: "show_day", LLMSkipped: false, Fallback: "jev_timeout"})
	out := buf.String()
	for _, want := range []string{"llm-fallback", "calendar", "show_day", "jev_timeout"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q: %s", want, out)
		}
	}
}
