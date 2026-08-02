// Package gatecheck keeps the Makefile's test gates honest: every gate's
// -run selection must match at least one real test, every tagged runtime
// test must be reachable from some gate, and every gate must be reachable
// from a root target or carry an owned exemption. The selection semantics
// are never reimplemented — matching is evaluated by invoking
// `go test -list` with the gate's own tags.
package gatecheck

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Gate is one `go test` invocation extracted from a Makefile recipe.
type Gate struct {
	Target   string
	Line     int
	Packages []string
	Tags     string
	Run      string
}

var targetRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+):`)

// ParseGates extracts every `$(GO) test`/`go test` recipe line with its
// enclosing target, package list, -tags value, and -run pattern
// (Makefile `$$` unescaped to `$`).
func ParseGates(makefile string) []Gate {
	var gates []Gate
	target := ""
	for i, raw := range strings.Split(makefile, "\n") {
		if m := targetRe.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(raw, "\t") {
			target = m[1]
			continue
		}
		if !strings.Contains(raw, "$(GO) test") && !strings.Contains(raw, "\tgo test") {
			continue
		}
		gate := Gate{Target: target, Line: i + 1}
		fields := strings.Fields(raw)
		for j := range fields {
			switch {
			case fields[j] == "-tags" && j+1 < len(fields):
				gate.Tags = fields[j+1]
			case fields[j] == "-run" && j+1 < len(fields):
				gate.Run = strings.ReplaceAll(strings.Trim(fields[j+1], "'"), "$$", "$")
			case strings.HasPrefix(fields[j], "./"):
				gate.Packages = append(gate.Packages, fields[j])
			}
		}
		gates = append(gates, gate)
	}
	return gates
}

// ReachableTargets walks both edge kinds a Makefile has — target-line
// prerequisites (`a: b c`) and `$(MAKE) <target>` recipe calls — from the
// given roots and returns every transitively reachable target name.
func ReachableTargets(makefile string, roots ...string) map[string]bool {
	recipes := map[string][]string{}
	prereqs := map[string][]string{}
	target := ""
	for _, raw := range strings.Split(makefile, "\n") {
		if m := targetRe.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(raw, "\t") {
			target = m[1]
			rest := strings.TrimSpace(raw[len(m[0]):])
			if rest != "" && !strings.HasPrefix(target, ".") {
				prereqs[target] = append(prereqs[target], strings.Fields(rest)...)
			}
			continue
		}
		if target != "" && strings.HasPrefix(raw, "\t") {
			recipes[target] = append(recipes[target], raw)
		}
	}
	makeRe := regexp.MustCompile(`\$\(MAKE\)\s+([A-Za-z0-9_.-]+)`)
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if reachable[name] {
			return
		}
		reachable[name] = true
		for _, dep := range prereqs[name] {
			visit(dep)
		}
		for _, line := range recipes[name] {
			for _, m := range makeRe.FindAllStringSubmatch(line, -1) {
				visit(m[1])
			}
		}
	}
	for _, root := range roots {
		visit(root)
	}
	return reachable
}

// ListTests invokes `go test -list` from the repository root and returns
// the matched test names — the gate's real selection under its own tags.
func ListTests(repoRoot string, packages []string, tags, pattern string) ([]string, error) {
	// Spell every output- and inventory-shaping option explicitly. In
	// particular, an ambient GOFLAGS=-tags=... must not contaminate the
	// default inventory and make a tag-only test disappear from the set the
	// integrity check compares. Likewise, JSON output is not line-oriented
	// `go test -list` output and must not reach the parser below.
	args := make([]string, 0, 5+len(packages)+2)
	args = append(args, "test", "-tags", tags, "-json=false", "-v=false")
	args = append(args, packages...)
	args = append(args, "-list", pattern)
	cmd := exec.Command("go", args...) // #nosec G204 -- executable is fixed; arguments are repository-owned gate metadata.
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go test -list %q: %w\n%s", pattern, err, out)
	}
	var tests []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") || strings.HasPrefix(line, "Fuzz") ||
			strings.HasPrefix(line, "Example") || strings.HasPrefix(line, "Benchmark") {
			tests = append(tests, line)
		}
	}
	return tests, nil
}

// Exemption is one owned exception: an unreachable gate target, an
// uncovered test, or a whole build tag with its own runner.
type Exemption struct {
	Kind   string // "gate", "test", or "tag"
	Name   string
	Owner  string
	Reason string
}

// ParseExemptions reads the exemption ledger: one entry per line,
// `kind name owner reason...`; blank lines and #-comments skipped.
func ParseExemptions(content string) ([]Exemption, error) {
	var out []Exemption
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return nil, fmt.Errorf("exemptions line %d: need `kind name owner reason...`", i+1)
		}
		out = append(out, Exemption{
			Kind:   fields[0],
			Name:   fields[1],
			Owner:  fields[2],
			Reason: strings.Join(fields[3:], " "),
		})
	}
	return out, nil
}

// UncoveredTests returns inventory entries not present in any gate's
// matched selection and not exempted — the silent-rot finding.
func UncoveredTests(inventory []string, matched [][]string, exemptions []Exemption) []string {
	covered := map[string]bool{}
	for _, set := range matched {
		for _, name := range set {
			covered[name] = true
		}
	}
	exempt := map[string]bool{}
	for _, e := range exemptions {
		if e.Kind == "test" {
			exempt[e.Name] = true
		}
	}
	var out []string
	for _, name := range inventory {
		if !covered[name] && !exempt[name] {
			out = append(out, name)
		}
	}
	return out
}

// UnreachableGates returns gates whose targets are neither reachable from
// the roots nor exempted — a gate nobody runs is a gate that rots.
func UnreachableGates(gates []Gate, reachable map[string]bool, exemptions []Exemption) []Gate {
	exempt := map[string]bool{}
	for _, e := range exemptions {
		if e.Kind == "gate" {
			exempt[e.Name] = true
		}
	}
	var out []Gate
	for _, g := range gates {
		if !reachable[g.Target] && !exempt[g.Target] {
			out = append(out, g)
		}
	}
	return out
}
