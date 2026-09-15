package llvm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var unshareDefinitionRe = regexp.MustCompile(`(?ms)^define void @(unshare\.type\d+)\(ptr %val\) \{\n.*?^\}\n`)
var unshareNameRe = regexp.MustCompile(`@(unshare\.type\d+)\b`)
var unshareLeafRe = regexp.MustCompile(`call (?:ptr @rt_big(?:int|uint|float)_unshare|void @rt_(?:array_unshare_walk|range_unshare))\(`)

// A site must demand a real walk, whose transitive bodies are defined once.
// Other sites in the same module can demand other walks: an int reply does not
// belong to the caller's float capture. The module-wide check starts at all
// direct calls OUTSIDE walk definitions, so an unreferenced body or a cycle of
// bodies cannot manufacture its own demand.
func assertUnshareBodiesDefinedOnce(t *testing.T, ir, body string) {
	t.Helper()
	if err := unshareBodyCensus(ir, body); err != nil {
		t.Fatalf("%v:\n%s", err, ir)
	}
}

func unshareBodyCensus(ir, site string) error {
	definitions := map[string]string{}
	for _, m := range unshareDefinitionRe.FindAllStringSubmatch(ir, -1) {
		if _, exists := definitions[m[1]]; exists {
			return fmt.Errorf("%s is defined more than once", m[1])
		}
		// The definition's own name is not an edge. Calls to itself in the
		// body remain edges and must still reach a real leaf.
		definitions[m[1]] = m[0][strings.IndexByte(m[0], '\n')+1:]
	}
	siteCalls := unshareCallTargetRe.FindAllStringSubmatch(site, -1)
	if len(siteCalls) == 0 {
		return fmt.Errorf("the selected site calls no walk")
	}
	outside := unshareDefinitionRe.ReplaceAllString(ir, "")
	moduleCalls := unshareCallTargetRe.FindAllStringSubmatch(outside, -1)
	closure := func(calls [][]string) (map[string]bool, error) {
		reached := map[string]bool{}
		pending := make([]string, 0, len(calls))
		for _, call := range calls {
			pending = append(pending, call[1])
		}
		for len(pending) > 0 {
			name := pending[0]
			pending = pending[1:]
			if reached[name] {
				continue
			}
			walk, exists := definitions[name]
			if !exists {
				return nil, fmt.Errorf("%s is reached but has no definition", name)
			}
			reached[name] = true
			for _, edge := range unshareNameRe.FindAllStringSubmatch(walk, -1) {
				pending = append(pending, edge[1])
			}
		}
		return reached, nil
	}
	if _, err := closure(siteCalls); err != nil {
		return err
	}
	reached, err := closure(moduleCalls)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !reached[name] {
			return fmt.Errorf("%s is defined but no module site reaches it", name)
		}
		// A composite may contain only calls to child walks. It must reach
		// an actual runtime leaf through them; a leafless cycle is not work.
		pending, seen, hasLeaf := []string{name}, map[string]bool{}, false
		for len(pending) > 0 {
			next := pending[0]
			pending = pending[1:]
			if seen[next] {
				continue
			}
			seen[next] = true
			walk := definitions[next]
			if unshareLeafRe.MatchString(walk) {
				hasLeaf = true
				break
			}
			for _, edge := range unshareNameRe.FindAllStringSubmatch(walk, -1) {
				pending = append(pending, edge[1])
			}
		}
		if !hasLeaf {
			return fmt.Errorf("%s reaches no runtime unshare leaf", name)
		}
	}
	return nil
}

// These small IR strings test the census, not the runtime. The source-driven
// crossing tests exercise the emitted ABI and the ownership operations.
const unshareCensusSite = "  call void @unshare.type1(ptr %slot)\n"

const unshareCensusModule = `define void @site(ptr %slot) {
  call void @unshare.type1(ptr %slot)
  ret void
}
define void @other_site(ptr %slot) {
  call void @unshare.type2(ptr %slot)
  call void @unshare.type3(ptr %slot)
  call void @unshare.type5(ptr %slot)
  ret void
}
define void @unshare.type1(ptr %val) {
  call void @unshare.type6(ptr %val)
  ret void
}
define void @unshare.type2(ptr %val) {
  %private = call ptr @rt_bigint_unshare(ptr %val)
  ret void
}
define void @unshare.type3(ptr %val) {
  %private = call ptr @rt_biguint_unshare(ptr %val)
  ret void
}
define void @unshare.type4(ptr %val) {
  %private = call ptr @rt_bigfloat_unshare(ptr %val)
  ret void
}
define void @unshare.type5(ptr %val) {
  call void @rt_range_unshare(ptr %val)
  ret void
}
define void @unshare.type6(ptr %val) {
  call void @rt_array_unshare_walk(ptr %val, i64 8, ptr @unshare.type4)
  ret void
}
`

func TestUnshareBodyCensusAcceptsIndependentSites(t *testing.T) {
	if err := unshareBodyCensus(unshareCensusModule, unshareCensusSite); err != nil {
		t.Fatal(err)
	}
}

func TestUnshareBodyCensusRejectsBrokenDemand(t *testing.T) {
	leaf := emittedDefinition(unshareCensusModule, "unshare.type2")
	cases := []struct{ name, ir, site, want string }{
		{"missing_definition", strings.Replace(unshareCensusModule, leaf, "", 1), unshareCensusSite, "has no definition"},
		{"duplicate_definition", unshareCensusModule + leaf, unshareCensusSite, "defined more than once"},
		{"orphan", strings.Replace(unshareCensusModule, "  call void @unshare.type2(ptr %slot)\n", "", 1), unshareCensusSite, "no module site reaches it"},
		{"empty_leaf", strings.Replace(unshareCensusModule, "  %private = call ptr @rt_bigint_unshare(ptr %val)\n", "", 1), unshareCensusSite, "reaches no runtime unshare leaf"},
		{"wrong_runtime_leaf", strings.Replace(unshareCensusModule, "@rt_bigint_unshare(", "@rt_bigint_clone(", 1), unshareCensusSite, "reaches no runtime unshare leaf"},
		{"leafless_cycle", strings.Replace(unshareCensusModule, "  %private = call ptr @rt_bigint_unshare(ptr %val)", "  call void @unshare.type2(ptr %val)", 1), unshareCensusSite, "reaches no runtime unshare leaf"},
		{"missing_site_call", unshareCensusModule, "ret void\n", "selected site calls no walk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := unshareBodyCensus(tc.ir, tc.site)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("broken demand %s: err=%v, want %q", tc.name, err, tc.want)
			}
		})
	}
}
