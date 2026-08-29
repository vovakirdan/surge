package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// One authority for a suspension frame's storage.
//
// The frame used to be reserved at a size this emitter wrote into the rt_alloc
// call and given back by a per-type body that wrote the same size again, while
// the runtime's own reclamation asked the type's DESCRIPTOR. Three statements of
// one width, and nothing made them agree. Both ends name the descriptor now, so
// this asks the only question that can still come apart: whether they name the
// SAME one.
const frameStorageProgram = `async fn add(a: int, b: int) -> int {
    checkpoint().await();
    return a + b;
}

@entrypoint
fn main() -> int {
    return compare add(2, 4).await() {
        Success(v) => v;
        Cancelled() => 9;
    };
}
`

var (
	frameAllocRe   = regexp.MustCompile(`call ptr @rt_frame_alloc\(ptr @(__surge_value_ops_type\d+)\)`)
	frameReleaseRe = regexp.MustCompile(`call void @rt_frame_release\(ptr @(__surge_value_ops_type\d+), ptr `)
	frameLayoutRe  = regexp.MustCompile(
		`@(__surge_value_ops_type\d+) = constant %struct\.rt_value_ops \{ %struct\.rt_value_layout \{ i64 (\d+),`)
)

func TestAFrameIsReservedAndReleasedThroughOneDescriptor(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, frameStorageProgram)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	allocs := frameAllocRe.FindAllStringSubmatch(ir, -1)
	if len(allocs) != 1 {
		t.Fatalf("the module reserves %d frames through rt_frame_alloc, want 1", len(allocs))
	}
	descriptor := allocs[0][1]

	releases := frameReleaseRe.FindAllStringSubmatch(ir, -1)
	if len(releases) == 0 {
		t.Fatal("the cancelled return gives its frame back through nothing at all")
	}
	for _, release := range releases {
		if release[1] != descriptor {
			t.Errorf("a frame reserved from @%s is released through @%s; "+
				"two descriptors are two widths", descriptor, release[1])
		}
	}

	// A per-type release body is the second authority this change removed: it
	// carried the size as a literal, so a frame whose layout moved was freed at
	// the width the emitter last wrote.
	if strings.Contains(ir, "define void @release.frame.type") {
		t.Error("the module still defines a per-type frame release; the width is stated twice again")
	}

	// The descriptor both ends name has to be one this module actually defines,
	// with a width in it. Without this the two calls could agree on a symbol
	// that resolves to nothing.
	for _, layout := range frameLayoutRe.FindAllStringSubmatch(ir, -1) {
		if layout[1] != descriptor {
			continue
		}
		if layout[2] == "0" {
			t.Fatalf("@%s states a width of 0 bytes for a frame that carries a resume point", descriptor)
		}
		return
	}
	t.Fatalf("the module names @%s at both ends of a frame and defines no such descriptor", descriptor)
}
