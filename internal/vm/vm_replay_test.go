package vm_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/vm"
)

func TestVMRecordReplayArgv(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_replay", "vm_record_replay_argv.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)

	var recBuf bytes.Buffer
	rec := vm.NewRecorder(&recBuf)
	rt := vm.NewRecordingRuntime(vm.NewTestRuntime([]string{"7"}, ""), rec)

	vm1 := vm.New(mirMod, rt, files, typesInterner, nil)
	vm1.Recorder = rec
	if vmErr := vm1.Run(); vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if vm1.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", vm1.ExitCode)
	}

	logPath := filepath.Join(t.TempDir(), "run.ndjson")
	if err := os.WriteFile(logPath, recBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	rp := vm.NewReplayerFromBytes(logBytes)

	vm2 := vm.New(mirMod, vm.NewTestRuntime([]string{"999"}, ""), files, typesInterner, nil)
	vm2.Replayer = rp
	vm2.RT = vm.NewReplayRuntime(vm2, rp)
	if vmErr := vm2.Run(); vmErr != nil {
		t.Fatalf("unexpected replay error: %v", vmErr.Error())
	}
	if vm2.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", vm2.ExitCode)
	}
	if rp.Remaining() != 0 {
		t.Fatalf("expected replay log fully consumed, remaining=%d", rp.Remaining())
	}
}

func TestVMRecordReplayStdin(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_replay", "vm_record_replay_stdin.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)

	var recBuf bytes.Buffer
	rec := vm.NewRecorder(&recBuf)
	rt := vm.NewRecordingRuntime(vm.NewTestRuntime(nil, "9"), rec)

	vm1 := vm.New(mirMod, rt, files, typesInterner, nil)
	vm1.Recorder = rec
	if vmErr := vm1.Run(); vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if vm1.ExitCode != 9 {
		t.Fatalf("expected exit code 9, got %d", vm1.ExitCode)
	}

	rp := vm.NewReplayerFromBytes(recBuf.Bytes())
	vm2 := vm.New(mirMod, vm.NewTestRuntime(nil, "999"), files, typesInterner, nil)
	vm2.Replayer = rp
	vm2.RT = vm.NewReplayRuntime(vm2, rp)
	if vmErr := vm2.Run(); vmErr != nil {
		t.Fatalf("unexpected replay error: %v", vmErr.Error())
	}
	if vm2.ExitCode != 9 {
		t.Fatalf("expected exit code 9, got %d", vm2.ExitCode)
	}
	if rp.Remaining() != 0 {
		t.Fatalf("expected replay log fully consumed, remaining=%d", rp.Remaining())
	}
}

func TestVMRecordReplayPanicOverflow(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_replay", "vm_record_replay_panic_overflow.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)

	var recBuf bytes.Buffer
	rec := vm.NewRecorder(&recBuf)
	rt := vm.NewRecordingRuntime(vm.NewTestRuntime(nil, ""), rec)

	vm1 := vm.New(mirMod, rt, files, typesInterner, nil)
	vm1.Recorder = rec
	vmErr1 := vm1.Run()
	if vmErr1 == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr1.Code != vm.PanicIntOverflow {
		t.Fatalf("expected %v, got %v", vm.PanicIntOverflow, vmErr1.Code)
	}

	rp := vm.NewReplayerFromBytes(recBuf.Bytes())
	vm2 := vm.New(mirMod, vm.NewTestRuntime(nil, ""), files, typesInterner, nil)
	vm2.Replayer = rp
	vm2.RT = vm.NewReplayRuntime(vm2, rp)
	vmErr2 := vm2.Run()
	if vmErr2 == nil {
		t.Fatal("expected replay panic, got nil")
	}
	if vmErr2.Code != vm.PanicIntOverflow {
		t.Fatalf("expected %v, got %v", vm.PanicIntOverflow, vmErr2.Code)
	}

	if vmErr2.FormatWithFiles(files) != vmErr1.FormatWithFiles(files) {
		t.Fatalf("replay panic mismatch:\nwant:\n%s\n\ngot:\n%s", vmErr1.FormatWithFiles(files), vmErr2.FormatWithFiles(files))
	}
	if rp.Remaining() != 0 {
		t.Fatalf("expected replay log fully consumed, remaining=%d", rp.Remaining())
	}
}
