package abimanifest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCanonicalManifestRoundTripAndGeneratedViews(t *testing.T) {
	root := testRepoRoot(t)
	manifest, canonical, err := LoadCanonical(filepath.Join(root, ManifestPath))
	if err != nil {
		t.Fatalf("load canonical manifest: %v", err)
	}
	if got := ContentHash(canonical); got != GeneratedManifestHash {
		t.Fatalf("content hash = %q, generated = %q", got, GeneratedManifestHash)
	}
	if GeneratedSentinelSymbol != SentinelPrefix+GeneratedManifestHash {
		t.Fatalf("sentinel = %q, want exact hash symbol", GeneratedSentinelSymbol)
	}
	wantView := BuildSchemaView(&manifest, GeneratedManifestHash)
	if !reflect.DeepEqual(GeneratedSchema, wantView) {
		gotJSON, _ := json.Marshal(GeneratedSchema)
		wantJSON, _ := json.Marshal(wantView)
		t.Fatalf("generated Go schema does not preserve the complete canonical logical view\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
	if err := CheckRepository(root); err != nil {
		t.Fatalf("checked-in generated views: %v", err)
	}
}

func TestManifestRejectsMalformedOrAmbiguousInput(t *testing.T) {
	canonical := readCanonicalManifest(t)
	tests := map[string][]byte{
		"duplicate key":  bytes.Replace(canonical, []byte(`"schema": "surge.runtime.typed_carrier_abi",`), []byte(`"schema": "surge.runtime.typed_carrier_abi", "schema": "surge.runtime.typed_carrier_abi",`), 1),
		"unknown field":  bytes.Replace(canonical, []byte(`"version": 2,`), []byte(`"version": 2, "target_triple": "forbidden",`), 1),
		"trailing value": append(append([]byte(nil), canonical...), []byte("{}\n")...),
		"empty input":    nil,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatal("Parse unexpectedly accepted malformed manifest")
			}
		})
	}
}

func TestManifestRejectsInvalidLLVMAttributePositionsAndTypes(t *testing.T) {
	base := loadTestManifest(t)
	tests := map[string]func(*Manifest){
		"parameter-only result attribute": func(manifest *Manifest) {
			manifest.RuntimeFunctions[0].Result.Attributes = []string{"readonly"}
		},
		"zeroext record result": func(manifest *Manifest) {
			manifest.RuntimeFunctions[0].Result.Type = TypeRef{Kind: "named", Name: "rt_value_layout"}
			manifest.RuntimeFunctions[0].Result.Attributes = []string{"zeroext"}
		},
		"pointer attribute on integer": func(manifest *Manifest) {
			manifest.Callbacks[0].Parameters[0].Type = TypeRef{Kind: "u64"}
			manifest.Callbacks[0].Parameters[0].Attributes = []string{"nocapture"}
		},
		"nullable marked nonnull": func(manifest *Manifest) {
			manifest.Callbacks[0].Parameters[0].Attributes = []string{"nonnull"}
		},
		"nonnull missing attribute": func(manifest *Manifest) {
			manifest.Callbacks[0].Parameters[1].Attributes = []string{"readonly"}
		},
		"implicit pointer nullability": func(manifest *Manifest) {
			manifest.Callbacks[0].Parameters[1].Type.Nullable = nil
		},
		"implicit function parameters": func(manifest *Manifest) {
			manifest.RuntimeFunctions[0].Parameters = nil
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			manifest := cloneManifest(t, base)
			mutate(&manifest)
			if err := Validate(&manifest); err == nil {
				t.Fatal("Validate unexpectedly accepted an invalid LLVM ABI contract")
			}
		})
	}
}

func TestManifestMemoryAttributesMatchCallbackEffects(t *testing.T) {
	manifest := loadTestManifest(t)
	var readonly, writeonly []string
	for _, callback := range manifest.Callbacks {
		for _, parameter := range callback.Parameters {
			key := callback.Name + "." + parameter.Name
			if slices.Contains(parameter.Attributes, "readonly") {
				readonly = append(readonly, key)
			}
			if slices.Contains(parameter.Attributes, "writeonly") {
				writeonly = append(writeonly, key)
			}
		}
	}
	slices.Sort(readonly)
	slices.Sort(writeonly)
	wantReadonly := []string{
		"rt_key_equal_fn.a",
		"rt_key_equal_fn.b",
		"rt_key_hash_fn.key",
		"rt_trace_visit_fn.root",
		"rt_value_clone_init_fn.src",
		"rt_value_copy_init_fn.src",
		"rt_value_cross_clone_init_fn.plan",
		"rt_value_cross_clone_init_fn.src",
		"rt_value_cross_move_init_fn.plan",
		"rt_value_plan_cross_fn.src",
		"rt_value_trace_fn.value",
		"rt_value_trace_fn.visitor",
	}
	wantWriteonly := []string{
		"rt_cross_alloc_fn.out",
		"rt_value_clone_init_fn.dst",
		"rt_value_copy_init_fn.dst",
		"rt_value_move_init_fn.dst",
		"rt_value_plan_cross_fn.out",
	}
	if !slices.Equal(readonly, wantReadonly) {
		t.Fatalf("readonly callback parameters require an effects audit: got %v, want %v", readonly, wantReadonly)
	}
	if !slices.Equal(writeonly, wantWriteonly) {
		t.Fatalf("writeonly callback parameters require an effects audit: got %v, want %v", writeonly, wantWriteonly)
	}
}

func TestManifestCrossInitDestinationAllowsRollbackReads(t *testing.T) {
	manifest := loadTestManifest(t)
	for _, name := range []string{"rt_value_cross_move_init_fn", "rt_value_cross_clone_init_fn"} {
		callback := findCallback(t, manifest, name)
		destination := callback.Parameters[0]
		if destination.Name != "dst" {
			t.Fatalf("%s first parameter = %q, want dst", name, destination.Name)
		}
		if slices.Contains(destination.Attributes, "writeonly") {
			t.Fatalf("%s.dst cannot be writeonly: returned-failure rollback reads partially initialized handles and active tags", name)
		}
		for _, required := range []string{"nonnull", "nocapture", "noalias"} {
			if !slices.Contains(destination.Attributes, required) {
				t.Fatalf("%s.dst lost %s after removing invalid writeonly", name, required)
			}
		}
		if !strings.Contains(callback.Result.Semantics, "rolls destination back to empty") {
			t.Fatalf("%s does not document the rollback that requires destination reads", name)
		}
	}
}

func TestLogicalViewPreservesResultAndStatusSemantics(t *testing.T) {
	manifest := loadTestManifest(t)
	view := BuildSchemaView(&manifest, "hash")
	plan := findFunctionView(t, view, "rt_value_plan_cross_fn")
	if plan.Result.Ownership != "status" || !reflect.DeepEqual(plan.Result.Attributes, []string{"zeroext"}) {
		t.Fatalf("plan result contract lost: %#v", plan.Result)
	}
	if !strings.Contains(plan.Result.Semantics, "source owned") {
		t.Fatalf("plan status semantics lost: %q", plan.Result.Semantics)
	}
	status := findEnumView(t, view, "rt_carrier_status")
	for _, value := range status.Values {
		if strings.TrimSpace(value.Semantics) == "" {
			t.Fatalf("status %s lost semantic contract", value.Name)
		}
		if strings.Contains(value.Name, "ALLOCATION") {
			t.Fatalf("allocator exhaustion leaked into returned carrier status: %s", value.Name)
		}
	}
	planRecord := findRecordView(t, view, "rt_cross_plan")
	if len(planRecord.Fields) < 2 || planRecord.Fields[1].Name != "mode" || planRecord.Fields[1].Type != "named:rt_cross_mode" || !strings.Contains(planRecord.Fields[1].Semantics, "before touching source") {
		t.Fatalf("cross plan does not freeze move/clone mode invariant: %#v", planRecord.Fields)
	}
}

func TestGeneratorIsDeterministicAndFailsOnStaleView(t *testing.T) {
	root := testRepoRoot(t)
	manifest, canonical, err := LoadCanonical(filepath.Join(root, ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Generate(&manifest, canonical)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(&manifest, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same canonical manifest produced different generated bytes")
	}

	tempRoot := t.TempDir()
	manifestPath := filepath.Join(tempRoot, ManifestPath)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteRepository(tempRoot); err != nil {
		t.Fatalf("write generated fixture: %v", err)
	}
	if err := CheckRepository(tempRoot); err != nil {
		t.Fatalf("fresh generated fixture rejected: %v", err)
	}
	stalePath := filepath.Join(tempRoot, generatedPaths[0])
	if err := os.WriteFile(stalePath, append(first[generatedPaths[0]], []byte("// stale\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckRepository(tempRoot); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale generated view error = %v", err)
	}
}

func TestManifestHasNoTargetOrProgramTypePolicy(t *testing.T) {
	canonical := readCanonicalManifest(t)
	for _, forbidden := range []string{"target_triple", "pointer_size", "address_bits", "max_abi_align", "TypeID", "type_id", "field_offset"} {
		if bytes.Contains(canonical, []byte(forbidden)) {
			t.Fatalf("target/program-specific policy %q leaked into process-wide manifest", forbidden)
		}
	}
}

func cloneManifest(t *testing.T, manifest Manifest) Manifest {
	t.Helper()
	canonical, err := CanonicalBytes(&manifest)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := Parse(canonical)
	if err != nil {
		t.Fatal(err)
	}
	return clone
}

func loadTestManifest(t *testing.T) Manifest {
	t.Helper()
	manifest, _, err := LoadCanonical(filepath.Join(testRepoRoot(t), ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readCanonicalManifest(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testRepoRoot(t), ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func findFunctionView(t *testing.T, view SchemaView, name string) FunctionView {
	t.Helper()
	for _, function := range view.Functions {
		if function.Name == name {
			return function
		}
	}
	t.Fatalf("function view %q missing", name)
	return FunctionView{}
}

func findCallback(t *testing.T, manifest Manifest, name string) Function {
	t.Helper()
	for _, callback := range manifest.Callbacks {
		if callback.Name == name {
			return callback
		}
	}
	t.Fatalf("callback %q missing", name)
	return Function{}
}

func findEnumView(t *testing.T, view SchemaView, name string) EnumView {
	t.Helper()
	for _, enum := range view.Enums {
		if enum.Name == name {
			return enum
		}
	}
	t.Fatalf("enum view %q missing", name)
	return EnumView{}
}

func findRecordView(t *testing.T, view SchemaView, name string) RecordView {
	t.Helper()
	for _, record := range view.Records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("record view %q missing", name)
	return RecordView{}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate repository root: %v", err)
	}
	return root
}
