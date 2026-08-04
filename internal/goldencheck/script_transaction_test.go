package goldencheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func commandWithMoveFault(t *testing.T, fixture scriptFixture, mode string) *exec.Cmd {
	t.Helper()
	realMove, err := exec.LookPath("mv")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(fixture.root, "fake-move-bin")
	writeTestFile(t, filepath.Join(fakeBin, "mv"), `#!/usr/bin/env bash
set -euo pipefail
source_path="${1:-}"
target_path="${2:-}"
if [[ "${MOVE_FAULT:?}" == first-failure && "${source_path}" == "${LIVE_GOLDEN_DIR:?}" && "${target_path}" == */original ]]; then
	exit 71
fi
if [[ "${MOVE_FAULT}" == second-failure && "${source_path}" == */worktree/testdata/golden && "${target_path}" == "${LIVE_GOLDEN_DIR}" ]]; then
	exit 72
fi
"${REAL_MV:?}" "$@"
if [[ "${MOVE_FAULT}" == signal-after-first && "${source_path}" == "${LIVE_GOLDEN_DIR}" && "${target_path}" == */original ]]; then
	kill -TERM "${PPID}"
fi
if [[ "${MOVE_FAULT}" == signal-after-install && "${source_path}" == */worktree/testdata/golden && "${target_path}" == "${LIVE_GOLDEN_DIR}" ]]; then
	kill -TERM "${PPID}"
fi
`, 0o755)
	cmd := fixture.command(t, "", nil, nil, nil)
	cmd.Env = append(cmd.Env,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REAL_MV="+realMove,
		"LIVE_GOLDEN_DIR="+filepath.Join(fixture.root, "testdata", "golden"),
		"MOVE_FAULT="+mode,
	)
	return cmd
}

func TestGoldenScriptSwapFailuresRestoreLiveCorpus(t *testing.T) {
	for _, mode := range []string{"first-failure", "second-failure", "signal-after-first"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newScriptFixture(t, "valid.sg")
			goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
			before, err := Scan(goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			output, runErr := commandWithMoveFault(t, fixture, mode).CombinedOutput()
			if runErr == nil {
				t.Fatalf("script accepted %s\n%s", mode, output)
			}
			after, err := Scan(goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			if changes := Diff(before, after); len(changes) != 0 {
				t.Fatalf("%s changed live corpus: %#v\n%s", mode, changes, output)
			}
			assertNoGoldenStaging(t, fixture.root)
		})
	}
}

func TestGoldenScriptPropagatesSidecarDeletionFailure(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	stale := filepath.Join(fixture.root, "testdata", "golden", "orphan.tokens")
	writeTestFile(t, stale, "stale\n", 0o644)
	goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
	before, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	realFind, err := exec.LookPath("find")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(fixture.root, "fake-find-bin")
	writeTestFile(t, filepath.Join(fakeBin, "find"), `#!/usr/bin/env bash
set -euo pipefail
for argument in "$@"; do
	if [[ "${argument}" == -delete ]]; then
		exit 73
	fi
done
exec "${REAL_FIND:?}" "$@"
`, 0o755)
	cmd := fixture.command(t, "", nil, nil, nil)
	cmd.Env = append(cmd.Env,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REAL_FIND="+realFind,
	)
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("script ignored sidecar deletion failure\n%s", output)
	}
	after, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(before, after); len(changes) != 0 {
		t.Fatalf("deletion failure changed live corpus: %#v\n%s", changes, output)
	}
	assertNoGoldenStaging(t, fixture.root)
}

func TestGoldenScriptCleansPartialMktempFailure(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	residue := filepath.Join(fixture.root, "testdata", ".golden-update.partial")
	fakeBin := filepath.Join(fixture.root, "fake-mktemp-bin")
	writeTestFile(t, filepath.Join(fakeBin, "mktemp"), `#!/usr/bin/env bash
set -euo pipefail
mkdir "${MKTEMP_RESIDUE:?}"
printf '%s\n' "${MKTEMP_RESIDUE}"
exit 74
`, 0o755)
	cmd := fixture.command(t, "", nil, nil, nil)
	cmd.Env = append(cmd.Env,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MKTEMP_RESIDUE="+residue,
	)
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("script ignored mktemp failure\n%s", output)
	}
	if _, err := os.Lstat(residue); !os.IsNotExist(err) {
		t.Fatalf("partial mktemp directory remains: %v", err)
	}
	assertNoGoldenStaging(t, fixture.root)
}
