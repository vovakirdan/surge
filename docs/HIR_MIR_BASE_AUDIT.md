# HIR/MIR Base Audit

**Stage 0 — Frontend Readiness Assessment**
*December 2025*

This document audits the current Surge frontend infrastructure that will serve as the foundation for future HIR/MIR implementation. No new features are introduced; this is a documentation-only assessment.

---

## 1. Summary

The Surge frontend has solid foundations for HIR/MIR:

1. ✅ **Typed Expression Storage** — `ExprTypes` map provides complete type annotations for all expressions
2. ✅ **ID System** — Strongly-typed uint32 IDs (`ExprID`, `StmtID`, `TypeID`, `SymbolID`) with validation
3. ✅ **Type Interner** — Deduplication and structural equality for all types
4. ✅ **Borrow Tracking** — `BorrowTable` with place-based tracking, exported via `sema.Result`
5. ✅ **Function Instantiations** — `FunctionInstantiations` map tracks generic usage per function
6. ✅ **Module Caching** — `ModuleMeta`, `ModuleCache`, `DiskCache` ready for IR artifacts
7. ⚠️ **Gaps** — No explicit HIR layer; borrow data lacks graph structure; instantiation tracking split across two systems

**Overall Assessment:** The Typed AST layer can serve as input for HIR. Existing data structures are well-designed and exported appropriately.

---

## 2. Typed AST & IDs

### 2.1 ID Types

| ID Type | File | Purpose |
|---------|------|---------|
| `ast.ExprID` | `internal/ast/ids.go:5` | Expression nodes |
| `ast.StmtID` | `internal/ast/ids.go:6` | Statement nodes |
| `ast.TypeID` | `internal/ast/ids.go:7` | AST type expressions |
| `ast.ItemID` | `internal/ast/ids.go:4` | Top-level items |
| `types.TypeID` | `internal/types/types.go:5` | Resolved types in interner |
| `symbols.SymbolID` | `internal/symbols/ids.go:14` | Symbol table entries |
| `symbols.ScopeID` | `internal/symbols/ids.go:3` | Scope hierarchy |

All IDs use `uint32` with zero as invalid sentinel (`NoExprID`, `NoTypeID`, etc.). Each has an `IsValid()` method for validation.

### 2.2 Expression Type Storage

**Primary:** `sema.Result.ExprTypes` — `map[ast.ExprID]types.TypeID`
- Location: `internal/sema/check.go:24`
- Populated by: `typeExpr()` in `type_expr.go:392`
- Cache-hit check: `type_expr.go:20-22`

```go
// From type_expr.go:20-22 — cache lookup
if ty, ok := tc.result.ExprTypes[id]; ok {
    return ty  // Retrieve cached type
}
```

**Secondary:** `typeChecker.bindingTypes` — `map[symbols.SymbolID]types.TypeID`
- Location: `internal/sema/binding_types.go:15`
- Internal to typeChecker, also synced to `Symbol.Type`
- Stores types for let/param bindings

```go
// From binding_types.go:10-18 — setting binding type
func (tc *typeChecker) setBindingType(symID symbols.SymbolID, ty types.TypeID) {
    if !symID.IsValid() || ty == types.NoTypeID {
        return
    }
    if tc.bindingTypes == nil {
        tc.bindingTypes = make(map[symbols.SymbolID]types.TypeID)
    }
    tc.bindingTypes[symID] = ty
    tc.assignSymbolType(symID, ty)  // Also updates symbol table
}
```

### 2.3 Type Interner

**Location:** `internal/types/interner.go:32-48`

```go
type Interner struct {
    types    []Type              // Storage for type descriptors
    index    map[typeKey]TypeID  // Hash-based deduplication
    builtins Builtins            // Primitive TypeIDs (Int, Bool, String, etc.)
    structs  []StructInfo        // Nominal type metadata
    // ... unions, enums, aliases, tuples, functions
}
```

**Type Descriptor** (`internal/types/types.go:98-106`):
```go
type Type struct {
    Kind    Kind      // One of KindInt, KindStruct, KindArray, KindFn, etc.
    Elem    TypeID    // Element type (for pointers, arrays, references, etc.)
    Count   uint32    // Array count (ArrayDynamicLength for slices)
    Width   Width     // Numeric precision (8, 16, 32, 64 bits)
    Mutable bool      // For references: mutable vs immutable
    Payload uint32    // Index into metadata arrays for nominal types
}
```

**Assessment:** Ready to serve as HIR type backing. Each unique type structure maps to exactly one `TypeID` through hash-based interning.

---

## 3. Borrow Data

### 3.1 Core Structures

**Location:** `internal/sema/borrow.go`

| Structure | Lines | Purpose |
|-----------|-------|---------|
| `BorrowID` | 15-17 | Identifies active borrow (uint32) |
| `BorrowKind` | 20-26 | `BorrowShared` or `BorrowMut` |
| `Place` | 47-55 | Base symbol + projection path |
| `PlaceSegment` | 40-44 | Path element (Field/Index/Deref) |
| `BorrowInfo` | 63-70 | Full borrow metadata |
| `BorrowTable` | 94-112 | Central tracking structure |
| `Interval` | 57-61 | Lifetime: FromExpr to ToScope |

### 3.2 BorrowTable Fields

```go
type BorrowTable struct {
    infos        []BorrowInfo                      // All borrow metadata
    placeState   map[Place]borrowState             // Current state per place
    exprBorrow   map[ast.ExprID]BorrowID           // Expression → Borrow
    scopeBorrows map[symbols.ScopeID][]BorrowID    // Scope → Active borrows
    paths        map[placeKey][]PlaceSegment       // Path interning
}
```

### 3.3 BorrowInfo Structure

```go
type BorrowInfo struct {
    ID    BorrowID
    Kind  BorrowKind        // BorrowShared or BorrowMut
    Place Place             // Location (variable + field/index path)
    Span  source.Span
    Life  Interval          // FromExpr to ToScope
}
```

### 3.4 Export to Result

**Location:** `internal/sema/scope_stack.go:150-160`

```go
func (tc *typeChecker) flushBorrowResults() {
    if snapshot := tc.borrow.ExprBorrowSnapshot(); len(snapshot) > 0 {
        tc.result.ExprBorrows = snapshot
    }
    if infos := tc.borrow.Infos(); len(infos) > 0 {
        tc.result.Borrows = infos
    }
}
```

**Assessment:** Borrow data is exported via `sema.Result.ExprBorrows` and `sema.Result.Borrows`. The model is **imperative/state-machine based**, not graph-based. The place-based tracking with hierarchical paths is powerful but lives only inside the semantic pass until exported.

**For HIR:** May want to build explicit `BorrowGraph` structure from this data, converting the imperative model to an explicit graph for optimization passes.

---

## 4. Generic Instantiations

### 4.1 Function Instantiation Map

**Location:** `internal/sema/type_checker_instantiations.go:23-26`

```go
if tc.result.FunctionInstantiations == nil {
    tc.result.FunctionInstantiations = make(map[symbols.SymbolID][][]types.TypeID)
}
tc.result.FunctionInstantiations[symID] = append(
    tc.result.FunctionInstantiations[symID],
    append([]types.TypeID(nil), args...),
)
```

**Structure:** `map[symbols.SymbolID][][]types.TypeID`
- Key: Function's SymbolID
- Value: List of type argument combinations used

**Deduplication:** Via `fnInstantiationSeen` map with string keys (`"symID#arg1#arg2..."`)

### 4.2 Type Instantiation Cache

**Location:** `internal/sema/type_decl_instantiate.go:14-25`

```go
// Key format: strconv.FormatUint(uint64(symID), 10) + "#" + arg1 + "#" + arg2 ...
typeInstantiations map[string]types.TypeID
typeInstantiationInProgress map[string]struct{}  // Cycle detection
```

**Cycle Detection:** Tracks in-progress instantiations to prevent infinite recursion (e.g., `struct User { id: TypedId<User> }`).

**Supported Types:** Struct, Alias, and Union instantiations (see `instantiateTypeDecl()` lines 44-116).

### 4.3 Assessment

| Aspect | Status | Notes |
|--------|--------|-------|
| Function instantiations | ✅ Exported | Via `sema.Result.FunctionInstantiations` |
| Type instantiations | ⚠️ Internal | Uses separate cache, NOT exported |
| Key format | ⚠️ String-based | Not structured `InstantiationKey` type |
| Deduplication | ✅ Working | Via `fnInstantiationSeen` map |
| Cycle detection | ✅ Working | For type instantiation |

**For HIR:** Should unify into single `InstantiationKey` struct for both functions and types. Export type instantiation map alongside function instantiations.

---

## 5. ModuleMeta & Caching

### 5.1 ModuleMeta Structure

**Location:** `internal/project/modulemeta.go:31-43`

```go
type ModuleMeta struct {
    Name            string
    Path            string              // Normalized module path: "a/b"
    Dir             string              // Module directory path
    Kind            ModuleKind          // module or binary
    NoStd           bool                // @no_std pragma flag
    HasModulePragma bool
    Span            source.Span
    Imports         []ImportMeta        // Normalized import paths with spans
    Files           []ModuleFileMeta    // Source files with hashes
    ContentHash     Digest              // SHA256 hash of module's own content
    ModuleHash      Digest              // Module hash including dependencies
}
```

### 5.2 In-Memory Cache

**Location:** `internal/driver/modulecache.go`

```go
type ModuleCache struct {
    mu    sync.RWMutex
    byMod map[string]cached  // key: module path (canonical "a/b")
}

type cached struct {
    content project.Digest
    meta    *project.ModuleMeta
    broken  bool
    first   *diag.Diagnostic  // First error encountered
}
```

Thread-safe via `sync.RWMutex`. Validates via content hash comparison.

### 5.3 Disk Cache

**Location:** `internal/driver/dcache.go:31-59`

```go
type DiskPayload struct {
    Schema          uint16              // Version for safe evolution
    Name            string
    Path            string
    Dir             string
    Kind            uint8
    NoStd           bool
    HasModulePragma bool
    ImportPaths     []string
    FilePaths       []string
    FileHashes      []project.Digest
    ContentHash     project.Digest
    ModuleHash      project.Digest
    DependencyHash  project.Digest      // Hash of all dependency hashes
    Broken          bool
}
```

**Storage:** `~/.cache/surge/mods/{hex-hash}.mp` (msgpack binary format)

### 5.4 Assessment

**Ready for HIR/MIR extension:**
- Add `HIRData []byte` field to `DiskPayload`
- Increment `diskCacheSchemaVersion` for automatic invalidation
- `ModuleMeta` can hold in-memory `*HIRModule` pointer (not serialized)
- Existing parallel batch processing via `topo.Batches` ready for IR computation

---

## 6. Proposed TODOs for Stage 1 (HIR Introduction)

1. **Create `internal/hir` package** with:
   - `HIRModule`, `HIRFunc`, `HIRExpr`, `HIRStmt` types
   - `HNodeID` for HIR-specific node identification

2. **Define `InstantiationKey` struct:**
   ```go
   type InstantiationKey struct {
       SymID    symbols.SymbolID
       TypeArgs []types.TypeID
   }
   ```
   Replace string-based keys in both function and type instantiation tracking.

3. **Add HIR pass after successful sema:**
   - Input: AST + `ExprTypes` + `bindingTypes` + borrow data
   - Output: `HIRModule` with typed nodes

4. **Attach HIR to module result:**
   - Add `HIR *hir.Module` field to result structure
   - CLI flag `--emit-hir` for debug output

5. **Export type instantiation map:**
   - Currently internal to typeChecker
   - Should be in `sema.Result` alongside `FunctionInstantiations`

6. **Design `BorrowGraph` structure:**
   - Nodes: HIR bindings
   - Edges: borrow relationships
   - Build from existing `BorrowTable` data

7. **Add ownership qualifier to HIR bindings:**
   - Enum: `Own | Ref | RefMut | Ptr | Copy`
   - Derived from type and borrow info

8. **Document HIR node kinds:**
   - Minimal set for normalized representation
   - Preserve source mapping for diagnostics

---

## 7. Key File Reference

| Category | File | Key Content |
|----------|------|-------------|
| AST IDs | `internal/ast/ids.go` | All ID type definitions |
| Type Interner | `internal/types/interner.go` | Type deduplication |
| Sema Result | `internal/sema/check.go:22-29` | Result struct |
| ExprTypes | `internal/sema/type_expr.go` | Expression typing |
| Binding Types | `internal/sema/binding_types.go` | Variable type tracking |
| Borrow Table | `internal/sema/borrow.go` | Borrow tracking |
| Borrow Export | `internal/sema/scope_stack.go:150-160` | Result population |
| Fn Instantiations | `internal/sema/type_checker_instantiations.go` | Generic function tracking |
| Type Instantiations | `internal/sema/type_decl_instantiate.go` | Generic type tracking |
| Module Meta | `internal/project/modulemeta.go` | Module metadata |
| Disk Cache | `internal/driver/dcache.go` | IR caching ready |

---

## 8. Conclusion

The Surge frontend is **well-prepared** for HIR/MIR introduction:

- **Typed AST infrastructure** is complete and accessible
- **Borrow checker data** is comprehensive and exportable
- **Generic instantiation tracking** works but needs unification
- **Module caching** is designed for extension

No code changes are required for this audit stage. The frontend can serve as a solid foundation for the HIR layer described in `docs/IR.md`.
