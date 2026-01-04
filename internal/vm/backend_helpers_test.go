package vm

import (
	"os"
	"strings"
	"testing"
)

func requireVMBackend(t *testing.T) {
	t.Helper()
	backend := strings.TrimSpace(os.Getenv("SURGE_BACKEND"))
	if backend == "llvm" {
		t.Skip("skipping VM runtime tests for SURGE_BACKEND=llvm")
	}
}
