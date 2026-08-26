package sema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// adviceRendererFile is the one place user-facing clone advice may be written.
const adviceRendererFile = "clone_advice_render.go"

// TestCloneAdviceIsWrittenInOneePlace is the static census from the diagnostic
// contract.
//
// Before the shared renderer, eleven sites each spelled the way out for
// themselves, all of them unconditionally, and all of them naming `.__clone()`
// — the magic method rather than the operation. A sentence written at a site
// cannot consult the capability table, so the census keeps them in the table:
// a new emitter joins it instead of inventing a twelfth phrasing.
func TestCloneAdviceIsWrittenInOnePlace(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	offenders := make([]string, 0, 1)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == adviceRendererFile {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return true
			}
			if namesACloneCall(text) {
				offenders = append(offenders, name+": "+text)
			}
			return true
		})
	}
	if len(offenders) != 0 {
		t.Fatalf("clone advice written outside %s:\n  %s", adviceRendererFile, strings.Join(offenders, "\n  "))
	}
}

// namesACloneCall reports whether a literal spells a clone CALL a reader could
// copy into their program. That is the thing only the renderer may say.
//
// Two shapes look like calls and are not, and both are excluded by what they
// are rather than by where they live:
//
//   - a contract SIGNATURE (`__clone(self: &T) -> T`) describes a declaration
//     the author would write, not a call on a value;
//   - a generic operation's NAME (`Task<T>.clone()`) identifies which operation
//     is being discussed, and carries type parameters precisely because it is
//     not a spelling for any particular value.
//
// The bare method name is not a call either: lookups key on `"__clone"` and
// must keep doing so.
func namesACloneCall(text string) bool {
	if strings.Contains(text, "__clone(self:") {
		return false
	}
	if strings.Contains(text, "<T>.clone()") || strings.Contains(text, "<K, V>.keys()") {
		return false
	}
	return strings.Contains(text, "clone(") || strings.Contains(text, ".clone()")
}

// TestCloneAdviceCoversEverySiteByEveryCapability is the table from the
// contract, read as a table.
//
// The property under test is one sentence long and applies to every cell: a
// capability that cannot produce a clone must never be advised to call one.
// Everything else — which words, which order — is presentation; this is the
// part that is a defect when it is wrong.
func TestCloneAdviceCoversEverySiteByEveryCapability(t *testing.T) {
	sites := []struct {
		site cloneAdviceSite
		name string
	}{
		{adviceMovedTask, "moved task"},
		{adviceMovedValue, "moved value"},
		{adviceOwnedParam, "owned parameter marker"},
		{adviceBorrowIntoOwned, "borrow into owned"},
		{adviceReturnedBorrow, "returned borrow"},
		{adviceTaskBorrowsFrameLocal, "task borrows frame local"},
		{adviceReferenceInAggregate, "reference in aggregate"},
		{adviceChannelBorrow, "channel borrow"},
		{advicePartialMove, "partial move"},
		{adviceMoveOutOfSharedBorrow, "move out of shared borrow"},
		{adviceCompareArmPayload, "compare arm payload"},
	}
	states := []CloneState{CloneCopy, CloneValidMethod, CloneNonClonable}

	for _, site := range sites {
		for _, state := range states {
			sentence := cloneAdviceSentence(site.site, state, "value")
			if sentence == "" {
				t.Fatalf("%s at %s says nothing at all", site.name, state)
			}
			mentionsClone := strings.Contains(sentence, "clone(value)") || strings.Contains(sentence, "`value.clone()`")
			if state == CloneNonClonable && mentionsClone {
				t.Fatalf("%s advises a clone for a non-clonable type: %q", site.name, sentence)
			}
			if strings.Contains(sentence, "__clone()") {
				t.Fatalf("%s teaches the magic method instead of the operation: %q", site.name, sentence)
			}
		}
		// A deferred subject gets silence, not a guess: an undecided generic
		// must not acquire a clonability constraint through a hint.
		if sentence := cloneAdviceSentence(site.site, CloneDeferred, "value"); sentence != "" {
			t.Fatalf("%s advised a deferred generic: %q", site.name, sentence)
		}
	}
}

// TestCloneAdviceNamesTheFreeFunction pins the spelling itself.
//
// `clone(x)` is the route: the free function, which dispatches to `__clone`.
// `.clone()` is reserved for a local task handle, where it is a different
// operation on a different thing.
func TestCloneAdviceNamesTheFreeFunction(t *testing.T) {
	ordinary := cloneAdviceSentence(adviceMovedValue, CloneValidMethod, "widget")
	if !strings.Contains(ordinary, "clone(widget)") {
		t.Fatalf("an ordinary value is not advised through the free function: %q", ordinary)
	}
	task := cloneAdviceSentence(adviceMovedTask, CloneValidMethod, "handle")
	if !strings.Contains(task, "`handle.clone()`") {
		t.Fatalf("a task handle is not advised through its own operation: %q", task)
	}
	unnamed := cloneAdviceSentence(adviceMovedValue, CloneValidMethod, "")
	if strings.Contains(unnamed, "clone()") {
		t.Fatalf("an unnamed value was given an invented identifier: %q", unnamed)
	}
}
