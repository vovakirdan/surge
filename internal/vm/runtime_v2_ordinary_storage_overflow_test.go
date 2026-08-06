//go:build !golden
// +build !golden

package vm_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The layout-overflow negative controls.
//
// Every row is a type whose physical layout cannot exist on the target. What
// the battery pins is not that the compiler dislikes them — it is WHERE the
// refusal happens: before any storage is laid out or allocated, with a
// diagnostic that names the first type on the path that overflowed. An
// implementation that computes the layout, wraps, and allocates a small buffer
// for a type that asked for the whole address space produces no error at all.
//
// Three rows point the other way and must COMPILE cleanly, because their defect
// is a spurious overflow rather than a missed one: a zero-sized element whose
// stride was rounded up to its alignment, and a field offset that does not fit
// in a signed 32-bit integer, would both be caught here by a diagnostic that
// should not exist.
//
// One row is owed by a later workstream and says so rather than pretending.

const ordinaryStorageOverflowDir = "testdata/runtime-v2-ordinary-storage/overflow"

// overflowControl is one row of the battery.
//
// wantCode empty means the source must compile cleanly. wantPath is the layout
// path the diagnostic must name; it is empty for the rows whose overflowing
// site is the root type's own padding, where the message names the type and
// there is no member below it to point at.
type overflowControl struct {
	name     string
	wantCode string
	wantPath string
	owedBy   string
}

func (c overflowControl) relPath() string {
	return filepath.ToSlash(filepath.Join(ordinaryStorageOverflowDir, c.name+".sg"))
}

func ordinaryStorageOverflowControls() []overflowControl {
	return []overflowControl{
		{
			name:     "nested_fixed_array_length",
			wantCode: "SEM3180",
			wantPath: "Grid -> alias target [Row; 2] -> array element Row",
		},
		{
			name:     "array_stride_times_count",
			wantCode: "SEM3180",
			wantPath: "Line -> alias target [Half; 4] -> array element Half",
		},
		{
			name:     "struct_field_padding",
			wantCode: "SEM3180",
			wantPath: "FieldPadding -> field edge (Wide)",
		},
		{
			name:     "struct_tail_padding",
			wantCode: "SEM3180",
		},
		{
			name:     "packed_total_size",
			wantCode: "SEM3180",
			wantPath: "PackedTotal -> field edge (Wide)",
		},
		{
			name:     "over_aligned_round_up",
			wantCode: "SEM3180",
		},
		{
			name:     "alignment_target_width",
			wantCode: "SEM3181",
		},
		// Must compile: the multiplication is zero times anything.
		{name: "zero_sized_multiplication"},
		// Must compile: the offset is above the signed 32-bit range and must
		// survive intact rather than wrap or be refused.
		{name: "field_offset_above_int32"},
		{
			name: "envelope_sidecar_total",
			owedBy: "envelope and sidecar totals are not computed anywhere yet:" +
				" internal/layout sizes types only, and the crossing plan's" +
				" envelope + payload + sidecar arithmetic arrives with the" +
				" transport work that follows ordinary storage",
		},
	}
}

// TestRuntimeV2OrdinaryStorageOverflowBatteryIsComplete keeps the battery table
// and the source tree the same set, for the same reason the corpus does: a
// negative control nobody runs reads like coverage.
func TestRuntimeV2OrdinaryStorageOverflowBatteryIsComplete(t *testing.T) {
	root := repoRoot(t)

	declared := make(map[string]bool)
	for _, control := range ordinaryStorageOverflowControls() {
		if declared[control.name] {
			t.Fatalf("duplicate overflow row %q", control.name)
		}
		declared[control.name] = true
	}

	entries, err := os.ReadDir(filepath.Join(root, ordinaryStorageOverflowDir))
	if err != nil {
		t.Fatalf("read overflow directory: %v", err)
	}

	var extra []string
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sg") {
			t.Fatalf("unexpected entry %q in the overflow directory", entry.Name())
		}
		found++
		name := strings.TrimSuffix(entry.Name(), ".sg")
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("overflow sources without a row: %s", strings.Join(extra, ", "))
	}
	if found != len(declared) {
		t.Errorf("overflow rows = %d, sources = %d", len(declared), found)
	}
}

// TestRuntimeV2OrdinaryStorageOverflowBattery runs every negative control
// through the diagnostic front end, which is where the refusal has to happen:
// the rows never reach a backend, so a layout that overflows can never have
// reached an allocator either.
func TestRuntimeV2OrdinaryStorageOverflowBattery(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	env := envForParity(root)

	for _, control := range ordinaryStorageOverflowControls() {
		t.Run(control.name, func(t *testing.T) {
			if control.owedBy != "" {
				t.Skipf("owed by a later workstream: %s", control.owedBy)
			}

			stdout, stderr, code := runSurgeWithEnv(t, root, surge, env,
				"diag", "--format", "short", "--with-notes", control.relPath())

			if control.wantCode == "" {
				if code != 0 {
					t.Fatalf("exit code = %d, want 0: this row must compile cleanly,"+
						" so a diagnostic here is the defect\nstdout:\n%s\nstderr:\n%s",
						code, stdout, stderr)
				}
				if strings.TrimSpace(stdout) != "" {
					t.Fatalf("unexpected diagnostics on a row that must compile:\n%s", stdout)
				}
				return
			}

			if code == 0 {
				t.Fatalf("exit code = 0: the layout overflow was accepted\nstdout:\n%s", stdout)
			}

			errorLine := ""
			for _, line := range strings.Split(stdout, "\n") {
				if strings.HasPrefix(line, "error ") {
					errorLine = line
					break
				}
			}
			if errorLine == "" {
				t.Fatalf("no error diagnostic reported\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if !strings.Contains(errorLine, control.wantCode) {
				t.Fatalf("diagnostic = %q, want code %s", errorLine, control.wantCode)
			}

			if control.wantPath == "" {
				return
			}
			wantNote := "layout path: " + control.wantPath
			if !strings.Contains(stdout, wantNote) {
				t.Fatalf("missing the first overflowing type path.\nwant note: %s\ngot:\n%s",
					wantNote, stdout)
			}
		})
	}
}
