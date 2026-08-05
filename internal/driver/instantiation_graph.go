package driver

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// FinalizeInstantiationClosure builds the single backend-independent generic
// reachability authority for a diagnosed module graph. It is idempotent; HIR,
// mono, diagnostics, and language-server consumers share the stored result.
func FinalizeInstantiationClosure(ctx context.Context, res *DiagnoseResult, maxDepth int) error {
	if res == nil || res.Sema == nil || res.Symbols == nil {
		return nil
	}
	if res.Sema.InstantiationIdentity != nil && res.Sema.InstantiationClosure != nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sema.AddInstantiationCallableSeeds(res.Sema, rootInstantiationCallableSeeds(res))
	if err := mergeInstantiationResult(res, res.Sema, nil); err != nil {
		return fmt.Errorf("root instantiation graph: %w", err)
	}
	if root := res.rootRecord; root != nil {
		for _, fileID := range root.FileIDs {
			semaResult := root.Sema[fileID]
			if semaResult == nil || semaResult == res.Sema {
				continue
			}
			if err := mergeInstantiationResult(res, semaResult, nil); err != nil {
				return fmt.Errorf("root file instantiation graph: %w", err)
			}
		}
	}

	type recordEntry struct {
		path string
		rec  *moduleRecord
	}
	entries := make([]recordEntry, 0, len(res.moduleRecords))
	seen := make(map[*moduleRecord]struct{}, len(res.moduleRecords))
	for path, rec := range res.moduleRecords {
		if rec == nil || rec == res.rootRecord {
			continue
		}
		if _, duplicate := seen[rec]; duplicate {
			continue
		}
		seen[rec] = struct{}{}
		if rec.Meta != nil && rec.Meta.Path != "" {
			path = rec.Meta.Path
		}
		entries = append(entries, recordEntry{path: normalizeExportsKey(path), rec: rec})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	for _, entry := range entries {
		mapping := buildModuleSymbolRemap(res.Symbols, entry.rec)
		if isCoreModulePath(entry.path) {
			if coreMapping := buildCoreSymbolRemap(res.Symbols, entry.rec); len(coreMapping) > 0 {
				mapping = coreMapping
			}
		}
		cacheInstantiationSymbolRemap(entry.rec, res.Symbols.Table, mapping)
		for _, fileID := range entry.rec.FileIDs {
			if err := mergeInstantiationResult(res, entry.rec.Sema[fileID], mapping); err != nil {
				return fmt.Errorf("%s instantiation graph: %w", entry.path, err)
			}
		}
	}
	return finalizeInstantiationResult(res, maxDepth)
}

func rootInstantiationCallableSeeds(res *DiagnoseResult) []symbols.SymbolID {
	if res == nil || res.Symbols == nil || res.Symbols.Table == nil || res.Symbols.Table.Symbols == nil {
		return nil
	}
	set := make(map[symbols.SymbolID]struct{})
	addFile := func(builder *ast.Builder, fileID ast.FileID, symResult *symbols.Result) {
		if builder == nil || symResult == nil {
			return
		}
		file := builder.Files.Get(fileID)
		if file == nil {
			return
		}
		for _, itemID := range file.Items {
			fn, ok := builder.Items.Fn(itemID)
			if !ok || fn == nil || !fn.Body.IsValid() {
				continue
			}
			for _, id := range symResult.ItemSymbols[itemID] {
				sym := res.Symbols.Table.Symbols.Get(id)
				if sym != nil && sym.Kind == symbols.SymbolFunction && len(sym.TypeParams) == 0 {
					set[id] = struct{}{}
				}
			}
		}
	}
	if rec := res.rootRecord; rec != nil {
		for _, fileID := range rec.FileIDs {
			if symResult, ok := rec.Symbols[fileID]; ok {
				symCopy := symResult
				addFile(rec.Builder, fileID, &symCopy)
			}
		}
	} else {
		addFile(res.Builder, res.FileID, res.Symbols)
	}
	limit := res.Symbols.Table.Symbols.Len()
	for raw := 1; raw <= limit; raw++ {
		id := symbols.SymbolID(raw)
		sym := res.Symbols.Table.Symbols.Get(id)
		if sym != nil && sym.Kind == symbols.SymbolFunction && len(sym.TypeParams) == 0 && sym.Flags&symbols.SymbolFlagEntrypoint != 0 {
			set[id] = struct{}{}
		}
	}
	out := make([]symbols.SymbolID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func canonicalInstantiationSourceResolver(res *DiagnoseResult) func(source.FileID) (string, error) {
	index := canonicalInstantiationSourceIndex(res)
	return func(id source.FileID) (string, error) {
		if res == nil || res.FileSet == nil || !res.FileSet.HasFile(id) {
			return "", fmt.Errorf("unknown source file %d", id)
		}
		if err := index.errors[id]; err != nil {
			return "", err
		}
		if key := index.keys[id]; key != "" {
			return key, nil
		}
		return "", fmt.Errorf("source file %d has no canonical identity", id)
	}
}

type canonicalSourceOrigin struct {
	id       source.FileID
	physical string
}

type canonicalSourceIndex struct {
	keys    map[source.FileID]string
	errors  map[source.FileID]error
	reverse map[string][]canonicalSourceOrigin
}

func canonicalInstantiationSourceIndex(res *DiagnoseResult) canonicalSourceIndex {
	index := canonicalSourceIndex{
		keys:    make(map[source.FileID]string),
		errors:  make(map[source.FileID]error),
		reverse: make(map[string][]canonicalSourceOrigin),
	}
	if res == nil {
		return index
	}
	records := make([]*moduleRecord, 0, len(res.moduleRecords)+1)
	seen := make(map[*moduleRecord]struct{}, len(res.moduleRecords)+1)
	appendRecord := func(rec *moduleRecord) {
		if rec == nil {
			return
		}
		if _, exists := seen[rec]; exists {
			return
		}
		seen[rec] = struct{}{}
		records = append(records, rec)
	}
	appendRecord(res.rootRecord)
	for _, rec := range res.moduleRecords {
		appendRecord(rec)
	}
	for _, rec := range records {
		modulePath := ""
		if rec.Meta != nil {
			modulePath = normalizeExportsKey(rec.Meta.Path)
			for _, metaFile := range rec.Meta.Files {
				id := metaFile.Span.File
				if key, ok := portableModuleFilePath(metaFile.Path, modulePath); ok {
					index.set(id, key, sourcePhysicalPath(res, id))
				}
			}
		}
		for _, file := range rec.Files {
			if file == nil {
				continue
			}
			if index.keys[file.ID] != "" {
				continue
			}
			base := filepath.Base(file.Path)
			if modulePath != "" {
				index.set(file.ID, filepath.ToSlash(filepath.Join(modulePath, base)), file.Path)
				continue
			}
			if res.FileSet != nil {
				if relative, ok := containedRelativePath(file.Path, res.FileSet.BaseDir()); ok {
					index.set(file.ID, relative, file.Path)
				}
			}
		}
	}
	if res.FileSet != nil {
		for rawID := source.FileID(0); res.FileSet.HasFile(rawID); rawID++ {
			if index.keys[rawID] != "" {
				continue
			}
			file := res.FileSet.Get(rawID)
			if file == nil || file.Path == "" {
				index.errors[rawID] = fmt.Errorf("source file %d has no path", rawID)
				continue
			}
			if relative, ok := containedRelativePath(file.Path, res.FileSet.BaseDir()); ok {
				index.set(rawID, relative, file.Path)
				continue
			}
			// A full content digest prevents truncated-hash collisions. The
			// reverse index below still rejects identical-content, same-basename
			// external files because they have no stable namespace of their own.
			index.set(rawID, fmt.Sprintf("external/%s@%x", filepath.Base(file.Path), file.Hash), file.Path)
		}
	}
	return index
}

func portableModuleFilePath(path, modulePath string) (string, bool) {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return "", false
	}
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		if modulePath == "" {
			return "", false
		}
		return filepath.ToSlash(filepath.Join(modulePath, filepath.Base(path))), true
	}
	return filepath.ToSlash(path), true
}

func containedRelativePath(path, base string) (string, bool) {
	if path == "" || base == "" {
		return "", false
	}
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(relative)), true
}

func (index *canonicalSourceIndex) set(id source.FileID, key, physical string) {
	if index == nil {
		return
	}
	key = filepath.ToSlash(filepath.Clean(key))
	if key == "." || key == "" || filepath.IsAbs(key) {
		return
	}
	if previous := index.keys[id]; previous != "" && previous != key {
		index.errors[id] = fmt.Errorf("source file has conflicting canonical identities %q and %q; note: each source file must belong to exactly one logical module namespace", previous, key)
		return
	}
	index.keys[id] = key
	physical = filepath.Clean(physical)
	if physical != "." && physical != "" && !filepath.IsAbs(physical) {
		if absolute, err := filepath.Abs(physical); err == nil {
			physical = filepath.Clean(absolute)
		}
	}
	origins := index.reverse[key]
	for _, origin := range origins {
		if origin.id == id || (physical != "" && origin.physical == physical) {
			return
		}
		err := fmt.Errorf("canonical source identity %q is ambiguous across distinct files; note: add a stable module mapping or distinct logical source paths", key)
		index.errors[id] = err
		index.errors[origin.id] = err
	}
	index.reverse[key] = append(origins, canonicalSourceOrigin{id: id, physical: physical})
}

func sourcePhysicalPath(res *DiagnoseResult, id source.FileID) string {
	if res == nil || res.FileSet == nil || !res.FileSet.HasFile(id) {
		return ""
	}
	file := res.FileSet.Get(id)
	if file == nil {
		return ""
	}
	return file.Path
}

func mergeInstantiationResult(
	res *DiagnoseResult,
	src *sema.Result,
	mapping map[symbols.SymbolID]symbols.SymbolID,
) error {
	if res == nil || res.Sema == nil || src == nil {
		return nil
	}
	if src.TypeInterner != res.Sema.TypeInterner {
		return fmt.Errorf("instantiation graph uses a different type interner; note: module graphs must share one semantic type arena before merge")
	}
	if err := sema.CanonicalizeInstantiationGraphSources(src, canonicalInstantiationSourceResolver(res)); err != nil {
		return err
	}
	if src != res.Sema {
		sema.MergeInstantiationGraphs(res.Sema, src, mapping)
		sema.MergeFunctionCallEdges(res.Sema, src, mapping)
		if err := sema.MergeInstantiationTemplateParams(res.Sema, src, mapping); err != nil {
			return err
		}
	}
	return nil
}

func cacheInstantiationSymbolRemap(
	rec *moduleRecord,
	rootTable *symbols.Table,
	mapping map[symbols.SymbolID]symbols.SymbolID,
) {
	if rec == nil || rootTable == nil || len(mapping) == 0 {
		return
	}
	if rec.instantiationRemap == nil {
		rec.instantiationRemap = make(map[*symbols.Table]map[symbols.SymbolID]symbols.SymbolID)
	}
	rec.instantiationRemap[rootTable] = cloneInstantiationSymbolRemap(mapping)
}

func cachedInstantiationSymbolRemap(
	rec *moduleRecord,
	rootTable *symbols.Table,
) (map[symbols.SymbolID]symbols.SymbolID, bool) {
	if rec == nil || rootTable == nil || rec.instantiationRemap == nil {
		return nil, false
	}
	mapping, ok := rec.instantiationRemap[rootTable]
	return mapping, ok && len(mapping) > 0
}

func cloneInstantiationSymbolRemap(mapping map[symbols.SymbolID]symbols.SymbolID) map[symbols.SymbolID]symbols.SymbolID {
	if len(mapping) == 0 {
		return nil
	}
	clone := make(map[symbols.SymbolID]symbols.SymbolID, len(mapping))
	for from, to := range mapping {
		clone[from] = to
	}
	return clone
}

func finalizeInstantiationResult(res *DiagnoseResult, maxDepth int) error {
	if res == nil || res.Sema == nil || res.Symbols == nil {
		return nil
	}
	identity, err := sema.NewInstantiationKeyContext(
		res.Sema.TypeInterner,
		res.Symbols,
		canonicalInstantiationSourceResolver(res),
	)
	if err != nil {
		return err
	}
	res.Sema.InstantiationIdentity = &identity
	if err := res.Sema.FinalizeEntrypointCallables(); err != nil {
		return err
	}
	if err := res.Sema.FinalizeInstantiationClosure(identity, maxDepth); err != nil {
		return err
	}
	if res.Instantiations != nil {
		rebuilt, rebuildErr := mono.RebuildInstantiationMapFromClosure(
			res.Instantiations,
			res.Sema.InstantiationClosure,
			res.Sema.InstantiationIdentity,
		)
		if rebuildErr != nil {
			return rebuildErr
		}
		res.Instantiations = rebuilt
	}
	return nil
}
