package panicgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func newReader(raw []byte) io.Reader { return bytes.NewReader(raw) }

// AllowlistPath is where the ledger lives, relative to the repository root.
const AllowlistPath = "internal/panicgate/testdata/allowlist.json"

// Group is a reasoned disposition several sites share.
//
// The shape follows internal/ownershipgate/testdata/allowlist.json: a small set
// of dispositions a human wrote, and a row per subject that points at one. A
// reason written once and pointed at many times stays a reason; a reason
// copied three hundred times becomes a field nobody reads.
type Group struct {
	ID              string `json:"id"`
	Owner           string `json:"owner"`
	Disposition     string `json:"disposition"`
	Reason          string `json:"reason"`
	InvalidatedWhen string `json:"invalidated_when"`
}

// Row excuses one panic site.
//
// Message is not the key - Site is. It is recorded so that correcting a panic's
// wording is reported as a wording change at a site that is still recognised,
// instead of silently invalidating the row that explained it.
type Row struct {
	Site    string `json:"site"`
	Message string `json:"message"`
	Group   string `json:"group"`
}

// Allowlist is the whole ledger.
type Allowlist struct {
	Version int     `json:"version"`
	Groups  []Group `json:"groups"`
	Sites   []Row   `json:"sites"`
}

// LoadAllowlist reads the ledger, refusing a field the model does not know:
// a typo in a key is otherwise a row that silently excuses nothing.
func LoadAllowlist(path string) (*Allowlist, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- repository-owned path
	if err != nil {
		return nil, err
	}
	var out Allowlist
	dec := json.NewDecoder(newReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &out, nil
}

// Findings are the ways the ledger and the source can disagree. Each is
// reported separately because each has a different right answer.
type Findings struct {
	// Uncovered: a raise no fixture records and no row excuses.
	Uncovered []Site
	// Renumbered: a row whose site keeps its words but has changed ordinal,
	// because a raise was added or removed above it in the same function. It is
	// reported as a renumber rather than as a stale row plus an uncovered site,
	// so that adding one guard costs one clear finding per moved row instead of
	// two confusing ones.
	Renumbered []Drift
	// Stale: a row whose site no longer exists.
	Stale []Row
	// Drifted: a row whose site raises different words than it records.
	Drifted []Drift
	// Redundant: a row for a site a fixture now reaches. The excuse has been
	// overtaken by the coverage and should go, or the ledger stops describing
	// the tree - the failure mode ownershipgate's two-way check exists for.
	Redundant []Row
	// UnknownGroup: a row pointing at a disposition that is not defined.
	UnknownGroup []Row
	// UnusedGroup: a disposition no row points at.
	UnusedGroup []Group
	// Duplicate: two rows for one site.
	Duplicate []string
	// Unsorted names the first row out of order; the ledger is sorted so a
	// merge conflict is local and a diff is readable.
	Unsorted string
}

// Drift is a site whose wording no longer matches its row.
type Drift struct {
	Row      Row
	Site     Site
	Recorded string
	Actual   string
}

// Check compares the ledger against the sites the scan found and the messages
// the corpus records, in both directions.
func Check(sites []Site, coverage map[string][]string, list *Allowlist) Findings {
	var out Findings

	groups := map[string]Group{}
	for _, g := range list.Groups {
		groups[g.ID] = g
	}
	used := map[string]bool{}

	byKey := map[string]Site{}
	for _, s := range sites {
		byKey[s.Key()] = s
	}

	// Matching runs in three phases so that the two ways a ledger goes out of
	// date are told apart. An exact key-and-words match claims its site first.
	// What is left is offered the same words elsewhere in the same function -
	// that is a raise inserted above it, a renumber. Only then is a row with a
	// live key but different words called a drift.
	claimed := map[string]bool{}
	seen := map[string]bool{}
	prev := ""
	var pending []Row
	for _, row := range list.Sites {
		if row.Site <= prev && out.Unsorted == "" {
			out.Unsorted = row.Site
		}
		prev = row.Site
		if seen[row.Site] {
			out.Duplicate = append(out.Duplicate, row.Site)
			continue
		}
		seen[row.Site] = true
		if _, ok := groups[row.Group]; !ok {
			out.UnknownGroup = append(out.UnknownGroup, row)
			continue
		}
		used[row.Group] = true
		if site, ok := byKey[row.Site]; ok && site.Message == row.Message {
			claimed[site.Key()] = true
			if len(coverage[NormaliseMessage(site.Message)]) > 0 {
				out.Redundant = append(out.Redundant, row)
			}
			continue
		}
		pending = append(pending, row)
	}

	for _, row := range pending {
		if moved, ok := soleUnclaimedTwin(sites, claimed, row); ok {
			claimed[moved.Key()] = true
			out.Renumbered = append(out.Renumbered, Drift{
				Row: row, Site: moved, Recorded: row.Message, Actual: moved.Message,
			})
			continue
		}
		site, ok := byKey[row.Site]
		if !ok || claimed[site.Key()] {
			out.Stale = append(out.Stale, row)
			continue
		}
		claimed[site.Key()] = true
		out.Drifted = append(out.Drifted, Drift{
			Row: row, Site: site, Recorded: row.Message, Actual: site.Message,
		})
	}

	for _, s := range sites {
		if claimed[s.Key()] {
			continue
		}
		if len(coverage[NormaliseMessage(s.Message)]) > 0 {
			continue
		}
		out.Uncovered = append(out.Uncovered, s)
	}

	for _, g := range list.Groups {
		if !used[g.ID] {
			out.UnusedGroup = append(out.UnusedGroup, g)
		}
	}
	sort.Slice(out.Uncovered, func(i, j int) bool { return out.Uncovered[i].Key() < out.Uncovered[j].Key() })
	return out
}

// soleUnclaimedTwin finds the one unclaimed raise in the row's own function
// that still says exactly what the row records. "The one" is deliberate: when
// a function raises the same words twice there is no way to tell which of them
// the row meant, so the ambiguous case falls through and is reported as a
// drift or a stale row rather than guessed at.
func soleUnclaimedTwin(sites []Site, claimed map[string]bool, row Row) (Site, bool) {
	file, function, ok := splitSiteKey(row.Site)
	if !ok {
		return Site{}, false
	}
	var found Site
	n := 0
	for _, s := range sites {
		if s.File != file || s.Function != function || s.Message != row.Message || claimed[s.Key()] {
			continue
		}
		found = s
		n++
	}
	return found, n == 1
}

func splitSiteKey(key string) (file, function string, ok bool) {
	sep := strings.Index(key, "::")
	hash := strings.LastIndex(key, "#")
	if sep < 0 || hash < sep {
		return "", "", false
	}
	return key[:sep], key[sep+2 : hash], true
}

// RowFor renders the ledger line a site would need, so a failure hands the
// reader the text to paste rather than a rule to re-derive.
func RowFor(s *Site) string {
	row, err := json.Marshal(Row{Site: s.Key(), Message: s.Message, Group: "<pick a group>"})
	if err != nil {
		return s.Key()
	}
	return string(row)
}
