package ast

type (
	// главные сущености
	FileID uint32
	ItemID uint32
	StmtID uint32
	ExprID uint32
	TypeID uint32
	// подсущности
	PayloadID         uint32
	FnParamID         uint32
	AttrID            uint32
	TypeFieldID       uint32
	TypeUnionMemberID uint32
)

const (
	NoFileID          FileID            = 0
	NoItemID          ItemID            = 0
	NoStmtID          StmtID            = 0
	NoExprID          ExprID            = 0
	NoTypeID          TypeID            = 0
	NoPayloadID       PayloadID         = 0
	NoFnParamID       FnParamID         = 0
	NoAttrID          AttrID            = 0
	NoTypeFieldID     TypeFieldID       = 0
	NoTypeUnionMember TypeUnionMemberID = 0
)

func (id FileID) IsValid() bool            { return id != NoFileID }
func (id ItemID) IsValid() bool            { return id != NoItemID }
func (id StmtID) IsValid() bool            { return id != NoStmtID }
func (id ExprID) IsValid() bool            { return id != NoExprID }
func (id TypeID) IsValid() bool            { return id != NoTypeID }
func (id PayloadID) IsValid() bool         { return id != NoPayloadID }
func (id FnParamID) IsValid() bool         { return id != NoFnParamID }
func (id AttrID) IsValid() bool            { return id != NoAttrID }
func (id TypeFieldID) IsValid() bool       { return id != NoTypeFieldID }
func (id TypeUnionMemberID) IsValid() bool { return id != NoTypeUnionMember }
