package vm_test

import "testing"

func TestVMBoolComparisonsMatchTruthTable(t *testing.T) {
	source := `fn bool_equal(a: bool, b: bool) -> bool {
    return a == b;
}

fn bool_unequal(a: bool, b: bool) -> bool {
    return a != b;
}

@entrypoint
fn main() -> int {
    if !bool_equal(false, false) {
        return 1;
    }
    if bool_equal(false, true) {
        return 2;
    }
    if bool_equal(true, false) {
        return 3;
    }
    if !bool_equal(true, true) {
        return 4;
    }
    if bool_unequal(false, false) {
        return 5;
    }
    if !bool_unequal(false, true) {
        return 6;
    }
    if !bool_unequal(true, false) {
        return 7;
    }
    if bool_unequal(true, true) {
        return 8;
    }
    return 0;
}
`
	result := runProgramFromSource(t, source, runOptions{})
	if result.exitCode != 0 {
		t.Fatalf("bool comparison truth table failed at case %d:\n%s", result.exitCode, result.stderr)
	}
	if result.stderr != "" {
		t.Fatalf("unexpected stderr: %q", result.stderr)
	}
}
