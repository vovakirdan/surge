package driver

import (
	"path/filepath"
	"strings"
	"testing"
)

const entropyCertificateSource = `import stdlib/entropy as entropy;
fn bytes() -> Erring<byte[], Error> { return entropy.bytes(8:uint); }
`

func TestReturnOriginEntropyCertificate(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "entropy_certificate", entropyCertificateSource, false, nil)
	if !analysis.Complete() {
		t.Fatalf("entropy wrapper stayed unfinished: %+v", analysis.Pending)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "bytes", false, nil)
}

func TestReturnOriginEntropyCertificateRequiresExactStdlibIdentity(t *testing.T) {
	const source = `@intrinsic fn rt_entropy_bytes(len: uint) -> Erring<byte[], Error>;
fn bytes() -> Erring<byte[], Error> { return rt_entropy_bytes(8:uint); }
`
	call := "rt_entropy_bytes(8:uint)"
	start := strings.Index(source, call)
	f, analysis := analyzeOriginRoot(t, "entropy_certificate_name_control", source, false, nil)
	span := originSpan{start: start, end: start + len(call), snippet: call}
	if !originPendingAt(analysis, f.unit.SourceKey, span, genericConditionUnsupported) {
		t.Fatalf("forged entropy identity lost its refusal: %+v", originPendingWithin(analysis, f.unit.SourceKey, span.start, span.end))
	}
}

func TestReturnOriginEntropyDependentModulesFinish(t *testing.T) {
	repo := repoRootFromDriverTest(t)
	t.Setenv("SURGE_STDLIB", repo)
	paths := []string{
		"stdlib/entropy/entropy.sg",
		"stdlib/random/random.sg",
		"stdlib/uuid/uuid.sg",
		"testdata/golden/sema/valid/stdlib_entropy_api.sg",
		"testdata/golden/sema/valid/stdlib_random_api.sg",
		"testdata/golden/sema/valid/stdlib_uuid_api.sg",
	}
	for _, rel := range paths {
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			result, err := DiagnoseWithOptions(t.Context(), filepath.Join(repo, filepath.FromSlash(rel)), &DiagnoseOptions{
				Stage: DiagnoseStageAll, MaxDiagnostics: 64,
			})
			if err != nil {
				t.Fatalf("entropy-dependent program did not finish: %v", err)
			}
			if result.Bag.HasErrors() {
				t.Fatalf("entropy-dependent program diagnosed errors: %+v", result.Bag.Items())
			}
		})
	}
}
