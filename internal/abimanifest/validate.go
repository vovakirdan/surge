package abimanifest

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

var (
	cNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	cValuePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	allowedScalars   = stringSet("void", "u8", "u32", "u64", "usize", "uintptr")
	allowedInteger   = stringSet("u8", "u32", "u64")
	allowedRoles     = stringSet("layout", "cross_plan", "cross_allocator", "visitor", "operations", "slot_header", "descriptor_entry", "envelope_header", "handle")
	allowedAttrs     = stringSet("noalias", "nocapture", "nonnull", "readonly", "writeonly", "zeroext")
	allowedEffects   = stringSet("calls_user_clone", "calls_visitor", "may_allocate", "moves_source", "none", "plans_crossing", "reads_source", "returns_static_identity", "uses_plan_allocator", "writes_destination")
	allowedOwnership = stringSet(
		"borrowed", "borrowed_initialized", "context", "destination_empty",
		"hash", "none", "output_empty", "plan_limited_allocator", "predicate",
		"source_initialized", "status", "value", "whole_program_borrow",
	)
)

// Validate enforces a deliberately closed schema. It does not prescribe
// target sizes, alignments, offsets, program TypeIDs, or backend storage.
func Validate(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("missing manifest")
	}
	if manifest.Schema != SchemaName {
		return fmt.Errorf("schema = %q, want %q", manifest.Schema, SchemaName)
	}
	if manifest.Version != SchemaVersion {
		return fmt.Errorf("version = %d, want %d", manifest.Version, SchemaVersion)
	}
	if manifest.SentinelPrefix != SentinelPrefix {
		return fmt.Errorf("sentinel_prefix = %q, want %q", manifest.SentinelPrefix, SentinelPrefix)
	}
	if err := requireText("manifest semantics", manifest.Semantics); err != nil {
		return err
	}
	if len(manifest.Enums) == 0 || len(manifest.Records) == 0 || len(manifest.Callbacks) == 0 || len(manifest.RuntimeFunctions) == 0 {
		return fmt.Errorf("enums, records, callbacks, and runtime_functions must all be non-empty")
	}

	declarations := make(map[string]string)
	enumWidths := make(map[string]string, len(manifest.Enums))
	for index := range manifest.Enums {
		if err := reserveDeclaration(declarations, manifest.Enums[index].Name, "enum"); err != nil {
			return indexedError("enums", index, err)
		}
		enumWidths[manifest.Enums[index].Name] = manifest.Enums[index].Underlying
	}
	for index := range manifest.Records {
		if err := reserveDeclaration(declarations, manifest.Records[index].Name, "record"); err != nil {
			return indexedError("records", index, err)
		}
	}
	for index := range manifest.Callbacks {
		if err := reserveDeclaration(declarations, manifest.Callbacks[index].Name, "callback"); err != nil {
			return indexedError("callbacks", index, err)
		}
	}
	for index := range manifest.RuntimeFunctions {
		if err := reserveDeclaration(declarations, manifest.RuntimeFunctions[index].Name, "runtime function"); err != nil {
			return indexedError("runtime_functions", index, err)
		}
	}

	for index := range manifest.Enums {
		if err := validateEnum(&manifest.Enums[index]); err != nil {
			return indexedError("enums", index, err)
		}
	}
	for index := range manifest.Records {
		if err := validateRecord(&manifest.Records[index], declarations); err != nil {
			return indexedError("records", index, err)
		}
	}
	for index := range manifest.Callbacks {
		if err := validateFunction(&manifest.Callbacks[index], declarations, enumWidths, true); err != nil {
			return indexedError("callbacks", index, err)
		}
	}
	for index := range manifest.RuntimeFunctions {
		if err := validateFunction(&manifest.RuntimeFunctions[index], declarations, enumWidths, false); err != nil {
			return indexedError("runtime_functions", index, err)
		}
	}
	return nil
}

func validateEnum(enum *Enum) error {
	if !allowedInteger[enum.Underlying] {
		return fmt.Errorf("enum %q has unsupported underlying type %q", enum.Name, enum.Underlying)
	}
	if err := requireText("enum semantics", enum.Semantics); err != nil {
		return fmt.Errorf("enum %q: %w", enum.Name, err)
	}
	if len(enum.Values) == 0 {
		return fmt.Errorf("enum %q has no values", enum.Name)
	}
	maxValue := uint64(math.MaxUint64)
	switch enum.Underlying {
	case "u8":
		maxValue = math.MaxUint8
	case "u32":
		maxValue = math.MaxUint32
	}
	names := make(map[string]struct{})
	values := make(map[uint64]struct{})
	for index := range enum.Values {
		value := &enum.Values[index]
		if !cValuePattern.MatchString(value.Name) {
			return fmt.Errorf("enum %q value[%d] has invalid name %q", enum.Name, index, value.Name)
		}
		if _, exists := names[value.Name]; exists {
			return fmt.Errorf("enum %q has duplicate value name %q", enum.Name, value.Name)
		}
		if _, exists := values[value.Value]; exists {
			return fmt.Errorf("enum %q has duplicate numeric value %d", enum.Name, value.Value)
		}
		if value.Value > maxValue {
			return fmt.Errorf("enum %q value %q exceeds %s", enum.Name, value.Name, enum.Underlying)
		}
		if err := requireText("value semantics", value.Semantics); err != nil {
			return fmt.Errorf("enum %q value %q: %w", enum.Name, value.Name, err)
		}
		names[value.Name] = struct{}{}
		values[value.Value] = struct{}{}
	}
	return nil
}

func validateRecord(record *Record, declarations map[string]string) error {
	if !allowedRoles[record.Role] {
		return fmt.Errorf("record %q has unsupported role %q", record.Name, record.Role)
	}
	if err := requireText("record semantics", record.Semantics); err != nil {
		return fmt.Errorf("record %q: %w", record.Name, err)
	}
	if len(record.Fields) == 0 {
		return fmt.Errorf("record %q has no fields", record.Name)
	}
	seen := make(map[string]struct{})
	for index := range record.Fields {
		field := &record.Fields[index]
		if !cNamePattern.MatchString(field.Name) {
			return fmt.Errorf("record %q field[%d] has invalid name %q", record.Name, index, field.Name)
		}
		if _, exists := seen[field.Name]; exists {
			return fmt.Errorf("record %q has duplicate field %q", record.Name, field.Name)
		}
		if err := validateType(field.Type, declarations, false); err != nil {
			return fmt.Errorf("record %q field %q: %w", record.Name, field.Name, err)
		}
		if err := requireText("field semantics", field.Semantics); err != nil {
			return fmt.Errorf("record %q field %q: %w", record.Name, field.Name, err)
		}
		seen[field.Name] = struct{}{}
	}
	return nil
}

func validateFunction(function *Function, declarations, enumWidths map[string]string, callback bool) error {
	if callback && !strings.HasSuffix(function.Name, "_fn") {
		return fmt.Errorf("callback %q must end in _fn", function.Name)
	}
	if !callback && !strings.HasPrefix(function.Name, "rt_") {
		return fmt.Errorf("runtime function %q must start with rt_", function.Name)
	}
	if err := requireText("function semantics", function.Semantics); err != nil {
		return fmt.Errorf("function %q: %w", function.Name, err)
	}
	if err := validateType(function.Result.Type, declarations, true); err != nil {
		return fmt.Errorf("function %q result: %w", function.Name, err)
	}
	if err := validateOwnership(function.Result.Ownership); err != nil {
		return fmt.Errorf("function %q result: %w", function.Name, err)
	}
	if err := validateAttributes(function.Result.Type, function.Result.Attributes, declarations, enumWidths, true); err != nil {
		return fmt.Errorf("function %q result: %w", function.Name, err)
	}
	if err := requireText("result semantics", function.Result.Semantics); err != nil {
		return fmt.Errorf("function %q result: %w", function.Name, err)
	}
	if err := validateUniqueStrings("effect", function.Effects, allowedEffects); err != nil {
		return fmt.Errorf("function %q: %w", function.Name, err)
	}

	if function.Parameters == nil {
		return fmt.Errorf("function %q parameters must be an explicit array", function.Name)
	}
	seen := make(map[string]struct{})
	for index := range function.Parameters {
		parameter := &function.Parameters[index]
		if !cNamePattern.MatchString(parameter.Name) {
			return fmt.Errorf("function %q parameter[%d] has invalid name %q", function.Name, index, parameter.Name)
		}
		if _, exists := seen[parameter.Name]; exists {
			return fmt.Errorf("function %q has duplicate parameter %q", function.Name, parameter.Name)
		}
		if err := validateType(parameter.Type, declarations, false); err != nil {
			return fmt.Errorf("function %q parameter %q: %w", function.Name, parameter.Name, err)
		}
		if err := validateOwnership(parameter.Ownership); err != nil {
			return fmt.Errorf("function %q parameter %q: %w", function.Name, parameter.Name, err)
		}
		if err := validateAttributes(parameter.Type, parameter.Attributes, declarations, enumWidths, false); err != nil {
			return fmt.Errorf("function %q parameter %q: %w", function.Name, parameter.Name, err)
		}
		if err := requireText("parameter semantics", parameter.Semantics); err != nil {
			return fmt.Errorf("function %q parameter %q: %w", function.Name, parameter.Name, err)
		}
		seen[parameter.Name] = struct{}{}
	}
	return nil
}

func validateType(ref TypeRef, declarations map[string]string, allowVoid bool) error {
	switch ref.Kind {
	case "void", "u8", "u32", "u64", "usize", "uintptr":
		if !allowedScalars[ref.Kind] || (!allowVoid && ref.Kind == "void") {
			return fmt.Errorf("unsupported scalar type %q", ref.Kind)
		}
		if ref.Name != "" || ref.Const != nil || ref.Nullable != nil {
			return fmt.Errorf("scalar type %q has pointer/named fields", ref.Kind)
		}
	case "named":
		kind, exists := declarations[ref.Name]
		if !exists || (kind != "enum" && kind != "record") {
			return fmt.Errorf("unknown named type %q", ref.Name)
		}
		if ref.Const != nil || ref.Nullable != nil {
			return fmt.Errorf("named type %q has pointer fields", ref.Name)
		}
	case "callback":
		if declarations[ref.Name] != "callback" {
			return fmt.Errorf("unknown callback type %q", ref.Name)
		}
		if ref.Const != nil || ref.Nullable == nil {
			return fmt.Errorf("callback type %q must state nullable explicitly", ref.Name)
		}
	case "pointer":
		if ref.Name != "void" && ref.Name != "u8" && declarations[ref.Name] != "record" {
			return fmt.Errorf("pointer has unsupported pointee %q", ref.Name)
		}
		if ref.Const == nil || ref.Nullable == nil {
			return fmt.Errorf("pointer to %q must state const and nullable explicitly", ref.Name)
		}
	default:
		return fmt.Errorf("unsupported type kind %q", ref.Kind)
	}
	return nil
}

func validateAttributes(ref TypeRef, attributes []string, declarations, enumWidths map[string]string, result bool) error {
	if err := validateUniqueStrings("attribute", attributes, allowedAttrs); err != nil {
		return err
	}
	set := make(map[string]bool, len(attributes))
	for _, attribute := range attributes {
		set[attribute] = true
		if result && attribute != "nonnull" && attribute != "zeroext" {
			return fmt.Errorf("parameter-only attribute %q used on result", attribute)
		}
	}
	if set["readonly"] && set["writeonly"] {
		return fmt.Errorf("readonly and writeonly are mutually exclusive")
	}
	if ref.Kind != "pointer" && (set["noalias"] || set["nocapture"] || set["nonnull"] || set["readonly"] || set["writeonly"]) {
		return fmt.Errorf("pointer-only attribute used on %s", typeRefString(ref))
	}
	if ref.Kind == "pointer" && ref.Nullable != nil && !*ref.Nullable && !set["nonnull"] {
		return fmt.Errorf("non-null pointer %s must carry nonnull", typeRefString(ref))
	}
	if ref.Kind == "pointer" && ref.Nullable != nil && *ref.Nullable && set["nonnull"] {
		return fmt.Errorf("nullable pointer %s cannot carry nonnull", typeRefString(ref))
	}
	integerType := ref.Kind == "u8" || ref.Kind == "u32" || ref.Kind == "u64" || ref.Kind == "usize" || ref.Kind == "uintptr"
	if ref.Kind == "named" && declarations[ref.Name] == "enum" && allowedInteger[enumWidths[ref.Name]] {
		integerType = true
	}
	if set["zeroext"] && !integerType {
		return fmt.Errorf("zeroext used on unsupported type %s", typeRefString(ref))
	}
	return nil
}

func reserveDeclaration(declarations map[string]string, name, kind string) error {
	if !cNamePattern.MatchString(name) {
		return fmt.Errorf("invalid %s name %q", kind, name)
	}
	if previous, exists := declarations[name]; exists {
		return fmt.Errorf("declaration %q is both %s and %s", name, previous, kind)
	}
	declarations[name] = kind
	return nil
}

func validateOwnership(ownership string) error {
	if !allowedOwnership[ownership] {
		return fmt.Errorf("unsupported ownership %q", ownership)
	}
	return nil
}

func validateUniqueStrings(label string, values []string, allowed map[string]bool) error {
	if values == nil {
		return fmt.Errorf("%ss must be an explicit array", label)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !allowed[value] {
			return fmt.Errorf("unsupported %s %q", label, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func requireText(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be non-empty", label)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s has surrounding whitespace", label)
	}
	return nil
}

func indexedError(section string, index int, err error) error {
	return fmt.Errorf("%s[%d]: %w", section, index, err)
}

func stringSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func typeRefString(ref TypeRef) string {
	if ref.Kind == "named" {
		return ref.Kind + ":" + ref.Name
	}
	if ref.Kind == "callback" {
		nullability := "nonnull"
		if ref.Nullable != nil && *ref.Nullable {
			nullability = "nullable"
		}
		return ref.Kind + ":" + ref.Name + ":" + nullability
	}
	if ref.Kind == "pointer" {
		prefix := "mut"
		if ref.Const != nil && *ref.Const {
			prefix = "const"
		}
		nullability := "nonnull"
		if ref.Nullable != nil && *ref.Nullable {
			nullability = "nullable"
		}
		return prefix + "_ptr:" + ref.Name + ":" + nullability
	}
	return ref.Kind
}
