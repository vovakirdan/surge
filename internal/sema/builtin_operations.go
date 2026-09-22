package sema

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/types"
)

// dedupeBuiltinOperations folds the several records one compiler-provided
// operation receives when more than one module ingests the `extern` block that
// declares it. A builtin declaration names an operation the compiler
// implements instead of carrying a body, so those records describe one
// operation rather than rival implementations of it. A user declaration is
// never builtin and never folds here.
//
// The standard library's own record survives when one exists: its module path
// is reserved for the standard library (driver/module_validation.go), so a copy
// read from a path that sorts before `core/` cannot take its place. Otherwise
// the first record in canonical body-key order survives, the same on every run.
func dedupeBuiltinOperations(candidates []CallableCandidate) []CallableCandidate {
	chosen := make(map[string]int, len(candidates))
	for i := range candidates {
		if !candidates[i].Builtin {
			continue
		}
		key := builtinOperationKey(&candidates[i])
		if at, seen := chosen[key]; !seen || isCoreRuntimeModulePath(candidates[i].ModulePath) && !isCoreRuntimeModulePath(candidates[at].ModulePath) {
			chosen[key] = i
		}
	}
	kept := candidates[:0]
	for i := range candidates {
		if !candidates[i].Builtin || chosen[builtinOperationKey(&candidates[i])] == i {
			kept = append(kept, candidates[i])
		}
	}
	return kept
}

// builtinOperationKey identifies the operation a builtin declaration names: the
// receiver it dispatches on, and the exact signature and attributes it
// declares. Where the declaration was read from is deliberately absent, because
// the compiler implements one operation however many modules spell it out.
func builtinOperationKey(candidate *CallableCandidate) string {
	return fmt.Sprintf(
		"%s|%d|%s|(%s)->%s|(%s)->%d|attrs=%s|body=%t|async=%t|intrinsic=%t",
		candidate.ReceiverKey,
		candidate.ReceiverType,
		candidate.Name,
		joinTypeKeys(candidate.Params),
		candidate.Result,
		joinTypeIDs(candidate.ParamTypes),
		candidate.ResultType,
		strings.Join(candidate.Attrs, ","),
		candidate.HasBody,
		candidate.Async,
		candidate.Intrinsic,
	)
}

func joinTypeIDs(ids []types.TypeID) string {
	parts := make([]string, len(ids))
	for i := range ids {
		parts[i] = strconv.FormatUint(uint64(ids[i]), 10)
	}
	return strings.Join(parts, ",")
}
