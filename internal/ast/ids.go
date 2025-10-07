package ast

type (
	FileID uint32
	ItemID uint32
	StmtID uint32
	ExprID uint32
	TypeID uint32
)

const (
	NoFileID FileID = 0
	NoItemID ItemID = 0
	NoStmtID StmtID = 0
	NoExprID ExprID = 0
	NoTypeID TypeID = 0
)

func (id FileID) IsValid() bool { return id != NoFileID }
func (id ItemID) IsValid() bool { return id != NoItemID }
func (id StmtID) IsValid() bool { return id != NoStmtID }
func (id ExprID) IsValid() bool { return id != NoExprID }
func (id TypeID) IsValid() bool { return id != NoTypeID }
