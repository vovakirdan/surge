package vm_test

import (
	"strings"
	"testing"
)

// The program the P1z diagnostic's help text tells the user to write: bind the
// temporary, then pass the binding. It is the row that survives the fix -- the
// defect program itself stops compiling, so its VM3301 lives in the packet as Stage
// A evidence instead of here, where it would silently stop testing anything.
const statementTemporaryRepairSource = `fn id_ref(s: &string) -> &string { return s; }

fn measured(tail: string) -> int {
    let tmp: string = "a" + tail;
    let r: &string = id_ref(tmp);
    return len(r) to int;
}

@entrypoint
fn main() -> int {
    if measured("bcdef") != 6 { return 1; }
    print("statement-temporary-ok");
    return 0;
}
`

func TestStatementTemporaryRepairRunsClean(t *testing.T) {
	res := runProgramFromSource(t, statementTemporaryRepairSource, runOptions{captureStdout: true})
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stdout=%q stderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}
	if res.stdout != "statement-temporary-ok\n" {
		t.Fatalf("stdout=%q want %q", res.stdout, "statement-temporary-ok\n")
	}
	if strings.Contains(res.stderr, "VM3301") {
		t.Fatalf("VM3301 in stderr; the repair the help text recommends is not sound:\n%s", res.stderr)
	}
}
