package trace

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format represents the output format for trace events.
type Format uint8

const (
	FormatText   Format = iota // human-readable text
	FormatNDJSON               // newline-delimited JSON
)

// FormatEvent formats an event according to the specified format.
func FormatEvent(ev Event, format Format) []byte {
	switch format {
	case FormatNDJSON:
		return formatNDJSON(ev)
	case FormatText:
		return formatText(ev)
	default:
		return formatText(ev)
	}
}

// formatNDJSON formats an event as newline-delimited JSON.
func formatNDJSON(ev Event) []byte {
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

	data, _ := json.Marshal(j)
	data = append(data, '\n')
	return data
}

// formatText formats an event as human-readable text.
// Format: [timestamp] [indent]→/← name (detail)
func formatText(ev Event) []byte {
	var sb strings.Builder

	// Timestamp relative to start (in milliseconds)
	// For simplicity, we use the seq number as a proxy for ordering
	elapsed := float64(ev.Seq) * 0.001 // approximate
	sb.WriteString(fmt.Sprintf("[%7.3fms] ", elapsed))

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
