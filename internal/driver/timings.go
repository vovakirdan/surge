package driver

import (
	"encoding/json"
	"fmt"
	"strings"

	"surge/internal/diag"
	"surge/internal/observ"
	"surge/internal/source"
)

type timingPayload struct {
	Kind    string               `json:"kind"`
	Path    string               `json:"path,omitempty"`
	TotalMS float64              `json:"total_ms"`
	Phases  []observ.PhaseReport `json:"phases"`
}

func appendTimingDiagnostic(bag *diag.Bag, payload timingPayload) {
	if bag == nil {
		return
	}
	if payload.Kind == "" {
		payload.Kind = "pipeline"
	}

	msg := formatSummary(payload)

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	entry := &diag.Diagnostic{
		Severity: diag.SevInfo,
		Code:     diag.ObsTimings,
		Message:  msg,
		Primary:  source.Span{},
		Notes: []diag.Note{
			{Span: source.Span{}, Msg: string(data)},
		},
	}

	if bag.Add(entry) {
		return
	}
	overflow := diag.NewBag(len(bag.Items()) + 1)
	overflow.Add(entry)
	bag.Merge(overflow)
}

func formatSummary(payload timingPayload) string {
	var summary strings.Builder
	for i, phase := range payload.Phases {
		if phase.Name == "" {
			continue
		}
		if summary.Len() > 0 {
			summary.WriteString(" • ")
		}
		summary.WriteString(fmt.Sprintf("%s %.2fms", phase.Name, phase.DurationMS))
		if phase.Note != "" {
			summary.WriteString(fmt.Sprintf(" (%s)", phase.Note))
		}
		if i == len(payload.Phases)-1 {
			break
		}
	}
	total := fmt.Sprintf("total %.2fms", payload.TotalMS)
	if summary.Len() > 0 {
		summary.WriteString(" • ")
	}
	summary.WriteString(total)
	msg := fmt.Sprintf("timings (%s): %s", payload.Kind, summary.String())
	if payload.Path != "" {
		msg = fmt.Sprintf("%s — %s", msg, payload.Path)
	}
	return msg
}
