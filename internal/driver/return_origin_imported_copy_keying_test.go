package driver

import (
	"slices"
	"strings"
	"testing"
)

// An importer's copy is keyed as the declaration it copies, whatever path the
// import spelled: the module a directory makes, imported by one of its files;
// a module whose pragma names it apart from its directory; an aliased import;
// the extern methods of an intrinsic stdlib type, declared with a prelude
// copy's flags. Each copy reaches its declaration's body and promise, so a
// forwarded parameter is accepted and an escaping local is reported where it
// happens.
func TestReturnOriginImportedCopyKeyedAsDeclaration(t *testing.T) {
	leakFirst := "fn pick(a: &string, b: &string) -> &string {\n    return %s(a, b);\n}\n\nfn leak() -> &string {\n    let s: string = \"x\";\n    return %s(&s, &s);\n}\n"
	named := "pragma module::bar;\n\npub fn first(a: &string, b: &string) -> &string {\n    return a;\n}\n"
	for _, tc := range []struct {
		name    string
		files   map[string]string
		escapes []int
	}{
		{
			name: "module_imported_by_its_file",
			files: map[string]string{
				"app/lib/util.sg": importedCallableFirst,
				"app/main.sg":     "import ./lib/util::*;\n\n" + strings.ReplaceAll(leakFirst, "%s", "first"),
			},
			escapes: []int{9},
		},
		{
			name: "aliased_import",
			files: map[string]string{
				"app/lib/util.sg": importedCallableFirst,
				"app/main.sg":     "import ./lib/util::{first as pick_first};\n\n" + strings.ReplaceAll(leakFirst, "%s", "pick_first"),
			},
			escapes: []int{9},
		},
		{
			name: "module_named_apart_from_its_directory",
			files: map[string]string{
				"app/foo/foo.sg": named,
				"app/main.sg":    "import bar::first;\n\n" + strings.ReplaceAll(leakFirst, "%s", "first"),
			},
			escapes: []int{9},
		},
		{
			name: "stdlib_intrinsic_methods",
			files: map[string]string{
				"app/main.sg": "import stdlib/time as time;\n\nfn span(a: &time.Duration, b: time.Duration) -> int64 {\n    let d = a.sub(b);\n    return d.as_micros();\n}\n",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			escapes, pending := importedCallableOutcome(t, tc.files, "app/main.sg")
			if len(pending) != 0 {
				t.Fatalf("the imported copy stayed unfinished: %v", pending)
			}
			if !slices.Equal(escapes, tc.escapes) {
				t.Fatalf("escapes at lines %v, want %v", escapes, tc.escapes)
			}
		})
	}
}
