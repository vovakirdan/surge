package buildpipeline

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

const compileAdmissionBorrowSource = `fn escape() -> &int64 {
    let owned: int64 = 7;
    return &owned;
}
@entrypoint
fn main() -> int { return 0; }
`

const compileAdmissionOtherErrorSource = `@return_source type Subject = string;
@entrypoint
fn main() -> int { return 0; }
`

// A diagnostic error is not sufficient: the unsafe path already fails later
// during HIR merging. The refused source must never acquire root HIR either.
func TestCompileDiagnosticsCannotAuthorizeHIR(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name    string
		source  string
		allow   bool
		backend Backend
		code    diag.Code
		message string
		token   string
	}{
		{"unsafe_borrow_vm", compileAdmissionBorrowSource, true, BackendVM, diag.SemaBorrowEscapesReturn,
			"cannot return a borrow of local 'owned': it is freed when the function returns", "&owned"},
		{"unsafe_borrow_llvm", compileAdmissionBorrowSource, true, BackendLLVM, diag.SemaBorrowEscapesReturn,
			"cannot return a borrow of local 'owned': it is freed when the function returns", "&owned"},
		{"default_borrow_vm", compileAdmissionBorrowSource, false, BackendVM, diag.SemaBorrowEscapesReturn,
			"cannot return a borrow of local 'owned': it is freed when the function returns", "&owned"},
		{"unsafe_other_error_vm", compileAdmissionOtherErrorSource, true, BackendVM, diag.SemaError,
			"attribute '@return_source' is not allowed here", "return_source"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := writeAnalysisSource(t, test.source)
			result, stderr, err := compileWithCapturedStderr(t, &CompileRequest{
				TargetPath: path, BaseDir: testRepoRoot(t), MaxDiagnostics: 100,
				Backend: test.backend, AllowDiagnosticsError: test.allow,
			})
			if err == nil {
				t.Fatal("refused source unexpectedly compiled")
			}
			res := result.Diagnose
			if res == nil || res.File == nil || res.Builder == nil || res.Symbols == nil || res.Sema == nil || res.Bag == nil {
				t.Fatalf("diagnostic refusal lost original typed artifacts: result=%+v error=%v", result, err)
			}
			if string(res.File.Content) != test.source {
				t.Fatal("diagnostic refusal lost original source bytes")
			}
			if entryErr := ValidateEntrypoints(res); entryErr != nil {
				t.Fatalf("precondition: source has no valid entrypoint: %v", entryErr)
			}
			var refusal *diag.Diagnostic
			for _, diagnostic := range res.Bag.Items() {
				if diagnostic == nil || diagnostic.Severity < diag.SevError {
					continue
				}
				if refusal != nil || diagnostic.Code != test.code || diagnostic.Message != test.message {
					t.Fatalf("precondition: unexpected source diagnostic: %+v; error=%v", diagnostic, err)
				}
				refusal = diagnostic
			}
			if refusal == nil {
				t.Fatalf("original %s refusal missing: error=%v stderr=%q", test.code.ID(), err, stderr)
			}
			span := refusal.Primary
			if span.File != res.File.ID || span.Start >= span.End || uint64(span.End) > uint64(len(test.source)) ||
				!strings.Contains(test.source[span.Start:span.End], test.token) {
				t.Fatalf("original refusal has wrong source span: %+v", span)
			}
			if !strings.Contains(stderr, refusal.Message) {
				t.Fatalf("original refusal missing from stderr: %q", stderr)
			}
			if !strings.Contains(err.Error(), "diagnostics reported errors") {
				t.Fatalf("precondition: refusal reached a different error path: %T %v", err, err)
			}
			if result.MIR != nil {
				t.Fatal("refused source reached MIR")
			}
			t.Logf("public Compile refusal reached: code=%s span=%+v error_type=%T error=%v root_hir=%t mir=%t",
				refusal.Code.ID(), span, err, err, res.HIR != nil, result.MIR != nil)
			if res.HIR != nil {
				t.Fatal("mandatory semantic refusal still acquired root HIR")
			}
		})
	}
}
