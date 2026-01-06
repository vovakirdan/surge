package ast

type (
	// FileID identifies a source file.
	FileID uint32
	// ItemID identifies a top-level item.
	ItemID uint32
	// StmtID identifies a statement.
	StmtID uint32
	// ExprID identifies an expression.
	ExprID uint32
	// TypeID identifies a type expression.
	TypeID uint32
	// PayloadID indexes auxiliary expression payload data.
	PayloadID uint32
	// FnParamID identifies a function parameter.
	FnParamID uint32
	// TypeParamID identifies a type parameter.
	TypeParamID uint32
	// TypeParamBoundID identifies a type parameter bound.
	TypeParamBoundID uint32
	// ContractDeclID identifies a contract declaration.
	ContractDeclID uint32
	// ContractItemID identifies a contract item.
	ContractItemID uint32
	// ContractFieldID identifies a contract field.
	ContractFieldID uint32
	// ContractFnID identifies a contract function.
	ContractFnID uint32
	// AttrID identifies an attribute.
	AttrID uint32
	// ExternMemberID identifies a member of an extern block.
	ExternMemberID uint32
	// ExternFieldID identifies a field in an extern block.
	ExternFieldID uint32
	// ExternBlockID identifies an extern block.
	ExternBlockID uint32
	// TypeFieldID identifies a field in a struct type.
	TypeFieldID uint32
	// TypeUnionMemberID identifies a member of a union type.
	TypeUnionMemberID uint32
	// EnumVariantID identifies a variant of an enum type.
	EnumVariantID uint32
)

const (
	// NoFileID indicates no file.
	NoFileID           FileID            = 0
	// NoItemID indicates no item.
	NoItemID           ItemID            = 0
	NoStmtID           StmtID            = 0
	NoExprID           ExprID            = 0
	NoTypeID           TypeID            = 0
	NoPayloadID        PayloadID         = 0
	NoFnParamID        FnParamID         = 0
	NoTypeParamID      TypeParamID       = 0
	NoTypeParamBoundID TypeParamBoundID  = 0
	NoContractDeclID   ContractDeclID    = 0
	NoContractItemID   ContractItemID    = 0
	NoContractFieldID  ContractFieldID   = 0
	NoContractFnID     ContractFnID      = 0
	NoAttrID           AttrID            = 0
	NoExternMemberID   ExternMemberID    = 0
	NoExternFieldID    ExternFieldID     = 0
	NoExternBlockID    ExternBlockID     = 0
	NoTypeFieldID      TypeFieldID       = 0
	NoTypeUnionMember  TypeUnionMemberID = 0
	NoEnumVariantID    EnumVariantID     = 0
)

// IsValid reports whether the FileID is valid (non-zero).
func (id FileID) IsValid() bool { return id != NoFileID }

// IsValid reports whether the ItemID is valid (non-zero).
func (id ItemID) IsValid() bool { return id != NoItemID }

// IsValid reports whether the StmtID is valid (non-zero).
func (id StmtID) IsValid() bool { return id != NoStmtID }

// IsValid reports whether the ExprID is valid (non-zero).
func (id ExprID) IsValid() bool { return id != NoExprID }

// IsValid reports whether the TypeID is valid (non-zero).
func (id TypeID) IsValid() bool { return id != NoTypeID }

// IsValid reports whether the PayloadID is valid (non-zero).
func (id PayloadID) IsValid() bool { return id != NoPayloadID }

// IsValid reports whether the FnParamID is valid (non-zero).
func (id FnParamID) IsValid() bool { return id != NoFnParamID }

// IsValid reports whether the TypeParamID is valid (non-zero).
func (id TypeParamID) IsValid() bool { return id != NoTypeParamID }

// IsValid reports whether the TypeParamBoundID is valid (non-zero).
func (id TypeParamBoundID) IsValid() bool { return id != NoTypeParamBoundID }

// IsValid reports whether the ContractDeclID is valid (non-zero).
func (id ContractDeclID) IsValid() bool { return id != NoContractDeclID }

// IsValid reports whether the ContractItemID is valid (non-zero).
func (id ContractItemID) IsValid() bool { return id != NoContractItemID }

// IsValid reports whether the ContractFieldID is valid (non-zero).
func (id ContractFieldID) IsValid() bool { return id != NoContractFieldID }

// IsValid reports whether the ContractFnID is valid (non-zero).
func (id ContractFnID) IsValid() bool { return id != NoContractFnID }

// IsValid reports whether the AttrID is valid (non-zero).
func (id AttrID) IsValid() bool { return id != NoAttrID }

// IsValid reports whether the ExternMemberID is valid (non-zero).
func (id ExternMemberID) IsValid() bool { return id != NoExternMemberID }

// IsValid reports whether the ExternFieldID is valid (non-zero).
func (id ExternFieldID) IsValid() bool { return id != NoExternFieldID }

// IsValid reports whether the ExternBlockID is valid (non-zero).
func (id ExternBlockID) IsValid() bool { return id != NoExternBlockID }

// IsValid reports whether the TypeFieldID is valid (non-zero).
func (id TypeFieldID) IsValid() bool { return id != NoTypeFieldID }

// IsValid reports whether the TypeUnionMemberID is valid (non-zero).
func (id TypeUnionMemberID) IsValid() bool { return id != NoTypeUnionMember }

// IsValid reports whether the EnumVariantID is valid (non-zero).
func (id EnumVariantID) IsValid() bool { return id != NoEnumVariantID }
