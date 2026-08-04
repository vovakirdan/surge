package vm

import (
	"reflect"
	"strings"
	"testing"

	"surge/internal/abimanifest"
)

func TestRuntimeV2TypedCarrierLogicalSchemaParity(t *testing.T) {
	if !reflect.DeepEqual(typedCarrierABISchema, abimanifest.GeneratedSchema) {
		t.Fatal("VM typed-carrier schema differs from the canonical generated Go view")
	}
	if typedCarrierABISchema.Hash != abimanifest.GeneratedManifestHash || typedCarrierABISchema.Sentinel != abimanifest.GeneratedSentinelSymbol {
		t.Fatalf("VM ABI identity = %q/%q", typedCarrierABISchema.Hash, typedCarrierABISchema.Sentinel)
	}
	for _, record := range typedCarrierABISchema.Records {
		for _, field := range record.Fields {
			for _, forbidden := range []string{"target_triple", "address_bits", "pointer_size", "type_id"} {
				if strings.Contains(strings.ToLower(field.Name+":"+field.Type), forbidden) {
					t.Fatalf("VM logical schema depends on target/program policy: %s.%s=%s", record.Name, field.Name, field.Type)
				}
			}
		}
	}
}

func TestRuntimeV2TypedCarrierHandleAndCrossPlanContract(t *testing.T) {
	handle := vmABIRecord(t, "rt_typed_carrier_handle")
	if handle.Role != "handle" || len(handle.Fields) != 1 || handle.Fields[0].Type != "uintptr" {
		t.Fatalf("language handle must remain one opaque word: %#v", handle)
	}
	plan := vmABIRecord(t, "rt_cross_plan")
	if len(plan.Fields) < 2 || plan.Fields[0].Name != "ops" || plan.Fields[1].Name != "mode" || plan.Fields[1].Type != "named:rt_cross_mode" {
		t.Fatalf("cross plan does not pin descriptor and operation mode: %#v", plan.Fields)
	}
	if !strings.Contains(plan.Fields[1].Semantics, "before touching source") {
		t.Fatalf("cross plan mode mismatch ownership rule missing: %q", plan.Fields[1].Semantics)
	}
}

func vmABIRecord(t *testing.T, name string) abimanifest.RecordView {
	t.Helper()
	for _, record := range typedCarrierABISchema.Records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("VM ABI record %q missing", name)
	return abimanifest.RecordView{}
}
