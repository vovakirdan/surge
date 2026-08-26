package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

// A blocking submission names its two types as ids, and the runtime turns an id
// back into a descriptor. An id the operation census never saw answers the
// opaque word instead -- eight bytes, no drop, no move -- so the state would be
// freed at the word's width with its captures still inside it, and a result
// wider than a word would overflow the cell the job reserved for it.
//
// The refusal is here, in the emitter, because this is the last place the type
// is legible: the runtime has only the number. It is the same shape and the
// same reason as the crossing's `crossingStateTypeID`.
//
// A type with no bytes is deliberately NOT a refusal: a body that answers
// `nothing` describes nothing, and asking it for a descriptor would refuse
// every `blocking` that returns no value.
func TestBlockingTypeIDRefusesATypeWithNoDescriptor(t *testing.T) {
	stringsIn := source.NewInterner()
	typesIn := types.NewInterner()
	typesIn.Strings = stringsIn
	// No module, so no operation census: every type answers "not registered",
	// which is exactly the state a type the census skipped arrives in.
	fe := &funcEmitter{emitter: &Emitter{types: typesIn}}

	captures := typesIn.RegisterStruct(stringsIn.Intern("BlockingState"), source.Span{})
	if _, err := fe.blockingTypeID("captured state", captures); err == nil {
		t.Fatal("a captured state with no descriptor was accepted")
	} else if !strings.Contains(err.Error(), "captured state") ||
		!strings.Contains(err.Error(), "opaque word") {
		t.Fatalf("refusal does not name the role and the width it would be freed at: %v", err)
	}

	if _, err := fe.blockingTypeID("result", typesIn.Builtins().String); err == nil {
		t.Fatal("a result type with no descriptor was accepted")
	}

	// The two answers that must NOT be refusals.
	if got, err := fe.blockingTypeID("result", types.NoTypeID); err != nil || got != types.NoTypeID {
		t.Fatalf("absent type = %d, %v; want no type and no error", got, err)
	}
	nothing := typesIn.Builtins().Nothing
	if llvmTy, err := fe.emitter.llvmType(nothing); err != nil || llvmTy != "void" {
		t.Fatalf("premise: `nothing` must lower to void, got %q (%v)", llvmTy, err)
	}
	if got, err := fe.blockingTypeID("result", nothing); err != nil || got != nothing {
		t.Fatalf("a `nothing` result = %d, %v; want it carried through unrefused", got, err)
	}
}

// The submission carries the captures' TYPE, and the type it carries is one the
// module published a descriptor for.
//
// It used to carry a size and an alignment. Two integers can free the state
// block at the right width and can do nothing about what is inside it, which is
// why a blocking job abandoned every capture it held. The id is the whole fix:
// the runtime resolves it to the descriptor and destroys the members through
// the type's own drop before freeing the block.
//
// Asserting the id is REGISTERED, rather than merely present, is what stops the
// row passing on a number that happens to be there: an unregistered id resolves
// to the opaque word, which is a size and an alignment again by another name.
const blockingSubmissionFixture = `
@shard_movable
type Note = { id: int, text: string };

fn sink(n: own Note) -> int {
    return n.id;
}

async fn run() -> int {
    let note: Note = Note { id = 7, text = "captured" };
    let held: Task<int> = blocking { ret sink(own note); };
    let bare: Task<int> = blocking { ret 42; };
    let a: int = compare held.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    let b: int = compare bare.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    return a + b;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

var blockingSubmitCall = regexp.MustCompile(
	`call ptr @rt_blocking_submit\(i64 \d+, ptr \S+, i64 (\d+), i64 (\d+)\)`)

func TestBlockingSubmissionNamesARegisteredStateType(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, blockingSubmissionFixture)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(ir, "declare ptr @rt_blocking_submit(i64, ptr, i64, i64)") {
		t.Errorf("rt_blocking_submit is not declared as (fn id, state, state type, result type)")
	}

	registered := make(map[string]bool)
	for _, m := range regexp.MustCompile(`i64 (\d+), label %value_ops\.\d+`).FindAllStringSubmatch(ir, -1) {
		registered[m[1]] = true
	}
	if len(registered) == 0 {
		t.Fatal("the module published no value-operation descriptors at all")
	}

	calls := blockingSubmitCall.FindAllStringSubmatch(ir, -1)
	// Two bodies, one holding a capture and one holding none: the zero-sized
	// state is the block the old release skipped, so it has to be named too.
	if len(calls) != 2 {
		t.Fatalf("blocking submissions matched = %d, want 2; the call shape changed:\n%s",
			len(calls), strings.Join(grepLines(ir, "rt_blocking_submit"), "\n"))
	}
	for i, call := range calls {
		if !registered[call[1]] {
			t.Errorf("submission %d names state type#%s, which has no descriptor in this module",
				i, call[1])
		}
		if !registered[call[2]] {
			t.Errorf("submission %d names result type#%s, which has no descriptor in this module",
				i, call[2])
		}
	}
}

func grepLines(text, needle string) []string {
	var out []string
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, fmt.Sprintf("%d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	return out
}
