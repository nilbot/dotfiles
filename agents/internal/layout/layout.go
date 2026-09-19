// Package layout resolves the physical location of the four documentation
// roles. A repository with no manifest keeps the v1 docs/ layout; a repository
// with .agents/layout.json gets the layout that manifest declares. Resolution
// never writes and never creates a directory.
package layout

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

const (
	SchemaV1    = "agents.layout/v1"
	SchemaV2    = "agents.layout/v2"
	ManifestRel = ".agents/layout.json"

	StatusActive    = "active"
	StatusMigrating = "migrating"

	TemplateCodeRepo     = "code-repo"
	TemplateContentVault = "content-vault"
	TemplateCustom       = "custom"

	RoleDesign  = "design"
	RolePlans   = "plans"
	RoleJournal = "journal"
	RoleQNA     = "qna"

	MinMutVerFloorV2 = "0.6.0"
)

const (
	ProblemManifestJSON   = "manifest_json"
	ProblemManifestRead   = "manifest_read"
	ProblemSchemaUnknown  = "schema_unknown"
	ProblemRoleUnknown    = "role_unknown"
	ProblemRoleMissing    = "role_missing"
	ProblemRoleDuplicate  = "role_duplicate"
	ProblemDuplicatePath  = "role_duplicate_path"
	ProblemPathEmpty      = "path_empty"
	ProblemPathInvalid    = "path_invalid"
	ProblemPathAbsolute   = "path_absolute"
	ProblemPathEscapes    = "path_escapes"
	ProblemPathSymlink    = "path_symlink"
	ProblemPathInAgents   = "path_in_agents"
	ProblemPathOverlap    = "path_overlap"
	ProblemPathCase       = "path_case_collision"
	ProblemArchiveOverlap = "archive_overlap"
	ProblemStatusUnknown  = "status_unknown"
	ProblemMigration      = "migration_missing"
	ProblemMinMutVerFloor = "min_mut_ver_floor_invalid"
	ProblemLocalAgents    = "local_agents"
)

// Problem is one reason a layout is not usable. Path carries the offending
// role, manifest key, or store path; Detail is free-form context.
type Problem struct {
	Code   string `json:"code"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// MigrationFrom is the frozen v1 source, recorded explicitly. `--resume` never
// re-plans and never derives the source layout from the working tree.
type MigrationFrom struct {
	Schema  string            `json:"schema"`
	Stores  map[string]string `json:"stores"`
	Archive string            `json:"archive,omitempty"`
}

type Move struct {
	Role   string `json:"role"`
	From   string `json:"from"`
	To     string `json:"to"`
	State  string `json:"state"`  // "pending" | "moving" | "done"
	Files  int    `json:"files"`  // source-tree walk at plan time
	Bytes  int64  `json:"bytes"`  // source-tree walk at plan time
	Digest string `json:"digest"` // SHA-256 over sorted paths, sizes, contents
}

type Migration struct {
	From            MigrationFrom `json:"from"`
	StartedAt       string        `json:"started_at"`
	BackupTag       string        `json:"backup_tag"`
	CreatedManifest bool          `json:"created_manifest"`
	Phase           string        `json:"phase"`
	Moves           []Move        `json:"moves"`
}

// Manifest is the persisted layout document. There is no profile and no
// store_root: the four stores below are the only physical source of truth.
type Manifest struct {
	Schema         string            `json:"schema"`
	MinMutVerFloor string            `json:"min_mut_ver_floor,omitempty"`
	LayoutStatus   string            `json:"layout_status"`
	Archive        string            `json:"archive,omitempty"`
	Stores         map[string]string `json:"stores"`
	Migration      *Migration        `json:"migration,omitempty"`
}

// Layout is the resolved manifest plus where it was read from and every reason
// that resolution is not clean. ManifestPath is set whenever a manifest was
// found at .agents/layout.json, including when it could not be read, could not
// be parsed, or declares a schema this build does not know; an empty
// ManifestPath means no manifest was there and the implicit v1 layout was
// synthesized.
type Layout struct {
	Manifest
	ManifestPath string    `json:"manifest_path,omitempty"`
	Problems     []Problem `json:"problems,omitempty"`
}

var roleNames = []string{RoleDesign, RolePlans, RoleJournal, RoleQNA}

// Roles returns the four documentation roles in their canonical order.
func Roles() []string { return append([]string(nil), roleNames...) }

func hasProblem(ps []Problem, code string) bool {
	for _, p := range ps {
		if p.Code == code {
			return true
		}
	}
	return false
}

// HasProblem is the exported form for callers outside this package.
func HasProblem(ps []Problem, code string) bool { return hasProblem(ps, code) }

func v1Layout(root string) Layout {
	stores := make(map[string]string, len(roleNames))
	for _, r := range roleNames {
		stores[r] = "docs/" + r
	}
	archive := ""
	if info, err := os.Stat(filepath.Join(root, "docs", "archive")); err == nil && info.IsDir() {
		archive = "docs/archive"
	}
	return Layout{
		Manifest: Manifest{
			Schema:       SchemaV1,
			LayoutStatus: StatusActive,
			Archive:      archive,
			Stores:       stores,
		},
	}
}

// V1ForRoot is the exported spelling of the implicit layout, for callers that
// need to hand it to scaffold.
func V1ForRoot(root string) Layout { return v1Layout(root) }

type rawManifest struct {
	Schema         string          `json:"schema"`
	MinMutVerFloor string          `json:"min_mut_ver_floor"`
	LayoutStatus   string          `json:"layout_status"`
	Archive        string          `json:"archive"`
	Stores         json.RawMessage `json:"stores"`
	Migration      *Migration      `json:"migration"`
}

// Resolve reads .agents/layout.json and reports the layout it declares. A
// repository with no manifest resolves to the implicit v1 layout; a manifest
// whose schema is unknown resolves to a problem and deliberately guesses
// nothing about stores.
func Resolve(root string) Layout {
	data, err := os.ReadFile(filepath.Join(root, ManifestRel))
	if os.IsNotExist(err) {
		return v1Layout(root)
	}
	if err != nil {
		return Layout{
			ManifestPath: ManifestRel,
			Problems:     []Problem{{Code: ProblemManifestRead, Path: ManifestRel, Detail: err.Error()}},
		}
	}
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return Layout{
			ManifestPath: ManifestRel,
			Problems:     []Problem{{Code: ProblemManifestJSON, Path: ManifestRel, Detail: err.Error()}},
		}
	}
	if raw.Schema != SchemaV2 {
		detail := raw.Schema
		if detail == "" {
			detail = "schema missing"
		}
		return Layout{
			Manifest:     Manifest{Schema: raw.Schema},
			ManifestPath: ManifestRel,
			Problems:     []Problem{{Code: ProblemSchemaUnknown, Path: ManifestRel, Detail: detail}},
		}
	}
	l := Layout{
		Manifest: Manifest{
			Schema:         raw.Schema,
			MinMutVerFloor: raw.MinMutVerFloor,
			LayoutStatus:   raw.LayoutStatus,
			Archive:        raw.Archive,
			Migration:      raw.Migration,
			Stores:         map[string]string{},
		},
		ManifestPath: ManifestRel,
	}
	if dup := duplicateStoreRoles(raw.Stores); len(dup) > 0 {
		for _, role := range dup {
			l.Problems = append(l.Problems, Problem{Code: ProblemRoleDuplicate, Path: ManifestRel, Detail: role})
		}
	}
	stores := bytes.TrimSpace(raw.Stores)
	switch {
	case len(stores) == 0:
		l.Problems = append(l.Problems, Problem{Code: ProblemRoleMissing, Path: ManifestRel, Detail: "stores"})
	case stores[0] == '{':
		var m map[string]string
		if err := json.Unmarshal(raw.Stores, &m); err != nil {
			l.Problems = append(l.Problems, Problem{Code: ProblemManifestJSON, Path: ManifestRel, Detail: err.Error()})
			break
		}
		for role, path := range m {
			l.Stores[role] = path
		}
	default:
		l.Problems = append(l.Problems, Problem{Code: ProblemManifestJSON, Path: ManifestRel, Detail: "stores must be a JSON object"})
	}
	l.Problems = append(l.Problems, Validate(root, l)...)
	return l
}

// duplicateStoreRoles reports roles named more than once in the manifest's
// stores object. encoding/json keeps only the last value, so a duplicate is
// invisible after unmarshalling and has to be found in the raw bytes.
func duplicateStoreRoles(raw json.RawMessage) []string {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // consume '{'
		return nil
	}
	seen := map[string]bool{}
	var dup []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return dup
		}
		role, _ := tok.(string)
		if seen[role] {
			dup = append(dup, role)
		}
		seen[role] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return dup
		}
	}
	return dup
}

// AgentsIgnored reports whether this repository's .agents/ is excluded from
// version control, which is what makes a manifest here machine-local (design
// §0.7, Decision 6).
//
// It asks about both paths. Measured with git: a directory rule (`.agents/`,
// `.agents`, `/.agents`) matches the directory, but `/.agents/**` matches only
// the manifest path — so a single query misses a real spelling of the same
// intent. The union is the question; either match means mutation is refused.
func AgentsIgnored(root string) (bool, error) {
	for _, rel := range []string{ManifestRel, ".agents"} {
		ignored, err := repo.IsIgnored(root, rel)
		if err != nil {
			return false, err
		}
		if ignored {
			return true, nil
		}
	}
	return false, nil
}

// Validate reports every v2 rule the layout breaks. v1 is grandfathered: its
// paths are synthesized by this package, not read from repository input.
func Validate(root string, l Layout) []Problem {
	if l.Schema == SchemaV1 {
		return nil
	}
	var ps []Problem
	validRoles := map[string]bool{RoleDesign: true, RolePlans: true, RoleJournal: true, RoleQNA: true}
	for role := range l.Stores {
		if !validRoles[role] {
			ps = append(ps, Problem{Code: ProblemRoleUnknown, Path: role})
		}
	}
	for _, role := range roleNames {
		if _, ok := l.Stores[role]; !ok {
			ps = append(ps, Problem{Code: ProblemRoleMissing, Path: role})
		}
	}
	if l.LayoutStatus != StatusActive && l.LayoutStatus != StatusMigrating {
		ps = append(ps, Problem{Code: ProblemStatusUnknown, Detail: l.LayoutStatus})
	}
	if l.LayoutStatus == StatusMigrating {
		switch {
		case l.Migration == nil || l.Migration.From.Schema == "" ||
			len(l.Migration.From.Stores) == 0 || len(l.Migration.Moves) == 0:
			ps = append(ps, Problem{Code: ProblemMigration, Path: ManifestRel, Detail: "journal incomplete"})
		case !validPhase(l.Migration.Phase):
			ps = append(ps, Problem{Code: ProblemMigration, Path: ManifestRel, Detail: "phase " + l.Migration.Phase})
		}
	}
	if l.MinMutVerFloor == "" {
		ps = append(ps, Problem{Code: ProblemMinMutVerFloor, Detail: "required"})
	} else if _, err := parseVersion(l.MinMutVerFloor); err != nil {
		ps = append(ps, Problem{Code: ProblemMinMutVerFloor, Detail: l.MinMutVerFloor})
	}
	seen := map[string]string{}
	for role, path := range l.Stores {
		if p := validateRel(root, path); p != nil {
			ps = append(ps, *p)
			continue
		}
		if path == ".agents" || strings.HasPrefix(path, ".agents/") {
			ps = append(ps, Problem{Code: ProblemPathInAgents, Path: path})
		}
		// Both sides of every comparison are trimmed, because `context` and
		// `context/` are the same path: V4's rule is that two roles resolve to
		// the same place, and the trimmed spelling is what the manifest's
		// canonical form carries. Trimming only the current path would make the
		// verdict depend on map iteration order.
		path = strings.TrimSuffix(path, "/")
		for otherRole, otherPath := range seen {
			otherPath = strings.TrimSuffix(otherPath, "/")
			if path == otherPath {
				ps = append(ps, Problem{Code: ProblemDuplicatePath, Path: path, Detail: role + "=" + otherRole})
			}
			if pathPrefix(path, otherPath) || pathPrefix(otherPath, path) {
				ps = append(ps, Problem{Code: ProblemPathOverlap, Path: path, Detail: role + "=" + otherRole})
			}
			if path != otherPath && strings.EqualFold(path, otherPath) {
				ps = append(ps, Problem{Code: ProblemPathCase, Path: path, Detail: otherPath})
			}
		}
		seen[role] = path
	}
	if l.Archive != "" {
		if p := validateRel(root, l.Archive); p != nil {
			ps = append(ps, *p)
		}
		for role, path := range l.Stores {
			if pathPrefix(path, l.Archive) || pathPrefix(l.Archive, path) {
				ps = append(ps, Problem{Code: ProblemArchiveOverlap, Path: l.Archive, Detail: role})
			}
		}
	}
	if ignored, err := AgentsIgnored(root); err != nil {
		if !errors.Is(err, repo.ErrNotARepo) {
			// Fail closed: an unresolvable trackedness question must not read as a
			// pass. Outside a repository there is nothing to commit, so plain
			// fixture directories stay valid.
			ps = append(ps, Problem{Code: ProblemLocalAgents, Path: ManifestRel, Detail: err.Error()})
		}
	} else if ignored {
		ps = append(ps, Problem{Code: ProblemLocalAgents, Path: ManifestRel})
	}
	return ps
}

// validPhase reports whether a migrating journal records one of the four
// phases from design §0.5. The names are spelled out here rather than through
// the exported Phase* constants, which land with the planner that writes them.
func validPhase(phase string) bool {
	switch phase {
	case "planned", "moved", "pruned", "router":
		return true
	default:
		return false
	}
}

// validateRel enforces: relative, non-empty, no "..", inside root, and no
// symlink component. The returned code is specific; callers that validate a
// non-store path remap it to their own problem code.
func validateRel(root, path string) *Problem {
	if path == "" {
		return &Problem{Code: ProblemPathEmpty, Detail: "empty"}
	}
	if filepath.IsAbs(path) {
		return &Problem{Code: ProblemPathAbsolute, Path: path}
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return &Problem{Code: ProblemPathEscapes, Path: path}
	}
	abs := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &Problem{Code: ProblemPathEscapes, Path: path}
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	cur := root
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			break
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &Problem{Code: ProblemPathSymlink, Path: path}
		}
	}
	return nil
}

func pathPrefix(parent, child string) bool {
	parent = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(parent)), "/")
	child = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(child)), "/")
	return parent == child || strings.HasPrefix(child, parent+"/")
}

// Path returns the store that role resolves to, and whether the layout has one.
func Path(l Layout, role string) (string, bool) {
	p, ok := l.Stores[role]
	return p, ok
}

// MarshalManifest renders a manifest as the bytes this repository commits:
// two-space indented JSON with a trailing newline.
func MarshalManifest(m Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
