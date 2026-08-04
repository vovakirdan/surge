package abimanifest

import (
	"errors"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestRuntimeV2ABIManifestMakeTargetIsExplicitAndPhony(t *testing.T) {
	command := exec.Command("make", "-qp")
	command.Dir = testRepoRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("make -qp: %v: %s", err, output)
		}
	}
	database := string(output)
	if !regexp.MustCompile(`(?m)^runtime-v2-abi-manifest-check:`).MatchString(database) {
		t.Fatal("make -qp does not expose an explicit runtime-v2-abi-manifest-check target")
	}
	phony := false
	for _, line := range strings.Split(database, "\n") {
		if strings.HasPrefix(line, ".PHONY:") && slices.Contains(strings.Fields(line), "runtime-v2-abi-manifest-check") {
			phony = true
			break
		}
	}
	if !phony {
		t.Fatal("make -qp does not mark runtime-v2-abi-manifest-check phony")
	}
	for _, required := range []string{
		"command -v clang",
		"command -v clang++",
		"command -v llvm-nm",
		"command -v nm",
		"SURGE_REQUIRE_TYPED_CARRIER_ABI_TOOLS=1",
	} {
		if !strings.Contains(database, required) {
			t.Fatalf("make -qp typed-carrier gate is not fail-closed; missing %q", required)
		}
	}
}
