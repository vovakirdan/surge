package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "vm tests: cannot locate repository root")
		os.Exit(1)
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	if err := os.Setenv("SURGE_STDLIB", root); err != nil {
		fmt.Fprintf(os.Stderr, "vm tests: set SURGE_STDLIB: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
