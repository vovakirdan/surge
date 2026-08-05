package vm_test

import "testing"

func TestVMEntrypointArgvDefaultParsesSuppliedValue(t *testing.T) {
	sourceCode := `@entrypoint("argv") fn main(port: uint = 7:uint) -> int {
	return port to int;
}
`
	defaulted := runProgramFromSource(t, sourceCode, runOptions{})
	if defaulted.exitCode != 7 {
		t.Errorf("defaulted exit code = %d, want 7", defaulted.exitCode)
	}
	overridden := runProgramFromSource(t, sourceCode, runOptions{argv: []string{"11"}})
	if overridden.exitCode != 11 {
		t.Errorf("supplied argv exit code = %d, want 11", overridden.exitCode)
	}
}

func TestVMEntrypointStdinUsesResolvedUserFromStdin(t *testing.T) {
	sourceCode := `type Wrapped = { value: int };
extern<Wrapped> {
    pub fn from_stdin(_text: string) -> Erring<Wrapped, Error> {
        return Success(Wrapped { value = 31 });
    }
}
@entrypoint("stdin") fn main(value: Wrapped) -> int { return value.value; }
`
	result := runProgramFromSource(t, sourceCode, runOptions{stdin: "ignored"})
	if result.exitCode != 31 {
		t.Errorf("expected exit code 31, got %d", result.exitCode)
	}
}

func TestVMEntrypointStdinStringReceivesEOFAsEmptyString(t *testing.T) {
	sourceCode := `@entrypoint("stdin") fn main(text: string) -> int { return len(&text) to int; }
`
	empty := runProgramFromSource(t, sourceCode, runOptions{})
	if empty.exitCode != 0 {
		t.Errorf("EOF string length = %d, want 0", empty.exitCode)
	}
	provided := runProgramFromSource(t, sourceCode, runOptions{stdin: "abc"})
	if provided.exitCode != 3 {
		t.Errorf("provided string length = %d, want 3", provided.exitCode)
	}
}
