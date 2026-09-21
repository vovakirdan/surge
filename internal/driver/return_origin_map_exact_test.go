package driver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

// The four typed Map sources of TestAnalyzeTypedMapReturnOrigins, byte for byte, judged on
// the root unit only: the landed test also requires analysis.Complete(), which stays false
// while the census is not zero.
type exactMapSource struct {
	name, text, digest string
	escapes            []backingEscape
}

func exactMapSources() []exactMapSource {
	return []exactMapSource{
		{name: "external_shared_local_key", digest: "94ea0e42f3412b4f346d6f9436f7a85fd96423aa5018fd436802c2c27032cf87", escapes: []backingEscape{}, text: `fn probe(m: &Map<string, string>) -> Option<&string> {
    return { let key: string = "key"; ret m.get_ref(&key); };
}
`},
		{name: "external_mut_local_key", digest: "73ff731c8884901be958966290922f00c177b2307eb2016d40285932e498d2c6", escapes: []backingEscape{}, text: `fn probe(m: &mut Map<string, string>) -> Option<&mut string> {
    return { let key: string = "key"; ret m.get_mut(&key); };
}
`},
		{name: "local_shared_external_key", digest: "213989569c465cdceb5ee17b3be7ecd07098d9bc691ca07ed2e8da24eb19fe61", escapes: []backingEscape{{135, 158, "owned", 65, 126}, {48, 165, "owned", 65, 126}}, text: `fn probe(key: &string) -> Option<&string> {
    return {
        let owned: Map<string, string> = Map::<string, string>.new();
        ret owned.get_ref(key);
    };
}
`},
		{name: "local_mut_external_key", digest: "91cc74944d1709cb2a3de948f36063eede9f24afa0b66ded6edc5a61098f0abd", escapes: []backingEscape{{143, 166, "owned", 69, 134}, {52, 173, "owned", 69, 134}}, text: `fn probe(key: &string) -> Option<&mut string> {
    return {
        let mut owned: Map<string, string> = Map::<string, string>.new();
        ret owned.get_mut(key);
    };
}
`},
	}
}

// 1 parent + 4 leaves = 5 RUN.
func TestAnalyzeExactTypedMapReturnOrigins(t *testing.T) {
	for _, tc := range exactMapSources() {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
				t.Fatalf("PRECONDITION: frozen source changed: %s", got)
			}
			stdlib := detectStdlibRootFrom(".")
			if stdlib == "" {
				t.Fatal("PRECONDITION: full stdlib is unavailable")
			}
			t.Setenv("SURGE_STDLIB", stdlib)
			path := filepath.Join(t.TempDir(), "origin.sg")
			if err := os.WriteFile(path, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			res := returnOriginMapBeforeHook(t, path)
			inputs, err := collectReturnOriginUnits(res)
			if err != nil {
				t.Fatalf("PRECONDITION: full owning-unit collection: %v", err)
			}
			rootKey := ""
			for i := range inputs.units {
				if inputs.units[i].Builder == res.Builder && inputs.units[i].FileID == res.FileID {
					rootKey = inputs.units[i].SourceKey
				}
			}
			if rootKey == "" {
				t.Fatal("PRECONDITION: the root unit is missing")
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: full-unit origin analysis could not run: %v", err)
			}
			if rows := originPendingWithin(analysis, rootKey, 0, len(tc.text)); len(rows) != 0 {
				t.Errorf("root unit left Pending: %+v", rows)
			}
			var got, want []string
			for _, d := range analysis.Diagnostics {
				if d.Primary.File != res.File.ID {
					continue
				}
				note := "without exactly one note"
				if len(d.Notes) == 1 {
					note = fmt.Sprintf("%d:%d", d.Notes[0].Span.Start, d.Notes[0].Span.End)
				}
				got = append(got, fmt.Sprintf("%v %d:%d %q note %s", d.Code, d.Primary.Start, d.Primary.End, d.Message, note))
			}
			for _, e := range tc.escapes {
				message := fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", e.owner)
				want = append(want, fmt.Sprintf("%v %d:%d %q note %d:%d", diag.SemaBorrowEscapesReturn, e.start, e.end, message, e.noteStart, e.noteEnd))
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("root diagnostic multiset = %q, want %q", got, want)
			}
			if len(tc.escapes) == 0 {
				summary := requireReturnOriginSummary(t, analysis, "probe")
				if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, []uint32{0}) {
					t.Errorf("probe summary = %+v, want slots [0]", summary)
				}
			}
		})
	}
}
