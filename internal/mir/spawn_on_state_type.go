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
	name := fmt.Sprintf("__SpawnOnState$%s", funcName)
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterStruct(nameID, source.Span{})

	fields := make([]types.StructField, 0, len(captures))
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
