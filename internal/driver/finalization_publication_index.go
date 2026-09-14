package driver

import (
	"fmt"
	"sort"

	"surge/internal/sema"
	"surge/internal/source"
)

type finalizationPublicationIndex map[*moduleRecord][]sema.FinalizationCallableIdentity

func buildFinalizationPublicationIndex(res *DiagnoseResult) (finalizationPublicationIndex, error) {
	index := make(finalizationPublicationIndex)
	if res == nil || res.FileSet == nil {
		return index, nil
	}
	resolveSource := canonicalInstantiationSourceResolver(res)
	for _, rec := range finalizationPublicationRecords(res) {
		results := make([]*sema.Result, 0, len(rec.FileIDs))
		for _, fileID := range rec.FileIDs {
			results = append(results, rec.Sema[fileID])
		}
		callables, err := captureFinalizationCallables(results, resolveSource)
		if err != nil {
			return nil, err
		}
		index[rec] = callables
	}
	if res.rootRecord == nil && res.Sema != nil {
		callables, err := captureFinalizationCallables([]*sema.Result{res.Sema}, resolveSource)
		if err != nil {
			return nil, err
		}
		index[nil] = callables
	}
	return index, nil
}

// Capture before merge or CopyInstantiationAuthority changes local candidates.
func captureFinalizationCallables(results []*sema.Result, resolveSource func(source.FileID) (string, error)) ([]sema.FinalizationCallableIdentity, error) {
	seen := make(map[sema.FinalizationCallableIdentity]struct{})
	for _, fileResult := range results {
		if fileResult == nil {
			continue
		}
		if err := sema.CanonicalizeInstantiationGraphSources(fileResult, resolveSource); err != nil {
			return nil, fmt.Errorf("finalization publication index: %w", err)
		}
		for _, candidate := range fileResult.CallableCandidates {
			identity := sema.FinalizationCallableIdentity{Symbol: candidate.Symbol, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey}
			if identity.Symbol.IsValid() && identity.BodyKey != "" {
				seen[identity] = struct{}{}
			}
		}
	}
	callables := make([]sema.FinalizationCallableIdentity, 0, len(seen))
	for identity := range seen {
		callables = append(callables, identity)
	}
	sort.Slice(callables, func(i, j int) bool {
		if callables[i].BodyKey != callables[j].BodyKey {
			return callables[i].BodyKey < callables[j].BodyKey
		}
		if callables[i].Symbol != callables[j].Symbol {
			return callables[i].Symbol < callables[j].Symbol
		}
		return callables[i].SourceKey < callables[j].SourceKey
	})
	return callables, nil
}

func finalizationPublicationRecords(res *DiagnoseResult) []*moduleRecord {
	if res == nil {
		return nil
	}
	records := make([]*moduleRecord, 0, len(res.moduleRecords)+1)
	seen := make(map[*moduleRecord]struct{}, len(res.moduleRecords)+1)
	if res.rootRecord != nil {
		records = append(records, res.rootRecord)
		seen[res.rootRecord] = struct{}{}
	}
	paths := make([]string, 0, len(res.moduleRecords))
	for path := range res.moduleRecords {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rec := res.moduleRecords[path]
		if rec == nil {
			continue
		}
		if _, exists := seen[rec]; exists {
			continue
		}
		seen[rec] = struct{}{}
		records = append(records, rec)
	}
	return records
}
