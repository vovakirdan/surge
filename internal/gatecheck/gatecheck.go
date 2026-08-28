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

// A leading `-flag` after `$(MAKE)` is an option to make, not the target being
// called, so the target is the first word after any run of them.
var makeCallRe = regexp.MustCompile(`\$\(MAKE\)\s+(?:-\S+\s+)*([A-Za-z0-9_.][A-Za-z0-9_.-]*)`)

// A recipe reference to a make variable, e.g. `$(RUNTIME_V2_SUBGATES)`.
var varRefRe = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_]*)\)`)

// A variable definition at column 0, e.g. `NAME := a b c`.
var varDefRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[:?+]?=(.*)$`)

// parseListVars reads the simple `NAME := words...` definitions, joining
// backslash continuations, so a recipe that walks a variable can be followed
// through it.
func parseListVars(makefile string) map[string][]string {
	vars := map[string][]string{}
	lines := strings.Split(makefile, "\n")
	for i := 0; i < len(lines); i++ {
		m := varDefRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		value := m[2]
		for strings.HasSuffix(strings.TrimRight(value, " \t"), "\\") && i+1 < len(lines) {
			value = strings.TrimSuffix(strings.TrimRight(value, " \t"), "\\")
			i++
			value += " " + lines[i]
		}
		vars[m[1]] = strings.Fields(value)
	}
	return vars
}

// ReachableTargets walks the three edge kinds this Makefile has — target-line
// prerequisites (`a: b c`), `$(MAKE) <target>` recipe calls, and a recipe that
// loops over a variable holding target names — from the given roots and returns
// every transitively reachable target name.
//
// The third kind is not decoration. `runtime-v2-check` names its sub-gates in
// RUNTIME_V2_SUBGATES and walks that list in one shell loop, precisely so a red
// sub-gate cannot stop the rows behind it the way a sequence of recipe lines
// does. Without this edge every one of those sub-gates would read as
// unreachable and the gate-integrity test would demand an exemption for a gate
// that in fact runs on every invocation.
//
// Only words that are themselves defined targets are followed, so a variable
// holding flags or paths contributes nothing. A misspelled roster entry is
// caught where it does damage instead: make refuses an unknown goal, so the
// aggregate's own call fails and is reported as that row's FAIL.
func ReachableTargets(makefile string, roots ...string) map[string]bool {
	recipes := map[string][]string{}
	prereqs := map[string][]string{}
	defined := map[string]bool{}
	target := ""
	for _, raw := range strings.Split(makefile, "\n") {
		if m := targetRe.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(raw, "\t") {
			target = m[1]
			defined[target] = true
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
	listVars := parseListVars(makefile)
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
			for _, m := range makeCallRe.FindAllStringSubmatch(line, -1) {
				visit(m[1])
			}
			for _, m := range varRefRe.FindAllStringSubmatch(line, -1) {
				for _, word := range listVars[m[1]] {
					if defined[word] {
						visit(word)
					}
				}
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
