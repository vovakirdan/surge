package panicgate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ScanCRuntime enumerates the panic raises in the C runtime.
//
// It runs to a fixed point: the seeded reporters find the wrappers that forward
// a parameter to them, those wrappers become reporters, and the pass repeats
// until nothing new is found. That is what keeps array_panic, map_panic,
// panic_msg and bignum_panic in scope without anybody remembering to list them.
func ScanCRuntime(root string) ([]Site, error) {
	dir := filepath.Join(root, "runtime", "native")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type file struct {
		rel  string
		body string
	}
	var files []file
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || (!strings.HasSuffix(name, ".c") && !strings.HasSuffix(name, ".h")) {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- repository-owned path
		if readErr != nil {
			return nil, readErr
		}
		files = append(files, file{rel: filepath.ToSlash(filepath.Join("runtime", "native", name)), body: stripCComments(string(raw))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	raisers := primitiveRaisers()
	var sites []Site
	for range 8 {
		sites = nil
		var forwards []forward
		for _, f := range files {
			fileSites, fileForwards := scanCFile(f.rel, f.body, raisers)
			sites = append(sites, fileSites...)
			forwards = append(forwards, fileForwards...)
		}
		if !addForwards(raisers, forwards) {
			break
		}
	}
	sortSites(sites)
	return sites, nil
}

var cAssignRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*(?:\[\s*\])?\s*=\s*("(?:[^"\\]|\\.)*")`)

// cAssignment is one `... name = "literal"` in a file, remembered with its line
// so a call can be resolved against the nearest one above it.
type cAssignment struct {
	line int
	name string
	text string
}

func collectCAssignments(body string) []cAssignment {
	var out []cAssignment
	for i, line := range strings.Split(body, "\n") {
		for _, m := range cAssignRe.FindAllStringSubmatch(line, -1) {
			out = append(out, cAssignment{line: i + 1, name: m[1], text: m[2]})
		}
	}
	return out
}

func resolveCAssignment(assigns []cAssignment, name string, before int) (string, bool) {
	best := -1
	var text string
	for _, a := range assigns {
		if a.name == name && a.line <= before && a.line > best {
			best = a.line
			text = a.text
		}
	}
	if best < 0 {
		return "", false
	}
	lit, ok := unquoteCString(text)
	return lit, ok
}

// scanCFile walks one translation unit, tracking which function the cursor is
// inside, and reports the raises found in it together with the forwards that
// make its own functions reporters.
func scanCFile(rel, body string, raisers map[string][]raiser) ([]Site, []forward) {
	assigns := collectCAssignments(body)
	var sites []Site
	var forwards []forward

	w := newCWalker(body)
	ordinals := map[string]int{}
	for {
		call, ok := w.nextCall()
		if !ok {
			break
		}
		if call.function == "" {
			continue // a declaration or a macro body, not a raise
		}
		for _, r := range raisers[call.name] {
			if r.Arg >= len(call.args) {
				continue
			}
			arg := stripCCasts(call.args[r.Arg])
			// A reporter handed one of the enclosing function's own parameters
			// is not deciding anything: the caller decided. Promote the
			// enclosing function and let its callers be the sites.
			if idx := indexOf(call.params, arg); idx >= 0 {
				forwards = append(forwards, forward{Function: call.function, Arg: idx, Resolve: r.Resolve})
				continue
			}
			message := Computed
			if lit, ok := literalCMessage(arg); ok {
				if msg, ok := r.Resolve(lit); ok {
					message = msg
				}
			} else if lit, ok := resolveCAssignment(assigns, arg, call.line); ok {
				if msg, ok := r.Resolve(lit); ok {
					message = msg
				}
			}
			ordinals[call.function]++
			sites = append(sites, Site{
				File:     rel,
				Function: call.function,
				Ordinal:  ordinals[call.function],
				Raiser:   call.name,
				Message:  message,
				Line:     call.line,
			})
		}
	}
	return sites, forwards
}

// literalCMessage reads a (possibly concatenated) C string literal, or a bare
// integer, from an argument's text.
func literalCMessage(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", false
	}
	if arg[0] == '"' {
		var b strings.Builder
		rest := arg
		for strings.HasPrefix(rest, "\"") {
			end := endOfCString(rest)
			if end < 0 {
				return "", false
			}
			part, ok := unquoteCString(rest[:end])
			if !ok {
				return "", false
			}
			b.WriteString(part)
			rest = strings.TrimSpace(rest[end:])
		}
		if rest != "" {
			return "", false
		}
		return b.String(), true
	}
	for _, r := range arg {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return arg, true
}

func endOfCString(s string) int {
	escaped := false
	for i := 1; i < len(s); i++ {
		switch {
		case escaped:
			escaped = false
		case s[i] == '\\':
			escaped = true
		case s[i] == '"':
			return i + 1
		}
	}
	return -1
}

func unquoteCString(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' || i+1 >= len(inner) {
			b.WriteByte(inner[i])
			continue
		}
		i++
		switch inner[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		default:
			b.WriteByte(inner[i])
		}
	}
	return b.String(), true
}

var cCastRe = regexp.MustCompile(`^\(\s*(?:const\s+|unsigned\s+|signed\s+|struct\s+)*[A-Za-z_][A-Za-z0-9_]*\s*\**\s*\)`)

// stripCCasts removes the casts the runtime writes around a message pointer,
// so `(const uint8_t*)msg` reads as `msg`.
func stripCCasts(arg string) string {
	arg = strings.TrimSpace(arg)
	for {
		m := cCastRe.FindString(arg)
		if m == "" {
			return arg
		}
		arg = strings.TrimSpace(arg[len(m):])
	}
}

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

// stripCComments blanks comments while preserving every newline, so reported
// line numbers stay true to the file on disk.
func stripCComments(src string) string {
	out := make([]byte, 0, len(src))
	var inLine, inBlock, inStr, inChar, escaped bool
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				out = append(out, c)
				continue
			}
			out = append(out, ' ')
			continue
		case inBlock:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				out = append(out, ' ', ' ')
				i++
				continue
			}
			if c == '\n' {
				out = append(out, c)
				continue
			}
			out = append(out, ' ')
			continue
		case inStr || inChar:
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case inStr && c == '"':
				inStr = false
			case inChar && c == '\'':
				inChar = false
			}
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			inLine = true
			out = append(out, ' ', ' ')
			i++
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			inBlock = true
			out = append(out, ' ', ' ')
			i++
			continue
		case c == '"':
			inStr = true
		case c == '\'':
			inChar = true
		}
		out = append(out, c)
	}
	return string(out)
}
