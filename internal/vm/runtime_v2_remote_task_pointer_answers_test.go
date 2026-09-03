//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// RV2-DEBT-309 (Wave F, F2): the thirty-one entry points whose block
// generated code stores untested -- the filesystem and socket results, the
// entropy result, argv, the heap stats, the string bytes view, the terminal
// event -- answer a valid block or end the process with the RT_OOM report,
// through rt_alloc_or_report / rt_tag_alloc_or_report. Two rows force the
// refusal on the exact allocation (the stand-only seam
// rt_test_alloc_refusals) and read the report: one for the plain-block path
// (rt_argv) and one for the tagged path (an FsResult error block).
func TestRuntimeV2RemoteTaskPointerAnswersReport(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarness(t)
	env := remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1")
	for _, row := range []struct {
		mode string
		want string
	}{
		{mode: "pointer-answer-alloc", want: "surge: fatal [RT_OOM]: argv allocation failed"},
		{mode: "pointer-answer-tag", want: "surge: fatal [RT_OOM]: fs result allocation failed"},
	} {
		row := row
		t.Run(row.mode, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, env)
			if code == 0 {
				t.Fatalf("a refused block did not end the process\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if !strings.Contains(stderr, row.want) {
				t.Fatalf("refusal was not reported; want %q\nstdout:\n%s\nstderr:\n%s", row.want, stdout, stderr)
			}
			if strings.Contains(stdout+stderr, "answered NULL") {
				t.Fatalf("the entry point answered NULL instead of reporting\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
		})
	}
}

// Rule 13: RV2_DEBT_309_NEGATIVE_CONTROL hands the NULL back from both
// reporters, and the callers -- which no longer test for it -- carry on the
// way generated code would: the argv array header and the FsResult block
// are written through the NULL, and the process dies of that, silently,
// not of a report. Both rows read no RT_OOM report where they must read
// one; the stand's own "answered NULL" message never appears either,
// because the builder never gets as far as answering.
func TestRuntimeV2RemoteTaskPointerAnswersNegativeControl(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t,
		"remote_task_behavior_pointer_answers_negative",
		[]string{"-DRV2_DEBT_309_NEGATIVE_CONTROL"})
	env := remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1")
	for _, mode := range []string{"pointer-answer-alloc", "pointer-answer-tag"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, mode, env)
			if code == 0 {
				t.Fatalf("negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if strings.Contains(stderr, "surge: fatal [RT_OOM]") {
				t.Fatalf("negative control still reported the refusal\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			// The death must be the NULL write, not the stand's own assertion:
			// a harness that got as far as judging the answer would name it.
			for _, judged := range []string{
				"answered NULL where it must report the refusal",
				"neither reported nor answered NULL",
			} {
				if strings.Contains(stderr, judged) {
					t.Fatalf("negative control died of the stand's judgement, not of the NULL write (%q)\nstdout:\n%s\nstderr:\n%s",
						judged, stdout, stderr)
				}
			}
		})
	}
}
