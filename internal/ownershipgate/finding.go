// Package ownershipgate contains deterministic reporting support for the MIR
// ownership verifier. It does not run compilation, so buildpipeline may import
// it without creating an import cycle.
package ownershipgate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"surge/internal/mir"
	"surge/internal/source"
)

// FindingKey is the checkout-independent identity of one ownership finding.
// The attempted corpus fixture is intentionally absent: imported source may be
// reached through many roots, but the source span is the finding's real owner.
type FindingKey struct {
	Source            string `json:"source"`
	Function          string `json:"function"`
	LocalID           int32  `json:"local_id"`
	LocalName         string `json:"local_name"`
	ConsumingKind     string `json:"consuming_kind"`
	ConsumingPosition string `json:"consuming_position"`
	StartLine         uint32 `json:"start_line"`
	StartColumn       uint32 `json:"start_column"`
	EndLine           uint32 `json:"end_line"`
	EndColumn         uint32 `json:"end_column"`
	DefSite           string `json:"def_site"`
	ConsumingSite     string `json:"consuming_site"`
}

// NormalizeFinding resolves a verifier finding through the compilation's real
// FileSet and rejects spans that cannot be represented relative to repoRoot.
func NormalizeFinding(repoRoot string, files *source.FileSet, finding *mir.OwnershipFinding) (FindingKey, error) {
	var key FindingKey
	if finding == nil {
		return key, fmt.Errorf("normalize ownership finding: missing finding")
	}
	if files == nil {
		return key, fmt.Errorf("normalize ownership finding: missing file set")
	}
	if finding.Span == (source.Span{}) {
		return key, fmt.Errorf("normalize ownership finding: missing source span")
	}
	if !files.HasFile(finding.Span.File) {
		return key, fmt.Errorf("normalize ownership finding: unknown file id %d", finding.Span.File)
	}
	file := files.Get(finding.Span.File)
	if file == nil || file.Path == "" {
		return key, fmt.Errorf("normalize ownership finding: file %d has no path", finding.Span.File)
	}
	if finding.Span.Start > finding.Span.End || uint64(finding.Span.End) > uint64(len(file.Content)) {
		return key, fmt.Errorf("normalize ownership finding: invalid span %s for %s", finding.Span, file.Path)
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return key, fmt.Errorf("normalize ownership finding root: %w", err)
	}
	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(files.BaseDir(), path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return key, fmt.Errorf("normalize ownership finding source: %w", err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return key, fmt.Errorf("normalize ownership finding source: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return key, fmt.Errorf("normalize ownership finding: source %q is outside repository %q", path, root)
	}
	if strings.TrimSpace(finding.ConsumingPosition) == "" {
		return key, fmt.Errorf("normalize ownership finding: missing consuming position")
	}

	start, end := files.Resolve(finding.Span)
	return FindingKey{
		Source:            filepath.ToSlash(filepath.Clean(rel)),
		Function:          finding.Function,
		LocalID:           int32(finding.Local),
		LocalName:         finding.LocalName,
		ConsumingKind:     finding.ConsumingKind.String(),
		ConsumingPosition: finding.ConsumingPosition,
		StartLine:         start.Line,
		StartColumn:       start.Col,
		EndLine:           end.Line,
		EndColumn:         end.Col,
		DefSite:           finding.DefSite,
		ConsumingSite:     finding.ConsumingSite,
	}, nil
}

// DedupeFindings returns exact keys once, in deterministic order.
func DedupeFindings(findings []FindingKey) []FindingKey {
	set := make(map[FindingKey]struct{}, len(findings))
	for i := range findings {
		set[findings[i]] = struct{}{}
	}
	out := make([]FindingKey, 0, len(set))
	for finding := range set {
		out = append(out, finding)
	}
	sort.Slice(out, func(i, j int) bool {
		return findingOrderKey(&out[i]) < findingOrderKey(&out[j])
	})
	return out
}

func findingOrderKey(finding *FindingKey) string {
	return strings.Join([]string{
		finding.Source,
		finding.Function,
		strconv.FormatInt(int64(finding.LocalID), 10),
		finding.LocalName,
		finding.ConsumingKind,
		finding.ConsumingPosition,
		strconv.FormatUint(uint64(finding.StartLine), 10),
		strconv.FormatUint(uint64(finding.StartColumn), 10),
		strconv.FormatUint(uint64(finding.EndLine), 10),
		strconv.FormatUint(uint64(finding.EndColumn), 10),
		finding.DefSite,
		finding.ConsumingSite,
	}, "\x00")
}

func (f FindingKey) String() string {
	local := fmt.Sprintf("L%d", f.LocalID)
	if f.LocalName != "" {
		local += "(" + f.LocalName + ")"
	}
	return fmt.Sprintf("%s:%d:%d: %s: %s[%s] of %s (def %s) at %s",
		f.Source, f.StartLine, f.StartColumn, f.Function, f.ConsumingKind,
		f.ConsumingPosition, local, f.DefSite, f.ConsumingSite)
}
