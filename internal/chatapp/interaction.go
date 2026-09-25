// Copyright 2026 Sneat.app

package chatapp

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	sneatrules "github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/strongo/aichat/ai/cloudproto"
)

type interactionReporter interface {
	ReportInteraction(context.Context, cloudproto.InteractionReport) error
}

type turnReport struct {
	id     string
	text   string
	output pipeline.Output
}

func (h *handler) reportCommand(turn *turnReport, status, outcome, action, actionStatus string) tea.Cmd {
	if turn == nil || h.reporter == nil || turn.id == "" {
		return nil
	}
	report := cloudproto.InteractionReport{
		InteractionID: turn.id, Product: Product,
		UserMessageChars: utf8.RuneCountInString(turn.text),
		UserMessageWords: len(strings.Fields(turn.text)),
		Status:           status, Outcome: outcome,
		WasCancelled: status == "cancelled",
	}
	for _, a := range turn.output.Trace.Attempts {
		method := detectionMethod(a.Provider)
		report.DetectionSteps = append(report.DetectionSteps, cloudproto.DetectionStep{Method: method, Detector: a.Provider, Result: a.Outcome})
	}
	if turn.output.Decision != nil {
		d := turn.output.Decision
		if d.Module.Value != "" {
			if len(report.DetectionSteps) == 0 {
				report.DetectionSteps = append(report.DetectionSteps, cloudproto.DetectionStep{Method: "unknown", Detector: "decision", Result: "decided"})
			}
			step := &report.DetectionSteps[len(report.DetectionSteps)-1]
			step.Domains = []string{d.Module.Value}
			if d.Intent.Value != "" {
				detectedAction := d.Module.Value + "." + d.Intent.Value
				step.Actions = []string{detectedAction}
				if action == "" {
					action = detectedAction
				}
			}
		}
	}
	if turn.output.NeedsLLM {
		result := "answered"
		if status != "completed" {
			result = status
		}
		step := cloudproto.DetectionStep{Method: "llm", Detector: "main-chat", Result: result}
		if action != "" {
			step.Actions = []string{action}
		}
		report.DetectionSteps = append(report.DetectionSteps, step)
	}
	if action != "" && actionStatus != "" {
		report.ActionExecutions = []cloudproto.ActionExecution{{Action: action, Status: actionStatus}}
	}
	reporter, baseCtx := h.reporter, h.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(baseCtx), time.Second)
		defer cancel()
		_ = reporter.ReportInteraction(ctx, report)
		return nil
	}
}

func reportAction(out pipeline.Output) string {
	if out.Decision == nil {
		return ""
	}
	d := out.Decision
	if d.Module.Value == "" || d.Intent.Value == "" {
		return ""
	}
	return d.Module.Value + "." + d.Intent.Value
}

func detectionMethod(provider string) string {
	if provider == sneatrules.Name {
		return "deterministic"
	}
	if strings.Contains(strings.ToLower(provider), "cloud") || strings.Contains(strings.ToLower(provider), "jev") {
		return "jev"
	}
	if strings.Contains(strings.ToLower(provider), "llm") {
		return "llm"
	}
	return "unknown"
}
