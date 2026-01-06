package ast

// Visibility описывает доступность элемента (private/public и т.д.).
type Visibility uint8

const (
	// VisPrivate indicates that the item is private (default).
	VisPrivate Visibility = iota
	// VisPublic indicates that the item is public.
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
