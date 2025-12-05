package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/trace"
)

// parseModuleDir parses all .sg files in a directory and returns the AST builder,
// file IDs, and source files. It supports preloaded files from an existing builder.
func parseModuleDir(
	ctx context.Context,
	fs *source.FileSet,
	dir string,
	bag *diag.Bag,
	strs *source.Interner,
	builder *ast.Builder,
	preloaded map[string]ast.FileID,
) (retBuilder *ast.Builder, retFileIDs []ast.FileID, retFiles []*source.File, retErr error) {
	tracer := trace.FromContext(ctx)
	span := trace.Begin(tracer, trace.ScopeModule, "parse_module_dir", 0)
	span.WithExtra("dir", dir)
	defer func() {
		if len(retFileIDs) > 0 {
			span.End(fmt.Sprintf("files=%d", len(retFileIDs)))
		} else {
			span.End("")
		}
	}()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	paths := make([]string, 0, len(entries))
	dirNorm := filepath.ToSlash(filepath.Clean(dir))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if filepath.Ext(ent.Name()) != ".sg" {
			continue
		}
		paths = append(paths, filepath.Join(dir, ent.Name()))
	}
	existing := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		existing[filepath.ToSlash(p)] = struct{}{}
	}
	for key := range preloaded {
		normKey := filepath.ToSlash(key)
		keyDir := filepath.ToSlash(filepath.Dir(normKey))
		if keyDir == "." {
			keyDir = dirNorm
		}
		if keyDir == dirNorm {
			if _, ok := existing[normKey]; !ok {
				paths = append(paths, normKey)
			}
		}
	}
	if len(paths) == 0 {
		return nil, nil, nil, errModuleNotFound
	}
	sort.Strings(paths)
	if builder == nil {
		builder = ast.NewBuilder(ast.Hints{}, strs)
	}
	fileIDs := make([]ast.FileID, 0, len(paths))
	files := make([]*source.File, 0, len(paths))
	for _, p := range paths {
		normPath := filepath.ToSlash(p)
		if id, ok := preloaded[normPath]; ok && builder != nil {
			if existingID, okFile := fs.GetLatest(normPath); okFile {
				if file := fs.Get(existingID); file != nil {
					fileIDs = append(fileIDs, id)
					files = append(files, file)
					continue
				}
			}
		}
		fileID, err := fs.Load(p)
		if err != nil {
			return nil, nil, nil, err
		}
		file := fs.Get(fileID)
		if bag != nil {
			diagnoseTokenize(file, bag)
		}
		var parsed ast.FileID
		builder, parsed = diagnoseParseWithBuilder(ctx, fs, file, bag, builder)
		fileIDs = append(fileIDs, parsed)
		files = append(files, file)
	}
	return builder, fileIDs, files, nil
}
