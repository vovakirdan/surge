package driver

import (
	"testing"

	"surge/internal/diag"
)

// Core has two producers of a string's view: the method `bytes()` (the receiver
// path of a call) and the intrinsic `rt_string_bytes_view` it forwards to (the
// argument path). Each keeps its `&string` borrowed while the view lives. A view
// of a field keeps the whole value borrowed, as a window of a field does.
func TestBytesViewFromEveryCoreProducerBlocksItsString(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"intrinsic_view_blocks_move", bytesViewSink + `fn a(s: string) -> uint8 {
    let v = rt_string_bytes_view(&s);
    sink(own s);
    return v[0];
}
`, "own s);", diag.SemaBorrowMove},
		{"intrinsic_view_blocks_reassignment", `fn b(t: string) -> uint8 {
    let mut s: string = t;
    let v = rt_string_bytes_view(&s);
    s = "c" + "d";
    return v[0];
}
`, `s = "c" + "d"`, diag.SemaBorrowMutation},
		{"view_of_a_field_blocks_moving_its_value", `type Box = { s: string };

fn sinkb(b: own Box) -> nothing {
    return nothing;
}

fn a(b: Box) -> uint8 {
    let v = b.s.bytes();
    sinkb(own b);
    return v[0];
}
`, "own b)", diag.SemaBorrowMove},
		{"window_of_a_field_twin", `type Bag = { xs: int[] };

fn sinkb(b: own Bag) -> nothing {
    return nothing;
}

fn a(b: Bag) -> int {
    let w = b.xs[[0..2]];
    sinkb(own b);
    return w[0];
}
`, "own b)", diag.SemaBorrowMove},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, row.text, row.code, row.snippet)
		})
	}
}
