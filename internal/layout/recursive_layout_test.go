package layout_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestLayoutEngine_RecursiveOptionStructReportsError(t *testing.T) {
	sourceCode := `type Node = { next: Node? }

@entrypoint
fn main() -> int { return 0; }
`
	res := diagnoseSemaFromSource(t, sourceCode, true)
	if res.Bag == nil || !res.Bag.HasErrors() {
		t.Fatal("expected sema error for recursive type, got none")
	}
	if !bagHasCode(res.Bag, diag.SemaRecursiveUnsized) {
		t.Fatalf("expected %v diagnostic, got %+v", diag.SemaRecursiveUnsized, res.Bag.Items())
	}
	nodeType := resolveTypeSymbol(t, res, "Node")

	// Ensure Node? was lowered through Option<T> tag union (Some<T> | nothing), not a special pointer type.
	nodeInfo, ok := res.Sema.TypeInterner.StructInfo(nodeType)
	if !ok || nodeInfo == nil || len(nodeInfo.Fields) != 1 {
		t.Fatalf("expected Node to be a struct with 1 field, got %+v", nodeInfo)
	}
	optType := nodeInfo.Fields[0].Type
	unionInfo, ok := res.Sema.TypeInterner.UnionInfo(optType)
	if !ok || unionInfo == nil {
		t.Fatalf("expected Node.next to be a union type (Option<Node>), got type#%d", optType)
	}
	if unionInfo.Name != res.Symbols.Table.Strings.Intern("Option") {
		gotName, _ := res.Symbols.Table.Strings.Lookup(unionInfo.Name)
		t.Fatalf("expected union name Option, got %q", gotName)
	}
	someName := res.Symbols.Table.Strings.Intern("Some")
	seenSome := false
	seenNothing := false
	for _, m := range unionInfo.Members {
		switch m.Kind {
		case types.UnionMemberNothing:
			seenNothing = true
		case types.UnionMemberTag:
			if m.TagName == someName && len(m.TagArgs) == 1 && m.TagArgs[0] == nodeType {
				seenSome = true
			}
		}
	}
	if !seenSome || !seenNothing {
		t.Fatalf("expected Option<Node> members Some(Node) and nothing, got %+v", unionInfo.Members)
	}

	le := layout.New(layout.X86_64LinuxGNU(), res.Sema.TypeInterner)
	_, err := le.LayoutOf(nodeType)
	if err == nil {
		t.Fatal("expected recursive layout error, got nil")
	}
	var lerr *layout.LayoutError
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *layout.LayoutError, got %T (%v)", err, err)
	}
	if lerr.Kind != layout.LayoutErrRecursiveUnsized {
		t.Fatalf("expected LayoutErrRecursiveUnsized, got kind=%d (%v)", lerr.Kind, lerr)
	}
	if len(lerr.Cycle) == 0 {
		t.Fatalf("expected non-empty cycle path, got %+v", lerr)
	}
}

func TestLayoutEngine_RecursiveReferenceStructIsSized(t *testing.T) {
	sourceCode := `type Node = { next: &Node }

@entrypoint
fn main() -> int { return 0; }
`
	res := diagnoseSemaFromSource(t, sourceCode, false)
	nodeType := resolveTypeSymbol(t, res, "Node")

	le := layout.New(layout.X86_64LinuxGNU(), res.Sema.TypeInterner)
	l, err := le.LayoutOf(nodeType)
	if err != nil {
		t.Fatalf("unexpected layout error: %v", err)
	}
	if l.Size != 8 || l.Align != 8 {
		t.Fatalf("expected Node layout size=8 align=8, got size=%d align=%d", l.Size, l.Align)
	}
}

func diagnoseSemaFromSource(t *testing.T, sourceCode string, allowErrors bool) *driver.DiagnoseResult {
	t.Helper()

	// Создаём временную директорию для теста
	tmpDir := t.TempDir()

	// Создаём временный файл с исходным кодом
	tmpFile, err := os.CreateTemp(tmpDir, "layout_recursive_*.sg")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() {
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			t.Fatalf("failed to remove temp file: %v", removeErr)
		}
	}()

	if _, err = tmpFile.WriteString(sourceCode); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			t.Fatalf("failed to close temp file: %v", closeErr)
		}
		t.Fatalf("write temp file: %v", err)
	}
	if err = tmpFile.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	// Получаем абсолютный путь к файлу
	absPath, err := filepath.Abs(tmpFile.Name())
	if err != nil {
		t.Fatalf("get absolute path: %v", err)
	}

	res, err := driver.DiagnoseWithOptions(context.Background(), absPath, &driver.DiagnoseOptions{
		Stage:          driver.DiagnoseStageSema,
		MaxDiagnostics: 100,
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if res.Bag.HasErrors() && !allowErrors {
		var sb strings.Builder
		for _, d := range res.Bag.Items() {
			sb.WriteString(d.Message)
			sb.WriteString("\n")
		}
		t.Fatalf("unexpected sema errors:\n%s", sb.String())
	}
	if res.Sema == nil || res.Sema.TypeInterner == nil {
		t.Fatal("missing type interner")
	}
	if res.Symbols == nil || res.Symbols.Table == nil || res.Symbols.Table.Strings == nil || res.Symbols.Table.Symbols == nil {
		t.Fatal("missing symbols table")
	}
	return res
}

func resolveTypeSymbol(t *testing.T, res *driver.DiagnoseResult, name string) types.TypeID {
	t.Helper()

	if res == nil || res.Symbols == nil || res.Symbols.Table == nil || res.Symbols.Table.Strings == nil || res.Symbols.Table.Symbols == nil {
		t.Fatal("missing symbols table")
	}
	if res.Sema == nil || res.Sema.TypeInterner == nil {
		t.Fatal("missing sema/type interner")
	}

	nameID := res.Symbols.Table.Strings.Intern(name)
	resolver := symbols.NewResolver(res.Symbols.Table, res.Symbols.FileScope, symbols.ResolverOptions{
		CurrentFile: res.FileID,
	})
	symID, ok := resolver.LookupOne(nameID, symbols.SymbolType.Mask())
	if !ok {
		t.Fatalf("type symbol %s not found", name)
	}
	sym := res.Symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Type == types.NoTypeID {
		t.Fatalf("invalid type symbol %s", name)
	}
	return sym.Type
}

func bagHasCode(bag *diag.Bag, code diag.Code) bool {
	if bag == nil {
		return false
	}
	for _, d := range bag.Items() {
		if d.Code == code {
			return true
		}
	}
	return false
}
