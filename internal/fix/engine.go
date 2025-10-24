package fix

// todo: интеграция с git:
// По умолчанию создавать .bak только для незатрекинных файлов.
// Флаг --staged-only (работать по git diff --name-only --staged).
// Флаг --since HEAD~1 (фильтр по изменённым файлам).

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"surge/internal/diag"
	"surge/internal/source"
)

// ErrNoFixes is returned when no fixes were applied.
var ErrNoFixes = errors.New("no applicable fixes found")

// ApplyMode determines selection strategy for fixes.
type ApplyMode uint8

const (
	ApplyModeOnce ApplyMode = iota
	ApplyModeAll
	ApplyModeID
)

// ApplyOptions configures how fixes are selected.
type ApplyOptions struct {
	Mode     ApplyMode
	TargetID string
}

// AppliedFix records a successfully applied fix.
type AppliedFix struct {
	ID            string
	Title         string
	Code          diag.Code
	Message       string
	Applicability diag.FixApplicability
	PrimaryPath   string
	EditCount     int
}

// SkippedFix captures a skipped or failed fix with a reason.
type SkippedFix struct {
	ID     string
	Title  string
	Reason string
}

// FileChange summarises modifications performed on a file.
type FileChange struct {
	Path      string
	EditCount int
}

// ApplyResult aggregates applied fixes, skipped ones, and file changes.
type ApplyResult struct {
	Applied     []AppliedFix
	Skipped     []SkippedFix
	FileChanges []FileChange
}

type candidate struct {
	diag  diag.Diagnostic
	fix   diag.Fix
	order int
}

// Apply collects fixes from diagnostics, selects a subset according to opts, and applies them.
func Apply(fs *source.FileSet, diagnostics []diag.Diagnostic, opts ApplyOptions) (*ApplyResult, error) {
	result := &ApplyResult{
		Applied:     make([]AppliedFix, 0),
		Skipped:     make([]SkippedFix, 0),
		FileChanges: make([]FileChange, 0),
	}
	if fs == nil {
		return result, fmt.Errorf("fix: FileSet is nil")
	}

	ctx := diag.FixBuildContext{FileSet: fs}
	candidates, buildSkips := gatherCandidates(ctx, diagnostics)
	result.Skipped = append(result.Skipped, buildSkips...)

	if len(candidates) == 0 {
		return result, ErrNoFixes
	}

	sortCandidates(candidates)

	selected, selectionSkips := selectCandidates(candidates, opts)
	result.Skipped = append(result.Skipped, selectionSkips...)

	if len(selected) == 0 {
		return result, ErrNoFixes
	}

	applied, skippedDuringApply, changes, err := applyCandidates(fs, selected)
	result.Applied = append(result.Applied, applied...)
	result.Skipped = append(result.Skipped, skippedDuringApply...)
	result.FileChanges = append(result.FileChanges, changes...)

	if err != nil {
		return result, err
	}
	if len(result.Applied) == 0 {
		return result, ErrNoFixes
	}
	return result, nil
}

// gatherCandidates builds a list of candidate fixes from diagnostics and reports any skips encountered.
//
// For each diagnostic that has fixes, it materializes fixes via diag.MaterializeFixes. Diagnostics whose
// fixes fail to materialize or materialized fixes that contain no edits are recorded as SkippedFix entries.
// If a materialized fix has an empty ID, gatherCandidates synthesizes one using the diagnostic code, file,
// start position, and the fix index. Each produced candidate is given a monotonically increasing `order`
// value to provide a deterministic insertion order for later stable sorting.
//
// Returns the collected candidates and any SkippedFix records.
func gatherCandidates(ctx diag.FixBuildContext, diagnostics []diag.Diagnostic) ([]candidate, []SkippedFix) {
	cands := make([]candidate, 0)
	skips := make([]SkippedFix, 0)

	order := 0
	for _, d := range diagnostics {
		if len(d.Fixes) == 0 {
			continue
		}

		resolved, err := diag.MaterializeFixes(ctx, d.Fixes)
		if err != nil {
			skips = append(skips, SkippedFix{
				Title:  d.Message,
				Reason: fmt.Sprintf("failed to build fixes: %v", err),
			})
			continue
		}

		for idx, f := range resolved {
			if len(f.Edits) == 0 {
				skips = append(skips, SkippedFix{
					ID:     f.ID,
					Title:  f.Title,
					Reason: "fix has no edits",
				})
				continue
			}
			if f.ID == "" {
				f.ID = fmt.Sprintf("%s-%d-%d-%d", d.Code.ID(), d.Primary.File, d.Primary.Start, idx)
			}
			cands = append(cands, candidate{
				diag:  d,
				fix:   f,
				order: order,
			})
			order++
		}
	}
	return cands, skips
}

// sortCandidates sorts the candidate slice in-place to produce a deterministic
// selection order used by the apply pipeline.
//
// The sort keys, in precedence order, are: file (Primary.File), span start
// (Primary.Start), span end (Primary.End), candidate insertion order
// (candidate.order), diagnostic code (diag.Code), fix preference (IsPreferred,
// preferred first), fix ID, and finally fix Title.
func sortCandidates(candidates []candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		di, dj := candidates[i].diag, candidates[j].diag
		if di.Primary.File != dj.Primary.File {
			return di.Primary.File < dj.Primary.File
		}
		if di.Primary.Start != dj.Primary.Start {
			return di.Primary.Start < dj.Primary.Start
		}
		if di.Primary.End != dj.Primary.End {
			return di.Primary.End < dj.Primary.End
		}
		if candidates[i].order != candidates[j].order {
			return candidates[i].order < candidates[j].order
		}
		if di.Code != dj.Code {
			return di.Code < dj.Code
		}
		if candidates[i].fix.IsPreferred != candidates[j].fix.IsPreferred {
			return candidates[i].fix.IsPreferred && !candidates[j].fix.IsPreferred
		}
		if candidates[i].fix.ID != candidates[j].fix.ID {
			return candidates[i].fix.ID < candidates[j].fix.ID
		}
		return candidates[i].fix.Title < candidates[j].fix.Title
	})
}

func selectCandidates(candidates []candidate, opts ApplyOptions) ([]candidate, []SkippedFix) {
	switch opts.Mode {
	case ApplyModeID:
		for _, cand := range candidates {
			if cand.fix.ID == opts.TargetID {
				if cand.fix.RequiresAll { // если fix требует всех fixes, то пропускаем
					return nil, []SkippedFix{{
						ID:     opts.TargetID,
						Reason: "fix requires all fixes to be applied",
					}}
				}
				return []candidate{cand}, nil
			}
		}
		return nil, []SkippedFix{{
			ID:     opts.TargetID,
			Reason: "fix id not found",
		}}
	case ApplyModeAll:
		selected := make([]candidate, 0, len(candidates))
		skipped := make([]SkippedFix, 0)
		for _, cand := range candidates {
			if cand.fix.Applicability == diag.FixApplicabilityAlwaysSafe {
				selected = append(selected, cand)
				continue
			}
			skipped = append(skipped, SkippedFix{
				ID:     cand.fix.ID,
				Title:  cand.fix.Title,
				Reason: fmt.Sprintf("applicability is %s", cand.fix.Applicability.String()),
			})
		}
		return selected, skipped
	case ApplyModeOnce:
		var selected []candidate
		var fallback *candidate
		skipped := make([]SkippedFix, 0)
		for i := range candidates {
			cand := candidates[i]
			if cand.fix.RequiresAll {
				skipped = append(skipped, SkippedFix{
					ID:     cand.fix.ID,
					Title:  cand.fix.Title,
					Reason: "fix requires all fixes to be applied",
				})
				continue
			}
			if cand.fix.Applicability == diag.FixApplicabilityAlwaysSafe {
				selected = []candidate{cand}
				break
			}
			if fallback == nil {
				tmp := cand
				fallback = &tmp
			}
		}
		if len(selected) == 0 && fallback != nil {
			selected = []candidate{*fallback}
		}
		return selected, skipped
	default:
		return nil, nil
	}
}

func applyCandidates(fs *source.FileSet, selected []candidate) ([]AppliedFix, []SkippedFix, []FileChange, error) {
	buffers := make(map[source.FileID][]byte)
	appliedEdits := make(map[source.FileID][]diag.TextEdit)
	fileEditCount := make(map[source.FileID]int)
	dirtyFiles := make(map[source.FileID]bool)

	applied := make([]AppliedFix, 0, len(selected))
	skipped := make([]SkippedFix, 0)

	baseDir := fs.BaseDir()

	for _, cand := range selected {
		buckets := groupEditsByFile(cand.fix.Edits)
		stagedBuffers := make(map[source.FileID][]byte)
		stagedEdits := make(map[source.FileID][]diag.TextEdit)
		totalEdits := 0
		var skipReason string

		stagedApplied := make(map[source.FileID][]diag.TextEdit)

		for fileID, edits := range buckets {
			file := fs.Get(fileID)
			if file.Flags&source.FileVirtual != 0 {
				skipReason = "target file is virtual"
				break
			}

			if conflictsWithExisting(appliedEdits[fileID], edits) {
				skipReason = fmt.Sprintf("conflicts with previously applied edits in %s", file.FormatPath("auto", baseDir))
				break
			}

			base := buffers[fileID]
			if base == nil {
				base = append([]byte(nil), file.Content...)
			}
			working := append([]byte(nil), base...)

			sort.SliceStable(edits, func(i, j int) bool {
				if edits[i].Span.Start == edits[j].Span.Start {
					return edits[i].Span.End > edits[j].Span.End
				}
				return edits[i].Span.Start > edits[j].Span.Start
			})

			existingApplied := append([]diag.TextEdit(nil), appliedEdits[fileID]...)

			for _, edit := range edits {
				start := int(edit.Span.Start) + cumulativeDelta(existingApplied, int(edit.Span.Start))
				end := int(edit.Span.End) + cumulativeDelta(existingApplied, int(edit.Span.End))
				if start < 0 || end < start || end > len(working) {
					skipReason = "edit span out of range"
					break
				}
				if edit.OldText != "" && string(working[start:end]) != edit.OldText {
					skipReason = "existing text does not match expected content"
					break
				}
				suffix := append([]byte(nil), working[end:]...)
				working = append(append(working[:start], []byte(edit.NewText)...), suffix...)
				existingApplied = insertEditSorted(existingApplied, edit)
			}
			if skipReason != "" {
				break
			}
			stagedBuffers[fileID] = working
			copied := make([]diag.TextEdit, len(edits))
			for i, e := range edits {
				copied[i] = copyEdit(e)
			}
			stagedEdits[fileID] = copied
			stagedApplied[fileID] = existingApplied
			totalEdits += len(edits)
		}

		if skipReason != "" {
			skipped = append(skipped, SkippedFix{
				ID:     cand.fix.ID,
				Title:  cand.fix.Title,
				Reason: skipReason,
			})
			continue
		}

		for fileID, buf := range stagedBuffers {
			buffers[fileID] = buf
			appliedEdits[fileID] = stagedApplied[fileID]
			fileEditCount[fileID] += len(stagedEdits[fileID])
			dirtyFiles[fileID] = true
		}

		applied = append(applied, AppliedFix{
			ID:            cand.fix.ID,
			Title:         cand.fix.Title,
			Code:          cand.diag.Code,
			Message:       cand.diag.Message,
			Applicability: cand.fix.Applicability,
			PrimaryPath:   formatFilePath(fs, cand.diag.Primary.File),
			EditCount:     totalEdits,
		})
	}

	if len(applied) == 0 {
		return applied, skipped, nil, nil
	}

	fileChanges := make([]FileChange, 0, len(dirtyFiles))
	for fileID := range dirtyFiles {
		buf := buffers[fileID]
		file := fs.Get(fileID)

		mode := os.FileMode(0o644)
		if info, err := os.Stat(file.Path); err == nil {
			mode = info.Mode()
		}

		if err := os.WriteFile(file.Path, buf, mode); err != nil {
			return applied, skipped, fileChanges, fmt.Errorf("write %s: %w", file.Path, err)
		}

		fileChanges = append(fileChanges, FileChange{
			Path:      file.FormatPath("relative", baseDir),
			EditCount: fileEditCount[fileID],
		})
	}

	sort.SliceStable(fileChanges, func(i, j int) bool {
		return fileChanges[i].Path < fileChanges[j].Path
	})

	return applied, skipped, fileChanges, nil
}

func conflictsWithExisting(existing []diag.TextEdit, edits []diag.TextEdit) bool {
	for _, prev := range existing {
		for _, cand := range edits {
			if spansConflict(prev, cand) {
				return true
			}
		}
	}
	return false
}

// spansConflict reports whether two text edits' spans overlap.
// Spans are treated as half-open intervals [Start, End). Two zero-length edits
// (Start == End) never conflict. A zero-length edit conflicts with a non-zero
// span if its position is within that span (Start <= pos < End). For two
// non-zero spans, any overlap yields a conflict.
func spansConflict(a, b diag.TextEdit) bool {
	aStart, aEnd := a.Span.Start, a.Span.End
	bStart, bEnd := b.Span.Start, b.Span.End

	if aStart == aEnd && bStart == bEnd {
		return false
	}
	if aStart == aEnd {
		return bStart <= aStart && aStart < bEnd
	}
	if bStart == bEnd {
		return aStart <= bStart && bStart < aEnd
	}
	return aStart < bEnd && bStart < aEnd
}

func groupEditsByFile(edits []diag.TextEdit) map[source.FileID][]diag.TextEdit {
	buckets := make(map[source.FileID][]diag.TextEdit)
	for _, edit := range edits {
		e := copyEdit(edit)
		buckets[edit.Span.File] = append(buckets[edit.Span.File], e)
	}
	return buckets
}

func copyEdit(e diag.TextEdit) diag.TextEdit {
	return diag.TextEdit{
		Span:    e.Span,
		NewText: e.NewText,
		OldText: e.OldText,
	}
}

func cumulativeDelta(edits []diag.TextEdit, pos int) int {
	delta := 0
	for _, e := range edits {
		eStart := int(e.Span.Start)
		if eStart > pos {
			break
		}
		eEnd := int(e.Span.End)
		length := eEnd - eStart
		change := len(e.NewText) - length
		if eEnd <= pos {
			delta += change
		}
	}
	return delta
}

func insertEditSorted(edits []diag.TextEdit, edit diag.TextEdit) []diag.TextEdit {
	insertIdx := sort.Search(len(edits), func(i int) bool {
		if edits[i].Span.Start == edit.Span.Start {
			return edits[i].Span.End >= edit.Span.End
		}
		return edits[i].Span.Start > edit.Span.Start
	})
	edits = append(edits, diag.TextEdit{})
	copy(edits[insertIdx+1:], edits[insertIdx:])
	edits[insertIdx] = edit
	return edits
}

func formatFilePath(fs *source.FileSet, fileID source.FileID) string {
	if fs == nil {
		return ""
	}
	file := fs.Get(fileID)
	if file == nil {
		return ""
	}
	return file.FormatPath("auto", fs.BaseDir())
}
