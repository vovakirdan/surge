package vm

type mapKeyKind uint8

const (
	mapKeyString mapKeyKind = iota
	mapKeyInt
	mapKeyUint
	mapKeyBigInt
	mapKeyBigUint
)

type mapKey struct {
	kind mapKeyKind
	i64  int64
	u64  uint64
	str  string
}

type mapEntry struct {
	Key   Value
	Value Value
}
