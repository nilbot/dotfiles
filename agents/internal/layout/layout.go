// Package layout resolves the physical location of the four documentation
// roles. A repository with no manifest keeps the v1 docs/ layout; a repository
// with .agents/layout.json gets the layout that manifest declares. Resolution
// never writes and never creates a directory.
package layout

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
