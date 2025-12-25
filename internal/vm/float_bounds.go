package vm

import (
	"fmt"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

var (
	float16MinVal = mustParseFloat("-65504.0")
	float16MaxVal = mustParseFloat("65504.0")
	float32MinVal = mustParseFloat("-3.402_823_466_385_2886e+38")
	float32MaxVal = mustParseFloat("3.402_823_466_385_2886e+38")
	float64MinVal = mustParseFloat("-1.797_693_134_862_3157e+308")
	float64MaxVal = mustParseFloat("1.797_693_134_862_3157e+308")
)

func mustParseFloat(text string) bignum.BigFloat {
	f, err := bignum.ParseFloat(text)
	if err != nil {
		panic(fmt.Sprintf("vm: parse float bound %q: %v", text, err))
	}
	return f
}

func floatBounds(width types.Width) (minVal, maxVal bignum.BigFloat, ok bool) {
	switch width {
	case types.Width16:
		return float16MinVal, float16MaxVal, true
	case types.Width32:
		return float32MinVal, float32MaxVal, true
	case types.Width64:
		return float64MinVal, float64MaxVal, true
	default:
		return bignum.BigFloat{}, bignum.BigFloat{}, false
	}
}

func floatFitsWidth(value bignum.BigFloat, width types.Width) bool {
	if width == types.WidthAny {
		return true
	}
	minVal, maxVal, ok := floatBounds(width)
	if !ok {
		return true
	}
	return value.Cmp(minVal) >= 0 && value.Cmp(maxVal) <= 0
}
