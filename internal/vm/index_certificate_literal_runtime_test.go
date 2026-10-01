package vm_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These rows combine the statement-literal proof with an index transfer.
// Reachable startup string storage is measured on the base control too;
// it is not a claim of complete reclamation at process exit.
var indexCertificateLiteralRows = []struct {
	name, want string
	reachable  int
}{
	{"map_get_mut", "2\n", 18},
	{"s01_string_index", "98\n", 20},
}

func TestIndexCertificateLiteralRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range indexCertificateLiteralRows {
		t.Run(row.name, func(t *testing.T) {
			result := runProgramFromSource(t, indexCertificateProbeSource(t, row.name), runOptions{captureStdout: true})
			if result.exitCode != 0 || result.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q; stderr=%s", result.exitCode, result.stdout, row.want, result.stderr)
			}
		})
	}
}

func TestIndexCertificateLiteralRunsLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range indexCertificateLiteralRows {
		t.Run(row.name, func(t *testing.T) {
			binary := buildLLVMProgramFromSource(t, indexCertificateProbeSource(t, row.name))
			out, err, code := runIndexCertificateValgrind(t, binary, envWithStdlib(root), 120*time.Second)
			if code != 0 || out != row.want || hasValgrindMemcheckError(err) || !strings.Contains(err, "ERROR SUMMARY: 0 errors") {
				t.Fatalf("valgrind exit=%d stdout=%q want=%q; stderr=%s", code, out, row.want, err)
			}
			for _, kind := range []string{"definitely", "indirectly", "possibly"} {
				if !regexp.MustCompile(kind + ` lost:\s*0 bytes in 0 blocks`).MatchString(err) {
					t.Fatalf("literal fixture has lost blocks:\n%s", err)
				}
			}
			want := fmt.Sprintf(`still reachable:\s*%d bytes in 1 blocks`, row.reachable)
			if !regexp.MustCompile(want).MatchString(err) {
				t.Fatalf("literal startup accounting changed:\n%s", err)
			}
			t.Logf("literal startup accounting (not full reclamation):\n%s", err)
		})
	}
}
