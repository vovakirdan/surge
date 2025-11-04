//go:build surge_debug

package symbols

import "fmt"

func debugScopeMismatch(expected, actual ScopeID) {
	panic(fmt.Sprintf("resolver scope mismatch: expected %d, got %d", expected, actual))
}
