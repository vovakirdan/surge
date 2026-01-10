package mir

import (
	"fmt"

	"surge/internal/source"
	"surge/internal/types"
)

func buildBlockingStateStruct(typesIn *types.Interner, funcName string, captures []blockingCaptureInfo) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: blocking: missing type interner")
	}
	if funcName == "" {
		funcName = "anon"
	}
	name := fmt.Sprintf("__BlockingState$%s", funcName)
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterStruct(nameID, source.Span{})

	fields := make([]types.StructField, 0, len(captures))
	for _, cap := range captures {
		fieldName := cap.FieldName
		if fieldName == "" {
			fieldName = cap.Name
		}
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
