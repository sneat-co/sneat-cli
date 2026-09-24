// Package diag is Sneat's per-turn diagnostics record and slog emission
// (brief §15/§8).
//
// It does not depend on strongo/aichat's own ai/diag package: that package
// was still an empty stub in the parallel aichat-ai lane's worktree as of
// this slice (see the final report). This type's shape mirrors what the
// brief asks for so swapping to ai/diag.Turn later is a rename, not a
// redesign.
package diag

import (
	"context"
	"log/slog"
	"time"
)

// Path names which leg of the pipeline answered a turn.
type Path string

const (
	PathDeterministic Path = "deterministic"
	PathDecision      Path = "decision"
	PathLLM           Path = "llm"
	PathLLMFallback   Path = "llm-fallback"
)

// Turn is one chat turn's diagnostics. Never populate Text/user content
// fields here -- this record is designed to be logged at debug level without
// leaking what the user said or the model answered.
type Turn struct {
	Path            Path
	Module          string
	Intent          string
	RequiredScopes  []string
	ActualScopes    []string
	LLMSkipped      bool
	Provider        string
	Model           string
	InputTokens     int64
	OutputTokens    int64
	DecisionLatency time.Duration
	LLMLatency      time.Duration
	Fallback        string // reason the chain fell through, if any
	Err             string // error class/message, never a full stack or user text
}

// Log emits t at debug level, gated by enabled (the CLI's --debug / verbosity
// flag) so diagnostics never appear on a normal run.
func Log(ctx context.Context, logger *slog.Logger, enabled bool, t Turn) {
	if !enabled || logger == nil {
		return
	}
	attrs := []any{
		"path", string(t.Path),
		"module", t.Module,
		"intent", t.Intent,
		"requiredScopes", t.RequiredScopes,
		"actualScopes", t.ActualScopes,
		"llmSkipped", t.LLMSkipped,
		"provider", t.Provider,
		"model", t.Model,
		"inputTokens", t.InputTokens,
		"outputTokens", t.OutputTokens,
		"decisionLatencyMs", t.DecisionLatency.Milliseconds(),
		"llmLatencyMs", t.LLMLatency.Milliseconds(),
	}
	if t.Fallback != "" {
		attrs = append(attrs, "fallback", t.Fallback)
	}
	if t.Err != "" {
		attrs = append(attrs, "err", t.Err)
	}
	logger.DebugContext(ctx, "aichat turn", attrs...)
}
