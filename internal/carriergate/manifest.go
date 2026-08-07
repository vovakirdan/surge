package carriergate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

const digestAlgorithm = "sha256-length-prefixed-v1"

var allowanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Allowance records one narrowly legitimate source shape.
type Allowance struct {
	ID              string  `json:"id"`
	Finding         Finding `json:"finding"`
	Reason          string  `json:"reason"`
	SafeBecause     string  `json:"safe_because"`
	InvalidatedWhen string  `json:"invalidated_when"`
}

// MigrationCarrier records one carrier introduced after the base commit,
// together with the work that removes it.
//
// It is deliberately NOT part of the base census. That census is a census of a
// commit — it is re-derived by scanning that commit's tree — so a carrier which
// did not exist there cannot be written into it without making it disagree with
// the thing it is a census of.
//
// It is equally not an allowance. An allowance says a shape is legitimate and
// permanent; this says the opposite, that a shape is temporary and someone has
// named when it goes. Both fields are required for that reason: a carrier
// nobody has promised to remove is a regression wearing a different label.
type MigrationCarrier struct {
	Finding Finding `json:"finding"`
	// RetiredBy names the work that deletes this carrier.
	RetiredBy string `json:"retired_by"`
	// TrackedAs names the row that owns it until then.
	TrackedAs string `json:"tracked_as"`
}

// CategoryManifest freezes one independently ratcheted category. Legacy is
// the immutable exact-base set; Allow references justified members of it;
// Migration records what this epic added and who removes it.
type CategoryManifest struct {
	ID             string             `json:"id"`
	RetireToZero   bool               `json:"retire_to_zero"`
	BaselineCount  int                `json:"baseline_count"`
	BaselineDigest string             `json:"baseline_digest"`
	Legacy         []Finding          `json:"legacy"`
	Allow          []Allowance        `json:"allow"`
	Migration      []MigrationCarrier `json:"migration"`
}

// Manifest is the reviewed exact-base legacy carrier census.
type Manifest struct {
	Version         int                `json:"version"`
	BaseCommit      string             `json:"base_commit"`
	DigestAlgorithm string             `json:"digest_algorithm"`
	BaselineCount   int                `json:"baseline_count"`
	BaselineDigest  string             `json:"baseline_digest"`
	Scope           []Scope            `json:"scope"`
	Categories      []CategoryManifest `json:"categories"`
}

// LoadManifest decodes and validates a repository-owned manifest.
func LoadManifest(filePath string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(filePath) // #nosec G304 -- repository-owned gate input
	if err != nil {
		return manifest, fmt.Errorf("read carrier manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode carrier manifest: %w", err)
	}
	var trailer any
	if err := decoder.Decode(&trailer); err != io.EOF {
		return manifest, fmt.Errorf("decode carrier manifest trailer: %w", err)
	}
	if err := ValidateManifest(&manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// ValidateManifest rejects scope/category drift and noncanonical evidence.
func ValidateManifest(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("carrier manifest is nil")
	}
	if manifest.Version != ManifestVersion || manifest.BaseCommit != EpicBaseCommit {
		return fmt.Errorf("carrier manifest version/base = %d/%q, want %d/%q", manifest.Version, manifest.BaseCommit, ManifestVersion, EpicBaseCommit)
	}
	if manifest.DigestAlgorithm != digestAlgorithm {
		return fmt.Errorf("carrier manifest digest algorithm = %q", manifest.DigestAlgorithm)
	}
	if !reflect.DeepEqual(manifest.Scope, requiredScopes) {
		return fmt.Errorf("carrier manifest scope differs from required production scope")
	}
	if len(manifest.Categories) != len(requiredCategories) {
		return fmt.Errorf("carrier manifest has %d categories, want %d", len(manifest.Categories), len(requiredCategories))
	}
	all := make([]Finding, 0, manifest.BaselineCount)
	seen := make(map[findingKey]string, manifest.BaselineCount)
	allowIDs := make(map[string]struct{})
	for index := range manifest.Categories {
		category := &manifest.Categories[index]
		if category.ID != requiredCategories[index] || !category.RetireToZero {
			return fmt.Errorf("carrier category %d = %q/retire=%t, want %q/true", index, category.ID, category.RetireToZero, requiredCategories[index])
		}
		if category.Legacy == nil || category.Allow == nil || category.Migration == nil {
			return fmt.Errorf("carrier category %s requires explicit legacy, allow and migration arrays", category.ID)
		}
		if !findingsCanonical(category.Legacy) || !allowancesCanonical(category.Allow) ||
			!migrationsCanonical(category.Migration) {
			return fmt.Errorf("carrier category %s entries are not canonical", category.ID)
		}
		categoryFindings := append([]Finding(nil), category.Legacy...)
		categoryKeys := make(map[findingKey]struct{}, len(categoryFindings))
		for i := range categoryFindings {
			finding := &categoryFindings[i]
			if err := validateFinding(finding, category.ID); err != nil {
				return err
			}
			key := keyFor(finding)
			if previous, exists := seen[key]; exists {
				return fmt.Errorf("duplicate carrier finding %s (already in %s)", formatFinding(finding), previous)
			}
			seen[key] = category.ID
			categoryKeys[key] = struct{}{}
		}
		allowedKeys := make(map[findingKey]struct{}, len(category.Allow))
		for i := range category.Allow {
			allowance := &category.Allow[i]
			if !allowanceIDPattern.MatchString(allowance.ID) {
				return fmt.Errorf("carrier allowance has invalid id %q", allowance.ID)
			}
			if _, exists := allowIDs[allowance.ID]; exists {
				return fmt.Errorf("duplicate carrier allowance id %q", allowance.ID)
			}
			allowIDs[allowance.ID] = struct{}{}
			if strings.TrimSpace(allowance.Reason) == "" || strings.TrimSpace(allowance.SafeBecause) == "" || strings.TrimSpace(allowance.InvalidatedWhen) == "" {
				return fmt.Errorf("carrier allowance %s requires rationale and invalidation", allowance.ID)
			}
			if err := validateFinding(&allowance.Finding, category.ID); err != nil {
				return err
			}
			key := keyFor(&allowance.Finding)
			if _, exists := categoryKeys[key]; !exists {
				return fmt.Errorf("carrier allowance %s is not in the frozen category baseline", allowance.ID)
			}
			if _, exists := allowedKeys[key]; exists {
				return fmt.Errorf("duplicate carrier allowance finding %s", formatFinding(&allowance.Finding))
			}
			allowedKeys[key] = struct{}{}
		}
		migrationKeys := make(map[findingKey]struct{}, len(category.Migration))
		for i := range category.Migration {
			carrier := &category.Migration[i]
			if strings.TrimSpace(carrier.RetiredBy) == "" || strings.TrimSpace(carrier.TrackedAs) == "" {
				return fmt.Errorf("carrier migration entry %s requires the work that retires it and the row that tracks it",
					formatFinding(&carrier.Finding))
			}
			if err := validateFinding(&carrier.Finding, category.ID); err != nil {
				return err
			}
			key := keyFor(&carrier.Finding)
			if _, exists := categoryKeys[key]; exists {
				return fmt.Errorf("carrier migration entry %s is in the base census and belongs in legacy",
					formatFinding(&carrier.Finding))
			}
			if _, exists := migrationKeys[key]; exists {
				return fmt.Errorf("duplicate carrier migration entry %s", formatFinding(&carrier.Finding))
			}
			migrationKeys[key] = struct{}{}
		}
		// The migration set is deliberately absent from the count and the
		// digest below: those describe the base commit, and these did not
		// exist at it.
		if category.BaselineCount != len(categoryFindings) || category.BaselineDigest != Digest(categoryFindings) {
			return fmt.Errorf("carrier category %s baseline count/digest mismatch", category.ID)
		}
		all = append(all, categoryFindings...)
	}
	if manifest.BaselineCount != len(all) || manifest.BaselineDigest != Digest(all) {
		return fmt.Errorf("carrier manifest baseline count/digest mismatch")
	}
	return nil
}

func validateFinding(finding *Finding, category string) error {
	if finding.Category != category || finding.Token == "" || finding.Evidence == "" || finding.Ordinal < 1 {
		return fmt.Errorf("invalid carrier finding %+v for category %s", finding, category)
	}
	if strings.ContainsRune(finding.Path, 0) || strings.Contains(finding.Path, "\\") || !fs.ValidPath(finding.Path) || path.Clean(finding.Path) != finding.Path {
		return fmt.Errorf("invalid carrier finding path %q", finding.Path)
	}
	for _, scope := range requiredScopes {
		if finding.Path != scope.Root && strings.HasPrefix(finding.Path, scope.Root+"/") && hasExtension(finding.Path, scope.Extensions) && !excludedPath(finding.Path) {
			return nil
		}
	}
	return fmt.Errorf("carrier finding path %q is outside production scope", finding.Path)
}

// Digest hashes findings with unambiguous length-prefixed fields.
func Digest(findings []Finding) string {
	canonical := append([]Finding(nil), findings...)
	sortFindings(canonical)
	hash := sha256.New()
	var integer [8]byte
	for i := range canonical {
		finding := &canonical[i]
		for _, field := range []string{finding.Category, finding.Path, finding.Token, finding.Evidence} {
			binary.BigEndian.PutUint64(integer[:], uint64(len(field)))
			_, _ = hash.Write(integer[:])
			_, _ = hash.Write([]byte(field))
		}
		binary.BigEndian.PutUint64(integer[:], finding.Ordinal)
		_, _ = hash.Write(integer[:])
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool { return findingLess(&findings[i], &findings[j]) })
}

func findingLess(left, right *Finding) bool {
	if cmp := compareFindingFields(left, right); cmp != 0 {
		return cmp < 0
	}
	return left.Ordinal < right.Ordinal
}

func findingsCanonical(findings []Finding) bool {
	return sort.SliceIsSorted(findings, func(i, j int) bool { return findingLess(&findings[i], &findings[j]) })
}

func migrationsCanonical(carriers []MigrationCarrier) bool {
	return sort.SliceIsSorted(carriers, func(i, j int) bool {
		return findingLess(&carriers[i].Finding, &carriers[j].Finding)
	})
}

func allowancesCanonical(allowances []Allowance) bool {
	return sort.SliceIsSorted(allowances, func(i, j int) bool { return allowances[i].ID < allowances[j].ID })
}

func sortMigrations(carriers []MigrationCarrier) {
	sort.Slice(carriers, func(i, j int) bool {
		return findingLess(&carriers[i].Finding, &carriers[j].Finding)
	})
}
