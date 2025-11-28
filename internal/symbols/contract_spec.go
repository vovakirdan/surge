package symbols

import (
	"surge/internal/source"
	"surge/internal/types"
)

// ContractMethod captures a single method requirement of a contract.
type ContractMethod struct {
	Name   source.StringID
	Params []types.TypeID
	Result types.TypeID
	Span   source.Span
	Attrs  []source.StringID
	Public bool
	Async  bool
}

// ContractSpec aggregates field and method requirements for a contract.
type ContractSpec struct {
	Fields     map[source.StringID]types.TypeID
	FieldAttrs map[source.StringID][]source.StringID
	Methods    map[source.StringID][]ContractMethod
}

// NewContractSpec allocates an empty contract spec with pre-sized maps.
func NewContractSpec() *ContractSpec {
	return &ContractSpec{
		Fields:     make(map[source.StringID]types.TypeID),
		FieldAttrs: make(map[source.StringID][]source.StringID),
		Methods:    make(map[source.StringID][]ContractMethod),
	}
}

// AddField registers a field requirement.
func (c *ContractSpec) AddField(name source.StringID, typ types.TypeID, attrs []source.StringID) {
	if c == nil || name == source.NoStringID {
		return
	}
	c.Fields[name] = typ
	if len(attrs) > 0 {
		c.FieldAttrs[name] = append([]source.StringID(nil), attrs...)
	}
}

// AddMethod registers a method requirement.
func (c *ContractSpec) AddMethod(m *ContractMethod) {
	if c == nil || m == nil || m.Name == source.NoStringID {
		return
	}
	clone := ContractMethod{
		Name:   m.Name,
		Params: append([]types.TypeID(nil), m.Params...),
		Result: m.Result,
		Span:   m.Span,
		Attrs:  append([]source.StringID(nil), m.Attrs...),
		Public: m.Public,
		Async:  m.Async,
	}
	c.Methods[m.Name] = append(c.Methods[m.Name], clone)
}

func cloneContractSpec(spec *ContractSpec) *ContractSpec {
	if spec == nil {
		return nil
	}
	out := NewContractSpec()
	for name, typ := range spec.Fields {
		out.Fields[name] = typ
	}
	for name, attrs := range spec.FieldAttrs {
		out.FieldAttrs[name] = append([]source.StringID(nil), attrs...)
	}
	for _, methods := range spec.Methods {
		for i := range methods {
			out.AddMethod(&methods[i])
		}
	}
	return out
}

// CloneContractSpec produces a deep copy of the provided contract spec.
func CloneContractSpec(spec *ContractSpec) *ContractSpec {
	return cloneContractSpec(spec)
}
