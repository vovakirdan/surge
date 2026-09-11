package llvm

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This reads the emitted function, not emitter bookkeeping. Each numeric heap
// arm has exactly one predecessor: the block whose same-word tag/NULL test
// selects it. Requiring that immediate edge is stronger than merely finding a
// guard somewhere earlier in the text, including inside a different union arm.
type numericIRBlock struct {
	name  string
	lines []string
	preds map[string]bool
}

type numericIRDef struct {
	block string
	text  string
}

var numericIRLabel = regexp.MustCompile(`label %([A-Za-z0-9_.$-]+)`)

func numericIRBlocks(t *testing.T, body string) ([]*numericIRBlock, map[string]numericIRDef) {
	t.Helper()
	var blocks []*numericIRBlock
	byName := map[string]*numericIRBlock{}
	defs := map[string]numericIRDef{}
	var block *numericIRBlock
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line == "}" || strings.HasPrefix(line, "define ") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			name := strings.TrimSuffix(line, ":")
			if byName[name] != nil {
				t.Fatalf("duplicate LLVM block %s:\n%s", name, body)
			}
			block = &numericIRBlock{name: name, preds: map[string]bool{}}
			blocks = append(blocks, block)
			byName[name] = block
			continue
		}
		if block == nil {
			t.Fatalf("instruction outside a block: %s", line)
		}
		if name, rhs, ok := strings.Cut(line, " = "); ok {
			if _, duplicate := defs[name]; duplicate {
				t.Fatalf("duplicate SSA definition %s:\n%s", name, body)
			}
			defs[name] = numericIRDef{block.name, rhs}
		}
		block.lines = append(block.lines, line)
	}
	if len(blocks) == 0 {
		t.Fatalf("no emitted blocks:\n%s", body)
	}
	for _, b := range blocks {
		terminators := 0
		for i, line := range b.lines {
			if strings.HasPrefix(line, "br ") || strings.HasPrefix(line, "ret ") ||
				strings.HasPrefix(line, "switch ") || line == "unreachable" {
				terminators++
				// Numeric fixtures emit the complete switch on one line.
				if i != len(b.lines)-1 {
					t.Fatalf("instruction after terminator in %s: %v", b.name, b.lines)
				}
				for _, match := range numericIRLabel.FindAllStringSubmatch(line, -1) {
					target := byName[match[1]]
					if target == nil {
						t.Fatalf("%s branches to missing block %s", b.name, match[1])
					}
					target.preds[b.name] = true
				}
			}
		}
		if terminators != 1 {
			t.Fatalf("block %s has %d terminators, want one: %v", b.name, terminators, b.lines)
		}
	}
	return blocks, defs
}

type numericIRGuard struct{ from, word string }

var numericIRBranch = regexp.MustCompile(`^br i1 (%[\w.]+), label %([\w.]+), label %([\w.]+)$`)
var numericIRAnd = regexp.MustCompile(`^and i1 (%[\w.]+), (%[\w.]+)$`)
var numericIRNonnull = regexp.MustCompile(`^icmp ne ptr (%[\w.]+), null$`)
var numericIRAligned = regexp.MustCompile(`^icmp eq i64 (%[\w.]+), 0$`)
var numericIRLowBit = regexp.MustCompile(`^and i64 (%[\w.]+), 1$`)

func numericHeapGuards(t *testing.T, blocks []*numericIRBlock, defs map[string]numericIRDef) map[string]numericIRGuard {
	t.Helper()
	guards := map[string]numericIRGuard{}
	for _, b := range blocks {
		branch := numericIRBranch.FindStringSubmatch(b.lines[len(b.lines)-1])
		if branch == nil {
			continue
		}
		both := numericIRAnd.FindStringSubmatch(defs[branch[1]].text)
		if both == nil {
			continue
		}
		nonnull := numericIRNonnull.FindStringSubmatch(defs[both[1]].text)
		aligned := numericIRAligned.FindStringSubmatch(defs[both[2]].text)
		if nonnull == nil || aligned == nil {
			continue
		}
		lowBit := numericIRLowBit.FindStringSubmatch(defs[aligned[1]].text)
		if lowBit == nil || defs[lowBit[1]].text != "ptrtoint ptr "+nonnull[1]+" to i64" {
			t.Fatalf("numeric tag and NULL checks do not test the same word in %s", b.name)
		}
		for _, name := range []string{branch[1], both[1], both[2], aligned[1], lowBit[1]} {
			if defs[name].block != b.name {
				t.Fatalf("guard definition %s is not in its condition block %s", name, b.name)
			}
		}
		if branch[2] == branch[3] {
			t.Fatalf("heap and inline edges have the same target in %s", b.name)
		}
		guards[branch[2]] = numericIRGuard{b.name, nonnull[1]}
	}
	return guards
}

type numericIRCounts struct{ retain, release, clone, unshare int }

var numericIRGEP = regexp.MustCompile(`^getelementptr inbounds i8, ptr (%[\w.]+), i64 (\d+)$`)
var numericIRMemory = regexp.MustCompile(`(?:load i32,|store i32 [^,]+,) ptr (%[\w.]+)(?:,|$)`)
var numericIRCall = regexp.MustCompile(`call (?:void|ptr) @rt_big(int|uint)_(retain|release|clone|unshare)\(ptr (%[\w.]+)\)`)

func assertNumericHeapGuards(t *testing.T, body, kind string) numericIRCounts {
	t.Helper()
	if strings.Contains(body, retainScratchGlobal) {
		t.Fatalf("integer lifecycle touches float scratch:\n%s", body)
	}
	blocks, defs := numericIRBlocks(t, body)
	guards := numericHeapGuards(t, blocks, defs)
	if len(guards) == 0 {
		t.Fatalf("numeric lifecycle guard census is zero:\n%s", body)
	}
	offset := uint64(8)
	if kind == "uint" {
		offset = 4
	}
	check := func(b *numericIRBlock, word string) {
		g, ok := guards[b.name]
		if !ok || g.word != word || len(b.preds) != 1 || !b.preds[g.from] {
			t.Fatalf("RC operation on %s lacks same-word heap domination in %s:\n%s", word, b.name, body)
		}
	}
	var counts numericIRCounts
	for _, b := range blocks {
		for _, line := range b.lines {
			rhs := line
			if _, value, ok := strings.Cut(line, " = "); ok {
				rhs = value
			}
			if gep := numericIRGEP.FindStringSubmatch(rhs); gep != nil && strings.HasPrefix(defs[gep[1]].text, "load ptr, ") {
				check(b, gep[1])
				if gep[2] != strconv.FormatUint(offset, 10) {
					t.Fatalf("%s RC offset = %s, want %d", kind, gep[2], offset)
				}
			}
			if mem := numericIRMemory.FindStringSubmatch(line); mem != nil {
				address := defs[mem[1]]
				if gep := numericIRGEP.FindStringSubmatch(address.text); gep != nil {
					check(b, gep[1])
					if address.block != b.name || gep[2] != strconv.FormatUint(offset, 10) {
						t.Fatalf("RC address is outside its heap arm or has the wrong offset: %s", line)
					}
					if strings.HasPrefix(line, "store i32 ") {
						counts.retain++
					}
				} else if strings.HasPrefix(address.text, "load ptr, ") {
					t.Fatalf("integer RC access omitted its suffix offset: %s", line)
				}
			}
			if call := numericIRCall.FindStringSubmatch(line); call != nil {
				check(b, call[3])
				if call[1] != kind {
					t.Fatalf("wrong numeric lifecycle kind: %s", line)
				}
				switch call[2] {
				case "retain":
					t.Fatal("Copy retain must be inline, not a runtime call")
				case "release":
					counts.release++
				case "clone":
					counts.clone++
					assertNumericUpdateStoredInHeapArm(t, blocks, b.name, line)
				case "unshare":
					counts.unshare++
					assertNumericUpdateStoredInHeapArm(t, blocks, b.name, line)
				}
			}
		}
	}
	if counts == (numericIRCounts{}) {
		t.Fatalf("guards were emitted without any numeric lifecycle work:\n%s", body)
	}
	return counts
}

func assertNumericUpdateStoredInHeapArm(t *testing.T, blocks []*numericIRBlock, heap, call string) {
	t.Helper()
	result, _, ok := strings.Cut(call, " = ")
	if !ok {
		t.Fatalf("numeric update discarded its new owner: %s", call)
	}
	stores := 0
	for _, b := range blocks {
		for _, line := range b.lines {
			if strings.HasPrefix(line, "store ptr "+result+", ") {
				stores++
				if b.name != heap {
					t.Fatalf("numeric update result escaped its heap arm into %s: %s", b.name, line)
				}
			}
		}
	}
	if stores != 1 {
		t.Fatalf("numeric update store census = %d, want one: %s", stores, call)
	}
}

func requireNumericOperation(t *testing.T, counts numericIRCounts, operation string, want int) {
	t.Helper()
	got := map[string]int{"retain": counts.retain, "release": counts.release, "clone": counts.clone, "unshare": counts.unshare}[operation]
	if got < want || (want > 1 && got != want) {
		t.Fatalf("%s lifecycle census = %d, want %d", operation, got, want)
	}
}
