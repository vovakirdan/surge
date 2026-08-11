package gatecheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A document that tells a reader to run `make X` is making a claim about the
// Makefile, and this file holds that claim to the same standard as a gate: a
// promised command must exist. It grew out of an absent gate that three
// documents called mandatory for months.

// documentedMakeTargetInline matches an inline-code mention of a command:
// `make foo`.
var documentedMakeTargetInline = regexp.MustCompile("`make ([a-z0-9][a-z0-9-]*)")

// documentedMakeTargetCommand matches a bare command line inside a fenced
// code block: an optional shell prompt, optional leading VAR=value
// assignments, then `make <target>`. Epic 23b's authoritative closeout list is
// exactly this shape and carries no backticks at all, so an inline-code-only
// matcher cannot see the single most important command list in the epic. The
// anchor is what keeps prose out: only a line that IS a make command counts,
// never the word "make" in the middle of a sentence.
var documentedMakeTargetCommand = regexp.MustCompile(
	`^[[:space:]]*(?:[$#][[:space:]]+)?(?:[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+)*make[[:space:]]+([a-z0-9][a-z0-9-]*)`)

// DocumentedMakeTargets returns every make target a document tells its reader
// to run, in first-mention order: inline-code mentions anywhere, plus bare
// command lines inside fenced code blocks.
func DocumentedMakeTargets(document string) []string {
	seen := map[string]bool{}
	var found []string
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		found = append(found, name)
	}
	fenced := false
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		for _, m := range documentedMakeTargetInline.FindAllStringSubmatch(line, -1) {
			add(m[1])
		}
		if fenced {
			if m := documentedMakeTargetCommand.FindStringSubmatch(line); m != nil {
				add(m[1])
			}
		}
	}
	return found
}

// RunnableGateSections returns the part of an epic document that claims to
// list gates a reader is supposed to be able to RUN, and "" for a document
// that makes no such claim.
//
// The scope is deliberate and narrow. LIVENESS_PROBES.md is a catalogue of
// probes to run when a surface is touched, and a "Required Commands" section
// is an epic's evidence contract — both promise the named commands exist
// today. Execution PLANS are the opposite: they name the targets a wave is
// about to build, which is how this project writes plans, so holding them to
// this rule would block every lane's commit on a target that is still to be
// written. Everything else (task lists, evidence narratives, candidate
// backlogs) is out of scope for the same reason.
func RunnableGateSections(name, document string) string {
	if strings.EqualFold(filepath.Base(name), "LIVENESS_PROBES.md") {
		return document
	}
	if strings.Contains(strings.ToLower(filepath.Base(name)), "execution-plan") {
		return ""
	}
	var sections []string
	level, inSection, fenced := 0, false, false
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		} else if !fenced && strings.HasPrefix(line, "#") {
			depth := len(line) - len(strings.TrimLeft(line, "#"))
			if inSection && depth <= level {
				inSection = false
			}
			if !inSection && strings.Contains(strings.ToLower(line), "required commands") {
				inSection, level = true, depth
			}
		}
		if inSection {
			sections = append(sections, line)
		}
	}
	return strings.Join(sections, "\n")
}

// MissingDocumentedTargets returns the make targets a document names but the
// Makefile does not define and no exemption owns.
func MissingDocumentedTargets(document, makefile string, exemptions []Exemption) []string {
	defined := map[string]bool{}
	for _, line := range strings.Split(makefile, "\n") {
		if m := targetRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "\t") {
			defined[m[1]] = true
		}
	}
	for _, e := range exemptions {
		if e.Kind == "target" {
			defined[e.Name] = true
		}
	}
	var missing []string
	for _, name := range DocumentedMakeTargets(document) {
		if !defined[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// The defect that created this gate was not a wrong gate, it was an absent
// one: `make runtime-v2-carrier-sanitizer-check` was named as mandatory and
// nothing defined it, so it never ran and nobody noticed. This check makes
// that shape impossible to repeat wherever a document promises a runnable
// gate.
//
// Two of the three places that name this gate are visible to it: epic 23b's
// section 12 closeout list names it twice, once as a bare line in the fenced
// command block and once in inline code. The third, LIVENESS_PROBES.md, names
// the target WITHOUT the word `make`, and a matcher for bare backticked
// identifiers would flag half the prose in these documents — so that mention
// is knowingly out of reach here.
func TestEveryEpicDocumentedMakeTargetExists(t *testing.T) {
	root := repoRoot(t)
	makefile := makefileText(t)
	ledger, err := os.ReadFile(filepath.Join(root, "internal", "gatecheck", "exemptions.txt"))
	if err != nil {
		t.Fatalf("read exemptions: %v", err)
	}
	exemptions, err := ParseExemptions(string(ledger))
	if err != nil {
		t.Fatalf("parse exemptions: %v", err)
	}
	docs := filepath.Join(root, "docs", "runtime-v2-epics")
	scanned := 0
	sawFencedClosingList := false
	// Subdirectories are walked: the epic's own task directories carry
	// evidence documents, and a "Required Commands" list there promises a
	// runnable gate exactly as the top-level one does.
	walkErr := filepath.WalkDir(docs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path) // #nosec G304 -- repository-owned documentation tree.
		if readErr != nil {
			return readErr
		}
		scope := RunnableGateSections(entry.Name(), string(data))
		if scope == "" {
			return nil
		}
		scanned++
		rel, relErr := filepath.Rel(docs, path)
		if relErr != nil {
			rel = path
		}
		if strings.Contains(entry.Name(), "23b-inline-storage") {
			for _, name := range DocumentedMakeTargets(scope) {
				// A bare fenced line, spelled with no backticks anywhere in
				// the document: proof the fenced matcher is live rather than
				// the inline one carrying this document alone.
				if name == "cppcheck" {
					sawFencedClosingList = true
				}
			}
		}
		for _, name := range MissingDocumentedTargets(scope, makefile, exemptions) {
			t.Errorf("%s names `make %s`, which no Makefile target defines and no exemption owns", rel, name)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk epic docs: %v", walkErr)
	}
	if scanned == 0 {
		t.Fatal("no document claiming runnable gates was scanned; the check would pass vacuously")
	}
	if !sawFencedClosingList {
		t.Fatal("epic 23b section 12's fenced command list was not read; the fenced-block matcher is dead")
	}
}

// The negative control for the check above, on synthetic input: it must flag a
// documented target that does not exist, must not flag one that does, and must
// see a bare command inside a fenced block as well as an inline mention.
func TestMissingDocumentedTargetsNegativeControl(t *testing.T) {
	makefile := "real-check:\n\t$(GO) test ./internal/vm -run '^TestX$$' -count=1 -v\n"
	document := "Run `make real-check` twice, then `make phantom-check`."
	missing := MissingDocumentedTargets(document, makefile, nil)
	if len(missing) != 1 || missing[0] != "phantom-check" {
		t.Fatalf("absent documented target not flagged: %v", missing)
	}
	if flagged := MissingDocumentedTargets("`make real-check`", makefile, nil); len(flagged) != 0 {
		t.Fatalf("existing target wrongly flagged: %v", flagged)
	}
	owned := []Exemption{{Kind: "target", Name: "phantom-check", Owner: "o", Reason: "planned"}}
	if flagged := MissingDocumentedTargets(document, makefile, owned); len(flagged) != 0 {
		t.Fatalf("owned exemption not honored: %v", flagged)
	}

	fenced := "Record:\n\n```bash\nEPIC_BASE=\"x\"\nmake real-check\nEPIC_BASE=\"$EPIC_BASE\" make fenced-phantom\n```\n\nMake sure to note it.\n"
	if flagged := MissingDocumentedTargets(fenced, makefile, nil); len(flagged) != 1 ||
		flagged[0] != "fenced-phantom" {
		t.Fatalf("bare fenced command list not read as commands: %v", flagged)
	}
	// Prose is not a command list: "make sure" outside a fence must not
	// become a phantom target, and neither must it inside one.
	if flagged := MissingDocumentedTargets("Then make sure the tree is clean.", makefile, nil); len(flagged) != 0 {
		t.Fatalf("prose read as a make command: %v", flagged)
	}
}

// The scope rule is itself a gate: it decides which documents may block a
// commit. It must cover an evidence contract and the probe catalogue, and it
// must leave execution plans alone — a plan naming the target its wave is
// about to build is correct, not broken.
func TestRunnableGateSectionsScope(t *testing.T) {
	document := "# Epic\n\n## 3. Plan\n\nRun `make future-thing` once it exists.\n\n" +
		"## 12. Required Commands And Evidence\n\n```bash\nmake real-check\n```\n\n## 13. Risks\n\n`make other-thing`\n"
	scope := RunnableGateSections("23z-epic.md", document)
	if !strings.Contains(scope, "make real-check") {
		t.Fatalf("the required-commands section was not selected:\n%s", scope)
	}
	if strings.Contains(scope, "future-thing") || strings.Contains(scope, "other-thing") {
		t.Fatalf("scope leaked past the required-commands section:\n%s", scope)
	}
	if scope := RunnableGateSections("23b-wave-d-execution-plan.md", document); scope != "" {
		t.Fatalf("an execution plan is in scope; every planned target would block a commit:\n%s", scope)
	}
	whole := RunnableGateSections("LIVENESS_PROBES.md", document)
	if !strings.Contains(whole, "future-thing") || !strings.Contains(whole, "real-check") {
		t.Fatalf("the probe catalogue is scanned whole:\n%s", whole)
	}
	if scope := RunnableGateSections("14-phase4-remote-channels.md", "# Epic\n\nNo command list here.\n"); scope != "" {
		t.Fatalf("a document with no runnable-gate claim is in scope:\n%s", scope)
	}
}
