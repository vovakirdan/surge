package driver

import (
	"encoding/json"
	"fmt"

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
	msg := fmt.Sprintf("timings (%s): total %.2f ms", payload.Kind, payload.TotalMS)
	if payload.Path != "" {
		msg = fmt.Sprintf("%s — %s", msg, payload.Path)
	}

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
