package types

import (
	"strings"
	"testing"

	"surge/internal/source"
)

func TestCanonicalTypeArgsKeyIgnoresInternerAllocationOrder(t *testing.T) {
	makeArgs := func(padding int) (*Interner, []TypeID) {
		in := NewInterner()
		in.Strings = source.NewInterner()
		for i := range padding {
			in.RegisterStruct(in.Strings.Intern("Padding"), source.Span{File: source.FileID(i + 1)})
		}
		array := in.Intern(MakeArray(in.Builtins().Int32, 7))
		return in, []TypeID{in.Intern(MakeReference(array, false)), in.Builtins().String}
	}

	leftInterner, leftArgs := makeArgs(0)
	rightInterner, rightArgs := makeArgs(3)
	left, err := (CanonicalKeyContext{Types: leftInterner}).TypeArgsKey(leftArgs)
	if err != nil {
		t.Fatalf("left key: %v", err)
	}
	right, err := (CanonicalKeyContext{Types: rightInterner}).TypeArgsKey(rightArgs)
	if err != nil {
		t.Fatalf("right key: %v", err)
	}
	if left != right {
		t.Fatalf("allocation order changed canonical key:\nleft  %q\nright %q", left, right)
	}
}

func TestCanonicalTypeKeyUsesRemappedParamOwnerThroughNestedTypes(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	param := in.RegisterTypeParam(in.Strings.Intern("T"), 11, 0, false, NoTypeID)
	nested := in.Intern(MakeArray(in.Intern(MakeReference(param, false)), 4))

	ctx := CanonicalKeyContext{Types: in}
	before, err := ctx.TypeKey(nested)
	if err != nil {
		t.Fatalf("key before remap: %v", err)
	}
	in.RemapTypeParamOwners(map[uint32]uint32{11: 91})
	after, err := ctx.TypeKey(nested)
	if err != nil {
		t.Fatalf("key after remap: %v", err)
	}
	if before == after {
		t.Fatalf("owner remap did not change nested parameter identity: %q", before)
	}
	if !strings.Contains(after, "param:91:0:false") {
		t.Fatalf("remapped owner missing from key: %q", after)
	}
}

func TestCanonicalTypeKeyKeepsDistinctAliases(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	target := in.Builtins().Int
	left := in.RegisterAlias(in.Strings.Intern("Left"), source.Span{File: 1, Start: 1, End: 2})
	right := in.RegisterAlias(in.Strings.Intern("Right"), source.Span{File: 1, Start: 3, End: 4})
	in.SetAliasTarget(left, target)
	in.SetAliasTarget(right, target)

	ctx := CanonicalKeyContext{
		Types: in,
		ResolveNominal: func(_ Kind, name string, _ source.Span) (string, error) {
			return "demo::" + name, nil
		},
	}
	leftKey, err := ctx.TypeKey(left)
	if err != nil {
		t.Fatalf("left alias key: %v", err)
	}
	rightKey, err := ctx.TypeKey(right)
	if err != nil {
		t.Fatalf("right alias key: %v", err)
	}
	if leftKey == rightKey {
		t.Fatalf("distinct aliases collapsed to %q", leftKey)
	}
}

func TestCanonicalTypeKeyBoundsNestedShapes(t *testing.T) {
	in := NewInterner()
	id := in.Builtins().Int
	for range canonicalTypeKeyDepthLimit + 1 {
		id = in.Intern(MakeArray(id, 1))
	}
	_, err := (CanonicalKeyContext{Types: in}).TypeKey(id)
	if err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("expected bounded nesting error, got %v", err)
	}
}

func TestCanonicalTypeKeyDoesNotDescendIntoRecursiveNominalFields(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	node := in.RegisterStruct(in.Strings.Intern("Node"), source.Span{File: 7, Start: 10, End: 14})
	in.SetStructFields(node, []StructField{{Name: in.Strings.Intern("next"), Type: node}})
	key, err := (CanonicalKeyContext{
		Types: in,
		ResolveNominal: func(_ Kind, name string, _ source.Span) (string, error) {
			return "graph::" + name, nil
		},
	}).TypeKey(node)
	if err != nil {
		t.Fatalf("recursive nominal key: %v", err)
	}
	if !strings.Contains(key, "Node") {
		t.Fatalf("nominal identity missing from key: %q", key)
	}
}

func TestCanonicalNominalKeyUsesResolverNotFileAllocationID(t *testing.T) {
	makeNode := func(file source.FileID) (TypeID, CanonicalKeyContext) {
		in := NewInterner()
		in.Strings = source.NewInterner()
		span := source.Span{File: file, Start: 10, End: 14}
		node := in.RegisterStruct(in.Strings.Intern("Node"), span)
		ctx := CanonicalKeyContext{
			Types: in,
			ResolveNominal: func(kind Kind, name string, got source.Span) (string, error) {
				if kind != KindStruct || name != "Node" || got != span {
					t.Fatalf("unexpected nominal lookup: kind=%s name=%q span=%v", kind, name, got)
				}
				return "pkg/model::Node", nil
			},
		}
		return node, ctx
	}

	leftNode, leftCtx := makeNode(3)
	rightNode, rightCtx := makeNode(99)
	left, err := leftCtx.TypeKey(leftNode)
	if err != nil {
		t.Fatalf("left nominal key: %v", err)
	}
	right, err := rightCtx.TypeKey(rightNode)
	if err != nil {
		t.Fatalf("right nominal key: %v", err)
	}
	if left != right {
		t.Fatalf("FileID allocation order leaked into key: left=%q right=%q", left, right)
	}
}

func TestCanonicalNominalKeyFailsWithoutPostMergeResolver(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	node := in.RegisterStruct(in.Strings.Intern("Node"), source.Span{File: 1})
	_, err := (CanonicalKeyContext{Types: in}).TypeKey(node)
	if err == nil || !strings.Contains(err.Error(), "post-merge nominal identity resolver") {
		t.Fatalf("expected exact missing-resolver error, got %v", err)
	}
}

func TestCanonicalNominalKeyKeepsSameNameDeclarationsFromDistinctModules(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	name := in.Strings.Intern("Node")
	left := in.RegisterStruct(name, source.Span{File: 1, Start: 10, End: 14})
	right := in.RegisterStruct(name, source.Span{File: 2, Start: 10, End: 14})
	ctx := CanonicalKeyContext{
		Types: in,
		ResolveNominal: func(_ Kind, name string, decl source.Span) (string, error) {
			if decl.File == 1 {
				return "pkg/left::" + name, nil
			}
			return "pkg/right::" + name, nil
		},
	}
	leftKey, err := ctx.TypeKey(left)
	if err != nil {
		t.Fatalf("left key: %v", err)
	}
	rightKey, err := ctx.TypeKey(right)
	if err != nil {
		t.Fatalf("right key: %v", err)
	}
	if leftKey == rightKey {
		t.Fatalf("distinct module declarations collapsed to %q", leftKey)
	}
}
