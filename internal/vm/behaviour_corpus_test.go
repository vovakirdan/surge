package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The behavioural corpus is the set of recorded programs under
// testdata/golden/vm_*: a source file, the output it must produce, and the exit
// code it must exit with. Running them is the only thing in the repository that
// can observe a wrong runtime answer, so the comparison lives in one place
// rather than in a copy per fixture directory — thirteen copies of it is how a
// rule ends up applied thirteen slightly different ways.

// behaviouralBackends names the backends the corpus is checked on.
//
// The VM lane always runs. It costs about a quarter-second per fixture, which
// belongs in the pre-commit hook. The native lane costs about six seconds per
// fixture, because clang compiles and links the runtime for each one, so it is
// asked for deliberately — at an important step and in CI — rather than paid on
// every commit.
//
// Asking for a lane and not getting it is the one failure this must not have:
// when the native lane is requested and the toolchain is missing, it FAILS. A
// lane that quietly skips is precisely the shape of gate this corpus exists to
// replace.
func behaviouralBackends(t *testing.T) []string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("SURGE_BEHAVIOUR_BACKENDS"))
	if raw == "" {
		return []string{"vm"}
	}
	var backends []string
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		switch name {
		case "":
		case "vm":
			backends = append(backends, name)
		case "llvm":
			for _, tool := range []string{"clang", "ar"} {
				if _, err := exec.LookPath(tool); err != nil {
					t.Fatalf("SURGE_BEHAVIOUR_BACKENDS asked for the llvm lane but %s is not installed: %v", tool, err)
				}
			}
			backends = append(backends, name)
		default:
			t.Fatalf("SURGE_BEHAVIOUR_BACKENDS names an unknown backend %q", name)
		}
	}
	if len(backends) == 0 {
		t.Fatal("SURGE_BEHAVIOUR_BACKENDS is set but names no backend")
	}
	return backends
}

// A directory used to be able to sweep fixtures out of its own run by file-name
// suffix. Nothing does any more, and the option is gone rather than left unused:
// every fixture that carried a recorded answer under such a sweep turned out to
// agree on both backends, so the sweeps were standing in for nothing while
// silently taking each new fixture with them. A fixture that genuinely must not
// run says so for itself, with a leading underscore, which is per-fixture and
// visible in the directory listing. Reintroducing a sweep means teaching
// internal/panicgate about it, which refuses a recorded answer nothing runs.

// runBehaviourCorpus checks every recorded program in dir on every backend under
// test, comparing real output against the recorded .out and the real exit code
// against the recorded .code.
func runBehaviourCorpus(t *testing.T, dir string) {
	t.Helper()
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	backends := behaviouralBackends(t)

	absDir := filepath.Join(root, "testdata", "golden", dir)
	entries, err := os.ReadDir(absDir)
	if err != nil {
		t.Fatalf("read %s dir: %v", dir, err)
	}

	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sg") {
			continue
		}
		// A leading underscore marks a fixture the corpus records but does not
		// run — the convention the rest of testdata/golden already uses.
		if strings.HasPrefix(ent.Name(), "_") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".sg")
		for _, backend := range backends {
			if !fixtureRunsOn(t, absDir, name, backend) {
				continue
			}
			t.Run(caseName(name, backend, backends), func(t *testing.T) {
				runBehaviourCase(t, root, surge, dir, absDir, name, backend)
			})
		}
	}
}

// caseName keeps the single-backend subtest names exactly as they were, so a
// recorded failure list from the VM lane still reads the same, and only names
// the backend once there is more than one to tell apart.
func caseName(name, backend string, backends []string) string {
	if len(backends) == 1 {
		return name
	}
	return name + "/" + backend
}

func runBehaviourCase(t *testing.T, root, surge, dir, absDir, name, backend string) {
	t.Helper()
	sgRel := filepath.ToSlash(filepath.Join("testdata", "golden", dir, name+".sg"))

	wantOutBytes, err := os.ReadFile(filepath.Join(absDir, name+".out"))
	if err != nil {
		t.Fatalf("read %s.out: %v", name, err)
	}
	wantOut := string(wantOutBytes)

	wantCode := 0
	if b, err := os.ReadFile(filepath.Join(absDir, name+".code")); err == nil {
		n, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if parseErr != nil {
			t.Fatalf("parse %s.code: %v", name, parseErr)
		}
		wantCode = n
	}

	// A .flags sidecar carries the switches this program needs, one per line —
	// a fuzzed scheduler and its seed, for instance.
	cmdArgs := []string{"run", "--backend=" + backend}
	if b, err := os.ReadFile(filepath.Join(absDir, name+".flags")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				cmdArgs = append(cmdArgs, line)
			}
		}
	}
	cmdArgs = append(cmdArgs, sgRel)

	// The native lane runs single-worker, and that is what makes this a PARITY
	// comparison rather than a scheduler one. The VM is single-worker by
	// construction, so holding the native side to one worker asks the two
	// backends the same question: given this program, what answer. Left at the
	// default the native lane runs on one worker per core and a recorded
	// interleaving stops being a fact about the program — measured on this
	// corpus, eight async fixtures match byte-for-byte at one worker and vary
	// run to run at thirty-two, which made the lane's failure list churn
	// without anything changing.
	//
	// It does not follow that multi-worker behaviour goes unchecked: it is
	// checked by its own lane, which asserts what multiple workers actually
	// promise. See TestBehaviourCorpusMT.
	env := envWithStdlib(root)
	if backend == "llvm" {
		env = envForParity(root)
	}
	stdout, stderr, code := runSurgeEnv(t, root, surge, env, cmdArgs...)

	if code != wantCode {
		t.Fatalf("[%s] exit code: want %d, got %d\nstdout:\n%s\nstderr:\n%s", backend, wantCode, code, stdout, stderr)
	}
	// A program that exits non-zero records what it printed on the way out, which
	// is the panic on stderr; one that exits zero records its stdout.
	got, other, otherName := stdout, stderr, "stderr"
	if wantCode != 0 {
		got, other, otherName = stderr, stdout, "stdout"
	}
	if other != "" {
		t.Fatalf("[%s] unexpected %s:\n%s", backend, otherName, other)
	}
	if got != wantOut {
		t.Fatalf("[%s] output mismatch:\nwant:\n%s\n\ngot:\n%s", backend, wantOut, got)
	}
}

// fixtureRunsOn reports whether this fixture is checked on this backend.
//
// A fixture is checked everywhere unless a .backends sidecar says otherwise.
// Some programs are one-sided by construction rather than by defect: one asks
// the VM how many heap objects it allocated, another passes a VM-only scheduler
// flag. Naming the backends in a file the reader can open is the point — an
// exclusion that lives in test code is an exclusion nobody finds.
func fixtureRunsOn(t *testing.T, absDir, name, backend string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(absDir, name+".backends"))
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == backend {
			return true
		}
	}
	return false
}
