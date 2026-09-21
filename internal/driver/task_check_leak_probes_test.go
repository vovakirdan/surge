package driver

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/diag"
)

// RV2-DEBT-365, the task check's half. Every program in the leak table hands a task out of a
// frame the task still borrows; running it reads freed storage (`panic VM3301` on the VM, a
// silent wrong answer on native). Each is a ROOT program over the real core, diagnosed through
// the public path, and must be refused by the task check: SEM3139 where the return names the
// borrowing task, SEM3021 where the task leaves by a way the return cannot name. The control
// table is the sound programs those shapes could be mistaken for.

type taskCheckProbe struct {
	name, digest, want, text string
}

// taskCheckErrorCodes diagnoses one frozen program through sema and answers the sorted set of
// its error codes, with the diagnostics themselves for a row that wants to read them.
func taskCheckErrorCodes(t *testing.T, probe taskCheckProbe) (string, []*diag.Diagnostic) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(probe.text))); got != probe.digest {
		t.Fatalf("PRECONDITION: frozen probe changed: %s", got)
	}
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "origin.sg")
	if err := os.WriteFile(path, []byte(probe.text), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true}
	res, err := DiagnoseWithOptions(t.Context(), path, &opts)
	// A program the task check accepts can still come back unfinished: return-origin does not
	// answer `spawn`, `async`, `blocking` or a non-own `.await()` yet (RV2-DEBT-365). That is
	// not this row's question, and it is only ever asked of a program with a clean bag.
	var unfinished *returnOriginUnfinishedError
	if err != nil && !errors.As(err, &unfinished) {
		t.Fatalf("PRECONDITION: the probe did not reach the task check: %v", err)
	}
	if res == nil || res.Bag == nil {
		return "", nil
	}
	seen := map[string]bool{}
	var errs []*diag.Diagnostic
	for _, d := range res.Bag.Items() {
		if d.Severity >= diag.SevError {
			seen[d.Code.ID()] = true
			errs = append(errs, d)
			t.Logf("TASK_CHECK %s %s: %s", probe.name, d.Code.ID(), d.Message)
		}
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return strings.Join(codes, ","), errs
}

func TestTaskCheckRefusesLeakedTaskProbes(t *testing.T) {
	for _, probe := range append(append([]taskCheckProbe{}, taskCheckLeakProbes...), taskCheckReviewLeakProbes...) {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
				t.Fatalf("error codes %q, want %q: the leaked task is not refused by the task check (RV2-DEBT-365)", got, probe.want)
			}
		})
	}
}

func TestTaskCheckKeepsSoundTaskPrograms(t *testing.T) {
	for _, probe := range taskCheckControlProbes {
		t.Run(probe.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, probe); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}

// The refusal names the binding that dies and says how to keep the task inside the frame.
func TestTaskCheckLeakRefusalSaysWhatDies(t *testing.T) {
	_, errs := taskCheckErrorCodes(t, taskCheckLeakProbes[0])
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
	d := errs[0]
	if !strings.Contains(d.Message, "it borrows 'l'") || len(d.Notes) == 0 || !strings.Contains(d.Notes[0].Msg, "the task borrowed 'l' here") {
		t.Fatalf("refusal does not name the borrowed binding: %q notes %+v", d.Message, d.Notes)
	}
	text := taskCheckLeakProbes[0].text
	if got := text[d.Primary.Start:d.Primary.End]; got != "t" {
		t.Fatalf("primary span reads %q, want the returned handle `t`", got)
	}
	if got := text[d.Notes[0].Span.Start:d.Notes[0].Span.End]; got != "&l" {
		t.Fatalf("note span reads %q, want the lending argument `&l`", got)
	}
}
