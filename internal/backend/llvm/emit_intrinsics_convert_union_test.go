package llvm

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"surge/internal/mir"
)

type conversionErrorArmLayout struct {
	caseIndex      int
	payloadOffset  uint64
	payloadSize    uint64
	temporaryAlign uint64
}

func TestConversionErrorsMaterialiseTheirBareUnionMemberWithoutHeapTemporary(t *testing.T) {
	tests := map[string]string{
		"from_bytes": `@entrypoint
fn main() -> int {
    let bytes: byte[] = [0xC3:uint8, 0x28:uint8];
    let result = string.from_bytes(&bytes);
    compare result {
        Success(_) => return 1;
        err => {
            let _ = err;
            return 0;
        }
    }
    return 2;
}
`,
		"from_str": `@entrypoint
fn main() -> int {
    let text: string = "not-a-number";
    let result = uint64.from_str(&text);
    compare result {
        Success(_) => return 1;
        err => {
            let _ = err;
            return 0;
        }
    }
    return 2;
}
`,
	}

	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			module, result := lowerMIRFromSource(t, source)
			emitter := &Emitter{mod: module, types: result.Sema.TypeInterner, syms: result.Symbols.Table}
			arm := findConversionErrorArmLayout(t, emitter)
			ir, err := EmitModule(module, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
			if err != nil {
				t.Fatalf("emit LLVM IR: %v", err)
			}
			body := functionBody(t, ir, findMIRFunc(t, module, "main").ID)
			assertBareErrorMemberLayout(t, body, arm)
		})
	}
}

func findConversionErrorArmLayout(t *testing.T, emitter *Emitter) conversionErrorArmLayout {
	t.Helper()

	var found *conversionErrorArmLayout
	for unionType, published := range emitter.mod.Meta.UnionCases {
		hasSuccess := false
		bareCount := 0
		for _, member := range published {
			hasSuccess = hasSuccess || member.Kind == mir.UnionCaseTag && member.Name == "Success"
			if member.Kind == mir.UnionCaseBareType {
				bareCount++
			}
		}
		if !hasSuccess || bareCount != 1 {
			continue
		}

		members, facts, err := emitter.unionCases(unionType)
		if err != nil {
			t.Fatalf("resolve conversion union type#%d: %v", unionType, err)
		}
		for _, member := range members {
			if member.Kind != mir.UnionCaseBareType {
				continue
			}
			physical, ok := facts.UnionCase(member.PhysicalCaseIndex)
			if !ok {
				t.Fatalf("conversion union type#%d has no physical case %d", unionType, member.PhysicalCaseIndex)
			}
			if found != nil {
				t.Fatal("more than one Erring-like conversion union was published")
			}
			found = &conversionErrorArmLayout{
				caseIndex:     member.PhysicalCaseIndex,
				payloadOffset: physical.PayloadOffset,
				payloadSize:   physical.PayloadSize,
			}
			if facts.Size != 24 || facts.Align != 8 {
				t.Fatalf("conversion union ABI changed: size/align=%d/%d, want 24/8", facts.Size, facts.Align)
			}
			errorFacts, layoutErr := emitter.layoutOf(member.BareType)
			if layoutErr != nil {
				t.Fatalf("resolve bare Error layout: %v", layoutErr)
			}
			if errorFacts.Size != 16 || errorFacts.Align != 8 {
				t.Fatalf("Error ABI changed: size/align=%d/%d, want 16/8", errorFacts.Size, errorFacts.Align)
			}
			found.temporaryAlign = errorFacts.Align
		}
	}
	if found == nil {
		t.Fatal("no Erring-like conversion union with one bare Error member was published")
	}
	if found.caseIndex != 1 || found.payloadOffset != 8 || found.payloadSize != 16 {
		t.Fatalf("conversion Error arm ABI changed: case/offset/size=%d/%d/%d, want 1/8/16",
			found.caseIndex, found.payloadOffset, found.payloadSize)
	}
	return *found
}

func assertBareErrorMemberLayout(t *testing.T, body string, arm conversionErrorArmLayout) {
	t.Helper()

	caseIndex := strconv.Itoa(arm.caseIndex)
	payloadOffset := strconv.FormatUint(arm.payloadOffset, 10)
	payloadSize := strconv.FormatUint(arm.payloadSize, 10)
	temporaryAlign := strconv.FormatUint(arm.temporaryAlign, 10)
	tagStores := regexp.MustCompile(`store i32 `+caseIndex+`, ptr (%[lt][0-9]+), align [0-9]+`).FindAllStringSubmatch(body, -1)
	for _, store := range tagStores {
		// The storage is cleared word by word before the tag, so the payload
		// offset may be addressed more than once; the payload's own address is
		// the one the memcpy fills.
		payloadPattern := `(%t[0-9]+) = getelementptr inbounds i8, ptr ` + regexp.QuoteMeta(store[1]) + `, i64 ` + payloadOffset
		var copy []string
		for _, payload := range regexp.MustCompile(payloadPattern).FindAllStringSubmatch(body, -1) {
			copyPattern := `call void @llvm\.memcpy\.p0\.p0\.i64\(ptr align [0-9]+ ` +
				regexp.QuoteMeta(payload[1]) + `, ptr align [0-9]+ (%[lt][0-9]+), i64 ` + payloadSize + `, i1 false\)`
			if copy = regexp.MustCompile(copyPattern).FindStringSubmatch(body); len(copy) == 2 {
				break
			}
		}
		if len(copy) != 2 {
			continue
		}

		source := regexp.QuoteMeta(copy[1])
		inlineDefinition := regexp.MustCompile(`(?m)^  ` + source + ` = alloca \[` + payloadSize +
			` x i8\], align ` + temporaryAlign + `$`)
		if inlineDefinition.MatchString(body) {
			return
		}
		heapDefinition := regexp.MustCompile(`(?m)^  ` + source + ` = call ptr @rt_alloc\(i64 ` +
			payloadSize + `, i64 ` + temporaryAlign + `\)$`)
		if heapDefinition.MatchString(body) {
			t.Fatalf("Error payload source %s escaped to the heap:\n%s", copy[1], strings.TrimSpace(body))
		}
		t.Fatalf("Error payload source %s has no exact %s-byte/%s-aligned inline definition:\n%s",
			copy[1], payloadSize, temporaryAlign, strings.TrimSpace(body))
	}

	t.Fatalf("Error arm was not materialised as direct case %d with its %d-byte payload at offset %d:\n%s",
		arm.caseIndex, arm.payloadSize, arm.payloadOffset, strings.TrimSpace(body))
}
