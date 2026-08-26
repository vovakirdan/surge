package llvm

import (
	"fmt"
	"regexp"
	"testing"
)

// A union payload that is a REFERENCE is stored as a pointer, whatever its
// pointee's shape, so reading it out loads that pointer.
//
// The case metadata the emitter works from records payload types with their
// `&` stripped, so `Option<&Pair>` and `Option<Pair>` name the same payload
// type there while laying out differently: eight bytes of pointer in the
// first, sixteen bytes of composite in the second. Reading the first as if it
// were the second answers with the ADDRESS of the pointer slot -- one
// indirection short -- and `q.a` then reads the pointer's own bytes as the
// field. It went unseen while every reference payload pointed at a word,
// because a word-shaped pointee is read through a load either way; a map of
// composites, whose `__index` answers `Option<&V>`, read its values as 0.
func TestEmitTagPayloadOfReferenceLoadsThePointer(t *testing.T) {
	sourceCode := `type Pair = { a: int, b: int };

fn first(r: Option<&Pair>) -> int {
    return compare r {
        Some(q) => q.a;
        nothing => 0 - 1;
    };
}

@entrypoint
fn main() -> int {
    let r: Option<&Pair> = nothing;
    return first(r);
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "first").ID))

	// The payload sits past the four-byte tag, at offset 8. Every address
	// computed there must be read as a pointer; storing the address itself
	// anywhere is the defect.
	payloadAddrs := regexp.MustCompile(`(%\w+) = getelementptr inbounds i8, ptr %\w+, i64 8\n`).FindAllStringSubmatch(body, -1)
	if len(payloadAddrs) == 0 {
		t.Fatalf("missing payload address at offset 8 in first():\n%s", body)
	}
	loaded := 0
	for _, m := range payloadAddrs {
		addr := regexp.QuoteMeta(m[1])
		if regexp.MustCompile(`store ptr ` + addr + `, ptr`).MatchString(body) {
			t.Fatalf("payload address %s of Option<&Pair> is stored as the payload instead of being loaded through:\n%s", m[1], body)
		}
		if regexp.MustCompile(`load ptr, ptr ` + addr + `\b`).MatchString(body) {
			loaded++
		}
	}
	if loaded == 0 {
		t.Fatalf("no `load ptr` from the Option<&Pair> payload address in first():\n%s", body)
	}
}
