package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDoctorReportsReadyWhenRequiredChecksPass(t *testing.T) {
	stdlibRoot := makeDoctorStdlibRoot(t)
	var out bytes.Buffer
	failed, err := runDoctor(&out, doctorEnv{
		LookupPath: fakeLookPath(map[string]string{
			"clang":  "/usr/bin/clang",
			"ar":     "/usr/bin/ar",
			"llc":    "/usr/bin/llc",
			"ld.lld": "/usr/bin/ld.lld",
		}),
		Version: func() versionInfo { return versionInfo{Version: "0.1.12"} },
		Stdlib:  func(string) string { return stdlibRoot },
	})
	if err != nil {
		t.Fatalf("runDoctor error: %v", err)
	}

	if failed != 0 {
		t.Fatalf("failed = %d, want 0\n%s", failed, out.String())
	}
	if !strings.Contains(out.String(), "[ok] stdlib: "+stdlibRoot) {
		t.Fatalf("missing stdlib ok line:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "ready") {
		t.Fatalf("missing ready line:\n%s", out.String())
	}
}

func TestRunDoctorFailsOnlyRequiredChecks(t *testing.T) {
	var out bytes.Buffer
	failed, err := runDoctor(&out, doctorEnv{
		LookupPath: fakeLookPath(map[string]string{
			"clang": "/usr/bin/clang",
			"ar":    "/usr/bin/ar",
		}),
		Version: func() versionInfo { return versionInfo{Version: "0.1.12"} },
		Stdlib:  func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("runDoctor error: %v", err)
	}

	if failed != 1 {
		t.Fatalf("failed = %d, want 1\n%s", failed, out.String())
	}
	if !strings.Contains(out.String(), "[error] stdlib: not found") {
		t.Fatalf("missing stdlib error:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "[warn] llc: not found") {
		t.Fatalf("missing optional llc warning:\n%s", out.String())
	}
	if strings.Contains(out.String(), "ready") {
		t.Fatalf("unexpected ready line on failed checks:\n%s", out.String())
	}
}

func TestRunDoctorRejectsIncompleteStdlibRoot(t *testing.T) {
	root := makeDoctorCoreOnlyRoot(t)
	var out bytes.Buffer
	failed, err := runDoctor(&out, doctorEnv{
		LookupPath: fakeLookPath(map[string]string{
			"clang":  "/usr/bin/clang",
			"ar":     "/usr/bin/ar",
			"llc":    "/usr/bin/llc",
			"ld.lld": "/usr/bin/ld.lld",
		}),
		Version: func() versionInfo { return versionInfo{Version: "0.1.12"} },
		Stdlib:  func(string) string { return root },
	})
	if err != nil {
		t.Fatalf("runDoctor error: %v", err)
	}

	if failed != 1 {
		t.Fatalf("failed = %d, want 1\n%s", failed, out.String())
	}
	if !strings.Contains(out.String(), "[error] stdlib: "+root+" (missing stdlib directory; reinstall Surge)") {
		t.Fatalf("missing incomplete stdlib error:\n%s", out.String())
	}
	if strings.Contains(out.String(), "ready") {
		t.Fatalf("unexpected ready line on incomplete stdlib:\n%s", out.String())
	}
}

func makeDoctorStdlibRoot(t *testing.T) string {
	t.Helper()
	root := makeDoctorCoreOnlyRoot(t)
	if err := os.Mkdir(filepath.Join(root, "stdlib"), 0o755); err != nil {
		t.Fatalf("create stdlib dir: %v", err)
	}
	return root
}

func makeDoctorCoreOnlyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	coreDir := filepath.Join(root, "core")
	if err := os.Mkdir(coreDir, 0o755); err != nil {
		t.Fatalf("create core dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coreDir, "intrinsics.sg"), []byte("// test\n"), 0o644); err != nil {
		t.Fatalf("create intrinsics: %v", err)
	}
	return root
}

func fakeLookPath(paths map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", errors.New("not found")
	}
}
