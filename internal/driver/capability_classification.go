package driver

import (
	"sort"

	"surge/internal/sema"
	"surge/internal/types"
)

// classifyCapabilities attaches the whole-program capability authority to a
// finalized semantic result.
//
// It is called from the two WHOLE-PROGRAM authority sites and from nowhere
// else: the pre-pass in CombineHIRWithModulesWithOptions, and the module-record
// path in finalizeParallelModuleRecords. The per-file path in
// finalizeParallelFileResults never classifies — it holds one file's view, and
// a capability derived from that view would answer for imported types out of a
// file's ignorance of them.
//
// What makes a site authoritative is WHICH SITE IT IS, not whether module
// records happen to exist there. A single-file build reaches
// CombineHIRWithModulesWithOptions with no records at all, and it is still the
// whole program: with no imports, its own facts are already complete. Gating on
// record presence would silently skip exactly that build and leave it with no
// capability answers, or with a set of false ones — which is the same failure
// wearing a more convincing face.
//
// Records are an INPUT to the fact merge that runs just before this. They are
// not a precondition for classifying what the merge produced.
func classifyCapabilities(res *DiagnoseResult) error {
	if res == nil || res.Sema == nil {
		return nil
	}
	classifier, err := res.Sema.NewCapabilityClassifier()
	if err != nil {
		return err
	}
	res.Sema.Capabilities = classifier
	return nil
}

// wholeProgramCapabilityAuthority builds one classifier for every module the
// record path is about to finalize.
//
// That path walks modules one at a time, and each module's aggregate result
// carries only its OWN callable catalog. A `__clone` private to one module is
// absent from another module's list, so a classifier built per module would
// call one type clonable while finalizing its owner and non-clonable while
// finalizing its importer — one program with two answers, and nothing to say
// which is the program's. Folding every record into one view first removes the
// question rather than resolving it later.
//
// The fold is the same pure OR the fact pre-pass performs, and it deliberately
// does not validate: a contradiction is reported once, by the per-module merge
// that runs inside the loop, in the voice that names its contributing modules.
func wholeProgramCapabilityAuthority(records map[string]*moduleRecord) (*sema.CapabilityClassifier, error) {
	seed := &sema.Result{}
	facts := sema.NewTypeAttrFactMerge()
	catalogued := make(map[sema.FinalizationCallableIdentity]struct{})
	folded := make(map[*moduleRecord]struct{}, len(records))

	paths := make([]string, 0, len(records))
	for path := range records {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		rec := records[path]
		if rec == nil {
			continue
		}
		if _, done := folded[rec]; done {
			continue
		}
		folded[rec] = struct{}{}
		if seed.TypeInterner == nil {
			seed.TypeInterner = recordTypeInterner(rec)
		}
		mergeCopyTypesFromRecord(seed, rec)
		foldRecordTypeAttrFacts(facts, seed, rec, recordModulePath(rec, path))
		foldRecordCallableCandidates(seed, rec, catalogued)
	}
	if seed.TypeInterner == nil {
		return nil, nil
	}
	return seed.NewCapabilityClassifier()
}

// foldRecordCallableCandidates adds one record's callables to the whole-program
// catalog, keeping the first record to name each one. Records are folded in
// sorted order and file ids are walked in declaration order, so which copy of a
// duplicated callable survives is the same on every run.
func foldRecordCallableCandidates(
	dst *sema.Result,
	rec *moduleRecord,
	catalogued map[sema.FinalizationCallableIdentity]struct{},
) {
	if dst == nil || rec == nil || rec.Sema == nil {
		return
	}
	for _, fileID := range rec.FileIDs {
		src := rec.Sema[fileID]
		if src == nil {
			continue
		}
		for i := range src.CallableCandidates {
			candidate := src.CallableCandidates[i]
			identity := sema.FinalizationCallableIdentity{
				Symbol: candidate.Symbol, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey,
			}
			if _, duplicate := catalogued[identity]; duplicate {
				continue
			}
			catalogued[identity] = struct{}{}
			dst.CallableCandidates = append(dst.CallableCandidates, candidate)
		}
	}
}

func recordTypeInterner(rec *moduleRecord) *types.Interner {
	if rec == nil || rec.Sema == nil {
		return nil
	}
	for _, fileID := range rec.FileIDs {
		if src := rec.Sema[fileID]; src != nil && src.TypeInterner != nil {
			return src.TypeInterner
		}
	}
	return nil
}
