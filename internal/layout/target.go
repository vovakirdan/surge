package layout

// Target describes the ABI target triple and its pointer properties.
//
// Step B scope: only x86_64-linux-gnu is implemented.
type Target struct {
	Triple       string // e.g. "x86_64-linux-gnu"
	AddressBits  uint8
	PointerSize  uint64 // bytes
	PointerAlign uint64 // bytes
	MaxABIAlign  uint64 // bytes
}

// X86_64LinuxGNU returns the target specification for 64-bit Linux on x86.
func X86_64LinuxGNU() Target {
	return Target{
		Triple:       "x86_64-linux-gnu",
		AddressBits:  64,
		PointerSize:  8,
		PointerAlign: 8,
		MaxABIAlign:  1 << 32,
	}
}
