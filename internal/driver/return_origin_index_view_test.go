package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

// Scalar indexing of the exact core BytesView intrinsic produces a byte and therefore
// carries neither the view's reference origin nor its storage loan. Reference-bearing
// user indexers and views of local storage remain fail-closed.
const originIndexViewSource = `fn byte_from_view(s: &string) -> uint8 {
    let view = s.bytes();
    return view[0];
}

fn byte_from_owned_view(view: BytesView) -> uint8 {
    return view[0];
}

type RefBox = { values: int[] };
extern<RefBox> {
    fn __index(self: &RefBox, index: int) -> &int {
        return self.values[index];
    }
}

fn user_index_ref(box: &RefBox) -> &int {
    return box[0];
}

fn local_index_ref() -> &int {
    let box: RefBox = RefBox { values = [7] };
    return box[0];
}

fn local_view() -> BytesView {
    let text: string = "a" + "b";
    return text.bytes();
}
`

const originIndexViewSourceDigest = "584e04e9d083093e45ca7864ff61f42221a6c7b48becbc6be4f73c9359d5709d"

func TestReturnOriginSelectedContainerIndex(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(originIndexViewSource))); got != originIndexViewSourceDigest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	f, analysis := analyzeOriginRoot(t, "selected_container_index", originIndexViewSource, true, nil)
	rows := []struct {
		name string
		fn   originSpan
	}{
		{"borrowed_view_byte_finishes", originSpan{0, 88, originIndexViewSource[0:88]}},
		{"owned_view_byte_finishes", originSpan{90, 163, originIndexViewSource[90:163]}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			originExactPending(t, analysis, f.unit.SourceKey, row.fn, nil)
			originNoEscape(t, analysis, f.owner.File.ID, row.fn)
		})
	}

	canaries := []struct {
		name          string
		fn            originSpan
		selectedIndex bool
	}{
		{"user_index_returning_reference", originSpan{311, 373, originIndexViewSource[311:373]}, true},
		{"local_reference_through_index", originSpan{375, 473, originIndexViewSource[375:473]}, true},
		{"local_bytes_view", originSpan{475, 566, originIndexViewSource[475:566]}, false},
	}
	for _, row := range canaries {
		t.Run(row.name, func(t *testing.T) {
			pending := originPendingWithin(analysis, f.unit.SourceKey, row.fn.start, row.fn.end)
			refused := slices.ContainsFunc(pending, func(p sema.ReturnOriginPending) bool {
				return p.Reason != ""
			})
			selectedRefusal := slices.ContainsFunc(pending, func(p sema.ReturnOriginPending) bool {
				return p.Reason == "index requires its selected container transfer"
			})
			refused = refused || slices.ContainsFunc(analysis.Diagnostics, func(d diag.Diagnostic) bool {
				return d.Code == diag.SemaBorrowEscapesReturn && d.Primary.File == f.owner.File.ID &&
					int(d.Primary.Start) >= row.fn.start && int(d.Primary.End) <= row.fn.end
			})
			if !refused {
				t.Errorf("canary diagnosed clean; pending=%+v diagnostics=%+v", pending, analysis.Diagnostics)
			}
			if row.selectedIndex && !selectedRefusal {
				t.Errorf("reference-bearing selected index lost its named refusal: %+v", pending)
			}
		})
	}
}
