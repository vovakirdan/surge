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
	name := blockingStateTypePrefix + funcName
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterStruct(nameID, source.Span{})

	// Same lifecycle word, same field 0, as the other two frame kinds — so the
	// one predicate that decides who is a frame also decides who carries the
	// word, and at one offset.
	fields := make([]types.StructField, 0, len(captures)+1)
	fields = append(fields, types.StructField{
		Name: typesIn.Strings.Intern(FrameStateField),
		Type: typesIn.Builtins().Int,
	})
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
