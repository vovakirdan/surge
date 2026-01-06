package vm

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"surge/internal/source"
)

// BreakpointKind distinguishes breakpoint types.
type BreakpointKind uint8

const (
	// BKFileLine represents a file:line breakpoint.
	BKFileLine BreakpointKind = iota
	// BKFuncEntry represents a function entry breakpoint.
	BKFuncEntry
)

// Breakpoint represents a debugger breakpoint.
type Breakpoint struct {
	ID   int
	Kind BreakpointKind

	// BKFileLine:
	File    string
	FileAbs string
	Line    int

	// BKFuncEntry:
	FuncName string
}

// Summary returns a string representation of the breakpoint.
func (bp *Breakpoint) Summary() string {
	if bp == nil {
		return "<nil>"
	}
	switch bp.Kind {
	case BKFileLine:
		return fmt.Sprintf("#%d %s:%d", bp.ID, bp.File, bp.Line)
	case BKFuncEntry:
		return fmt.Sprintf("#%d fn:%s", bp.ID, bp.FuncName)
	default:
		return fmt.Sprintf("#%d <unknown>", bp.ID)
	}
}

// Breakpoints manages a collection of breakpoints.
type Breakpoints struct {
	nextID int
	list   []*Breakpoint
}

// NewBreakpoints creates a new Breakpoints collection.
func NewBreakpoints() *Breakpoints {
	return &Breakpoints{nextID: 1}
}

// AddFileLine adds a file:line breakpoint.
func (bps *Breakpoints) AddFileLine(file string, line int) (*Breakpoint, error) {
	if line <= 0 {
		return nil, fmt.Errorf("invalid line %d", line)
	}
	file = filepath.Clean(file)
	if file == "." || file == "" {
		return nil, fmt.Errorf("invalid file %q", file)
	}

	abs, err := filepath.Abs(file)
	if err != nil {
		abs = ""
	} else {
		abs = filepath.Clean(abs)
	}

	bp := &Breakpoint{
		ID:      bps.allocID(),
		Kind:    BKFileLine,
		File:    file,
		FileAbs: abs,
		Line:    line,
	}
	bps.list = append(bps.list, bp)
	return bp, nil
}

// AddFuncEntry adds a function entry breakpoint.
func (bps *Breakpoints) AddFuncEntry(funcName string) (*Breakpoint, error) {
	funcName = strings.TrimSpace(funcName)
	if funcName == "" {
		return nil, fmt.Errorf("empty function name")
	}
	bp := &Breakpoint{
		ID:       bps.allocID(),
		Kind:     BKFuncEntry,
		FuncName: funcName,
	}
	bps.list = append(bps.list, bp)
	return bp, nil
}

// Delete removes a breakpoint by ID.
func (bps *Breakpoints) Delete(id int) bool {
	if bps == nil || id <= 0 {
		return false
	}
	for i, bp := range bps.list {
		if bp != nil && bp.ID == id {
			copy(bps.list[i:], bps.list[i+1:])
			bps.list[len(bps.list)-1] = nil
			bps.list = bps.list[:len(bps.list)-1]
			return true
		}
	}
	return false
}

// List returns all breakpoints.
func (bps *Breakpoints) List() []*Breakpoint {
	if bps == nil || len(bps.list) == 0 {
		return nil
	}
	out := make([]*Breakpoint, 0, len(bps.list))
	out = append(out, bps.list...)
	return out
}

// Match checks if any breakpoint matches the given stop point.
func (bps *Breakpoints) Match(vm *VM, sp StopPoint) (*Breakpoint, bool) {
	if bps == nil || len(bps.list) == 0 {
		return nil, false
	}

	var (
		filePath string
		line     int
		okSpan   bool
		isEntry  bool
	)
	if vm != nil && vm.Files != nil && (sp.Span.Start != 0 || sp.Span.End != 0) {
		file, start, ok := resolveSpanStart(vm.Files, sp.Span)
		if ok {
			filePath = filepath.Clean(file)
			line = int(start.Line)
			okSpan = true
		}
	}

	if vm != nil && len(vm.Stack) > 0 {
		f := &vm.Stack[len(vm.Stack)-1]
		isEntry = f.Func != nil && f.BB == f.Func.Entry && f.IP == 0
	}

	for _, bp := range bps.list {
		if bp == nil {
			continue
		}
		switch bp.Kind {
		case BKFuncEntry:
			if isEntry && bp.FuncName == sp.FuncName {
				return bp, true
			}
		case BKFileLine:
			if !okSpan || bp.Line != line {
				continue
			}
			if bp.File == filePath {
				return bp, true
			}
			if bp.FileAbs != "" {
				abs, err := filepath.Abs(filePath)
				if err == nil && filepath.Clean(abs) == bp.FileAbs {
					return bp, true
				}
			}
		}
	}

	return nil, false
}

// ParseFileLineSpec parses a file:line breakpoint specification.
func ParseFileLineSpec(spec string) (file string, line int, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", 0, fmt.Errorf("empty spec")
	}

	colon := strings.LastIndex(spec, ":")
	if colon <= 0 || colon >= len(spec)-1 {
		return "", 0, fmt.Errorf("expected <file:line>, got %q", spec)
	}
	file = spec[:colon]
	lineStr := spec[colon+1:]
	n, err := strconv.Atoi(lineStr)
	if err != nil || n <= 0 {
		return "", 0, fmt.Errorf("invalid line %q", lineStr)
	}
	return file, n, nil
}

func resolveSpanStart(files *source.FileSet, span source.Span) (path string, start source.LineCol, ok bool) {
	if files == nil || (span.Start == 0 && span.End == 0) {
		return "", source.LineCol{}, false
	}
	f := files.Get(span.File)
	if f == nil {
		return "", source.LineCol{}, false
	}
	start, _ = files.Resolve(span)
	return f.Path, start, true
}

func (bps *Breakpoints) allocID() int {
	if bps.nextID <= 0 {
		bps.nextID = 1
	}
	id := bps.nextID
	bps.nextID++
	return id
}
