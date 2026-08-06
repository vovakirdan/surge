package driver

import (
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// installReachableBodyTypes contributes the types named inside reachable bodies
// to the required-operation derivation.
//
// Signatures survive the merge, so sema can read them off the merged result on
// its own. Bodies do not: a semantic result records expression types for the
// file it checked, and only the driver holds every file's result. A value that
// is built, used and released inside one function appears in no signature
// anywhere, and it still needs its operations emitted, so those files have to
// be asked.
//
// Contribution is decided per FILE, which is coarser than the closure's
// per-callable liveness and deliberately so. A file that contributes no live
// callable contributes nothing, and inside one that does, this admits the types
// its dead functions name as well: expression types are recorded per file, and
// narrowing further would mean re-deciding, from the AST, which expressions
// belong to which body. Erring wide costs an implementation nobody calls; erring
// narrow costs a value with no way to perform an operation it advertises.
func installReachableBodyTypes(res *DiagnoseResult) {
	if res == nil || res.Sema == nil {
		return
	}
	live := liveCallableSourceKeys(res.Sema)
	seeds := make(map[types.TypeID]struct{}, 256)
	collect := func(fileResult *sema.Result) {
		if fileResult == nil || !contributesLiveCallable(fileResult, live) {
			return
		}
		for _, id := range fileResult.ExprTypes {
			seeds[id] = struct{}{}
		}
		for _, id := range fileResult.BindingTypes {
			seeds[id] = struct{}{}
		}
	}
	records := finalizationPublicationRecords(res)
	for _, rec := range records {
		for _, fileID := range rec.FileIDs {
			collect(rec.Sema[fileID])
		}
	}
	if len(records) == 0 {
		// A single-file build has no records and is still the whole program.
		collect(res.Sema)
	}
	res.Sema.ReachableBodyTypes = seeds
}

// liveCallableSourceKeys names the files the finalized closure keeps alive.
func liveCallableSourceKeys(result *sema.Result) map[string]struct{} {
	closure := result.InstantiationClosure
	if closure == nil {
		return nil
	}
	symbolsLive := make(map[symbols.SymbolID]struct{}, len(closure.LiveCallables)+len(closure.Instances))
	for _, id := range closure.LiveCallables {
		symbolsLive[id] = struct{}{}
	}
	for i := range closure.Instances {
		symbolsLive[closure.Instances[i].Template] = struct{}{}
	}
	keys := make(map[string]struct{}, len(symbolsLive))
	for i := range result.CallableCandidates {
		candidate := &result.CallableCandidates[i]
		if _, alive := symbolsLive[candidate.Symbol]; alive && candidate.SourceKey != "" {
			keys[candidate.SourceKey] = struct{}{}
		}
	}
	return keys
}

// contributesLiveCallable reports whether one file declares a callable the
// closure kept. Every candidate a file records carries that file's source key,
// so one match settles the file.
func contributesLiveCallable(fileResult *sema.Result, live map[string]struct{}) bool {
	for i := range fileResult.CallableCandidates {
		if _, alive := live[fileResult.CallableCandidates[i].SourceKey]; alive {
			return true
		}
	}
	return false
}
