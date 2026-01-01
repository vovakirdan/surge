package vm

import (
	"encoding/json"
	"fmt"
	"strings"

	"surge/internal/source"
)

type LogPolicy struct {
	Overflow string `json:"overflow"`
	Bounds   string `json:"bounds"`
}

type LogHeader struct {
	V      int       `json:"v"`
	Kind   string    `json:"kind"`
	Surge  string    `json:"surge"`
	Policy LogPolicy `json:"policy"`
}

type LogValue struct {
	Type string          `json:"type"`
	V    json.RawMessage `json:"v"`
}

type LogIntrinsicEvent struct {
	Kind string     `json:"kind"`
	Name string     `json:"name"`
	Args []LogValue `json:"args,omitempty"`
	Ret  LogValue   `json:"ret"`
}

type LogExitEvent struct {
	Kind string `json:"kind"`
	Code int    `json:"code"`
}

type LogPanicEvent struct {
	Kind string   `json:"kind"`
	Code string   `json:"code"`
	Msg  string   `json:"msg"`
	At   string   `json:"at"`
	Bt   []string `json:"bt"`
}

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

func LogString(s string) LogValue {
	return LogValue{Type: "string", V: mustJSON(s)}
}

func LogStringArray(v []string) LogValue {
	cp := append([]string(nil), v...)
	return LogValue{Type: "string[]", V: mustJSON(cp)}
}

func LogInt(v int) LogValue {
	return LogValue{Type: "int", V: mustJSON(v)}
}

func LogInt64(v int64) LogValue {
	return LogValue{Type: "int64", V: mustJSON(v)}
}

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
