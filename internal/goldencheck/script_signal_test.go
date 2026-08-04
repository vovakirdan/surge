package goldencheck

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGoldenScriptSignalAfterInstallRestoresLiveCorpus(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
	before, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandWithMoveFault(t, fixture, "signal-after-install")
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("script accepted post-install TERM\n%s", output)
	}
	after, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(before, after); len(changes) != 0 {
		t.Fatalf("post-install TERM changed live corpus: %#v\n%s", changes, output)
	}
	assertNoGoldenStaging(t, fixture.root)
}

func TestGoldenScriptSignalsPreserveLiveCorpus(t *testing.T) {
	tests := []struct {
		name   string
		signal syscall.Signal
	}{
		{name: "INT", signal: syscall.SIGINT},
		{name: "TERM", signal: syscall.SIGTERM},
		{name: "HUP", signal: syscall.SIGHUP},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, "valid.sg")
			goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
			before, err := Scan(goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(fixture.root, "blocked")
			cmd := fixture.command(t, "", nil, nil, nil)
			cmd.Env = append(cmd.Env, "BLOCK_PHASE=diagnostics", "BLOCK_MARKER="+marker)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if startErr := cmd.Start(); startErr != nil {
				t.Fatal(startErr)
			}
			waitForMarker(t, marker, cmd.Process.Pid)
			if signalErr := syscall.Kill(-cmd.Process.Pid, test.signal); signalErr != nil {
				t.Fatalf("send %s: %v", test.name, signalErr)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case waitErr := <-done:
				if waitErr == nil {
					t.Fatalf("script accepted %s\n%s", test.name, output.String())
				}
			case <-time.After(5 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-done
				t.Fatalf("script did not stop after %s", test.name)
			}
			after, err := Scan(goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			if changes := Diff(before, after); len(changes) != 0 {
				t.Fatalf("%s changed live corpus: %#v", test.name, changes)
			}
			assertNoGoldenStaging(t, fixture.root)
		})
	}
}

func waitForMarker(t *testing.T, marker string, processGroup int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(-processGroup, syscall.SIGKILL)
	t.Fatalf("fake generator did not reach blocking phase")
}
