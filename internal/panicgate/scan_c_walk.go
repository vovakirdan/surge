package panicgate

import "strings"

// cCall is one call found inside a function body.
type cCall struct {
	name     string
	args     []string
	line     int
	function string   // the enclosing function, "" at file scope
	params   []string // that function's parameter names
}

// cWalker is a brace- and quote-aware pass over a comment-stripped translation
// unit. It exists instead of a regular expression because the two questions the
// scan asks - which function am I inside, and where does this argument list end
// - are both about nesting, and a regular expression answers neither.
type cWalker struct {
	src  string
	i    int
	line int

	depth      int
	parenDepth int
	function   string
	params     []string
	candidate  string
	candArgs   []string
}

func newCWalker(src string) *cWalker { return &cWalker{src: src, line: 1} }

func (w *cWalker) nextCall() (cCall, bool) {
	for w.i < len(w.src) {
		c := w.src[w.i]
		switch {
		case c == '\n':
			w.line++
			w.i++
		case c == '"' || c == '\'':
			w.skipQuoted(c)
		case c == '#' && w.atLineStart():
			w.skipDirective()
		case c == '{':
			w.depth++
			if w.depth == 1 {
				w.function, w.params = w.candidate, w.candArgs
			}
			w.candidate, w.candArgs = "", nil
			w.i++
		case c == '}':
			w.depth--
			if w.depth <= 0 {
				w.depth = 0
				w.function, w.params = "", nil
			}
			w.candidate, w.candArgs = "", nil
			w.i++
		case c == ';':
			if w.depth == 0 {
				w.candidate, w.candArgs = "", nil
			}
			w.i++
		case c == '(':
			w.parenDepth++
			w.i++
		case c == ')':
			if w.parenDepth > 0 {
				w.parenDepth--
			}
			w.i++
		case isIdentStart(c):
			outer := w.parenDepth
			name, args, line, ok := w.readCallish()
			if !ok {
				continue
			}
			if w.depth == 0 && outer > 0 {
				// An identifier inside a signature's own parameter list, such
				// as a function-pointer parameter, is not the function being
				// declared.
				continue
			}
			if w.depth == 0 {
				// A signature at file scope: remember it until a `{` proves it
				// is a definition or a `;` proves it is a declaration.
				w.candidate, w.candArgs = name, paramNames(args)
				continue
			}
			return cCall{name: name, args: args, line: line, function: w.function, params: w.params}, true
		default:
			w.i++
		}
	}
	return cCall{}, false
}

func (w *cWalker) atLineStart() bool {
	for j := w.i - 1; j >= 0; j-- {
		switch w.src[j] {
		case ' ', '\t':
			continue
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func (w *cWalker) skipDirective() {
	for w.i < len(w.src) && w.src[w.i] != '\n' {
		if w.src[w.i] == '\\' && w.i+1 < len(w.src) && w.src[w.i+1] == '\n' {
			w.line++
			w.i += 2
			continue
		}
		w.i++
	}
}

func (w *cWalker) skipQuoted(quote byte) {
	w.i++
	for w.i < len(w.src) {
		c := w.src[w.i]
		if c == '\\' {
			if w.i+1 < len(w.src) && w.src[w.i+1] == '\n' {
				w.line++
			}
			w.i += 2
			continue
		}
		if c == '\n' {
			w.line++
		}
		w.i++
		if c == quote {
			return
		}
	}
}

// readCallish consumes an identifier and, when it is immediately followed by an
// argument list, the balanced list too.
func (w *cWalker) readCallish() (name string, args []string, line int, ok bool) {
	start := w.i
	for w.i < len(w.src) && isIdentPart(w.src[w.i]) {
		w.i++
	}
	name = w.src[start:w.i]
	line = w.line
	j := w.i
	for j < len(w.src) && (w.src[j] == ' ' || w.src[j] == '\t' || w.src[j] == '\n') {
		j++
	}
	if j >= len(w.src) || w.src[j] != '(' {
		return "", nil, 0, false
	}
	for ; w.i < j; w.i++ {
		if w.src[w.i] == '\n' {
			w.line++
		}
	}
	body, okArgs := balancedParen(w.src, w.i)
	if !okArgs {
		return "", nil, 0, false
	}
	// The cursor stops just inside the argument list rather than past it, so a
	// call nested in another call's arguments is still visited. Swallowing the
	// list is how `if (!deque_ensure_space(...))` hid a raise from the scan.
	w.i++
	w.parenDepth++
	return name, splitTopLevel(body), line, true
}

// balancedParen returns the text inside the `( ... )` that starts at open.
func balancedParen(src string, open int) (string, bool) {
	if open >= len(src) || src[open] != '(' {
		return "", false
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '"', '\'':
			quote := src[i]
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i++
				} else if src[i] == quote {
					break
				}
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open+1 : i], true
			}
		}
	}
	return "", false
}

// splitTopLevel splits an argument list on the commas that are not nested
// inside a parenthesis, bracket, brace or literal.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '"', '\'':
			quote := s[i]
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i++
				} else if s[i] == quote {
					break
				}
				i++
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if tail := strings.TrimSpace(s[start:]); tail != "" || len(out) > 0 {
		out = append(out, tail)
	}
	return out
}

// paramNames reads the declared names out of a parameter list: the last
// identifier of each parameter, which is the name in every declaration style
// this runtime uses.
func paramNames(params []string) []string {
	var out []string
	for _, p := range params {
		p = strings.TrimSpace(p)
		if p == "" || p == "void" {
			continue
		}
		if idx := strings.IndexByte(p, '['); idx >= 0 {
			p = p[:idx]
		}
		last := ""
		cur := strings.Builder{}
		for i := range len(p) {
			if isIdentPart(p[i]) {
				cur.WriteByte(p[i])
				continue
			}
			if cur.Len() > 0 {
				last = cur.String()
				cur.Reset()
			}
		}
		if cur.Len() > 0 {
			last = cur.String()
		}
		out = append(out, last)
	}
	return out
}

// cReporters names the functions in a translation unit that both write to
// stderr and exit the process - which is what reporting a panic IS, whatever
// it is called. It reuses the walker's own attribution of a call to its
// enclosing function, so it cannot disagree with the scan about where a call
// sits.
func cReporters(src string) map[string]bool {
	writes := map[string]bool{}
	exits := map[string]bool{}
	w := newCWalker(src)
	for {
		call, ok := w.nextCall()
		if !ok {
			break
		}
		if call.function == "" {
			continue
		}
		switch call.name {
		case "rt_write_stderr":
			writes[call.function] = true
		case "_exit":
			exits[call.function] = true
		}
	}
	out := map[string]bool{}
	for fn := range exits {
		if writes[fn] {
			out[fn] = true
		}
	}
	return out
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }
