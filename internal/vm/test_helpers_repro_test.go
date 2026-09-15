package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func llvmReproCommand(root, stdlibRoot, srcPath, outputPath string, argv []string) string {
	relPath, err := filepath.Rel(root, srcPath)
	if err != nil {
		relPath = srcPath
	}
	var args string
	if len(argv) > 0 {
		args = " " + strings.Join(argv, " ")
	}
	return fmt.Sprintf("cd %s && SURGE_STDLIB=%s go run ./cmd/surge build %s --emit-mir --emit-llvm --keep-tmp --print-commands && SURGE_STDLIB=%s %s%s", root, stdlibRoot, relPath, stdlibRoot, outputPath, args)
}

func TestLLVMReproCommandUsesSelectedStdlibRoot(t *testing.T) {
	for _, name := range []string{"repository_root", "fixture_root"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			stdlibRoot := root
			if name == "fixture_root" {
				stdlibRoot = filepath.Join(root, "fixture", "stdlib-root")
			}
			fakeBin := filepath.Join(root, "bin")
			if err := os.Mkdir(fakeBin, 0o700); err != nil {
				t.Fatal(err)
			}
			outputPath := filepath.Join(fakeBin, "main")
			for path, script := range map[string]string{
				filepath.Join(fakeBin, "go"): "#!/bin/sh\nprintf '%s' \"$SURGE_STDLIB\" > \"$REPRO_BUILD_ENV_PATH\"\n",
				outputPath:                   "#!/bin/sh\nprintf '%s' \"$SURGE_STDLIB\" > \"$REPRO_RUN_ENV_PATH\"\n",
			} {
				if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			buildEnvPath := filepath.Join(root, "build.env")
			runEnvPath := filepath.Join(root, "run.env")
			env := envWithStdlib("ambient-wrong-root")
			env = overrideEnvVar(env, "PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			env = overrideEnvVar(env, "REPRO_BUILD_ENV_PATH", buildEnvPath)
			env = overrideEnvVar(env, "REPRO_RUN_ENV_PATH", runEnvPath)
			repro := llvmReproCommand(root, stdlibRoot, filepath.Join(root, "main.sg"), outputPath, []string{"7"})
			cmd := exec.Command("sh", "-c", repro)
			cmd.Env = env
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("execute repro: %v\n%s", err, output)
			}
			for _, path := range []string{buildEnvPath, runEnvPath} {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read observed environment %s: %v", path, err)
				}
				if string(got) != stdlibRoot {
					t.Fatalf("observed environment %s: want %q, got %q", path, stdlibRoot, got)
				}
			}
		})
	}
}
