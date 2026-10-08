package vm_test

import "testing"

func TestChannelSendMovedStringCancellation(t *testing.T) {
	t.Setenv("SURGE_THREADS", "1")
	src := `
async fn sender(ch: Channel<string>, ready: Channel<int>) -> int {
    ready.send(1);
    let value = "parked payload";
    ch.send(own value);
    return 0;
}
@entrypoint
fn main() -> int {
    let result = (async {
        let ch = Channel::<string>::new(1:uint);
        let ready = Channel::<int>::new(0:uint);
        let value = "buffered payload";
        ch.send(own value);
        let child = spawn sender(ch, ready);
        let _ = ready.recv();
        checkpoint().await();
        child.cancel();
        compare child.await() {
            Cancelled() => print("cancelled");
            Success(_) => print("unexpected");
        };
        ch.close();
        ret 0;
    }).await();
    compare result { Success(v) => { return v; } Cancelled() => { return 1; } };
}`
	got := runProgramFromSource(t, src, runOptions{captureStdout: true})
	if got.exitCode != 0 || got.stdout != "cancelled\n" || got.stderr != "" {
		t.Fatalf("cancelled owning send: exit=%d stdout=%q stderr=%s", got.exitCode, got.stdout, got.stderr)
	}
}
