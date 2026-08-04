// Package abimanifest owns the canonical Runtime V2 typed-carrier ABI schema.
package abimanifest

// Canonical schema constants identify the only accepted typed-carrier ABI.
const (
	SchemaName     = "surge.runtime.typed_carrier_abi"
	SchemaVersion  = uint32(2)
	SentinelPrefix = "__surge_runtime_abi_typed_carrier_v2_"
)

// Manifest is the target-independent source of truth for the typed-carrier ABI.
// Slice order is ABI-significant and is preserved by every generated view.
type Manifest struct {
	Schema           string     `json:"schema"`
	Version          uint32     `json:"version"`
	SentinelPrefix   string     `json:"sentinel_prefix"`
	Semantics        string     `json:"semantics"`
	Enums            []Enum     `json:"enums"`
	Records          []Record   `json:"records"`
	Callbacks        []Function `json:"callbacks"`
	RuntimeFunctions []Function `json:"runtime_functions"`
}

// Enum has a fixed-width C representation rather than implementation-defined
// C enum storage.
type Enum struct {
	Name       string      `json:"name"`
	Underlying string      `json:"underlying"`
	Semantics  string      `json:"semantics"`
	Values     []EnumValue `json:"values"`
}

// EnumValue assigns one fixed numeric value and its ownership semantics.
type EnumValue struct {
	Name      string `json:"name"`
	Value     uint64 `json:"value"`
	Semantics string `json:"semantics"`
}

// Record is one ordered public ABI record declaration.
type Record struct {
	Name      string  `json:"name"`
	Role      string  `json:"role"`
	Semantics string  `json:"semantics"`
	Fields    []Field `json:"fields"`
}

// Field is one ordered ABI record member.
type Field struct {
	Name      string  `json:"name"`
	Type      TypeRef `json:"type"`
	Semantics string  `json:"semantics"`
}

// TypeRef is deliberately small. It describes ABI-level scalar, named,
// callback-pointer, and data-pointer types without target byte sizes.
type TypeRef struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Const    *bool  `json:"const,omitempty"`
	Nullable *bool  `json:"nullable,omitempty"`
}

// Function describes a callback or exported runtime function contract.
type Function struct {
	Name       string      `json:"name"`
	Semantics  string      `json:"semantics"`
	Result     Result      `json:"result"`
	Parameters []Parameter `json:"parameters"`
	Effects    []string    `json:"effects"`
}

// Result describes a function result's ABI and ownership contract.
type Result struct {
	Type       TypeRef  `json:"type"`
	Ownership  string   `json:"ownership"`
	Attributes []string `json:"attributes"`
	Semantics  string   `json:"semantics"`
}

// Parameter describes one ordered function parameter contract.
type Parameter struct {
	Name       string   `json:"name"`
	Type       TypeRef  `json:"type"`
	Ownership  string   `json:"ownership"`
	Attributes []string `json:"attributes"`
	Semantics  string   `json:"semantics"`
}

// SchemaView is the compact logical declaration set consumed by compiler and
// VM parity tests. It intentionally contains no target offsets or TypeIDs.
type SchemaView struct {
	Hash      string
	Sentinel  string
	Enums     []EnumView
	Records   []RecordView
	Functions []FunctionView
}

// EnumView is the generated logical view of a fixed-width enum.
type EnumView struct {
	Name       string
	Underlying string
	Semantics  string
	Values     []NamedValueView
}

// NamedValueView is the generated logical view of one enum value.
type NamedValueView struct {
	Name      string
	Value     uint64
	Semantics string
}

// RecordView is the generated logical view of one ordered record.
type RecordView struct {
	Name      string
	Role      string
	Semantics string
	Fields    []FieldView
}

// FieldView is the generated logical view of one record field.
type FieldView struct {
	Name      string
	Type      string
	Semantics string
}

// FunctionView preserves the complete logical ABI contract of one function.
type FunctionView struct {
	Name       string
	Semantics  string
	Result     ResultView
	Parameters []ParameterView
	Effects    []string
}

// ResultView preserves generated result ABI and ownership semantics.
type ResultView struct {
	Type       string
	Ownership  string
	Attributes []string
	Semantics  string
}

// ParameterView preserves generated parameter ABI and ownership semantics.
type ParameterView struct {
	Name       string
	Type       string
	Ownership  string
	Attributes []string
	Semantics  string
}
