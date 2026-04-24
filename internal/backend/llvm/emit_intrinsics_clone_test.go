package llvm

import (
	"regexp"
	"testing"
)

func TestEmitTaskCloneLoadsHandleValue(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let worker = spawn async {
        return 1;
    };
    let worker2 = worker.clone();
    compare worker2.await() {
        Success(v) => print(v to string);
        Cancelled() => print("cancelled");
    };
    return 0;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if regexp.MustCompile(`call ptr @rt_task_clone\(ptr %l\d+\)`).MatchString(ir) {
		t.Fatalf("task clone used the local slot address instead of the loaded handle:\n%s", ir)
	}
	if !regexp.MustCompile(`load ptr, ptr %l\d+\n  %t\d+ = call ptr @rt_task_clone\(ptr %t\d+\)`).MatchString(ir) {
		t.Fatalf("task clone did not load the task handle before calling rt_task_clone:\n%s", ir)
	}
}

func TestEmitCloneErrorPayloadInErringCompare(t *testing.T) {
	sourceCode := `pragma module;

pub tag Help(string);
pub tag ErrorDiag(Error);

pub type ParseDiag = Help(string) | ErrorDiag(Error);

extern<ParseDiag> {
    pub fn pretty(self: &ParseDiag) -> string! {
        return compare self {
            Help(s) => clone(s);
            ErrorDiag(e) => clone(e);
        };
    }
}

@entrypoint
fn main() -> int {
    let diag: ParseDiag = ErrorDiag(Error { message = "boom", code = 1:uint });
    compare diag.pretty() {
        Success(text) => {
            print(text);
            return 0;
        }
        err => {
            print(err.message);
            return 1;
        }
    };
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if regexp.MustCompile(`call [^(]+ @__clone\(`).MatchString(ir) {
		t.Fatalf("Error clone leaked as an unresolved external __clone call:\n%s", ir)
	}
}
