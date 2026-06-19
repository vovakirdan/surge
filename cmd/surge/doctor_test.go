package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunDoctorReportsReadyWhenRequiredChecksPass(t *testing.T) {
	var out bytes.Buffer
	failed, err := runDoctor(&out, doctorEnv{
		LookupPath: fakeLookPath(map[string]string{
			"clang":  "/usr/bin/clang",
			"ar":     "/usr/bin/ar",
			"llc":    "/usr/bin/llc",
			"ld.lld": "/usr/bin/ld.lld",
		}),
		Version: func() versionInfo { return versionInfo{Version: "0.1.12"} },
		Stdlib:  func(string) string { return "/opt/surge/share/surge" },
	})
	if err != nil {
		t.Fatalf("runDoctor error: %v", err)
	}

	if failed != 0 {
		t.Fatalf("failed = %d, want 0\n%s", failed, out.String())
	}
	if !strings.Contains(out.String(), "[ok] stdlib: /opt/surge/share/surge") {
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
}

func fakeLookPath(paths map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", errors.New("not found")
	}
}
