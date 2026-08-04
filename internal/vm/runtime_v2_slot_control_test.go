//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var slotControlModes = []string{"read", "exclusive", "stale", "ordering", "storage", "zst"}

func TestRuntimeV2SlotControlProtocol(t *testing.T) {
	bin := buildRuntimeV2SlotControlHarness(t, "slot-control", nil, false)
	runRuntimeV2SlotControlModes(t, bin, nil, false)
}

func TestRuntimeV2SlotControlAddressAndUndefinedSanitizers(t *testing.T) {
	flags := []string{
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	}
	bin := buildRuntimeV2SlotControlHarness(t, "slot-control-asan-ubsan", flags, true)
	env := slotControlHarnessEnv(
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=1",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	)
	runRuntimeV2SlotControlModes(t, bin, env, false)
}

func TestRuntimeV2SlotControlThreadSanitizer(t *testing.T) {
	flags := []string{"-fsanitize=thread", "-fno-omit-frame-pointer", "-O1", "-g"}
	bin := buildRuntimeV2SlotControlHarness(t, "slot-control-tsan", flags, true)
	runRuntimeV2SlotControlModes(t, bin, slotControlHarnessEnv("TSAN_OPTIONS=halt_on_error=1"), true)
}

func TestRuntimeV2SlotControlIsOwnerPrivateAndCallbackFree(t *testing.T) {
	root := repoRoot(t)
	header := readSlotControlFile(t, filepath.Join(root, "runtime", "native", "rt_slot_control.h"))
	if count := strings.Count(header, "rt_slot_header slot;"); count != 1 {
		t.Fatalf("SlotControl must contain exactly one authoritative slot header, got %d", count)
	}
	if count := strings.Count(header, "const rt_value_ops* operations;"); count != 2 {
		t.Fatalf("control and token must bind the concrete B2 descriptor, got %d declarations", count)
	}
	if strings.Contains(header, "const void* operations_identity") {
		t.Fatal("SlotControl must not erase the concrete B2 descriptor identity")
	}

	for _, name := range []string{
		"rt_slot_control.h",
		"rt_slot_control_internal.h",
		"rt_slot_control.c",
		"rt_slot_claim.c",
		"rt_slot_exclusive.c",
	} {
		source := readSlotControlFile(t, filepath.Join(root, "runtime", "native", name))
		for _, forbidden := range []string{"pthread_", "->move_init", "->copy_init", "->clone_init", "->drop_in_place", "->trace", "->cross_move_init"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s must not lock or invoke ValueOps callbacks; found %q", name, forbidden)
			}
		}
	}
}

func TestRuntimeV2SlotControlRequiredSanitizersFailClosed(t *testing.T) {
	t.Setenv("SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS", "1")
	if slotControlSanitizerSkipAllowed() {
		t.Fatal("the mandatory make gate must not permit sanitizer skips")
	}
	t.Setenv("SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS", "0")
	if !slotControlSanitizerSkipAllowed() {
		t.Fatal("an explicit developer run may skip a sanitizer unavailable on its host")
	}

	root := repoRoot(t)
	makefile := readSlotControlFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS=1 $(GO) test") {
		t.Fatal("runtime-v2-slot-control-check must require sanitizer support")
	}
	source := readSlotControlFile(t, filepath.Join(root, "internal", "vm", "runtime_v2_slot_control_test.go"))
	skipGuard := "&& slotControl" + "SanitizerSkipAllowed()"
	if count := strings.Count(source, skipGuard); count != 2 {
		t.Fatalf("both sanitizer skip sites must be guarded by mandatory mode, got %d", count)
	}
}

func buildRuntimeV2SlotControlHarness(
	t *testing.T,
	name string,
	extraFlags []string,
	optionalSanitizer bool,
) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 SlotControl acceptance: %v", err)
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), name)
	strictFlags := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Wpedantic",
		"-Werror",
		"-Wshadow",
		"-Wconversion",
		"-Wsign-conversion",
		"-Wcast-qual",
		"-Wcast-align",
		"-Wstrict-prototypes",
		"-Wmissing-prototypes",
		"-Wold-style-definition",
		"-Wformat=2",
		"-Wundef",
		"-Wdouble-promotion",
		"-fno-common",
		"-pthread",
	}
	args := append(strictFlags, extraFlags...)
	args = append(args,
		"-I"+filepath.Join(root, "runtime", "native"),
		"-I"+filepath.Join(root, "internal", "vm", "testdata"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_harness.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_protocol_cases.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_order_cases.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_edge_cases.c"),
		filepath.Join(root, "runtime", "native", "rt_slot_control.c"),
		filepath.Join(root, "runtime", "native", "rt_slot_claim.c"),
		filepath.Join(root, "runtime", "native", "rt_slot_exclusive.c"),
		"-o", bin,
	)
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil {
		return bin
	}
	if optionalSanitizer && slotControlSanitizerSkipAllowed() &&
		slotControlSanitizerUnavailable(string(output)) {
		t.Skipf("requested sanitizer is unavailable:\n%s", output)
	}
	t.Fatalf("build SlotControl harness: %v\n%s", err, output)
	return ""
}

func runRuntimeV2SlotControlModes(t *testing.T, bin string, env []string, allowTSanMappingSkip bool) {
	t.Helper()
	for _, mode := range slotControlModes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(bin, mode)
			if env != nil {
				cmd.Env = env
			}
			output, err := cmd.CombinedOutput()
			if err == nil {
				return
			}
			if allowTSanMappingSkip && slotControlSanitizerSkipAllowed() &&
				strings.Contains(string(output), "ThreadSanitizer: unexpected memory mapping") {
				t.Skipf("host cannot start the TSan runtime:\n%s", output)
			}
			t.Fatalf("SlotControl mode %q failed: %v\n%s", mode, err, output)
		})
	}
}

func slotControlSanitizerUnavailable(output string) bool {
	return strings.Contains(output, "unsupported option") ||
		strings.Contains(output, "unsupported argument") ||
		strings.Contains(output, "cannot find") && strings.Contains(output, "libclang_rt")
}

func slotControlSanitizerSkipAllowed() bool {
	return os.Getenv("SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS") != "1"
}

func slotControlHarnessEnv(values ...string) []string {
	env := os.Environ()
	for _, value := range values {
		key := strings.SplitN(value, "=", 2)[0] + "="
		filtered := env[:0]
		for _, existing := range env {
			if !strings.HasPrefix(existing, key) {
				filtered = append(filtered, existing)
			}
		}
		env = append(filtered, value)
	}
	return env
}

func readSlotControlFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
