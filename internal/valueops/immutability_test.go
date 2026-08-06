package valueops

import (
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"

	"surge/internal/types"
)

// allowedDependencies is the exact transitive package set internal/valueops may
// reach. It is written out in full rather than described by a rule, so that
// gaining a dependency is a deliberate edit to this list and not a silent
// consequence of an import.
//
// The compiler analysis packages are absent on purpose: a registry that could
// reach sema, hir, mono, mir, driver or buildpipeline could hold a live view
// onto compiler state, and the whole point of freezing plain data is that it
// cannot. internal/types and internal/layout are allowed, and everything else
// here arrives transitively through those two and internal/symbols.
var allowedDependencies = []string{
	"surge/internal/ast",
	"surge/internal/diag",
	"surge/internal/dialect",
	"surge/internal/fix",
	"surge/internal/layout",
	"surge/internal/project",
	"surge/internal/source",
	"surge/internal/symbols",
	"surge/internal/token",
	"surge/internal/types",
	"surge/internal/valueops",
}

// TestPackageReachesOnlyTheAllowedDependencies enforces the independence
// guarantee mechanically. The standalone-ness of this package is the deliverable,
// so it is checked by the build graph and not by review.
func TestPackageReachesOnlyTheAllowedDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go tool unavailable: %v", err)
	}
	out, err := exec.Command("go", "list", "-deps", "surge/internal/valueops").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, out)
	}

	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if pkg := strings.TrimSpace(line); strings.HasPrefix(pkg, "surge/") {
			got = append(got, pkg)
		}
	}
	sort.Strings(got)

	allowed := make(map[string]bool, len(allowedDependencies))
	for _, pkg := range allowedDependencies {
		allowed[pkg] = true
	}
	for _, pkg := range got {
		if !allowed[pkg] {
			t.Errorf("internal/valueops depends on %s, which is not in the allowed set", pkg)
		}
	}
	seen := make(map[string]bool, len(got))
	for _, pkg := range got {
		seen[pkg] = true
	}
	for _, pkg := range allowedDependencies {
		if !seen[pkg] {
			t.Errorf("allowed set lists %s, which is no longer a dependency; prune the list", pkg)
		}
	}

	// Named explicitly so a failure says which boundary was crossed.
	for _, forbidden := range []string{
		"surge/internal/sema", "surge/internal/hir", "surge/internal/mono",
		"surge/internal/mir", "surge/internal/driver", "surge/internal/buildpipeline",
	} {
		if seen[forbidden] {
			t.Errorf("internal/valueops depends on %s; the registry must not reach compiler state", forbidden)
		}
	}
}

// TestExportedSurfaceCarriesNoLiveReferences walks the exported shape of every
// public type and refuses anything a caller could use to reach shared mutable
// storage. Slices of value types are allowed because they are cloned on return;
// pointers, interfaces, funcs, chans and maps are not, because they cannot be.
func TestExportedSurfaceCarriesNoLiveReferences(t *testing.T) {
	registryType := reflect.TypeOf(Registry{})
	for i := range registryType.NumField() {
		if field := registryType.Field(i); field.IsExported() {
			t.Errorf("Registry.%s is exported; the frozen state must stay private", field.Name)
		}
	}

	seen := map[reflect.Type]bool{}
	for _, root := range []any{Entry{}, KeyEntry{}, Capabilities{}, Evidence{}, Input{}, Root{}, KeyOps{}} {
		rootType := reflect.TypeOf(root)
		assertOwnedValues(t, rootType.Name(), rootType, seen)
	}
}

func assertOwnedValues(t *testing.T, path string, rt reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	switch rt.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		t.Errorf("%s is a %s; the exported surface must carry owned values only", path, rt.Kind())
	case reflect.Map:
		// Input is the caller's own plain-data description and is consumed by
		// value at Finalize, so its maps never become registry state.
		if path != "Input.Values" && path != "Input.Keys" {
			t.Errorf("%s is a map; the exported surface must carry owned values only", path)
			return
		}
		assertOwnedValues(t, path+"[key]", rt.Key(), seen)
		assertOwnedValues(t, path+"[value]", rt.Elem(), seen)
	case reflect.Slice, reflect.Array:
		assertOwnedValues(t, path+"[]", rt.Elem(), seen)
	case reflect.Struct:
		if seen[rt] {
			return
		}
		seen[rt] = true
		for i := range rt.NumField() {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			assertOwnedValues(t, path+"."+field.Name, field.Type, seen)
		}
	default:
	}
}

// TestMutatingAReturnedEntryDoesNotAffectTheRegistry is the behavioural half of
// immutability: the reflection walk says the shape permits owning, this says the
// code actually owns.
func TestMutatingAReturnedEntryDoesNotAffectTheRegistry(t *testing.T) {
	entry := testEntry(types.TypeID(100))
	registry := mustFinalize(t, valueInput(entry))
	before := mustHash(t, registry)

	borrowed, err := registry.Value(entry.Type)
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	borrowed.Evidence.ShardPath[0] = 4242
	borrowed.Evidence.CrossClonePath[0] = 4242

	after, err := registry.Value(entry.Type)
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if after.Evidence.ShardPath[0] != 7 || after.Evidence.CrossClonePath[0] != 9 {
		t.Errorf("mutating a returned entry's evidence paths changed the registry: %+v", after.Evidence)
	}
	if got := mustHash(t, registry); got != before {
		t.Errorf("registry hash moved from %s to %s after a caller mutated its own copy", before, got)
	}
}

// TestMutatingTheInputAfterFinalizeDoesNotAffectTheRegistry closes the other
// direction: the caller keeps its slices, and the registry does not share them.
func TestMutatingTheInputAfterFinalizeDoesNotAffectTheRegistry(t *testing.T) {
	entry := testEntry(types.TypeID(101))
	callerPath := entry.Evidence.ShardPath
	registry := mustFinalize(t, valueInput(entry))
	before := mustHash(t, registry)

	callerPath[0] = 4242

	stored, err := registry.Value(entry.Type)
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if stored.Evidence.ShardPath[0] != 7 {
		t.Errorf("the registry shares evidence storage with its input: %v", stored.Evidence.ShardPath)
	}
	if got := mustHash(t, registry); got != before {
		t.Errorf("registry hash moved from %s to %s after the caller mutated its own input", before, got)
	}
}

// TestTwoCallersGetIndependentCopies keeps sharing between callers impossible
// even where entry storage is shared inside the registry.
func TestTwoCallersGetIndependentCopies(t *testing.T) {
	registry := mustFinalize(t, valueInput(testEntry(types.TypeID(102)), testEntry(types.TypeID(103))))

	first, err := registry.Value(types.TypeID(102))
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	second, err := registry.Value(types.TypeID(103))
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	first.Evidence.ShardPath[0] = 4242
	if second.Evidence.ShardPath[0] != 7 {
		t.Error("two callers of a shared entry received the same evidence storage")
	}
}
