package panicgate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Dead is a fixture the corpus has an answer on file for and never runs.
//
// This is the failure the coverage half of the gate cannot survive on its own:
// a recorded .out counted as coverage while nothing executes it is a gate that
// is green because it is not running. It has happened here twice - a whole
// directory swept by a `_panics.sg` suffix, taking every future fixture with
// it - so the wiring is checked rather than assumed.
type Dead struct {
	Fixture string
	Reason  string
}

var corpusCallRe = regexp.MustCompile(`runBehaviourCorpus\(t,\s*"([^"]+)"\s*([^)]*)\)`)
var quotedRe = regexp.MustCompile(`"([^"]*)"`)

// DeadFixtures reports every fixture with a recorded exit code that the
// behavioural corpus does not execute: one excluded by a directory's suffix
// sweep, and one in a directory no corpus test names at all.
//
// A fixture whose name begins with an underscore is deliberately not run and is
// not reported; that is the corpus's own opt-out and it is per-fixture, which
// is the property a suffix sweep lacks.
func DeadFixtures(root string) ([]Dead, error) {
	swept, err := corpusSelection(root)
	if err != nil {
		return nil, err
	}
	goldenDir := filepath.Join(root, "testdata", "golden")
	dirs, err := os.ReadDir(goldenDir)
	if err != nil {
		return nil, err
	}
	var out []Dead
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entries, readErr := os.ReadDir(filepath.Join(goldenDir, dir.Name()))
		if readErr != nil {
			return nil, readErr
		}
		suffixes, wired := swept[dir.Name()]
		for _, ent := range entries {
			name := ent.Name()
			if ent.IsDir() || !strings.HasSuffix(name, ".code") || strings.HasPrefix(name, "_") {
				continue
			}
			base := strings.TrimSuffix(name, ".code")
			fixture := dir.Name() + "/" + base
			switch {
			case !wired:
				out = append(out, Dead{
					Fixture: fixture,
					Reason:  "no behavioural corpus test names the directory " + dir.Name(),
				})
			case hasAnySuffix(base+".sg", suffixes):
				out = append(out, Dead{
					Fixture: fixture,
					Reason:  "excluded by the directory's skipSuffixes sweep",
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fixture < out[j].Fixture })
	return out, nil
}

// corpusSelection reads which fixture directory each test in internal/vm runs,
// and, for the ones that go through the shared corpus runner, which suffixes it
// sweeps out.
//
// Two directories - vm_debug and vm_entrypoint - are driven by runners of their
// own because they need a debugger script or a stdin, so naming the directory
// anywhere in the package's tests counts as wiring it. That is a looser test
// than reading the runner, and deliberately so: the failure worth catching here
// is a directory nothing mentions at all.
func corpusSelection(root string) (map[string][]string, error) {
	dir := filepath.Join(root, "internal", "vm")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sources := map[string]string{}
	out := map[string][]string{}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- repository-owned path
		if readErr != nil {
			return nil, readErr
		}
		sources[name] = string(raw)
		for _, m := range corpusCallRe.FindAllStringSubmatch(string(raw), -1) {
			var suffixes []string
			for _, q := range quotedRe.FindAllStringSubmatch(m[2], -1) {
				suffixes = append(suffixes, q[1])
			}
			out[m[1]] = suffixes
		}
	}
	goldenDir := filepath.Join(root, "testdata", "golden")
	dirs, err := os.ReadDir(goldenDir)
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, ok := out[d.Name()]; ok {
			continue
		}
		for _, src := range sources {
			if strings.Contains(src, `"`+d.Name()+`"`) {
				out[d.Name()] = nil
				break
			}
		}
	}
	return out, nil
}

func hasAnySuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if s != "" && strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}
