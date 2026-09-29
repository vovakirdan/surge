package driver

import (
	"crypto/sha256"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/source"
)

// An array of optional reference payloads was the nominal payload the analysis had to keep the local owner of. The
// payload holds a reference, so the array is refused by SEM3138 (the containment rule holds at any depth) before any
// analysis runs, whether it borrows an outside parameter or a local; the local owner cannot escape through it. Each
// row pins that refusal as the bag's only error code, at each place it is written.
func TestAnalyzeTypedReturnOriginNominalPayload(t *testing.T) {
	const prefix = "tag Some<T>(T); type Option<T> = Some(T) | nothing;\n" +
		"@intrinsic fn wrap(@return_source value: &string) -> Option<&string>;\n"
	for _, dynamic := range []bool{false, true} {
		for _, local := range []bool{false, true} {
			name, argument := "fixed_external", "outside"
			if dynamic {
				name = "dynamic_external"
			}
			if local {
				name, argument = strings.Replace(name, "external", "local", 1), "&owned"
			}
			t.Run(name, func(t *testing.T) {
				result := "ret [wrap(" + argument + ")];"
				if dynamic {
					result = "let values: Option<&string>[] = [wrap(" + argument + ")]; ret values;"
				}
				src := prefix + "fn probe(outside: &string) -> int {\n" +
					"    let escaped = { let owned: string = \"local\"; " + result + " };\n" +
					"    return 1;\n}\n"
				t.Logf("RETURN_ORIGIN_NOMINAL_SOURCE case=%s sha256=%x source=%q", name, sha256.Sum256([]byte(src)), src)
				root := t.TempDir()
				files := source.NewFileSetWithBase(root)
				file := files.Get(files.AddVirtual(filepath.Join(root, "origin.sg"), []byte(src)))
				bag := diag.NewBag(64)
				builder, fileID := diagnoseParseWithStrings(t.Context(), files, file, bag, source.NewInterner(), parser.DirectiveModeOff)
				resolved := diagnoseSymbols(builder, fileID, bag, "origin", file.Path, root, nil)
				diagnoseSema(t.Context(), builder, fileID, bag, nil, resolved, "origin", false, nil)
				// The dynamic case also spells the array type, which is refused where it is written.
				want := []string{"wrap(" + argument + ")"}
				if dynamic {
					want = []string{"Option<&string>", "wrap(" + argument + ")"}
				}
				var got []string
				for _, d := range bag.Items() {
					if d.Severity < diag.SevError {
						continue
					}
					if d.Code != diag.SemaRefInAggregate {
						t.Fatalf("error %s besides SEM3138: %+v", d.Code.ID(), *d)
					}
					got = append(got, src[d.Primary.Start:d.Primary.End])
				}
				if !slices.Equal(got, want) {
					t.Fatalf("SEM3138 at %q, want exactly %q", got, want)
				}
			})
		}
	}
}
