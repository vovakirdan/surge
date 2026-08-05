package driver

import (
	"fmt"
	"sort"

	"surge/internal/sema"
)

type finalizationPublicationIndex map[*moduleRecord][]sema.FinalizationCallableIdentity

func buildFinalizationPublicationIndex(res *DiagnoseResult) (finalizationPublicationIndex, error) {
	index := make(finalizationPublicationIndex)
	if res == nil || res.FileSet == nil {
		return index, nil
	}
	resolveSource := canonicalInstantiationSourceResolver(res)
	for _, rec := range finalizationPublicationRecords(res) {
		seen := make(map[sema.FinalizationCallableIdentity]struct{})
		for _, fileID := range rec.FileIDs {
			fileResult := rec.Sema[fileID]
			if fileResult == nil {
				continue
			}
			if err := sema.CanonicalizeInstantiationGraphSources(fileResult, resolveSource); err != nil {
				return nil, fmt.Errorf("finalization publication index: %w", err)
			}
			for i := range fileResult.CallableCandidates {
				candidate := &fileResult.CallableCandidates[i]
				identity := sema.FinalizationCallableIdentity{
					Symbol: candidate.Symbol, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey,
				}
				if !identity.Symbol.IsValid() || identity.BodyKey == "" {
					continue
				}
				seen[identity] = struct{}{}
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
		index[rec] = callables
	}
	return index, nil
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
