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

func TestCrossPlanIsSelfContained(t *testing.T) {
	manifest := loadTestManifest(t)
	for _, record := range manifest.Records {
		if record.Name == "rt_cross_sidecar_shape" {
			t.Fatal("cross plan still exposes an instance-dependent sidecar shape record")
		}
	}
	plan := findRecord(t, manifest, "rt_cross_plan")
	wantFields := []string{
		"ops",
		"mode",
		"envelope_bytes",
		"payload_offset",
		"payload_bytes",
		"payload_align",
		"sidecar_bytes",
		"total_bytes",
		"sidecar_count",
	}
	gotFields := make([]string, 0, len(plan.Fields))
	for _, field := range plan.Fields {
		gotFields = append(gotFields, field.Name)
		if field.Name == "ops" {
			if field.Type.Kind != "pointer" || field.Type.Name != "rt_value_ops" || field.Type.Const == nil || !*field.Type.Const || field.Type.Nullable == nil || *field.Type.Nullable {
				t.Fatalf("cross plan ops is not a non-null process-static descriptor: %#v", field)
			}
			continue
		}
		if field.Type.Kind == "pointer" {
			t.Fatalf("cross plan contains an instance-dependent pointer: %#v", field)
		}
	}
	if !slices.Equal(gotFields, wantFields) {
		t.Fatalf("cross plan fields = %v, want self-contained POD fields %v", gotFields, wantFields)
	}
	for _, phrase := range []string{"pod", "self-contained", "no instance-dependent pointer", "concurrent or reentrant", "no plan cleanup"} {
		if !strings.Contains(strings.ToLower(plan.Semantics), phrase) {
			t.Fatalf("cross plan semantics do not freeze %q: %q", phrase, plan.Semantics)
		}
	}
}

func TestCrossAllocatorExactAllowanceContract(t *testing.T) {
	tests := []struct {
		name               string
		plannedBytes       uintptr
		plannedAllocations uintptr
		attempts           []crossAllocationAttempt
		want               crossContractStatus
	}{
		{name: "exact", plannedBytes: 8, plannedAllocations: 2, attempts: []crossAllocationAttempt{acceptedCrossAllocation(3), acceptedCrossAllocation(5)}, want: crossContractOK},
		{name: "bytes under-consumed", plannedBytes: 8, plannedAllocations: 2, attempts: []crossAllocationAttempt{acceptedCrossAllocation(3), acceptedCrossAllocation(4)}, want: crossContractPlanMismatch},
		{name: "bytes over-consumed", plannedBytes: 8, plannedAllocations: 2, attempts: []crossAllocationAttempt{acceptedCrossAllocation(3), acceptedCrossAllocation(6)}, want: crossContractPlanMismatch},
		{name: "count under-consumed", plannedBytes: 8, plannedAllocations: 3, attempts: []crossAllocationAttempt{acceptedCrossAllocation(3), acceptedCrossAllocation(5)}, want: crossContractPlanMismatch},
		{name: "count over-consumed", plannedBytes: 8, plannedAllocations: 1, attempts: []crossAllocationAttempt{acceptedCrossAllocation(8), acceptedCrossAllocation(1)}, want: crossContractPlanMismatch},
		{name: "zero-size phantom", plannedBytes: 0, plannedAllocations: 0, attempts: []crossAllocationAttempt{acceptedCrossAllocation(0)}, want: crossContractPlanMismatch},
		{name: "allocator refusal", plannedBytes: 8, plannedAllocations: 1, attempts: []crossAllocationAttempt{refusedCrossAllocation(8)}, want: crossContractCapacity},
		{name: "allocator result size mismatch", plannedBytes: 8, plannedAllocations: 1, attempts: []crossAllocationAttempt{{size: 8, align: 1, accepted: true, returnedSize: 7, returnedAlign: 1}}, want: crossContractPlanMismatch},
		{name: "allocator result alignment mismatch", plannedBytes: 8, plannedAllocations: 1, attempts: []crossAllocationAttempt{{size: 8, align: 8, accepted: true, returnedSize: 8, returnedAlign: 4}}, want: crossContractPlanMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := applyCrossAllowanceContract(test.plannedBytes, test.plannedAllocations, test.attempts)
			if result.status != test.want {
				t.Fatalf("status = %s, want %s", result.status, test.want)
			}
			if test.want != crossContractOK && (!result.sourceOwned || !result.destinationEmpty) {
				t.Fatalf("failure did not preserve source and empty destination: %#v", result)
			}
			if test.name == "allocator refusal" {
				if result.remainingBytes != test.plannedBytes || result.remainingAllocations != test.plannedAllocations {
					t.Fatalf("refusal consumed allowance: %#v", result)
				}
			}
			if strings.HasPrefix(test.name, "allocator result") && (result.remainingBytes != test.plannedBytes || result.remainingAllocations != test.plannedAllocations) {
				t.Fatalf("invalid allocator result consumed allowance: %#v", result)
			}
			if test.name == "zero-size phantom" && result.callbackCalls != 0 {
				t.Fatalf("zero-size request reached allocator callback: %#v", result)
			}
			if test.want == crossContractOK && (result.remainingBytes != 0 || result.remainingAllocations != 0) {
				t.Fatalf("success retained allowance: %#v", result)
			}
			if test.want == crossContractOK && result.successfulCalls != test.plannedAllocations {
				t.Fatalf("sidecar_count does not equal successful calls: %#v", result)
			}
		})
	}

	manifest := loadTestManifest(t)
	allocator := findRecord(t, manifest, "rt_cross_allocator")
	gotFields := make([]string, 0, len(allocator.Fields))
	for _, field := range allocator.Fields {
		gotFields = append(gotFields, field.Name)
	}
	if want := []string{"context", "allocate", "remaining_bytes", "remaining_allocations"}; !slices.Equal(gotFields, want) {
		t.Fatalf("allocator fields = %v, want %v", gotFields, want)
	}
	for _, phrase := range []string{"only after", "refusal leaves both allowances unchanged", "zero-size", "plan_mismatch"} {
		if !strings.Contains(strings.ToLower(allocator.Semantics), phrase) {
			t.Fatalf("allocator semantics do not freeze %q: %q", phrase, allocator.Semantics)
		}
	}
}

func TestCrossCallbacksFreezePreflightRewalkAndRollbackContract(t *testing.T) {
	manifest := loadTestManifest(t)
	planCross := findCallback(t, manifest, "rt_value_plan_cross_fn")
	if want := []string{"plans_crossing", "reads_source", "writes_destination"}; !slices.Equal(planCross.Effects, want) {
		t.Fatalf("plan_cross must remain read-only and non-allocating: got effects %v, want %v", planCross.Effects, want)
	}
	assertSemanticsContains(t, "plan_cross", planCross.Semantics,
		"exact-byte", "exact-allocation-count", "exclusively movable", "immutable and pinned", "through both plan and apply")
	assertSemanticsContains(t, "plan_cross result", planCross.Result.Semantics,
		"self-contained plan", "source owned", "no plan cleanup")
	assertSemanticsContains(t, "plan_cross source", planCross.Parameters[0].Semantics,
		"exclusively movable", "immutable and pinned", "continuously through plan and apply")

	allocatorCallback := findCallback(t, manifest, "rt_cross_alloc_fn")
	assertSemanticsContains(t, "cross allocator callback", allocatorCallback.Semantics,
		"nonzero sidecar", "exactly the requested size", "one allocation", "refusal leaves both allowances unchanged")
	assertSemanticsContains(t, "cross allocator result", allocatorCallback.Result.Semantics,
		"exact nonzero request", "allowances unchanged", "zero-size request", "plan_mismatch", "before this callback is invoked")

	for _, name := range []string{"rt_value_cross_move_init_fn", "rt_value_cross_clone_init_fn"} {
		callback := findCallback(t, manifest, name)
		assertSemanticsContains(t, name, callback.Semantics,
			"deterministically re-walking", "same", "ops", "mode", "layout", "totals mismatch", "before source commit")
		assertSemanticsContains(t, name+" result", callback.Result.Semantics,
			"plan_mismatch", "partial destination", "empty", "source own", "remaining_bytes", "remaining_allocations", "zero")
		allocator := callback.Parameters[len(callback.Parameters)-1]
		assertSemanticsContains(t, name+" allocator", allocator.Semantics,
			"sidecar_bytes", "sidecar_count", "refusal leaves allowances unchanged", "both reach zero")
	}

	plan := findRecord(t, manifest, "rt_cross_plan")
	for _, fieldName := range []string{"sidecar_bytes", "sidecar_count"} {
		for _, field := range plan.Fields {
			if field.Name != fieldName {
				continue
			}
			assertSemanticsContains(t, "rt_cross_plan."+fieldName, field.Semantics, "exact", "successful", "sidecar allocator calls")
			break
		}
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

type crossContractStatus string

const (
	crossContractOK           crossContractStatus = "OK"
	crossContractCapacity     crossContractStatus = "CAPACITY"
	crossContractPlanMismatch crossContractStatus = "PLAN_MISMATCH"
)

type crossAllocationAttempt struct {
	size          uintptr
	align         uintptr
	accepted      bool
	returnedSize  uintptr
	returnedAlign uintptr
}

type crossAllowanceResult struct {
	status               crossContractStatus
	remainingBytes       uintptr
	remainingAllocations uintptr
	callbackCalls        uintptr
	successfulCalls      uintptr
	sourceOwned          bool
	destinationEmpty     bool
}

func acceptedCrossAllocation(size uintptr) crossAllocationAttempt {
	return crossAllocationAttempt{size: size, align: 1, accepted: true, returnedSize: size, returnedAlign: 1}
}

func refusedCrossAllocation(size uintptr) crossAllocationAttempt {
	return crossAllocationAttempt{size: size, align: 1, accepted: false}
}

// applyCrossAllowanceContract is a small executable oracle for the canonical
// manifest semantics. Production crossing arrives in a later Epic 23b wave;
// this keeps byte/count rollback behavior testable at the ABI-contract layer.
func applyCrossAllowanceContract(plannedBytes, plannedAllocations uintptr, attempts []crossAllocationAttempt) crossAllowanceResult {
	result := crossAllowanceResult{
		status:               crossContractOK,
		remainingBytes:       plannedBytes,
		remainingAllocations: plannedAllocations,
		sourceOwned:          true,
		destinationEmpty:     true,
	}
	rollback := func(status crossContractStatus) crossAllowanceResult {
		result.status = status
		result.sourceOwned = true
		result.destinationEmpty = true
		return result
	}
	for _, attempt := range attempts {
		if attempt.size == 0 || attempt.align == 0 || attempt.size > result.remainingBytes || result.remainingAllocations == 0 {
			return rollback(crossContractPlanMismatch)
		}
		result.callbackCalls++
		if !attempt.accepted {
			return rollback(crossContractCapacity)
		}
		if attempt.returnedSize != attempt.size {
			return rollback(crossContractPlanMismatch)
		}
		if attempt.returnedAlign < attempt.align || attempt.returnedAlign%attempt.align != 0 {
			return rollback(crossContractPlanMismatch)
		}
		result.remainingBytes -= attempt.size
		result.remainingAllocations--
		result.successfulCalls++
		result.destinationEmpty = false
	}
	if result.remainingBytes != 0 || result.remainingAllocations != 0 {
		return rollback(crossContractPlanMismatch)
	}
	result.sourceOwned = false
	return result
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

func findRecord(t *testing.T, manifest Manifest, name string) Record {
	t.Helper()
	for _, record := range manifest.Records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("record %q missing", name)
	return Record{}
}

func assertSemanticsContains(t *testing.T, subject, semantics string, phrases ...string) {
	t.Helper()
	lower := strings.ToLower(semantics)
	for _, phrase := range phrases {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			t.Fatalf("%s semantics do not freeze %q: %q", subject, phrase, semantics)
		}
	}
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
