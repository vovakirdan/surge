package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

func TestDiagnose_NoDependencyErrorForCleanImport(t *testing.T) {
	opts := DiagnoseOptions{
		Stage:          DiagnoseStageSyntax,
		MaxDiagnostics: 10,
	}

	src := "import foo::{}; // surge fix should replace '::{}' with ''\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "empty_import_group.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}

	for _, d := range res.Bag.Items() {
		if d.Code == diag.ProjDependencyFailed {
			t.Fatalf("unexpected dependency failure diagnostic: %+v", d)
		}
	}
}

func TestDiagnoseReportsUnresolvedSymbol(t *testing.T) {
	src := `
        fn demo() -> int {
            return missing;
        }
    `

	dir := t.TempDir()
	path := filepath.Join(dir, "unresolved.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}

	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if res.Bag.Len() == 0 {
		t.Fatalf("expected diagnostics, got none")
	}

	found := false
	for _, d := range res.Bag.Items() {
		if d.Code == diag.SemaUnresolvedSymbol {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unresolved symbol diagnostic, got %+v", res.Bag.Items())
	}
}

func TestDiagnoseDropReleasesBorrowReturnedFromMethodCall(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Fatal("failed to locate stdlib root for test")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	src := `
@entrypoint
fn main() -> nothing {
    let mut xs: int[] = [1, 2, 3];
    let item: &mut int = xs.get_mut(0);
    *item = 10;
    @drop item;
    let shared: &int[] = &xs;
    print(shared[0] to string);
    return nothing;
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "drop_returned_borrow.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}

	res, err := DiagnoseWithOptions(t.Context(), path, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if res.Bag.Len() != 0 {
		t.Fatalf("expected no diagnostics, got %+v", res.Bag.Items())
	}
	if res.Sema == nil {
		t.Fatal("expected sema artefacts")
	}

	dropBorrow := sema.NoBorrowID
	for _, ev := range res.Sema.BorrowEvents {
		if ev.Kind == sema.BorrowEvDrop && ev.Borrow != sema.NoBorrowID {
			dropBorrow = ev.Borrow
			break
		}
	}
	if dropBorrow == sema.NoBorrowID {
		t.Fatalf("expected @drop to target an active borrow, got events %+v", res.Sema.BorrowEvents)
	}

	dropEnded := false
	for _, ev := range res.Sema.BorrowEvents {
		if ev.Kind == sema.BorrowEvBorrowEnd && ev.Borrow == dropBorrow && ev.Note == "drop" {
			dropEnded = true
			break
		}
	}
	if !dropEnded {
		t.Fatalf("expected borrow_end on @drop for borrow %d, got events %+v", dropBorrow, res.Sema.BorrowEvents)
	}

	scopeEndedDropBorrow := false
	for _, ev := range res.Sema.BorrowEvents {
		if ev.Kind == sema.BorrowEvBorrowEnd && ev.Borrow == dropBorrow && ev.Note == "scope_end" {
			scopeEndedDropBorrow = true
			break
		}
	}
	if scopeEndedDropBorrow {
		t.Fatalf("borrow %d should end on @drop, not at scope end; events %+v", dropBorrow, res.Sema.BorrowEvents)
	}
}

func TestDiagnoseRejectsExitOnUnionErrorPath(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Fatal("failed to locate stdlib root for test")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	src := `
tag Help(string);
tag ErrorDiag(Error);
type ParseDiag = Help(string) | ErrorDiag(Error);

fn bad() -> Erring<int, ParseDiag> {
    let e: Error = { message = "bad", code = 1:uint };
    return ErrorDiag(e);
}

@entrypoint
fn main() {
    let result = bad();
    compare result {
        Success(v) => { print(v to string); }
        err => { exit(err); }
    }
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "exit_union.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}

	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if res.Bag.Len() == 0 {
		t.Fatalf("expected diagnostics, got none")
	}

	found := false
	for _, d := range res.Bag.Items() {
		if d.Code != diag.SemaTypeMismatch {
			continue
		}
		if d.Message == "exit requires ErrorLike-compatible argument with fields 'message: string' and 'code: int/uint'; got ParseDiag" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected exit ErrorLike diagnostic, got %+v", res.Bag.Items())
	}
}

func TestDiagnoseRejectsCompareArmBlockThatFallsThroughAsNothing(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Fatal("failed to locate stdlib root for test")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	src := `
fn source(flag: bool) -> Erring<string, Error> {
    if flag {
        return Success("hello");
    }
    return Error { message = "missing", code = 1:uint };
}

fn recover(flag: bool) -> Erring<string, Error> {
    let res = source(flag);
    return compare res {
        Success(text) => Success(text);
        err => {
            if err.code == 1:uint {
                let empty: string = "";
                Success(empty);
            } else {
                err;
            }
        }
    };
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "compare_arm_fallthrough.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}

	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if res.Bag.Len() == 0 {
		t.Fatalf("expected diagnostics, got none")
	}

	found := false
	for _, d := range res.Bag.Items() {
		if d.Code != diag.SemaTypeMismatch {
			continue
		}
		if strings.Contains(d.Message, "compare arm type mismatch") && strings.Contains(d.Message, "got nothing") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected compare arm mismatch diagnostic, got %+v", res.Bag.Items())
	}
}
