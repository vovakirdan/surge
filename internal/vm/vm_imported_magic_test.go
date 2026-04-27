package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVMImportedStdlibMagicBinaryOperator(t *testing.T) {
	root := repoRoot(t)
	stdlibRoot := t.TempDir()
	copyCoreForTempStdlib(t, root, stdlibRoot)
	writeOperatorReproModule(t, stdlibRoot)
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	sourceCode := `import stdlib/operator_repro as repro;

@entrypoint
fn main() -> int {
    let digest: repro.Digest = repro.Digest { value = 42:uint64 };
    let same: repro.Digest = repro.Digest { value = 42:uint64 };
    let other: repro.Digest = repro.Digest { value = 43:uint64 };

    let op_same: bool = digest != same;
    let direct_same: bool = digest.__ne(same);
    if op_same != direct_same {
        return 1;
    }
    if op_same {
        return 2;
    }

    let op_other: bool = digest != other;
    let direct_other: bool = digest.__ne(other);
    if op_other != direct_other {
        return 3;
    }
    if !op_other {
        return 4;
    }

    return 0;
}
`

	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 0 {
		t.Fatalf("expected imported stdlib operator overload to exit 0, got %d", result.exitCode)
	}
}

func TestVMImportedStdlibMagicMethods(t *testing.T) {
	root := repoRoot(t)
	stdlibRoot := t.TempDir()
	copyCoreForTempStdlib(t, root, stdlibRoot)
	writeMagicReproModule(t, stdlibRoot)
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	sourceCode := `import stdlib/magic_repro as repro;

@entrypoint
fn main() -> int {
    let digest: repro.Digest = repro.Digest { value = 42:uint64 };
    let same: repro.Digest = repro.Digest { value = 42:uint64 };
    if digest != same {
        return 1;
    }

    let empty: repro.Box = repro.Box { value = "" };
    if !empty {
        0:int;
    } else {
        return 2;
    }

    let original: repro.Box = repro.Box { value = "ok" };
    let cloned: repro.Box = clone(original);
    if cloned.value != "ok" {
        return 3;
    }

    let flag: repro.Flag = repro.Flag { value = 1 };
    if flag {
        0:int;
    } else {
        return 4;
    }

    if original to int != 7 {
        return 5;
    }

    let mut bag: repro.Bag = repro.Bag { values = [1, 2, 3] };
    if bag[1] != 2 {
        return 6;
    }
    bag[1] = 9;
    if bag.values[1] != 9 {
        return 7;
    }

    let mut sum: int = 0;
    for v in bag {
        sum = sum + v;
    }
    if sum != 13 {
        return 8;
    }

    let mut typed_sum: int = 0;
    for typed: int in bag {
        typed_sum = typed_sum + typed;
    }
    if typed_sum != 13 {
        return 9;
    }

    return 0;
}
`

	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 0 {
		t.Fatalf("expected imported stdlib magic methods to exit 0, got %d", result.exitCode)
	}
}

func TestVMBoundBoolMagicLoweredAfterMono(t *testing.T) {
	sourceCode := `contract BoolLike<T> {
    fn __bool(self: T) -> bool;
}

@copy
type Flag = { ok: bool };

extern<Flag> {
    fn __bool(self: Flag) -> bool {
        return self.ok;
    }
}

fn check<T: BoolLike>(v: T) -> int {
    if v {
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let yes: Flag = Flag { ok = true };
    if check::<Flag>(yes) != 1 {
        return 10;
    }

    let no: Flag = Flag { ok = false };
    if check::<Flag>(no) != 0 {
        return 11;
    }

    return 0;
}
`

	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 0 {
		t.Fatalf("expected bound __bool magic to exit 0, got %d", result.exitCode)
	}
}

func copyCoreForTempStdlib(t *testing.T, root, stdlibRoot string) {
	t.Helper()
	if err := os.CopyFS(filepath.Join(stdlibRoot, "core"), os.DirFS(filepath.Join(root, "core"))); err != nil {
		t.Fatalf("copy core stdlib: %v", err)
	}
}

func writeOperatorReproModule(t *testing.T, stdlibRoot string) {
	t.Helper()
	dir := filepath.Join(stdlibRoot, "stdlib", "operator_repro")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create operator repro module: %v", err)
	}
	source := `pragma module;

@copy
pub type Digest = {
    value: uint64,
};

extern<Digest> {
    pub fn __eq(self: Digest, other: Digest) -> bool {
        return self.value == other.value;
    }

    pub fn __ne(self: Digest, other: Digest) -> bool {
        return self.value != other.value;
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "operator_repro.sg"), []byte(source), 0o600); err != nil {
		t.Fatalf("write operator repro module: %v", err)
	}
}

func writeMagicReproModule(t *testing.T, stdlibRoot string) {
	t.Helper()
	dir := filepath.Join(stdlibRoot, "stdlib", "magic_repro")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create magic repro module: %v", err)
	}
	source := `pragma module;

@copy
pub type Digest = { value: uint64 };
pub type Box = { value: string };
pub type Flag = { value: int };
pub type Bag = { values: int[] };

extern<Digest> {
    pub fn __eq(self: Digest, other: Digest) -> bool {
        return self.value == other.value;
    }

    pub fn __ne(self: Digest, other: Digest) -> bool {
        return self.value != other.value;
    }
}

extern<Box> {
    pub fn __clone(self: &Box) -> Box {
        return Box { value = self.value.__clone() };
    }

    pub fn __not(self: &Box) -> bool {
        return self.value == "";
    }

    pub fn __to(self: &Box, _: int) -> int {
        return 7;
    }
}

extern<Flag> {
    pub fn __bool(self: &Flag) -> bool {
        return self.value != 0;
    }
}

extern<Bag> {
    pub fn __range(self: &Bag) -> Range<int> {
        return self.values.__range();
    }

    pub fn __index(self: &Bag, index: int) -> int {
        return self.values[index];
    }

    pub fn __index_set(self: &mut Bag, index: int, value: int) -> nothing {
        self.values[index] = value;
        return nothing;
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "magic_repro.sg"), []byte(source), 0o600); err != nil {
		t.Fatalf("write magic repro module: %v", err)
	}
}
