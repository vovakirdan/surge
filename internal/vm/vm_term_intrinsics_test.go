package vm_test

import (
	"testing"

	"surge/internal/vm"
)

func TestVMTermReadEventQueue(t *testing.T) {
	requireVMBackend(t)
	sourceCode := `import stdlib/term;

@entrypoint
fn main() -> int {
    let mut out: byte[] = [];
    let mut i: int = 0;
    while i < 3 {
        let ev = term_read_event();
        let code: uint8 = compare &ev {
            Key(_) => 75:uint8;
            Resize(_, _) => 82:uint8;
            Eof() => 69:uint8;
        };
        out.push(code);
        i = i + 1;
    }
    term_write(out);
    term_flush();
    return 0;
}
`
	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	rt.EnqueueTermEvents(
		vm.TermEventData{
			Kind: vm.TermEventKey,
			Key: vm.TermKeyEventData{
				Key:  vm.TermKeyData{Kind: vm.TermKeyChar, Char: uint32('a')},
				Mods: 0,
			},
		},
		vm.TermEventData{
			Kind: vm.TermEventResize,
			Cols: 120,
			Rows: 30,
		},
		vm.TermEventData{
			Kind: vm.TermEventEof,
		},
	)
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)
	if vmErr != nil {
		t.Fatalf("unexpected VM error: %s", vmErr.FormatWithFiles(files))
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if got := string(rt.TermOutput()); got != "KRE" {
		t.Fatalf("unexpected term output: %q", got)
	}
}

func TestVMTermCallsLog(t *testing.T) {
	requireVMBackend(t)
	sourceCode := `import stdlib/term;

@entrypoint
fn main() -> int {
    enter();
    write_str("hi");
    term_flush();
    exit();
    return 0;
}
`
	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)
	if vmErr != nil {
		t.Fatalf("unexpected VM error: %s", vmErr.FormatWithFiles(files))
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	calls := rt.TermCalls()
	if len(calls) != 8 {
		t.Fatalf("expected 8 term calls, got %d", len(calls))
	}
	if calls[0].Name != "term_enter_alt_screen" {
		t.Fatalf("unexpected call[0]: %s", calls[0].Name)
	}
	if calls[1].Name != "term_hide_cursor" {
		t.Fatalf("unexpected call[1]: %s", calls[1].Name)
	}
	if calls[2].Name != "term_set_raw_mode" || !calls[2].Enabled {
		t.Fatalf("unexpected call[2]: %s enabled=%v", calls[2].Name, calls[2].Enabled)
	}
	if calls[3].Name != "term_write" || string(calls[3].Bytes) != "hi" {
		t.Fatalf("unexpected call[3]: %s bytes=%q", calls[3].Name, string(calls[3].Bytes))
	}
	if calls[4].Name != "term_flush" {
		t.Fatalf("unexpected call[4]: %s", calls[4].Name)
	}
	if calls[5].Name != "term_set_raw_mode" || calls[5].Enabled {
		t.Fatalf("unexpected call[5]: %s enabled=%v", calls[5].Name, calls[5].Enabled)
	}
	if calls[6].Name != "term_show_cursor" {
		t.Fatalf("unexpected call[6]: %s", calls[6].Name)
	}
	if calls[7].Name != "term_exit_alt_screen" {
		t.Fatalf("unexpected call[7]: %s", calls[7].Name)
	}
}

func TestVMTermSizeOverride(t *testing.T) {
	requireVMBackend(t)
	sourceCode := `import stdlib/term;

@entrypoint
fn main() -> int {
    let (cols, rows) = term_size();
    if cols == 120 && rows == 30 {
        return 0;
    }
    return 1;
}
`
	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	rt.SetTermSize(120, 30)
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)
	if vmErr != nil {
		t.Fatalf("unexpected VM error: %s", vmErr.FormatWithFiles(files))
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}
