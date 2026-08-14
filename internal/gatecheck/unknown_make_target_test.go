package gatecheck

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// A GATE THAT DOES NOT EXIST MUST NOT REPORT SUCCESS.
//
// The Makefile ends with a catch-all pattern rule whose recipe does nothing. It
// is there so `make run prog.sg --backend llvm` can pass its trailing words to
// the program instead of make trying to BUILD each of them. Unconditional, that
// rule matched every unknown goal, so `make this-does-not-exist` exited 0 with no
// output — and so did any typo in a gate name.
//
// The cost was not hypothetical. `runtime-v2-carrier-sanitizer-check` was named
// mandatory in three documents while having no target at all, and everyone who
// ran it saw exit 0 (RV2-DEBT-199, RV2-DEBT-200). Every gate invocation in this
// repository, in CI or by hand, was unable to tell "green" from "not a target".
//
// The companion test in documented_make_targets_test.go asserts that a target a
// document PROMISES exists. This one asserts the other half: that make can still
// say NO. Neither is worth much without the other — a promise checker is useless
// if the thing it checks against answers yes to everything.
//
// `make -n` is used throughout: it resolves the goal and prints what it would do
// without running it, which is the question here and costs nothing.
func TestMakeRefusesAnUnknownTarget(t *testing.T) {
	root := repoRoot(t)

	runMake := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command("make", append([]string{"-n"}, args...)...) // #nosec G204 -- fixed executable; arguments are repository-owned target names.
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			return string(out), 0
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(out), exitErr.ExitCode()
		}
		t.Fatalf("gatecheck: run make %v: %v", args, err)
		return "", -1
	}

	t.Run("unknown goal fails loudly", func(t *testing.T) {
		out, code := runMake("this-target-does-not-exist-at-all")
		if code == 0 {
			t.Fatalf("make accepted a target that does not exist and exited 0; output:\n%s", out)
		}
		// The message matters as much as the code: a reader who mistyped a gate
		// name has to be told which name make could not find.
		if !strings.Contains(out, "this-target-does-not-exist-at-all") {
			t.Fatalf("make failed without naming the missing target; output:\n%s", out)
		}
	})

	t.Run("a typo in a gate name fails", func(t *testing.T) {
		// One character away from a real gate, which is the shape that actually
		// happens and the shape a catch-all is most dangerous for.
		if _, code := runMake("runtime-v2-heap-chek"); code == 0 {
			t.Fatal("make accepted a typo of runtime-v2-heap-check and exited 0")
		}
	})

	t.Run("a real gate still resolves", func(t *testing.T) {
		// Without this the test above would pass on a Makefile broken outright.
		if out, code := runMake("runtime-v2-heap-check"); code != 0 {
			t.Fatalf("make could not resolve a real gate (exit=%d); output:\n%s", code, out)
		}
	})

	t.Run("run still swallows its trailing words", func(t *testing.T) {
		// The reason the catch-all exists. `run` is the only goal that reads
		// MAKECMDGOALS, so narrowing the rule to it is what keeps this working
		// while everything else gets its errors back.
		//
		// Word arguments only: make parses a leading `--flag` as one of ITS
		// options and prints its own help, which has always been true and is not
		// what this test is about.
		if out, code := runMake("run", "some-program.sg", "extra-word"); code != 0 {
			t.Fatalf("make run stopped accepting trailing arguments (exit=%d); output:\n%s", code, out)
		}
	})
}
