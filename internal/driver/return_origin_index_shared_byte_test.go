package driver

import (
	"maps"
	"strings"
	"surge/internal/ast"
	"surge/internal/sema"
	"testing"
)

// A detached selection mutation proves: a shared byte result is insufficient authority when
// the selected declaration belongs to another nominal receiver.
func TestIndexCertificateSharedByteAuthority(t *testing.T) {
	const src = `type ByteBox = { values: uint8[] };
extern<ByteBox> {
 fn __index(self: &ByteBox, index: int) -> uint8 { return self.values[index]; }
}
type ByteBoxTwin = { values: uint8[] };
extern<ByteBoxTwin> {
 fn __index(self: &ByteBoxTwin, index: int) -> uint8 { return self.values[index]; }
}
fn read(box: &ByteBox) -> uint8 {
 return box[0];
}
`
	fn := patternFn(t, src, "fn read(")
	at := patternIn(t, src, fn, "box[0]")
	for _, mutate := range []bool{false, true} {
		name := "original"
		if mutate {
			name = "foreign_receiver"
		}
		t.Run(name, func(t *testing.T) {
			changed := 0
			f, analysis := analyzeOriginRoot(t, "review_shared_byte_"+name, src, false, func(f originalGenericFixture) {
				if !mutate {
					return
				}
				replacement := indexCallReplacement(t, f, func(c sema.CallableCandidate) bool {
					return c.Name == "__index" && strings.Contains(string(c.ReceiverKey), "ByteBoxTwin")
				})
				id := originExprAt(t, f.unit, f.owner.File.ID, at, ast.ExprIndex)
				for i := range f.inputs.units {
					unit := &f.inputs.units[i]
					if unit.Builder.Files.Get(unit.FileID).Span.File != f.owner.File.ID {
						continue
					}
					detached := *unit.Sema
					detached.IndexSymbols = maps.Clone(detached.IndexSymbols)
					if !detached.IndexSymbols[id].IsValid() || detached.IndexSymbols[id] == replacement {
						t.Fatal("PRECONDITION: selection is not distinct")
					}
					detached.IndexSymbols[id] = replacement
					unit.Sema = &detached
					changed++
				}
			})
			if mutate {
				if changed != 1 {
					t.Fatalf("changed units %d", changed)
				}
				if !originPendingAt(analysis, f.unit.SourceKey, at, indexCertContainer) {
					t.Fatalf("foreign shared-byte declaration accepted: %+v", originPendingWithin(analysis, f.unit.SourceKey, fn.start, fn.end))
				}
			} else {
				originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
				originNoEscape(t, analysis, f.owner.File.ID, fn)
				requireOriginSummary(t, analysis, f.owner.File.ID, "read", false, nil)
			}
		})
	}
}
