//go:build runtime_v2_pending

package vm_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

var slotControlModes = []string{
	"read", "exclusive", "stale", "ordering", "storage", "zst", "descriptor", "identity", "copy",
	"fifo", "park",
}

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
		if strings.Contains(source, "pthread_") {
			t.Fatalf("%s must not acquire a pthread lock", name)
		}
		// Scanning for `x->callback(` alone would miss `(*x->callback)(args)`
		// and `fn f = x->callback; f(args)`, which are ordinary C and are how a
		// callback gets invoked without ever spelling a call at the slot.
		if reads := slotReadsBeyondNullChecks(source, allValueOpsSlots); len(reads) != 0 {
			t.Fatalf("%s must not read ValueOps callback slots except to compare them "+
				"against NULL; found %q", name, reads)
		}
	}
}

// allValueOpsSlots is every rt_value_ops callback field, as the owner-private
// slot control may never invoke any of them.
const allValueOpsSlots = `move_init|copy_init|clone_init|drop_in_place|trace|plan_cross|` +
	`cross_move_init|cross_clone_init`

// slotReadsBeyondNullChecks returns every read of one of `slots` in `source`
// that is not a null comparison.
//
// It is stated as "what is allowed", not "what a call looks like". The only
// legitimate reason for anything outside the runtime's own copy helper to touch
// a callback slot is to check that the descriptor filled it, so the two null
// comparisons and the generated ABI's field-size assertion are blanked out and
// ANY remaining mention of a slot behind a `.` or `->` is reported: a call, a
// dereferenced call, a hoist into a local, an address taken, a macro that
// expands to any of those. The one shape that still escapes is dispatch that
// never spells the field name, which is why this is a discipline scan and not a
// proof.
func slotReadsBeyondNullChecks(source, slots string) []string {
	code := cCodeOnly(source)
	nullCheck := regexp.MustCompile(`(\.|->)\s*(` + slots + `)\s*(==|!=)\s*NULL`)
	// The generated ABI checks assert the field's size through a null-pointer
	// cast; that is a compile-time type query, not a read.
	abiSizeAssertion := regexp.MustCompile(`sizeof\(\(\(rt_value_ops\*\)0\)->(` + slots + `)\)`)
	scrubbed := abiSizeAssertion.ReplaceAllString(nullCheck.ReplaceAllString(code, ""), "")
	return regexp.MustCompile(`(\.|->)\s*(`+slots+`)\b`).FindAllString(scrubbed, -1)
}

// cCodeOnly strips C comments and string or character literals, so a discipline
// scan reads code. Without it a comment that names the forbidden form, or a
// _Static_assert message that quotes the field, is reported as a violation — and
// the scan's own failure text becomes the thing that fails it.
func cCodeOnly(source string) string {
	var code strings.Builder
	for index := 0; index < len(source); {
		rest := source[index:]
		switch {
		case strings.HasPrefix(rest, "//"):
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				return code.String()
			}
			index += end
		case strings.HasPrefix(rest, "/*"):
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				return code.String()
			}
			index += 2 + end + 2
			code.WriteByte(' ')
		case rest[0] == '"' || rest[0] == '\'':
			quote := rest[0]
			index++
			for index < len(source) && source[index] != quote {
				if source[index] == '\\' {
					index++
				}
				index++
			}
			index++
			code.WriteByte(' ')
		default:
			code.WriteByte(rest[0])
			index++
		}
	}
	return code.String()
}

// TestRuntimeV2SlotControlCopyInitTrapIsNamedAndUndispatched covers the half of
// RT_VALUE_FLAG_COPY the harness modes cannot: the value that fills the slot is a
// real declared runtime symbol, nothing in the runtime dispatches it, and
// reaching it through the raw callback pointer is loud rather than a silent
// zero-byte copy.
//
// The frozen rt_value_copy_init_fn signature carries no width, so no callback can
// copy on its own; rt_value_copy_init performs the copy while still holding the
// descriptor whose rt_value_layout.size is the width, and branches away from the
// trap. That is a discipline, so it is scanned for rather than trusted.
func TestRuntimeV2SlotControlCopyInitTrapIsNamedAndUndispatched(t *testing.T) {
	root := repoRoot(t)

	header := readSlotControlFile(t, filepath.Join(root, "runtime", "native", "rt_typed_carrier_abi.generated.h"))
	if !strings.Contains(header, "void rt_value_copy_init_unbound_trap(void* dst, const void* src);") {
		t.Fatal("the generated ABI header no longer declares rt_value_copy_init_unbound_trap; " +
			"a descriptor that sets RT_VALUE_FLAG_COPY would have nothing to bind")
	}
	checks := readSlotControlFile(t, filepath.Join(root, "runtime", "native", "rt_typed_carrier_abi_checks.generated.h"))
	if !strings.Contains(checks, "\"rt_value_copy_init_unbound_trap signature drift\"") {
		t.Fatal("the generated ABI checks no longer pin rt_value_copy_init_unbound_trap's signature")
	}

	// Only the runtime's own copy helper may read the slot at all. Everywhere
	// else the descriptor is in hand and rt_value_copy_init is the call.
	entries, err := os.ReadDir(filepath.Join(root, "runtime", "native"))
	if err != nil {
		t.Fatalf("read runtime/native: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "rt_value_ops.c" ||
			(!strings.HasSuffix(name, ".c") && !strings.HasSuffix(name, ".h")) {
			continue
		}
		source := readSlotControlFile(t, filepath.Join(root, "runtime", "native", name))
		if reads := slotReadsBeyondNullChecks(source, "copy_init"); len(reads) != 0 {
			t.Errorf("%s reads %q; the trap has no width and is not a copy, so copies go "+
				"through rt_value_copy_init(operations, dst, src)", name, reads)
		}
	}

	// The one exception is rt_value_ops.c, and it is pinned by TOTAL READS of
	// the slot rather than by one call shape. Counting `->copy_init(` alone
	// misses `(*operations->copy_init)(dst, src)`, which dispatches the trap
	// through a plain function-pointer deref and would abort in production for
	// every trap-bound Copy value; that evasion was demonstrated against the
	// call-shape count this replaced. A read is a read whatever spelling reaches
	// it, so the exception cannot grow in any spelling.
	//
	// The three reads inside rt_value_copy_init's own decision: the identity
	// compare in rt_value_copy_uses_runtime_width, the branch compare that
	// separates a specialization from the trap, and the one dispatch of that
	// specialization. `== NULL` guards do not count — the scanner strips them,
	// because a null check is not a use.
	//
	// The fourth is not a read at all: this file now DEFINES a descriptor of
	// its own — the opaque-word carrier a far channel holds and a C stand
	// builds — and naming a slot is how a descriptor is built. It is written
	// here rather than beside its caller precisely so that the scan stays at
	// zero everywhere else; a hand-written descriptor in any other file would
	// have to weaken the rule instead of being covered by this one count.
	helperReads := slotReadsBeyondNullChecks(
		readSlotControlFile(t, filepath.Join(root, "runtime", "native", "rt_value_ops.c")),
		"copy_init",
	)
	if len(helperReads) != 4 {
		t.Errorf("rt_value_ops.c reads copy_init %d times %v, want exactly 4 "+
			"(identity compare, specialization branch, the one dispatch of a "+
			"specialization, and the opaque-word descriptor's own definition); "+
			"a new read is a new way for the trap to reach a caller",
			len(helperReads), helperReads)
	}

	bin := buildRuntimeV2SlotControlHarness(t, "slot-control-copy-direct", nil, false)
	// The child dies by SIGABRT on purpose; run it somewhere a core dump is
	// discarded with the temporary directory instead of landing in the package.
	direct := exec.Command(bin, "copy-direct")
	direct.Dir = t.TempDir()
	output, err := direct.CombinedOutput()
	if err == nil {
		t.Fatalf("a descriptorless dispatch of the trap returned instead of refusing:\n%s", output)
	}
	// A nonzero exit is not enough: the harness reports its own failure when the
	// trap returns, so "the child failed" is true either way. The trap has to
	// take the process down before its caller can publish a destination it never
	// filled.
	// Matched on the wait status, not on the message. os/exec renders SIGABRT as
	// "signal: aborted" on Linux and "signal: abort trap" on Darwin, so a string
	// match here would quietly make this gate Linux-only.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the descriptorless dispatch ended as %v, not by a signal:\n%s", err, output)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGABRT {
		t.Fatalf("the descriptorless dispatch ended as %v, not by aborting:\n%s", err, output)
	}
	if strings.Contains(string(output), "returned from a descriptorless dispatch") {
		t.Fatalf("the trap returned to its caller instead of aborting:\n%s", output)
	}
	if !strings.Contains(string(output), "rt_value_copy_init_unbound_trap was dispatched through") {
		t.Fatalf("the descriptorless dispatch did not name itself:\n%s", output)
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
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_descriptor_cases.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_identity_cases.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_copy_cases.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_fifo_cases.c"),
		filepath.Join(root, "internal", "vm", "testdata", "slot_control_park_cases.c"),
		filepath.Join(root, "runtime", "native", "rt_slot_control.c"),
		filepath.Join(root, "runtime", "native", "rt_slot_claim.c"),
		filepath.Join(root, "runtime", "native", "rt_slot_exclusive.c"),
		filepath.Join(root, "runtime", "native", "rt_value_ops.c"),
		filepath.Join(root, "runtime", "native", "rt_typed_fifo.c"),
		filepath.Join(root, "runtime", "native", "rt_park_pool.c"),
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

// The move and drop slots get the discipline the copy slot already has.
//
// Until D3 the runtime had exactly one dispatch helper, for copies, and the
// requirement that every generated operation run detached from the owner lock
// lived in a comment with nothing enforcing it. A move or a drop dispatched
// straight through the descriptor takes that comment's word for it, and the
// failure it produces — a generated callback reentering the runtime under a
// lock it does not know is held — surfaces nowhere near the call.
//
// So the rule is the copy rule: the descriptor is in hand at every call site,
// and the helper is the only thing that reads the slot.
func TestRuntimeV2SlotControlMoveAndDropDispatchThroughTheDetachedHelpers(t *testing.T) {
	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, "runtime", "native"))
	if err != nil {
		t.Fatalf("read runtime/native: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "rt_value_ops.c" ||
			(!strings.HasSuffix(name, ".c") && !strings.HasSuffix(name, ".h")) {
			continue
		}
		source := readSlotControlFile(t, filepath.Join(root, "runtime", "native", name))
		if reads := slotReadsBeyondNullChecks(source, "move_init|drop_in_place"); len(reads) != 0 {
			t.Errorf("%s reads %q; a generated operation runs detached from the owner lock, "+
				"so moves go through rt_value_move_init_detached and drops through "+
				"rt_value_drop_in_place_detached", name, reads)
		}
	}

	// Inside the helper file the slots are pinned by TOTAL reads rather than by
	// call shape, for the reason the copy pin already records: counting
	// `->move_init(` alone misses `(*operations->move_init)(dst, src)`, which
	// dispatches through a plain deref and evades a shape count.
	//
	// One read each, and both are the dispatch itself. The lane refusal reads
	// no slot — it asks the lane, not the descriptor — which is what keeps the
	// count at one rather than two.
	helper := readSlotControlFile(t, filepath.Join(root, "runtime", "native", "rt_value_ops.c"))
	for _, slot := range []string{"move_init", "drop_in_place"} {
		reads := slotReadsBeyondNullChecks(helper, slot)
		if len(reads) != 2 {
			t.Errorf("rt_value_ops.c reads %s %d times %v, want exactly 2 (the dispatch, and "+
				"the opaque-word descriptor's own definition); a new read is a new way for the "+
				"callback to reach a caller that holds a lock",
				slot, len(reads), reads)
		}
	}
}
