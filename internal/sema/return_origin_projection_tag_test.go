package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"
)

// A field read through a reference and a tag built in its own template both
// keep one exact private source: the incoming value V(0), never its contents.
func TestReturnOriginProjectionAndTemplateTagPrivateRoots(t *testing.T) {
	for _, tc := range []struct {
		name, digest, body, text string
	}{
		{"projection_is_address", "db0097c704954eb44bde4fe28d9fb1df26e715ffdb985178b565a68849b64138", "read_text",
			"pragma no_std;\ntype Note = { text: string };\nfn read_text(n: &Note) -> &string {\n    return n.text;\n}\n"},
		{"template_tag_keeps_payload", "d5f61971745c699cd40af323566dc93fa0b76a544a4cddd5f5f83070e06c3318", "wrap",
			"pragma no_std;\ntag Hold<T>(T);\ntype Held<T> = Hold(T) | nothing;\nfn wrap<T>(value: T) -> Held<T> {\n    return Hold::<T>(value);\n}\nfn anchor() -> int64 {\n    return 1;\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
				t.Fatalf("PRECONDITION: frozen source changed: %s", got)
			}
			a := returnOriginConditionAnalyzer(t, tc.text)
			if len(a.report.Pending) != 0 || len(a.report.Diagnostics) != 0 {
				t.Errorf("unfinished private transfer: pending=%+v diagnostics=%+v", a.report.Pending, a.report.Diagnostics)
			}
			var fn *returnOriginFunction
			for _, candidate := range a.functions {
				if candidate.name == tc.body {
					fn = candidate
				}
			}
			if fn == nil {
				t.Fatalf("PRECONDITION: body %s is missing", tc.body)
			}
			fact := a.summaries[fn.key].value
			want := []returnOrigin{{kind: returnOriginParam, param: 0, selector: returnOriginInputValue}}
			if !fact.normal || len(fact.callables) != 0 || !slices.Equal(fact.roots, want) {
				t.Errorf("%s private result = %+v, want exactly V(0)", tc.body, fact)
			}
		})
	}
}
