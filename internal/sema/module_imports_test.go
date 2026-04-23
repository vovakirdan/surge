package sema

import (
	"testing"

	"surge/internal/symbols"
)

func TestModuleFunctionSignaturesEqualTreatsMissingBoolMetadataAsFalse(t *testing.T) {
	withoutMetadata := &symbols.FunctionSignature{
		Params: []symbols.TypeKey{"string"},
		Result: "JsonString",
	}
	withFalseMetadata := &symbols.FunctionSignature{
		Params:   []symbols.TypeKey{"string"},
		Variadic: []bool{false},
		Defaults: []bool{false},
		AllowTo:  []bool{false},
		Result:   "JsonString",
	}

	if !moduleFunctionSignaturesEqual(withoutMetadata, withFalseMetadata) {
		t.Fatalf("expected missing boolean metadata to match explicit false metadata")
	}

	withVariadic := &symbols.FunctionSignature{
		Params:   []symbols.TypeKey{"string"},
		Variadic: []bool{true},
		Result:   "JsonString",
	}
	if moduleFunctionSignaturesEqual(withoutMetadata, withVariadic) {
		t.Fatalf("expected true variadic metadata to differ from missing metadata")
	}
}
