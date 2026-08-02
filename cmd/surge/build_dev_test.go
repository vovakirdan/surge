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
type NodeId = uint;

type Node = {
    next: NodeId?,
    data: int,
}

fn walk(nodes: &Node[], start: NodeId?) -> nothing {
    let mut id: NodeId? = start;
    while true {
        compare id {
            Some(i) => {
                let n: &Node = nodes[(i to int)];
                id = *n.next;
            }
            nothing => { return nothing; }
        };
    }
}

@entrypoint
fn main() -> int {
    let nodes: Node[] = [];
    let start: NodeId? = nothing;
    walk(&nodes, start);
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
