package vm_test

import "testing"

// What a blocking BODY owns, measured under valgrind: the local it builds, the
// capture it only reads, the capture it consumes, and the `@copy` composite it
// receives a copy of.
//
// These are the rows runtime_v2_blocking_capture_reclamation_test.go said it
// could not pin: blocking bodies recorded no drop obligations at all
// (`dropObligationsSuppressed`, `internal/sema/drop_obligations.go`), so a
// string built inside the body had no scope-exit release and a capture the
// body merely read -- spent out of the job's state by the unpack, claimed by
// the worker before the body ran -- had no owner left. The earlier reproducer
// measured that loss as 219 bytes in one block, constant in the iteration count,
// and it was constant because ITS body CONSUMED its capture; a body that reads
// one loses it once per execution.
//
// Each program runs its body eight times so a per-execution loss is eight
// blocks, not one, and each prints a completion marker so a program that exited
// before its bodies ran cannot pass by leaking nothing. Only the native lane
// can witness these rows: the leak figure is valgrind's, and the VM has no
// blocking pool whose release could be the second owner.

// The body builds a string and returns an int. Nothing moves in, so the only
// thing to reclaim is what the body made -- and it made it eight times.
const runtimeV2BlockingBodyLocalSource = `
fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let job: Task<int> = blocking {
            let built: string = wide();
            ret built.__len() to int;
        };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 1600 {
        print("FAIL blocking body local total");
        return 1;
    }
    print("blocking-body-local-witness");
    return 0;
}
`

// The body READS its capture and returns. The state's unpack spent the field,
// the worker claimed the state before the body ran, so the body is the last
// owner and its return is where the string is released.
const runtimeV2BlockingReadCaptureSource = `
fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let text: string = wide();
        let job: Task<int> = blocking { ret text.__len() to int; };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 1600 {
        print("FAIL blocking read-capture total");
        return 1;
    }
    print("blocking-read-capture-witness");
    return 0;
}
`

// The twin: the body CONSUMES its capture by handing it to a by-value callee.
// The callee releases it; the body's return must find nothing live, or the
// string is freed twice -- which is the row that would go red if the body's
// registration were added without the move tracking that pairs with it.
const runtimeV2BlockingConsumedCaptureSource = `
fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

fn eat(s: string) -> int {
    return s.__len() to int;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let text: string = wide();
        let job: Task<int> = blocking { ret eat(text); };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 1600 {
        print("FAIL blocking consumed-capture total");
        return 1;
    }
    print("blocking-consumed-capture-witness");
    return 0;
}
`

// A `@copy` value composite is captured by copy: the caller keeps its binding
// and reads it after the body has run. `@copy` admits only Copy members, so the
// copy owns no heap and nobody drops anything for it -- the row pins that the
// caller's later read is a read of an intact value and that the state block the
// copy travelled in is still reclaimed.
const runtimeV2BlockingCopyCompositeSource = `
@copy
type Pair = { a: int, b: int };

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let p: Pair = Pair { a = i, b = 100 };
        let job: Task<int> = blocking { ret p.a + p.b; };
        let got: int = compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        total = total + got + p.a;
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 856 {
        print("FAIL blocking copy-composite total");
        return 1;
    }
    print("blocking-copy-composite-witness");
    return 0;
}
`

func TestRuntimeV2BlockingBodyLocalIsReclaimed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingBodyLocalSource, "blocking-body-local-witness")
}

func TestRuntimeV2BlockingReadCaptureIsReclaimed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingReadCaptureSource, "blocking-read-capture-witness")
}

func TestRuntimeV2BlockingConsumedCaptureIsReleasedOnce(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingConsumedCaptureSource, "blocking-consumed-capture-witness")
}

func TestRuntimeV2BlockingCopyCompositeCaptureIsReclaimed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingCopyCompositeSource, "blocking-copy-composite-witness")
}
