package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every accepted flat-matrix source is run by both backends. The native run
// uses strict Valgrind, including the expected missing-key panic (exit 1).
var indexCertificateProbeRows = []struct {
	name, want string
	exit       int
	runtimeTLS bool
}{
	{"m04b_param_elem_ok", "42\n", 0, false},
	{"m12_compound", "2\n", 0, false},
	{"m15_field_map_param", "8\n", 0, false},
	{"m17_deref_map", "2\n", 0, false},
	{"m18_store_local_ref_value", "3\n", 0, false},
	{"m19_missing_key_panics", "", 1, false},
	{"m20_growth_while_ref_in_scope", "1560\n", 0, false},
	{"m21_ref_map_param_store_ref_param", "1\n", 0, false},
	{"m33_async_param_elem", "5\n", 0, true},
	{"o02_own_read", "3\n", 0, false},
	{"u01_user_index_int_erased", "5\n", 0, false},
	{"u06_bytesview_index", "97\n", 0, false},
	{"hash64_basic", "", 0, false},
	{"map_composite_value", "10\n21\n1\n30\n", 0, false},
	{"map_growth_boundary", "10\n0\n9\n0\n9\n9\n", 0, false},
	{"map_index_get", "10\n", 0, false},
	{"map_index_set", "11\n", 0, false},
	{"review_shared_byte", "11\n", 0, false},
	{"review_map_option_scoped", "old\nnew!\n", 0, false},
	{"review_map_branch_scoped", "other\nnew!\n", 0, false},
	{"review_own_array_early", "10\n70\n", 0, false},
	{"runtime_map_read_through_a_formal", "42\n41\n7\n", 0, false},
	{"runtime_map_store_local_and_through_a_formal", "16\n2\n", 0, false},
	{"runtime_map_int_key_record_value", "51\n", 0, false},
	{"runtime_map_growth_by_index", "1560\n", 0, false},
	{"runtime_map_string_value", "3\nuno!\n", 0, false},
	{"runtime_own_container_read", "3\n", 0, false},
	{"runtime_nominal_int_index", "11\n", 0, false},
}

func indexCertificateProbeSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "index_certificate", name+".sg"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestIndexCertificateProbeRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range indexCertificateProbeRows {
		t.Run(row.name, func(t *testing.T) {
			if row.exit == 1 {
				root := repoRoot(t)
				source := filepath.Join(root, "testdata", "index_certificate", row.name+".sg")
				out, err, code := runSurgeWithEnv(t, root, buildSurgeBinary(t, root), envWithStdlib(root), "run", "--backend", "vm", source)
				if code != 1 || out != row.want || !strings.Contains(err, "panic: map key not found") {
					t.Fatalf("missing-key VM exit=%d stdout=%q stderr=%s", code, out, err)
				}
				return
			}
			result := runProgramFromSource(t, indexCertificateProbeSource(t, row.name), runOptions{captureStdout: true})
			if result.exitCode != row.exit || result.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want exit=%d stdout=%q\nstderr:\n%s", result.exitCode, result.stdout, row.exit, row.want, result.stderr)
			}
		})
	}
}

func TestIndexCertificateProbeRunsLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range indexCertificateProbeRows {
		t.Run(row.name, func(t *testing.T) {
			binary := buildLLVMProgramFromSource(t, indexCertificateProbeSource(t, row.name))
			out, err, code := runIndexCertificateValgrind(t, binary, envWithStdlib(root), 120*time.Second)
			if code != row.exit || out != row.want || hasValgrindMemcheckError(err) || !strings.Contains(err, "ERROR SUMMARY: 0 errors") {
				t.Fatalf("valgrind exit=%d stdout=%q want exit=%d stdout=%q\nstderr:\n%s", code, out, row.exit, row.want, err)
			}
			if row.exit == 0 && !row.runtimeTLS && !strings.Contains(err, "All heap blocks were freed") {
				t.Fatalf("a block outlived the program:\n%s", err)
			}
			// The async fixture starts worker and blocking pools. Their process-exit
			// accounting retains reachable state and pthread TLS, so this row proves
			// memory safety and no definite/indirect leak, not complete reclamation.
			if row.runtimeTLS {
				if !strings.Contains(err, "definitely lost: 0 bytes") || !strings.Contains(err, "indirectly lost: 0 bytes") {
					t.Fatalf("runtime fixture leaked definite or indirect blocks:\n%s", err)
				}
				t.Logf("runtime exit accounting (not a full reclamation claim):\n%s", err)
			}
			if row.exit == 1 && !strings.Contains(err, "panic: map key not found") {
				t.Fatalf("missing-key failure lost its panic: %s", err)
			}
		})
	}
}
