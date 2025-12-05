package trace

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format represents the output format for trace events.
type Format uint8

const (
	FormatAuto   Format = iota // auto-detect from file extension
	FormatText                 // human-readable text
	FormatNDJSON               // newline-delimited JSON
	FormatChrome               // Chrome Trace Viewer format (JSON)
)

// ParseFormat converts a string to Format.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "auto":
		return FormatAuto, nil
	case "text":
		return FormatText, nil
	case "ndjson":
		return FormatNDJSON, nil
	case "chrome":
		return FormatChrome, nil
	default:
		return FormatAuto, fmt.Errorf("invalid format: %q (expected: auto|text|ndjson|chrome)", s)
	}
}

// FormatEvent formats an event according to the specified format.
func FormatEvent(ev *Event, format Format) []byte {
	switch format {
	case FormatNDJSON:
		return formatNDJSON(ev)
	case FormatChrome:
		return formatChrome(ev)
	case FormatText:
		return formatText(ev)
	default:
		return formatText(ev)
	}
}

// formatNDJSON formats an event as newline-delimited JSON.
func formatNDJSON(ev *Event) []byte {
	type jsonEvent struct {
		Time     string            `json:"time"`
		Seq      uint64            `json:"seq"`
		Kind     string            `json:"kind"`
		Scope    string            `json:"scope"`
		SpanID   uint64            `json:"span_id"`
		ParentID uint64            `json:"parent_id,omitempty"`
		GID      uint64            `json:"gid,omitempty"`
		Name     string            `json:"name"`
		Detail   string            `json:"detail,omitempty"`
		Extra    map[string]string `json:"extra,omitempty"`
	}

	j := jsonEvent{
		Time:     ev.Time.Format("2006-01-02T15:04:05.000000Z07:00"),
		Seq:      ev.Seq,
		Kind:     ev.Kind.String(),
		Scope:    ev.Scope.String(),
		SpanID:   ev.SpanID,
		ParentID: ev.ParentID,
		GID:      ev.GID,
		Name:     ev.Name,
		Detail:   ev.Detail,
		Extra:    ev.Extra,
	}

	data, err := json.Marshal(j)
	if err != nil {
		// Fallback to empty JSON object if marshal fails
		data = []byte("{}\n")
		return data
	}
	data = append(data, '\n')
	return data
}

// formatText formats an event as human-readable text.
// Format: [timestamp] [indent]→/← name (detail)
func formatText(ev *Event) []byte {
	var sb strings.Builder

	// Display sequence number for ordering
	sb.WriteString(fmt.Sprintf("[seq %6d] ", ev.Seq))

	// Indentation based on parent ID (simplified - just use 0 or 2 spaces)
	if ev.ParentID > 0 {
		sb.WriteString("  ")
	}

	// Direction arrow
	switch ev.Kind {
	case KindSpanBegin:
		sb.WriteString("\u2192 ") // →
	case KindSpanEnd:
		sb.WriteString("\u2190 ") // ←
	case KindPoint:
		sb.WriteString("\u2022 ") // •
	case KindHeartbeat:
		sb.WriteString("\u2661 ") // ♡
	}

	// Name
	sb.WriteString(ev.Name)

	// Detail (if any)
	if ev.Detail != "" {
		sb.WriteString(" (")
		sb.WriteString(ev.Detail)
		sb.WriteString(")")
	}

	// Extra fields (compact format)
	if len(ev.Extra) > 0 {
		sb.WriteString(" {")
		first := true
		for k, v := range ev.Extra {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(v)
			first = false
		}
		sb.WriteString("}")
	}

	sb.WriteString("\n")
	return []byte(sb.String())
}

// formatChrome formats an event as Chrome Trace Viewer JSON.
// Chrome Trace Viewer format: https://docs.google.com/document/d/1CvAClvFfyA5R-PhYUmn5OOQtYMH4h6I0nSsKchNAySU
func formatChrome(ev *Event) []byte {
	type chromeEvent struct {
		Name string            `json:"name"`
		Cat  string            `json:"cat"`           // category
		Ph   string            `json:"ph"`            // phase: B/E/X/i
		Pid  uint64            `json:"pid"`           // process ID
		Tid  uint64            `json:"tid"`           // thread ID (goroutine ID)
		TS   int64             `json:"ts"`            // timestamp in microseconds
		Dur  int64             `json:"dur,omitempty"` // duration in microseconds (for "X" events)
		Args map[string]string `json:"args,omitempty"`
	}

	// Convert event kind to Chrome phase
	var phase string
	switch ev.Kind {
	case KindSpanBegin:
		phase = "B" // Begin
	case KindSpanEnd:
		phase = "E" // End
	case KindPoint:
		phase = "i" // Instant
	case KindHeartbeat:
		phase = "i" // Instant
	default:
		phase = "i"
	}

	// Convert timestamp to microseconds
	ts := ev.Time.UnixMicro()

	// Category from scope
	cat := ev.Scope.String()

	// Prepare args (detail + extra)
	args := make(map[string]string)
	if ev.Detail != "" {
		args["detail"] = ev.Detail
	}
	for k, v := range ev.Extra {
		args[k] = v
	}

	ce := chromeEvent{
		Name: ev.Name,
		Cat:  cat,
		Ph:   phase,
		Pid:  1,      // Single process
		Tid:  ev.GID, // Goroutine ID as thread ID
		TS:   ts,
		Args: args,
	}

	data, err := json.Marshal(ce)
	if err != nil {
		// Fallback
		data = []byte("{}")
	}

	return data
}
