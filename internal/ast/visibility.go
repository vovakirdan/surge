package ast

// Visibility описывает доступность элемента (private/public и т.д.).
type Visibility uint8

const (
	VisPrivate Visibility = iota
	VisPublic
)

func (v Visibility) String() string {
	switch v {
	case VisPublic:
		return "public"
	default:
		return "private"
	}
}
