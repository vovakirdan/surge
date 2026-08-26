package driver

// DefaultMaxDiagnostics sizes the bag when the options do not.
//
// A zero MaxDiagnostics is the zero value of an options struct, not a request
// for silence: a bag of capacity zero drops every diagnostic on Add, the
// caller reads a clean compile, and a refused program is carried on into HIR
// merge where a decision that was never published surfaces as an internal
// compiler error in place of the refusal. Every test helper that builds its
// options by hand is that caller.
const DefaultMaxDiagnostics = 100

func bagCapacity(maximum int) int {
	if maximum <= 0 {
		return DefaultMaxDiagnostics
	}
	return maximum
}
