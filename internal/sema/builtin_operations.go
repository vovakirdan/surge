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
// The catalog is ordered by canonical body key before this runs, so the record
// that survives is the same one on every run.
func dedupeBuiltinOperations(candidates []CallableCandidate) []CallableCandidate {
	seen := make(map[string]struct{}, len(candidates))
	kept := candidates[:0]
	for i := range candidates {
		if candidates[i].Builtin {
			key := builtinOperationKey(&candidates[i])
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
		}
		kept = append(kept, candidates[i])
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
