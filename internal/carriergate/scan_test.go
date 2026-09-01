package carriergate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScanIsLexicalCommentSafeAndDeterministic(t *testing.T) {
	files := map[string][]byte{
		"internal/backend/llvm/a.go":      []byte("package llvm\n// emitValueToI64 inttoptr\nvar _ = emitValueToI64\nvar _ = \"emitValueToI64 ptrtoint ptr %x to i64 inttoptronic\"\n"),
		"internal/backend/llvm/types.go":  []byte("package llvm\nfunc llvmType() string {\n switch tt.Kind {\n case types.KindStruct:\n  return \"ptr\"\n }\n return \"void\"\n}\n"),
		"internal/vm/- odd \n\tユ.go":      []byte("package vm\n// VKHandleStruct AllocTag\nvar _ = VKHandleStruct\nfunc f() { Heap.AllocTag() }\n"),
		"internal/asyncrt/a.go":           []byte("package asyncrt\n// any\ntype carrier struct { Value any }\nvar _ = \"any\"\n"),
		"runtime/native/a.c":              []byte("/* result_bits */ const char* s = \"send_bits\";\r\nuint64_t result_bits; uint64_t result_bits; uint64_t myresult_bits;\x00"),
		"runtime/native/a.h":              []byte("// uint64_t* buf; \\\nuint64_t* out_bits;\n/*\nconst uint64_t* out_bits;\n*/\nconst\nuint64_t\n*\nvalues;\nuint64_t* buf; uint64_t key; uint64_t value; uint64_t key_bits = 0; uint64_t value = 1; uint64_t state_drop_fn_id; uint64_t state_drop_fn_id_suffix;"),
		"internal/vm/ignored_test.go":     []byte("package vm\nvar _ = VKHandleTag\n"),
		"internal/vm/testdata/ignored.go": []byte("package vm\nvar _ = VKHandleTag\n"),
	}
	first := buildFixtureTree(t, files, false)
	second := buildFixtureTree(t, files, true)
	firstFindings, err := Scan(first)
	if err != nil {
		t.Fatalf("scan first fixture: %v", err)
	}
	secondFindings, err := Scan(second)
	if err != nil {
		t.Fatalf("scan reversed fixture: %v", err)
	}
	if !reflect.DeepEqual(firstFindings, secondFindings) || Digest(firstFindings) != Digest(secondFindings) {
		t.Fatal("scan depends on filesystem creation order")
	}

	wantTokens := map[string]int{
		categoryLLVMWordBridge + ":emitValueToI64":     1,
		categoryLLVMPointerWord + ":ptrtoint":          1,
		categoryLLVMCompositePtr + ":KindStruct->ptr":  1,
		categoryVMBoxKind + ":VKHandleStruct":          1,
		categoryCompositeBox + ":AllocTag":             1,
		categoryAsyncAny + ":any":                      1,
		categoryAsyncAny + ":carrier.Value->universal": 1,
		categoryNativePayloadBits + ":result_bits":     2,
		categoryNativeWord + ":buf":                    1,
		categoryNativeWord + ":key":                    1,
		categoryNativeWord + ":key_bits":               1,
		categoryNativeWord + ":value":                  1,
		categoryNativeWord + ":values":                 1,
		categoryNumericDrop + ":state_drop_fn_id":      1,
	}
	gotTokens := make(map[string]int)
	for _, finding := range firstFindings {
		gotTokens[finding.Category+":"+finding.Token]++
		if strings.Contains(finding.Evidence, "//") || strings.Contains(finding.Evidence, "/*") {
			t.Fatalf("comment became a finding: %+v", finding)
		}
	}
	if !reflect.DeepEqual(gotTokens, wantTokens) {
		t.Fatalf("tokens = %#v, want %#v", gotTokens, wantTokens)
	}
	assertDuplicateOrdinals(t, firstFindings, categoryNativePayloadBits, "result_bits", []uint64{1, 2})
	if !hasPathContaining(firstFindings, "- odd \n\tユ.go") {
		t.Fatal("special-character path was not preserved")
	}
}

func TestScanFailsClosedOnMalformedGo(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/vm/broken.go": []byte("package vm\nfunc broken( {\n"),
	}, false)
	if _, err := Scan(root); err == nil {
		t.Fatal("malformed production Go source was accepted")
	}
}

func TestScanRejectsSymlinksInProductionScopes(t *testing.T) {
	for _, name := range []string{"linked.go", "linked.txt"} {
		t.Run(name, func(t *testing.T) {
			root := buildFixtureTree(t, nil, false)
			target := filepath.Join(root, "outside.go")
			if err := os.WriteFile(target, []byte("package vm\nvar _ = VKHandleStruct\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			relative := filepath.ToSlash(filepath.Join("internal", "vm", name))
			link := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			_, err := Scan(root)
			if err == nil || !strings.Contains(err.Error(), "symlink") ||
				!strings.Contains(err.Error(), relative) {
				t.Fatalf("symlink error = %v, want actionable path", err)
			}
		})
	}
}

func TestDigestUsesLengthPrefixes(t *testing.T) {
	left := []Finding{{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "x\x00y", Evidence: "z", Ordinal: 1}}
	right := []Finding{{Category: categoryVMBoxKind, Path: "internal/vm/a.go", Token: "x", Evidence: "y\x00z", Ordinal: 1}}
	if Digest(left) == Digest(right) {
		t.Fatal("field-boundary collision in digest")
	}
	bad := left[0]
	bad.Path = "internal/vm/bad\x00.go"
	if err := validateFinding(&bad, bad.Category); err == nil {
		t.Fatal("NUL path accepted")
	}
}

func buildFixtureTree(t *testing.T, files map[string][]byte, reverse bool) string {
	t.Helper()
	root := t.TempDir()
	for _, scope := range requiredScopes {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(scope.Root)), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	slicesSort(paths)
	if reverse {
		for left, right := 0, len(paths)-1; left < right; left, right = left+1, right-1 {
			paths[left], paths[right] = paths[right], paths[left]
		}
	}
	for _, filePath := range paths {
		absolute := filepath.Join(root, filepath.FromSlash(filePath))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, files[filePath], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func assertDuplicateOrdinals(t *testing.T, findings []Finding, category, token string, want []uint64) {
	t.Helper()
	got := make([]uint64, 0, len(want))
	for _, finding := range findings {
		if finding.Category == category && finding.Token == token {
			got = append(got, finding.Ordinal)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordinals = %v, want %v", got, want)
	}
}

func hasPathContaining(findings []Finding, fragment string) bool {
	for _, finding := range findings {
		if strings.Contains(finding.Path, fragment) {
			return true
		}
	}
	return false
}
