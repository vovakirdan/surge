package mir

import (
	"fmt"

	"surge/internal/source"
	"surge/internal/types"
)

func buildSpawnOnStateStruct(typesIn *types.Interner, funcName string, captures []spawnOnCaptureInfo) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: spawn_on: missing type interner")
	}
	if funcName == "" {
		funcName = "anon"
	}
	name := spawnOnStateTypePrefix + funcName
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterStruct(nameID, source.Span{})

	// Same lifecycle word, same field 0, as the async frame. IsSuspensionFrameType
	// answers for all three kinds with one predicate, and a reader that trusts
	// that predicate finds the word at one offset whichever kind it got.
	//
	// It finds it only once it HAS a frame, and a crossing is the kind that may
	// not: a capture-less body is handed a null state, and this type is then
	// describing storage nobody allocated. The word is skipped in the literal
	// for exactly that count, so the type declaring a field here is not a
	// promise that a pointer of this type is non-null. A reader must ask that
	// first; the other two kinds always have the storage.
	fields := make([]types.StructField, 0, len(captures)+1)
	fields = append(fields, types.StructField{
		Name: typesIn.Strings.Intern(FrameStateField),
		Type: typesIn.Builtins().Int,
	})
	for _, cap := range captures {
		fieldName := cap.FieldName
		if fieldName == "" {
			fieldName = "__cap"
		}
		fields = append(fields, types.StructField{
			Name: typesIn.Strings.Intern(fieldName),
			Type: cap.Type,
		})
	}
	typesIn.SetStructFields(stateID, fields)
	return stateID, nil
}
