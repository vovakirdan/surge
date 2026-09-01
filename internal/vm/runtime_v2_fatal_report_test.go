package vm_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// assertRuntimeV2FatalEmitter exercises the C reporter directly. The emitted
// allocation E2E rows below prove the compiler reaches it; this probe pins the
// other codes, the explicit-width ABI, and the no-allocation implementation.
func assertRuntimeV2FatalEmitter(t *testing.T) {
	t.Helper()
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	native := filepath.Join(root, "runtime", "native")

	headerBytes, readHeaderErr := os.ReadFile(filepath.Join(native, "rt.h"))
	if readHeaderErr != nil {
		t.Fatalf("read fatal ABI header: %v", readHeaderErr)
	}
	header := string(headerBytes)
	for _, want := range []string{
		"typedef uint32_t rt_fatal_code;",
		"RT_FATAL_PANIC = 0",
		"RT_OOM = 1",
		"RT_TRAP = 2",
		"SURGE_RT_NORETURN void\nrt_fatal_static(rt_fatal_code code, const uint8_t* message, uint64_t message_length);",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("fatal C ABI missing %q", want)
		}
	}
	headerProbePath := filepath.Join(t.TempDir(), "fatal_header_probe.cc")
	headerProbe := `#include "rt.h"
void probe(void) { rt_fatal_code code = RT_OOM; (void)code; }
`
	if err := os.WriteFile(headerProbePath, []byte(headerProbe), 0o600); err != nil {
		t.Fatalf("write C++ fatal ABI probe: %v", err)
	}
	if out, err := exec.Command("clang", "-x", "c++", "-std=c++17", "-Wall", "-Wextra", "-Werror",
		"-I", native, "-fsyntax-only", headerProbePath).CombinedOutput(); err != nil {
		t.Fatalf("C++ fatal ABI header probe: %v\n%s", err, out)
	}

	fatalPath := filepath.Join(native, "rt_fatal.c")
	fatalBytes, readFatalErr := os.ReadFile(fatalPath)
	if readFatalErr != nil {
		t.Fatalf("read fatal emitter: %v", readFatalErr)
	}
	for _, forbidden := range []string{
		"malloc(", "calloc(", "realloc(", "snprintf(", "printf(", "strlen(",
		"rt_write_stderr(", "pthread_mutex",
	} {
		if strings.Contains(string(fatalBytes), forbidden) {
			t.Fatalf("fatal emitter uses %q; the refusal path must not allocate or take the I/O lock", forbidden)
		}
	}

	asyncPanicBytes, readRouterErr := os.ReadFile(filepath.Join(native, "rt_async_panic.c"))
	if readRouterErr != nil {
		t.Fatalf("read internal panic router: %v", readRouterErr)
	}
	panicRouter := strings.SplitN(string(asyncPanicBytes), "void fatal_oom_msg", 2)[0]
	if !strings.Contains(panicRouter, "rt_fatal_static(RT_FATAL_PANIC") || strings.Contains(panicRouter, "rt_panic(") {
		t.Fatalf("panic_msg must route internal failures to RT_FATAL_PANIC, got:\n%s", panicRouter)
	}

	temp := t.TempDir()
	probePath := filepath.Join(temp, "fatal_probe.c")
	probe := `#include "rt.h"
#include <stdlib.h>
#include <string.h>

int main(int argc, char** argv) {
    if (argc != 3) {
        return 99;
    }
    rt_fatal_code code = (rt_fatal_code)strtoul(argv[1], NULL, 10);
    const uint8_t* message = (const uint8_t*)argv[2];
    rt_fatal_static(code, message, (uint64_t)strlen(argv[2]));
    return 98;
}
`
	if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
		t.Fatalf("write fatal probe: %v", err)
	}
	bin := filepath.Join(temp, "fatal_probe")
	compile := exec.Command("clang", "-std=c11", "-Wall", "-Wextra", "-Werror", "-I", native,
		probePath, fatalPath, "-o", bin)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile fatal probe: %v\n%s", err, out)
	}

	for _, row := range []struct {
		name    string
		code    string
		message string
		want    string
	}{
		{name: "panic", code: "0", message: "internal invariant", want: "surge: fatal [PANIC]: internal invariant\n"},
		{name: "oom", code: "1", message: "could not allocate T", want: "surge: fatal [RT_OOM]: could not allocate T\n"},
		{name: "trap", code: "2", message: "reachable backend trap", want: "surge: fatal [RT_TRAP]: reachable backend trap\n"},
		{name: "invalid_code_is_a_trap", code: "99", message: "invalid fatal code", want: "surge: fatal [RT_TRAP]: invalid fatal code\n"},
		{name: "one_trailing_newline", code: "1", message: "already terminated\n", want: "surge: fatal [RT_OOM]: already terminated\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			cmd := exec.Command(bin, row.code, row.message)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("fatal probe error=%v stdout=%q stderr=%q, want exit 1", runErr, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || stderr.String() != row.want {
				t.Fatalf("fatal probe stdout=%q stderr=%q, want stdout empty and stderr=%q",
					stdout.String(), stderr.String(), row.want)
			}
		})
	}

	// The new internal fatal route must not absorb the language's own panic.
	// Link only rt_panic's section out of rt_io.c so this is the production
	// writer, not a copy of its expected text.
	panicProbePath := filepath.Join(temp, "panic_probe.c")
	panicProbe := `#include "rt.h"

int main(void) {
    static const uint8_t message[] = "ordinary user panic";
    rt_panic(message, sizeof(message) - 1);
    return 98;
}
`
	if err := os.WriteFile(panicProbePath, []byte(panicProbe), 0o600); err != nil {
		t.Fatalf("write ordinary panic probe: %v", err)
	}
	panicBin := filepath.Join(temp, "panic_probe")
	compile = exec.Command("clang", "-std=c11", "-Wall", "-Wextra", "-Werror",
		"-ffunction-sections", "-fdata-sections", "-I", native, panicProbePath,
		filepath.Join(native, "rt_io.c"), "-Wl,--gc-sections", "-pthread", "-o", panicBin)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile ordinary panic probe: %v\n%s", err, out)
	}
	cmd := exec.Command(panicBin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 || stdout.Len() != 0 ||
		stderr.String() != "panic: ordinary user panic\n" {
		t.Fatalf("ordinary panic error=%v stdout=%q stderr=%q, want exit 1 and unchanged panic line",
			runErr, stdout.String(), stderr.String())
	}
}
