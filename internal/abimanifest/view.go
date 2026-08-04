package abimanifest

// BuildSchemaView projects the full logical ABI without target layout policy.
func BuildSchemaView(manifest *Manifest, hash string) SchemaView {
	view := SchemaView{
		Hash:      hash,
		Sentinel:  manifest.SentinelPrefix + hash,
		Enums:     make([]EnumView, 0, len(manifest.Enums)),
		Records:   make([]RecordView, 0, len(manifest.Records)),
		Functions: make([]FunctionView, 0, len(manifest.Callbacks)+len(manifest.RuntimeFunctions)),
	}
	for _, enum := range manifest.Enums {
		enumView := EnumView{Name: enum.Name, Underlying: enum.Underlying, Semantics: enum.Semantics, Values: make([]NamedValueView, 0, len(enum.Values))}
		for _, value := range enum.Values {
			enumView.Values = append(enumView.Values, NamedValueView(value))
		}
		view.Enums = append(view.Enums, enumView)
	}
	for _, record := range manifest.Records {
		recordView := RecordView{Name: record.Name, Role: record.Role, Semantics: record.Semantics, Fields: make([]FieldView, 0, len(record.Fields))}
		for _, field := range record.Fields {
			recordView.Fields = append(recordView.Fields, FieldView{Name: field.Name, Type: typeRefString(field.Type), Semantics: field.Semantics})
		}
		view.Records = append(view.Records, recordView)
	}
	functions := append(append([]Function(nil), manifest.Callbacks...), manifest.RuntimeFunctions...)
	for index := range functions {
		function := &functions[index]
		functionView := FunctionView{
			Name:       function.Name,
			Semantics:  function.Semantics,
			Parameters: make([]ParameterView, 0, len(function.Parameters)),
			Result: ResultView{
				Type:       typeRefString(function.Result.Type),
				Ownership:  function.Result.Ownership,
				Attributes: append([]string{}, function.Result.Attributes...),
				Semantics:  function.Result.Semantics,
			},
			Effects: append([]string{}, function.Effects...),
		}
		for _, parameter := range function.Parameters {
			functionView.Parameters = append(functionView.Parameters, ParameterView{
				Name:       parameter.Name,
				Type:       typeRefString(parameter.Type),
				Ownership:  parameter.Ownership,
				Attributes: append([]string{}, parameter.Attributes...),
				Semantics:  parameter.Semantics,
			})
		}
		view.Functions = append(view.Functions, functionView)
	}
	return view
}
