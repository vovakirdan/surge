package ast

type (
	// FileID identifies a source file.
	FileID uint32
	ItemID uint32
	StmtID uint32
	ExprID uint32
	TypeID uint32
	// PayloadID indexes auxiliary expression payload data.
	PayloadID         uint32
	FnParamID         uint32
	TypeParamID       uint32
	TypeParamBoundID  uint32
	ContractDeclID    uint32
	ContractItemID    uint32
	ContractFieldID   uint32
	ContractFnID      uint32
	AttrID            uint32
	ExternMemberID    uint32
	ExternFieldID     uint32
	ExternBlockID     uint32
	TypeFieldID       uint32
	TypeUnionMemberID uint32
)

const (
	NoFileID           FileID            = 0
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
)

func (id FileID) IsValid() bool            { return id != NoFileID }
func (id ItemID) IsValid() bool            { return id != NoItemID }
func (id StmtID) IsValid() bool            { return id != NoStmtID }
func (id ExprID) IsValid() bool            { return id != NoExprID }
func (id TypeID) IsValid() bool            { return id != NoTypeID }
func (id PayloadID) IsValid() bool         { return id != NoPayloadID }
func (id FnParamID) IsValid() bool         { return id != NoFnParamID }
func (id TypeParamID) IsValid() bool       { return id != NoTypeParamID }
func (id TypeParamBoundID) IsValid() bool  { return id != NoTypeParamBoundID }
func (id ContractDeclID) IsValid() bool    { return id != NoContractDeclID }
func (id ContractItemID) IsValid() bool    { return id != NoContractItemID }
func (id ContractFieldID) IsValid() bool   { return id != NoContractFieldID }
func (id ContractFnID) IsValid() bool      { return id != NoContractFnID }
func (id AttrID) IsValid() bool            { return id != NoAttrID }
func (id ExternMemberID) IsValid() bool    { return id != NoExternMemberID }
func (id ExternFieldID) IsValid() bool     { return id != NoExternFieldID }
func (id ExternBlockID) IsValid() bool     { return id != NoExternBlockID }
func (id TypeFieldID) IsValid() bool       { return id != NoTypeFieldID }
func (id TypeUnionMemberID) IsValid() bool { return id != NoTypeUnionMember }
