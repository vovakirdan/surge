package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAsyncChannelCaptureLifecycle(t *testing.T) {
	for _, row := range []struct{ name, stdout string }{
		{"same_handle_twice", "7\n8\n"}, {"nested_async", "7\n"},
		{"captured_heap_int", "9223372036854775808\n"},
		{"captured_heap_uint", "18446744073709551616\n"},
		{"captured_float", "1.5E+0\n"}, {"cold_capture", "end\n"},
		{"boundary-cold_string_capture", "end\n"}, {"lifecycle-drop_outer_capture", "7\n"},
		{"lifecycle-cancel_ready_capture", "cancelled\n"}, {"boundary-cancel_string_capture", "cancelled\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", "async_channel_capture", row.name+".sg"))
			if err != nil {
				t.Fatal(err)
			}
			for _, workers := range []string{"1", "8"} {
				t.Run("workers-"+workers, func(t *testing.T) {
					t.Setenv("SURGE_THREADS", workers)
					got := runProgramFromSource(t, string(source), runOptions{captureStdout: true})
					if got.exitCode != 0 || got.stdout != row.stdout || got.stderr != "" {
						t.Fatalf("async capture lifecycle: exit=%d stdout=%q stderr=%s", got.exitCode, got.stdout, got.stderr)
					}
				})
			}
		})
	}
}
