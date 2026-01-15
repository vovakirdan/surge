package vm

import (
	"encoding/json"
	"fmt"
	"strings"

	"surge/internal/source"
)

// LogPolicy defines logging policy settings.
type LogPolicy struct {
	Overflow string `json:"overflow"`
	Bounds   string `json:"bounds"`
}

// LogHeader represents the header of a log file.
type LogHeader struct {
	V      int       `json:"v"`
	Kind   string    `json:"kind"`
	Surge  string    `json:"surge"`
	Policy LogPolicy `json:"policy"`
}

// LogValue represents a typed value in the log.
type LogValue struct {
	Type string          `json:"type"`
	V    json.RawMessage `json:"v"`
}

// LogIntrinsicEvent represents an intrinsic function call event.
type LogIntrinsicEvent struct {
	Kind string     `json:"kind"`
	Name string     `json:"name"`
	Args []LogValue `json:"args,omitempty"`
	Ret  LogValue   `json:"ret"`
}

// LogExitEvent represents a program exit event.
type LogExitEvent struct {
	Kind string `json:"kind"`
	Code int    `json:"code"`
}

// LogPanicEvent represents a panic event.
type LogPanicEvent struct {
	Kind string   `json:"kind"`
	Code string   `json:"code"`
	Msg  string   `json:"msg"`
	At   string   `json:"at"`
	Bt   []string `json:"bt"`
}

// NewLogHeader creates a new log header with default values.
func NewLogHeader() LogHeader {
	return LogHeader{
		V:     1,
		Kind:  "header",
		Surge: "",
		Policy: LogPolicy{
			Overflow: "panic",
			Bounds:   "panic",
		},
	}
}

// LogString creates a LogValue from a string.
func LogString(s string) LogValue {
	return LogValue{Type: "string", V: mustJSON(s)}
}

// LogStringArray creates a LogValue from a string array.
func LogStringArray(v []string) LogValue {
	cp := append([]string(nil), v...)
	return LogValue{Type: "string[]", V: mustJSON(cp)}
}

// LogInt creates a LogValue from an int.
func LogInt(v int) LogValue {
	return LogValue{Type: "int", V: mustJSON(v)}
}

// LogInt64 creates a LogValue from an int64.
func LogInt64(v int64) LogValue {
	return LogValue{Type: "int64", V: mustJSON(v)}
}

// MustDecodeString decodes a LogValue as a string.
func MustDecodeString(v LogValue) (string, error) {
	if v.Type != "string" {
		return "", fmt.Errorf("expected value type string, got %q", v.Type)
	}
	var out string
	if err := json.Unmarshal(v.V, &out); err != nil {
		return "", err
	}
	return out, nil
}

// MustDecodeStringArray decodes a LogValue as a string array.
func MustDecodeStringArray(v LogValue) ([]string, error) {
	if v.Type != "string[]" {
		return nil, fmt.Errorf("expected value type string[], got %q", v.Type)
	}
	var out []string
	if err := json.Unmarshal(v.V, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MustDecodeInt decodes a LogValue as an int.
func MustDecodeInt(v LogValue) (int, error) {
	if v.Type != "int" {
		return 0, fmt.Errorf("expected value type int, got %q", v.Type)
	}
	var out int
	if err := json.Unmarshal(v.V, &out); err != nil {
		return 0, err
	}
	return out, nil
}

// MustDecodeInt64 decodes a LogValue as an int64.
func MustDecodeInt64(v LogValue) (int64, error) {
	if v.Type != "int64" {
		return 0, fmt.Errorf("expected value type int64, got %q", v.Type)
	}
	var out int64
	if err := json.Unmarshal(v.V, &out); err != nil {
		return 0, err
	}
	return out, nil
}

type logTermSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// LogTermSize creates a LogValue from a terminal size pair.
func LogTermSize(cols, rows int) LogValue {
	return LogValue{Type: "term_size", V: mustJSON(logTermSize{Cols: cols, Rows: rows})}
}

// LogTermEvent creates a LogValue from a terminal event.
func LogTermEvent(ev TermEventData) LogValue {
	return LogValue{Type: "term_event", V: mustJSON(ev)}
}

// MustDecodeTermSize decodes a LogValue as a terminal size pair.
func MustDecodeTermSize(v LogValue) (cols, rows int, err error) {
	if v.Type != "term_size" {
		return 0, 0, fmt.Errorf("expected value type term_size, got %q", v.Type)
	}
	var out logTermSize
	if err := json.Unmarshal(v.V, &out); err != nil {
		return 0, 0, err
	}
	return out.Cols, out.Rows, nil
}

// MustDecodeTermEvent decodes a LogValue as a terminal event.
func MustDecodeTermEvent(v LogValue) (TermEventData, error) {
	if v.Type != "term_event" {
		return TermEventData{}, fmt.Errorf("expected value type term_event, got %q", v.Type)
	}
	var out TermEventData
	if err := json.Unmarshal(v.V, &out); err != nil {
		return TermEventData{}, err
	}
	return out, nil
}

// NewLogPanicEvent creates a LogPanicEvent from a VMError.
func NewLogPanicEvent(vmErr *VMError, files *source.FileSet) LogPanicEvent {
	if vmErr == nil {
		return LogPanicEvent{Kind: "panic"}
	}
	bt := make([]string, 0, len(vmErr.Backtrace))
	for _, f := range vmErr.Backtrace {
		bt = append(bt, fmt.Sprintf("%s@%s", f.FuncName, formatSpan(f.Span, files)))
	}
	return LogPanicEvent{
		Kind: "panic",
		Code: vmErr.Code.String(),
		Msg:  vmErr.Message,
		At:   formatSpan(vmErr.Span, files),
		Bt:   bt,
	}
}

// ParsePanicCode parses a panic code string into a PanicCode.
func ParsePanicCode(code string) (PanicCode, bool) {
	code = strings.TrimSpace(code)
	prefixLen := 0
	switch {
	case strings.HasPrefix(code, "VMX"):
		prefixLen = 3
	case strings.HasPrefix(code, "VM"):
		prefixLen = 2
	default:
		return 0, false
	}
	n := 0
	for i := prefixLen; i < len(code); i++ {
		ch := code[i]
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	if n == 0 {
		return 0, false
	}
	return PanicCode(n), true
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
