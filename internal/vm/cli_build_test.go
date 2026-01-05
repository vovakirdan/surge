package vm_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	parsedLineRE   = regexp.MustCompile(`(?m)^parsed [0-9]+(\.[0-9])? ms$`)
	diagnoseLineRE = regexp.MustCompile(`(?m)^diagnose [0-9]+(\.[0-9])? ms$`)
	builtLineRE    = regexp.MustCompile(`(?m)^built [0-9]+(\.[0-9])? ms$`)
)

func TestBuildUIOffTimings(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	srcRel := filepath.ToSlash(filepath.Join("test.sg"))

	cmd := exec.Command(surge, "build", "--backend=vm", "--ui=off", srcRel)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SURGE_STDLIB="+root)
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !parsedLineRE.MatchString(stdout) {
		t.Fatalf("missing parsed timing line in stdout:\n%s", stdout)
	}
	if !diagnoseLineRE.MatchString(stdout) {
		t.Fatalf("missing diagnose timing line in stdout:\n%s", stdout)
	}
	if !builtLineRE.MatchString(stdout) {
		t.Fatalf("missing built timing line in stdout:\n%s", stdout)
	}
}

func TestBuildDefaultBackendLLVM(t *testing.T) {
	root := repoRoot(t)
	ensureLLVMToolchain(t)
	surge := buildSurgeBinary(t, root)
	srcRel := filepath.ToSlash(filepath.Join("test.sg"))

	cmd := exec.Command(surge, "build", "--ui=off", srcRel)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SURGE_STDLIB="+root)
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	outputPath := parseBuiltPath(stdout)
	if outputPath == "" {
		t.Fatalf("missing built path in stdout:\n%s", stdout)
	}
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(root, outputPath)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if bytes.HasPrefix(data, []byte("#!/bin/sh")) {
		t.Fatalf("expected LLVM binary, got VM wrapper script at %s", outputPath)
	}
}

func parseBuiltPath(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "built ") && !strings.HasSuffix(line, " ms") {
			return strings.TrimSpace(strings.TrimPrefix(line, "built "))
		}
	}
	return ""
}
