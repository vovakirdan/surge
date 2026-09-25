package vm_test

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// THE DEFAULT OF A CORE RUNTIME HANDLE IS THE NULL HANDLE, ON BOTH BACKENDS.
//
// The native backend gives `Channel<T>`, `Task<T>` and `Range<T>` a null
// pointer as their default (internal/backend/llvm/emit_intrinsics_default.go:68-73).
// The VM built one from the handle's declared struct instead, and the storage
// walk refused it: `panic VM1999: storage: type#N has 1 members but 0 layout
// offsets`, at the default, before the program could do anything with it. The
// VM now gives the same null (internal/vm/runtime_handle_null.go), and every
// row below asserts ONE expected run -- stdout, exit code and the runtime's
// refusal -- against both backends, so the two cannot drift apart without one
// of them failing its own leg.
//
// A null that is only held, copied, dropped or overwritten does nothing on
// either backend. A null that is USED is refused, with the native runtime's
// own words: `async: null channel handle` for a channel (channel_from_handle,
// runtime/native/rt_channel_lane.h:499-505). The VM frames the words as
// `panic VM1203: ...` and the native runtime as `surge: fatal [PANIC]: ...`;
// the words and the exit code are the contract.
//
// Every row is a program the D2 return-origin gate accepts at this base. The
// gate refuses every `Task` and `Range` default ("opaque result borrowed-state
// classification is unsupported") and a `let` with no initializer ("default
// result is not proven Defaultable"), so those shapes cannot be rows here; the
// null `Task` is pinned below the gate by runtime_handle_null_internal_test.go.
// The channel binding row is the reported program with its implicit default
// spelled out.
//
// The last row is the `timeout` half of the same fix. A cancelled task answers
// pending at a `timeout` before it resolves the target, as rt_timeout_poll
// does, so a null target is never looked at there. With a LIVE target the VM
// used to go on instead and died with `panic VM1002: async payload owner
// capability mismatch`, where the native runtime finishes the task cancelled.

type defaultHandleRow struct {
	name   string
	source string
	stdout string // what both backends print
	exit   int    // what both backends exit with
	fault  string // the runtime refusal both backends name; "" for a clean run
}

type defaultHandleRun struct {
	stdout string
	exit   int
	fault  string
}

const defaultHandleHolderSource = `
type HolderCh = { ch: Channel<int>, n: int };
`

// nullChannelUse wraps one use of a null channel handle in a sync entrypoint.
func nullChannelUse(use string) string {
	return `
@entrypoint
fn main() -> int {
    let c = default::<Channel<int>>();
    print("before");
    ` + use + `
    print("after");
    return 0;
}
`
}

// nullChannelUseAsync is the same use made from inside a task, after a
// suspension, so the async lowering of the operation is what meets the null.
func nullChannelUseAsync(use string) string {
	return `
async fn hold() -> int {
    let c = default::<Channel<int>>();
    checkpoint().await();
    print("before");
    ` + use + `
    print("after");
    return 4;
}

@entrypoint
fn main() -> int {
    let t = spawn hold();
    return compare t.await() { Success(v) => v; Cancelled() => 90; };
}
`
}

var defaultHandleRows = []defaultHandleRow{
	{
		name: "channel_binding",
		source: `
@entrypoint
fn main() -> int {
    let mut ch = default::<Channel<int>>();
    ch = Channel::<int>::new(1:uint);
    ch.send(7);
    let got = ch.recv();
    compare got { Some(v) => print("channel-binding " + (v to string)); nothing => print("channel-binding none"); };
    return 3;
}
`,
		stdout: "channel-binding 7\n",
		exit:   3,
	},
	{
		// Held, copied, stored in a member, kept in a task frame across a
		// suspension, handed to a task as a parameter (whose initial frame
		// retains it), wrapped in a Mutex -- and never used. Each copy and each
		// holder is dropped at its scope's end, and not one of them may do
		// anything: the native drop and retain of NULL are no-ops, and so are
		// the VM's of H == 0.
		name: "default_dropped_unused",
		source: defaultHandleHolderSource + `
async fn hold() -> int {
    let c = default::<Channel<int>>();
    checkpoint().await();
    return 4;
}

async fn take(c: Channel<int>) -> int {
    checkpoint().await();
    return 2;
}

@entrypoint
fn main() -> int {
    let c = default::<Channel<int>>();
    let d = c;
    let h = default::<HolderCh>();
    let m = default::<Mutex>();
    let o: Option<Channel<int>> = Some(default::<Channel<int>>());
    compare o { Some(_) => print("some"); nothing => print("nothing"); };
    let t = spawn hold();
    let u = spawn take(d);
    let held = compare t.await() { Success(v) => v; Cancelled() => 90; };
    let took = compare u.await() { Success(v) => v; Cancelled() => 90; };
    print("dropped-unused " + (h.n to string) + " " + (held to string) + " " + (took to string));
    return 0;
}
`,
		stdout: "some\ndropped-unused 0 4 2\n",
		exit:   0,
	},
	{
		// Overwritten in a member and in an array slot, then used through the
		// new handle: the null that is displaced is dropped by the store, and
		// that drop is the no-op.
		name: "default_overwritten",
		source: defaultHandleHolderSource + `
@entrypoint
fn main() -> int {
    let mut h = default::<HolderCh>();
    h.ch = Channel::<int>::new(1:uint);
    h.ch.send(9);
    let a = h.ch.recv();
    let mut xs: Channel<int>[] = [];
    xs.push(default::<Channel<int>>());
    xs[0] = Channel::<int>::new(1:uint);
    xs[0].send(5);
    let b = xs[0].recv();
    let x = compare a { Some(v) => v; nothing => 0; };
    let y = compare b { Some(v) => v; nothing => 0; };
    print("overwritten " + (x to string) + " " + (y to string));
    return 0;
}
`,
		stdout: "overwritten 9 5\n",
		exit:   0,
	},
	{name: "null_send", source: nullChannelUse("c.send(1);"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_recv", source: nullChannelUse("let v = c.recv();"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_close", source: nullChannelUse("c.close();"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_try_send", source: nullChannelUse("let ok = c.try_send(1);"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_try_recv", source: nullChannelUse("let v = c.try_recv();"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_send_in_task", source: nullChannelUseAsync("c.send(1);"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_recv_in_task", source: nullChannelUseAsync("let v = c.recv();"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{name: "null_select_arm", source: nullChannelUseAsync("let w: int = select { c.recv() => 20; };"), stdout: "before\n", exit: 1, fault: "async: null channel handle"},
	{
		name: "null_member_send",
		source: defaultHandleHolderSource + `
@entrypoint
fn main() -> int {
    let h = default::<HolderCh>();
    print("before");
    h.ch.send(1);
    print("after");
    return 0;
}
`,
		stdout: "before\n",
		exit:   1,
		fault:  "async: null channel handle",
	},
	{
		// A default Mutex is a null channel underneath; locking it is a
		// receive on that channel.
		name: "null_mutex_lock",
		source: `
async fn run() -> int {
    let m = default::<Mutex>();
    print("before");
    m.lock().await();
    m.unlock();
    print("after");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(v) => v; Cancelled() => 91; };
}
`,
		stdout: "before\n",
		exit:   1,
		fault:  "async: null channel handle",
	},
	{
		name: "cancelled_task_timeout",
		source: `
async fn work() -> int {
    return 5;
}

async fn child() -> int {
    let n = spawn work();
    checkpoint().await();
    print("after-checkpoint");
    let r = timeout(n, 1000:uint);
    print("after-timeout");
    return compare r { Success(v) => v; Cancelled() => 1; };
}

async fn run() -> int {
    let t = spawn child();
    checkpoint().await();
    t.cancel();
    return compare t.await() { Success(v) => v; Cancelled() => 90; };
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(v) => v; Cancelled() => 91; };
}
`,
		stdout: "after-checkpoint\n",
		exit:   90,
	},
}

var (
	vmFaultRE     = regexp.MustCompile(`(?m)^panic VM\d+: (.*)$`)
	nativeFaultRE = regexp.MustCompile(`(?m)^surge: fatal \[PANIC\]: (.*)$`)
)

// faultOf reads the runtime's refusal out of a run's stderr, in the frame the
// backend prints it in, and "" when there is none.
func faultOf(re *regexp.Regexp, stderr string) string {
	if m := re.FindStringSubmatch(stderr); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func (row defaultHandleRow) check(t *testing.T, backend string, got defaultHandleRun, stderr string) {
	t.Helper()
	want := defaultHandleRun{stdout: row.stdout, exit: row.exit, fault: row.fault}
	if got != want {
		t.Fatalf("%s ran %s as stdout=%q exit=%d fault=%q, want stdout=%q exit=%d fault=%q\nstderr:\n%s",
			backend, row.name, got.stdout, got.exit, got.fault, want.stdout, want.exit, want.fault, stderr)
	}
}

func TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends(t *testing.T) {
	for _, row := range defaultHandleRows {
		t.Run(row.name, func(t *testing.T) {
			var vmRun, nativeRun *defaultHandleRun
			t.Run("vm", func(t *testing.T) {
				t.Setenv(backendEnvVar, backendVM)
				result := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
				exit := result.exitCode
				if strings.TrimSpace(result.stderr) != "" {
					// The in-process run hands a VM panic back as an error
					// beside the exit code; `surge run` turns it into exit 1
					// (cmd/surge/run.go, the vmErr branch), and that is the
					// process the native binary is compared with.
					exit = 1
				}
				got := defaultHandleRun{stdout: result.stdout, exit: exit, fault: faultOf(vmFaultRE, result.stderr)}
				// A VM failure that is not the expected refusal -- the storage
				// walk's VM1999 at the default, above all -- must not hide
				// behind a matching exit code.
				if row.fault == "" && strings.TrimSpace(result.stderr) != "" {
					t.Fatalf("vm reported a runtime error for a clean row:\n%s", result.stderr)
				}
				row.check(t, "vm", got, result.stderr)
				vmRun = &got
			})
			t.Run("llvm", func(t *testing.T) {
				got, stderr := runDefaultHandleNative(t, row)
				row.check(t, "llvm", got, stderr)
				nativeRun = &got
			})
			if vmRun != nil && nativeRun != nil && *vmRun != *nativeRun {
				t.Fatalf("the backends disagree on %s: vm=%+v llvm=%+v", row.name, *vmRun, *nativeRun)
			}
		})
	}
}

// runDefaultHandleNative builds a row natively and runs it single-threaded --
// backend parity is compared single-threaded (RV2-DEBT-188), and the timeout
// row's cancel races its target on more workers -- under valgrind when it is
// installed. Under valgrind a memory error fails any row, and a clean row
// must also lose nothing: a null that a drop or a retain dereferenced, or a
// displaced handle nobody released, shows up there and nowhere else.
func runDefaultHandleNative(t *testing.T, row defaultHandleRow) (defaultHandleRun, string) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, row.source, nil)
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_THREADS", "1")
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Logf("valgrind not installed; running %s natively without it", row.name)
		_, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
		return defaultHandleRun{stdout: result.stdout, exit: result.exitCode, fault: faultOf(nativeFaultRE, result.stderr)}, result.stderr
	}
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a memory error on %s\nstdout:\n%s\nstderr:\n%s", row.name, stdout, stderr)
	}
	if row.fault == "" {
		lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary for %s: %v\nstderr:\n%s", row.name, err, stderr)
		}
		if lostBytes != 0 || lostBlocks != 0 {
			t.Fatalf("%s lost %d bytes in %d blocks natively, want strict zero\nstderr:\n%s", row.name, lostBytes, lostBlocks, stderr)
		}
	}
	return defaultHandleRun{stdout: stdout, exit: exitCode, fault: faultOf(nativeFaultRE, stderr)}, stderr
}
