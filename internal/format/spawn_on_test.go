package format

import (
	"strings"
	"testing"
)

// TestFormatSpawnOnRemote checks that a `spawn on <dst> { ... }` remote-spawn
// expression (Epic 11 Block 3) formats canonically and round-trips: the `spawn`
// keyword and destination are preserved, and the formatted output reparses
// cleanly with the same top-level item kinds.
func TestFormatSpawnOnRemote(t *testing.T) {
	src := []byte("fn f() crosses -> int { return spawn on pool { ret 1; }; }\n")
	sf, builder, fileID := parseSource(t, src)
	formatted, err := FormatFile(sf, builder, fileID, Options{})
	if err != nil {
		t.Fatalf("FormatFile failed: %v", err)
	}
	got := string(formatted)
	if !strings.Contains(got, "spawn on pool") {
		t.Fatalf("expected formatted output to preserve `spawn on pool`, got %q", got)
	}
	if ok, msg := CheckRoundTrip(sf, Options{}, 128); !ok {
		t.Fatalf("CheckRoundTrip failed: %s", msg)
	}
}
