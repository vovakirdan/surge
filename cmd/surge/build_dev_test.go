package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"surge/internal/buildpipeline"
)

const ownershipBuildDevNegativeSource = `
tag Payload(string);
tag Empty();

type Slot = Payload(string) | Empty();

type Holder = { held: string }

// A payload bound out of a BORROWED union and stored into a projection: two
// owners for one string, accepted by the compiler and reported by the
// report-only verifier. See the twin control in internal/buildpipeline for why
// the previous walk over NodeId? had to be replaced — its id = *n.next is
// a compile error now, and a control the default build rejects cannot test what
// the dev flag does to a build that succeeds.
fn stash(slot: &Slot, out: &mut Holder) -> nothing {
    compare *slot {
        Payload(s) => { out.held = s; }
        Empty() => { return nothing; }
    };
    return nothing;
}

@entrypoint
fn main() -> int {
    let s: Slot = Payload("x");
    let mut h: Holder = Holder { held = "" };
    stash(&s, &mut h);
    print(h.held.__clone());
    return 0;
}
`

func TestBuildDevFlagEnablesOwnershipGate(t *testing.T) {
	t.Setenv("SURGE_STDLIB", surgeRepoRootForBuildTest(t))
	workspace := t.TempDir()
	chdirForTest(t, workspace)
	path := filepath.Join(workspace, "ownership-dev-negative.sg")
	if err := os.WriteFile(path, []byte(ownershipBuildDevNegativeSource), 0o600); err != nil {
		t.Fatalf("write ownership dev source: %v", err)
	}

	if err := executeBuildCommandForTest(path, false); err != nil {
		t.Fatalf("default build rejected report-only negative control: %v", err)
	}

	err := executeBuildCommandForTest(path, true)
	var ownershipErr *buildpipeline.OwnershipVerificationError
	if !errors.As(err, &ownershipErr) {
		t.Fatalf("build --dev error = %T %v, want *OwnershipVerificationError", err, err)
	}
	if len(ownershipErr.Findings) == 0 {
		t.Fatal("build --dev returned an ownership error without findings")
	}
}

func surgeRepoRootForBuildTest(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repository root %s: %v", root, err)
	}
	return root
}

func executeBuildCommandForTest(path string, dev bool) error {
	root := &cobra.Command{
		Use:           "surge",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().Int("max-diagnostics", 100, "")
	root.AddCommand(newBuildCommand())
	args := []string{"build", "--backend=vm", "--ui=off"}
	if dev {
		args = append(args, "--dev")
	}
	root.SetArgs(append(args, path))
	return root.Execute()
}
