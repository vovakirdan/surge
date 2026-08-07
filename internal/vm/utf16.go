package vm

import (
	"fortio.org/safecast"
)

// decodeUTF16Strict decodes a UTF-16 sequence strictly, rejecting invalid
// sequences rather than substituting a replacement character: a caller asking
// for text is entitled to know its input was not text.
func decodeUTF16Strict(units []uint16) (string, bool) {
	if len(units) == 0 {
		return "", true
	}
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 >= len(units) {
				return "", false
			}
			lo := units[i+1]
			if lo < 0xDC00 || lo > 0xDFFF {
				return "", false
			}
			code := 0x10000 + ((uint32(u) - 0xD800) << 10) + (uint32(lo) - 0xDC00)
			r, err := safecast.Conv[rune](code)
			if err != nil {
				return "", false
			}
			runes = append(runes, r)
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return "", false
		default:
			runes = append(runes, rune(u))
		}
	}
	return string(runes), true
}
