package vm_test

import (
	"testing"
	"time"
)

// A tag constructor handed a reference to a composite stores the address.
// The tag layout read the payload through the reference, so `Some::<&Rec>(r)`
// copied the record's bytes into the pointer slot and the arm's `r.a` read
// address 0x3 natively.
const tagReferencePayloadCompositeSource = `type Rec = { a: int, b: int };
type P = (int, int);
type SRec = { s: string, n: int };

fn rd(o: Option<&Rec>) -> int { return compare o { Some(r) => r.a + r.b; nothing => -1; }; }
fn rdp(o: Option<&P>) -> int { return compare o { Some(r) => r.0 + r.1; nothing => -1; }; }
fn wr(o: Option<&mut SRec>) -> nothing { compare o { Some(r) => { r.s = "w" * 30; r.n = 9; } nothing => {} }; return nothing; }

fn s1(r: &Rec) -> int { return rd(Some::<&Rec>(r)); }
fn s2(p: &P) -> int { return rdp(Some::<&P>(p)); }
fn s3(r: &mut SRec) -> nothing { wr(Some::<&mut SRec>(r)); return nothing; }

@entrypoint
fn main() -> int {
    let r = Rec{ a: 1, b: 2 };
    print(s1(&r) to string);
    let none: Option<&Rec> = nothing;
    print(rd(none) to string);
    let p: P = (3, 4);
    print(s2(&p) to string);
    let mut w = SRec{ s: "a" * 30, n: 1 };
    s3(&mut w);
    print(w.s);
    print(w.n to string);
    return 0;
}
`

const tagReferencePayloadCompositeWant = "3\n-1\n7\n" + "wwwwwwwwwwwwwwwwwwwwwwwwwwwwww\n" + "9\n"

func TestTagReferencePayloadOfACompositeRunsVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, tagReferencePayloadCompositeSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != tagReferencePayloadCompositeWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, tagReferencePayloadCompositeWant, res.stderr)
	}
}

func TestTagReferencePayloadOfACompositeLLVMValgrind(t *testing.T) {
	outputPath := buildLLVMProgramFromSource(t, tagReferencePayloadCompositeSource)
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
	if hasValgrindMemcheckError(stderr) || exitCode != 0 || stdout != tagReferencePayloadCompositeWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, tagReferencePayloadCompositeWant, stderr)
	}
}
