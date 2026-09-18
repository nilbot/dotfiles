# Layout Manifest and Store-Root Freedom Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the four documentation roles resolvable to any repository-relative
store through `.agents/layout.json`, without changing v1 filesystem or mutation
behavior, and without leaving a `docs/` shell in a content vault.

**Architecture:** A new read-only `agents/internal/layout` package resolves,
normalizes, validates, and version-checks a layout. One release, **v0.6.0**,
ships manifest parsing, the mutation guard, read-only `agents layout
show|validate|path`, atomic manifest writes, layout-aware
`scaffold.CreateWithLayout`, `agents init` layout flags, `agents layout migrate`
(plan/apply/resume), role-based skills, and legacy digests for both changed
skill assets. The non-mutating/mutating split is a code boundary tested with
version-stamped fixtures, not two releases. Drift and doctor consume the
resolved layout instead of joining `docs/`.

**Tech Stack:** Go 1.26+ standard library (`encoding/json`, `os`, `path/filepath`,
`crypto/sha256`), `agents/internal/repo` for Git, Markdown assets embedded with
`embed.FS`, GitHub Actions release workflow.

**Spec:** [docs/design/2026-09-18-layout-manifest-and-store-root-design.md](../design/2026-09-18-layout-manifest-and-store-root-design.md)

**Playbook:** [docs/plans/2026-09-18-layout-v2-migration-playbook.md](2026-09-18-layout-v2-migration-playbook.md)

## Global Constraints

- Schema string is exactly `agents.layout/v2`; the synthesized legacy schema is
  `agents.layout/v1`.
- Manifest path is exactly `.agents/layout.json`.
- `min_reader` written by v0.6.0 is exactly `0.6.0`.
- Profiles are exactly `code-repo`, `content-vault`, `custom`.
- Roles are exactly `design`, `plans`, `journal`, `qna`; all four are required
  in v2.
- `layout_status` is exactly `active` or `migrating`.
- The single release is `v0.6.0`. There is no intermediate release; a
  simulated older version in tests is `v0.5.99`.
- Exit codes: `OK=0`, `Advisory=1`, `Block=2`, `Malformed=3`, `Skip=4`,
  `NoRecord=5` (`agents/internal/exitcode`).
- No v1 filesystem or mutation behavior changes when `.agents/layout.json` is
  absent. Drift gains additive fields and doctor renames `docs:qna` to
  `layout:qna`; both are named in the design.
- Every changed embedded asset in v0.6.0 gets a legacy digest entry for its
  exact previous bytes, in the same release.
- The archive is never moved, rewritten, or walked for misplaced documents.
- No migration may copy a file; `git mv` is the only mechanism.
- All Go test invocations use `-count=1`; tests read tracked non-Go files.
- CLI Interface Documentation Invariant: `agents/README.md`, root `README.md`
  generated block, built-in help, `claude/skills/agents-tool/SKILL.md`, and
  affected docs update in the same change set.
- Direct pushes to `master` are forbidden; work lands through a PR that passes
  the `gate` job.
- The paperbubble repository stays frozen until the playbook is approved and
  v0.6.0 is installed and verified on every machine. No task in this plan runs
  against it.

## Locked Interfaces

```go
// agents/internal/layout/layout.go
package layout

const (
	SchemaV1    = "agents.layout/v1"
	SchemaV2    = "agents.layout/v2"
	ManifestRel = ".agents/layout.json"

	StatusActive    = "active"
	StatusMigrating = "migrating"

	ProfileCodeRepo     = "code-repo"
	ProfileContentVault = "content-vault"
	ProfileCustom       = "custom"

	RoleDesign  = "design"
	RolePlans   = "plans"
	RoleJournal = "journal"
	RoleQNA     = "qna"

	MinReaderV2 = "0.6.0"
)

type Problem struct {
	Code   string `json:"code"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type Move struct {
	Role  string `json:"role"`
	From  string `json:"from"`
	To    string `json:"to"`
	State string `json:"state"` // "pending" | "done"
}

type Migration struct {
	From      string `json:"from"`
	StartedAt string `json:"started_at"`
	Moves     []Move `json:"moves"`
}

type Manifest struct {
	Schema       string            `json:"schema"`
	MinReader    string            `json:"min_reader,omitempty"`
	Profile      string            `json:"profile,omitempty"`
	LayoutStatus string            `json:"layout_status"`
	StoreRoot    string            `json:"store_root,omitempty"`
	ContentRoot  string            `json:"content_root,omitempty"`
	Archive      string            `json:"archive,omitempty"`
	Stores       map[string]string `json:"stores"`
	Migration    *Migration        `json:"migration,omitempty"`
}

type Layout struct {
	Manifest
	ManifestPath string    `json:"manifest_path,omitempty"`
	Problems     []Problem `json:"problems,omitempty"`
}

func Roles() []string
func Resolve(root string) Layout
func V1ForRoot(root string) Layout
func Validate(root string, l Layout) []Problem
func HasProblem(ps []Problem, code string) bool
func Support(running string, l Layout) (ok bool, reason string)
func Path(l Layout, role string) (string, bool)

// agents/internal/layout/write.go (v0.6.0)
func MarshalManifest(m Manifest) ([]byte, error)
func WriteManifest(root string, m Manifest) error

// agents/internal/layout/migrate.go (v0.6.0)
type MigrateOptions struct {
	Profile     string
	StoreRoot   string
	Stores      map[string]string
	ContentRoot string
	Archive     string
	Running     string
	RouterState string // drift.RouterState as a string; "clean_current" or "clean_legacy"
}

type LinkCandidate struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Counts struct {
	Move    int `json:"move"`
	Keep    int `json:"keep"`
	Blocked int `json:"blocked"`
	Links   int `json:"links"`
}

type Plan struct {
	Repo           string          `json:"repo"`
	DryRun         bool            `json:"dry_run"`
	From           Layout          `json:"from"`
	To             Layout          `json:"to"`
	RouterAction   string          `json:"router"`
	Archive        string          `json:"archive,omitempty"`
	Moves          []Move          `json:"moves"`
	LinkCandidates []LinkCandidate `json:"link_candidates"`
	Blockers       []Blocker       `json:"blockers"`
	Counts         Counts          `json:"counts"`
}

func PlanMigration(root string, opts MigrateOptions) (Plan, error)
func ApplyMigration(root string, p Plan, backupTag string) error
func ResumeMigration(root string) error

// agents/internal/scaffold (v0.6.0)
func CreateWithLayout(root string, local bool, l layout.Layout) error

// agents/internal/drift
func InspectRepo(root string, runningVersion string) (DriftReport, error)
func CanonicalRouterDigestFor(l layout.Layout) string
```

### Shared test helpers

These helpers are defined once, in the package named beside them, and used by
the tests below. They are test scaffolding, never production code.

| Helper | Package | Behavior |
|---|---|---|
| `writeManifest(t, root, body)` | `layout` | creates `.agents/` and writes the raw body to `.agents/layout.json` |
| `mk(stores)` / `storesWith(role, path)` | `layout` | builds a valid-in-isolation v2 `Layout` with the four roles |
| `hasProblem(problems, code)` | `layout`, `drift` | reports whether a problem list contains a code |
| `hasBlocker(blockers, code)` | `layout` | reports whether a blocker list contains a code |
| `newGitV1Repo(t)` | `layout` | temp git repo, `scaffold.Create` v1 layout, clean tree |
| `newGitV1RepoWithContent(t)` | `layout` | the above plus `docs/design/a-design.md` and `docs/plans/a-plan.md` |
| `mkdirAll(t, root, rel)`, `writeFile(t, path, body)` | any | fail-fast filesystem helpers |
| `trackedBlobs(t, root, prefix...)` | `layout` | `git ls-files -s` restricted to prefixes: repo-relative path → blob hash |
| `gitOutput(t, root, args...)` | `layout`, `main` | runs Git and returns combined output, failing the test on error |
| `newRepoWithAgents(t)` | `main` | temp git repo with a v1 scaffold |
| `newV2RepoForCmd(t, storeRoot)` | `main` | temp git repo with a v2 manifest, stores, and a clean tree |
| `writeV2Layout(t, root, storeRoot)` | `drift` | writes an active v2 manifest plus the four store directories |
| `layoutRepo(stdout)` | `main` | resolves cwd → repository root with `.agents/`, printing the failure and returning an exit code |
| `testLayout(storeRoot)` | `scaffold` | the four-role `layout.Layout` used by scaffold tests |
| `snapshotTree(t, root)` | `main`, `scaffold` | sorted repository-relative path → mode + bytes, `.git` excluded |
| `findCheck(checks, name)` | `doctor` | finds a named `Check` |
| `newScaffoldedRepo(t)` / `newV2Repo(t, storeRoot)` | `doctor` | fixture repositories for the doctor tests |
| `setLayoutStatus(t, root, status)` | `doctor` | rewrites only `layout_status` in the fixture manifest |
| `deleteSkill(t, root)` | `main` | removes the v2 migration skill to give refresh something to write |
| `countMarkdownLinks(t, root, dir)` | `layout` | counts `](...)` links outside fenced code blocks |
| `rewriteLinks(t, root, candidates)` | `layout` | applies the `Old`→`New` mapping exactly as the skill does |
| `unresolvedLinks(t, root, dir)` | `layout` | lists relative link targets that do not resolve |

---

## Part A — layout inspection, guard, and read-only commands (ships in v0.6.0)

### Task 1: Resolve the implicit v1 and manifest v2 layouts

**Files:**
- Create: `agents/internal/layout/layout.go`
- Create: `agents/internal/layout/layout_test.go`

**Interfaces:**
- Consumes: `encoding/json`, `os`, `path/filepath`, `io/fs`.
- Produces: `layout.Resolve(root) Layout`, the `Manifest`, `Layout`, and
  `Problem` types, and the problem-code constants used by later tasks.

- [ ] **Step 1: Write the failing resolution tests**

```go
package layout

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestRel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveWithoutManifestIsImplicitV1(t *testing.T) {
	l := Resolve(t.TempDir())
	if l.Schema != SchemaV1 {
		t.Fatalf("schema = %q, want %q", l.Schema, SchemaV1)
	}
	if l.Stores[RoleQNA] != "docs/qna" || l.Stores[RolePlans] != "docs/plans" {
		t.Fatalf("v1 stores = %v", l.Stores)
	}
	if l.ManifestPath != "" || len(l.Problems) != 0 {
		t.Fatalf("v1 layout = %+v", l)
	}
}

func TestResolveV2MapIsCanonical(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_reader": "0.6.0",
  "profile": "content-vault",
  "layout_status": "active",
  "content_root": ".",
  "store_root": ".context",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}`)
	l := Resolve(root)
	if l.Schema != SchemaV2 || l.Profile != ProfileContentVault {
		t.Fatalf("layout = %+v", l)
	}
	if l.Stores[RoleDesign] != ".context/design" || l.Stores[RoleQNA] != ".context/qna" {
		t.Fatalf("stores = %v", l.Stores)
	}
	if len(l.Problems) != 0 {
		t.Fatalf("problems = %v", l.Problems)
	}
}

func TestResolveV2ShorthandExpands(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_reader": "0.6.0",
  "profile": "code-repo",
  "layout_status": "active",
  "store_root": ".context",
  "stores": ["design", "plans", "journal", "qna"]
}`)
	l := Resolve(root)
	if l.Stores[RoleJournal] != ".context/journal" {
		t.Fatalf("stores = %v", l.Stores)
	}
}

func TestResolveRejectsDuplicateStoreKeys(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_reader": "0.6.0",
  "profile": "custom",
  "layout_status": "active",
  "stores": {
    "design": "a/design",
    "design": "b/design",
    "plans": "a/plans",
    "journal": "a/journal",
    "qna": "a/qna"
  }
}`)
	l := Resolve(root)
	if !hasProblem(l.Problems, ProblemRoleDuplicate) {
		t.Fatalf("problems = %v, want %s", l.Problems, ProblemRoleDuplicate)
	}
}

func TestResolveUnknownSchemaIsUnsupportedNotGuessed(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{"schema":"agents.layout/v9","layout_status":"active","stores":{}}`)
	l := Resolve(root)
	if l.Schema != "agents.layout/v9" || !hasProblem(l.Problems, ProblemSchemaUnknown) {
		t.Fatalf("layout = %+v", l)
	}
	if l.Stores != nil {
		t.Fatalf("an unknown schema must not resolve stores: %v", l.Stores)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run 'TestResolve' -v`
Expected: FAIL — the `layout` package does not exist.

- [ ] **Step 3: Implement `layout.go`**

```go
// Package layout resolves the physical location of the four documentation
// roles. A repository with no manifest keeps the v1 docs/ layout; a repository
// with .agents/layout.json gets the layout that manifest declares. Resolution
// never writes and never creates a directory.
package layout

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	SchemaV1    = "agents.layout/v1"
	SchemaV2    = "agents.layout/v2"
	ManifestRel = ".agents/layout.json"

	StatusActive    = "active"
	StatusMigrating = "migrating"

	ProfileCodeRepo     = "code-repo"
	ProfileContentVault = "content-vault"
	ProfileCustom       = "custom"

	RoleDesign  = "design"
	RolePlans   = "plans"
	RoleJournal = "journal"
	RoleQNA     = "qna"

	MinReaderV2 = "0.6.0"
)

const (
	ProblemManifestJSON    = "manifest_json"
	ProblemManifestRead    = "manifest_read"
	ProblemSchemaUnknown   = "schema_unknown"
	ProblemRoleUnknown     = "role_unknown"
	ProblemRoleMissing     = "role_missing"
	ProblemRoleDuplicate   = "role_duplicate"
	ProblemDuplicatePath   = "role_duplicate_path"
	ProblemPathEmpty       = "path_empty"
	ProblemPathInvalid     = "path_invalid"
	ProblemPathAbsolute    = "path_absolute"
	ProblemPathEscapes     = "path_escapes"
	ProblemPathSymlink     = "path_symlink"
	ProblemPathInAgents    = "path_in_agents"
	ProblemPathOverlap     = "path_overlap"
	ProblemPathCase        = "path_case_collision"
	ProblemStoreRoot       = "store_root_mismatch"
	ProblemArchiveOverlap  = "archive_overlap"
	ProblemProfileUnknown  = "profile_unknown"
	ProblemStatusUnknown   = "status_unknown"
	ProblemMigration       = "migration_missing"
	ProblemMinReader       = "min_reader_invalid"
	ProblemLocalAgents     = "local_agents"
	ProblemContentRoot     = "content_root_invalid"
)

var roleNames = []string{RoleDesign, RolePlans, RoleJournal, RoleQNA}

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
			ContentRoot:  ".",
			Archive:      archive,
			Stores:       stores,
		},
	}
}

// V1ForRoot is the exported spelling of the implicit layout, for callers that
// need to hand it to scaffold.
func V1ForRoot(root string) Layout { return v1Layout(root) }

type rawManifest struct {
	Schema       string          `json:"schema"`
	MinReader    string          `json:"min_reader"`
	Profile      string          `json:"profile"`
	LayoutStatus string          `json:"layout_status"`
	StoreRoot    string          `json:"store_root"`
	ContentRoot  string          `json:"content_root"`
	Archive      string          `json:"archive"`
	Stores       json.RawMessage `json:"stores"`
	Migration    *Migration      `json:"migration"`
}

func Resolve(root string) Layout {
	data, err := os.ReadFile(filepath.Join(root, ManifestRel))
	if errors.Is(err, fs.ErrNotExist) {
		return v1Layout(root)
	}
	if err != nil {
		return Layout{Problems: []Problem{{Code: ProblemManifestRead, Path: ManifestRel, Detail: err.Error()}}}
	}
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return Layout{Problems: []Problem{{Code: ProblemManifestJSON, Path: ManifestRel, Detail: err.Error()}}}
	}
	if raw.Schema != SchemaV2 {
		return Layout{
			Manifest:     Manifest{Schema: raw.Schema},
			ManifestPath: ManifestRel,
			Problems:     []Problem{{Code: ProblemSchemaUnknown, Path: ManifestRel, Detail: raw.Schema}},
		}
	}
	l := Layout{
		Manifest: Manifest{
			Schema:       raw.Schema,
			MinReader:    raw.MinReader,
			Profile:      raw.Profile,
			LayoutStatus: raw.LayoutStatus,
			StoreRoot:    raw.StoreRoot,
			ContentRoot:  raw.ContentRoot,
			Archive:      raw.Archive,
			Migration:    raw.Migration,
			Stores:       map[string]string{},
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
	case stores[0] == '[':
		if raw.StoreRoot == "" {
			l.Problems = append(l.Problems, Problem{Code: ProblemStoreRoot, Path: ManifestRel, Detail: "required with the array form"})
			break
		}
		var names []string
		if err := json.Unmarshal(raw.Stores, &names); err != nil {
			l.Problems = append(l.Problems, Problem{Code: ProblemManifestJSON, Path: ManifestRel, Detail: err.Error()})
			break
		}
		for _, role := range names {
			if _, seen := l.Stores[role]; seen {
				l.Problems = append(l.Problems, Problem{Code: ProblemRoleDuplicate, Path: ManifestRel, Detail: role})
				continue
			}
			l.Stores[role] = filepath.ToSlash(filepath.Join(raw.StoreRoot, role))
		}
	default:
		l.Problems = append(l.Problems, Problem{Code: ProblemManifestJSON, Path: ManifestRel, Detail: "stores must be an object or an array"})
	}
	l.Problems = append(l.Problems, Validate(root, l)...)
	return l
}

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

func Path(l Layout, role string) (string, bool) {
	p, ok := l.Stores[role]
	return p, ok
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -count=1 ./internal/layout -run 'TestResolve' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/internal/layout
git commit -m "feat(layout): resolve implicit v1 and manifest v2 layouts"
```

---

### Task 2: Validate every rule and compare versions

**Files:**
- Modify: `agents/internal/layout/layout.go`
- Create: `agents/internal/layout/version.go`
- Create: `agents/internal/layout/validate_test.go`
- Create: `agents/internal/layout/version_test.go`

**Interfaces:**
- Consumes: `Resolve`, `Layout`, `Problem`.
- Produces: `Validate(root, l) []Problem`, `Support(running, l) (bool, string)`,
  and the V1–V19 validation rules from design §5.2.

- [ ] **Step 1: Write the failing validation and version tests**

```go
func TestValidateRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		l    Layout
		code string
	}{
		{"absolute", mk(storesWith(RoleDesign, "/tmp/design")), ProblemPathAbsolute},
		{"escape", mk(storesWith(RoleDesign, "../design")), ProblemPathEscapes},
		{"symlink", mk(storesWith(RoleDesign, "linked/design")), ProblemPathSymlink},
		{"agents", mk(storesWith(RoleDesign, ".agents/design")), ProblemPathInAgents},
		{"overlap", mk(storesWith(RoleDesign, "context")), ProblemPathOverlap},
		{"case", mk(caseCollisionStores()), ProblemPathCase},
		{"unknown-role", mk(map[string]string{"specs": "context/specs"}), ProblemRoleUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Validate(root, tc.l); !hasProblem(got, tc.code) {
				t.Fatalf("problems = %v, want %s", got, tc.code)
			}
		})
	}
}

func mk(stores map[string]string) Layout {
	return Layout{Manifest: Manifest{
		Schema: SchemaV2, MinReader: MinReaderV2, Profile: ProfileCustom,
		LayoutStatus: StatusActive, ContentRoot: ".", Stores: stores,
	}}
}

func storesWith(role, path string) map[string]string {
	m := map[string]string{
		RoleDesign: "context/design", RolePlans: "context/plans",
		RoleJournal: "context/journal", RoleQNA: "context/qna",
	}
	m[role] = path
	return m
}

func caseCollisionStores() map[string]string {
	return map[string]string{
		RoleDesign: "context/design", RolePlans: "Context/Design",
		RoleJournal: "context/journal", RoleQNA: "context/qna",
	}
}

func TestSupportRefusesBelowFloorAndAllowsV1Control(t *testing.T) {
	v2 := mk(storesWith(RoleDesign, "context/design"))
	if ok, reason := Support("v0.5.99", v2); ok || reason != "min_reader" {
		t.Fatalf("v0.5.99 on v2 = (%v, %q), want refused below the floor", ok, reason)
	}
	if ok, reason := Support("v0.6.0", v2); !ok {
		t.Fatalf("v0.6.0 on v2 refused: %q", reason)
	}
	v1 := v1Layout(t.TempDir())
	if ok, reason := Support("v0.5.99", v1); !ok {
		t.Fatalf("v0.5.99 on v1 refused: %q", reason)
	}
	if ok, reason := Support("dev", v2); ok || reason != "unreleased" {
		t.Fatalf("an unstamped build on v2 = (%v, %q), want refused as unreleased", ok, reason)
	}
	if ok, reason := Support("dev", v1); !ok {
		t.Fatalf("an unstamped build on v1 refused: %q", reason)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct{ a, b string; want int }{
		{"v0.6.0", "0.6.0", 0},
		{"0.5.99", "0.6.0", -1},
		{"v0.6.0", "0.6.0-rc.1", 1},
		{"v0.10.0", "v0.9.9", 1},
	}
	for _, tc := range cases {
		got, err := compareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("compare(%q, %q) = (%d, %v), want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run 'TestValidate|TestSupport|TestCompare' -v`
Expected: FAIL — `Validate`, `Support`, and `compareVersions` are undefined.

- [ ] **Step 3: Implement validation**

```go
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
	if l.Profile != ProfileCodeRepo && l.Profile != ProfileContentVault && l.Profile != ProfileCustom {
		ps = append(ps, Problem{Code: ProblemProfileUnknown, Detail: l.Profile})
	}
	if l.LayoutStatus != StatusActive && l.LayoutStatus != StatusMigrating {
		ps = append(ps, Problem{Code: ProblemStatusUnknown, Detail: l.LayoutStatus})
	}
	if l.LayoutStatus == StatusMigrating && (l.Migration == nil || l.Migration.From == "" || len(l.Migration.Moves) == 0) {
		ps = append(ps, Problem{Code: ProblemMigration, Path: ManifestRel})
	}
	if l.MinReader == "" {
		ps = append(ps, Problem{Code: ProblemMinReader, Detail: "required"})
	} else if _, err := parseVersion(l.MinReader); err != nil {
		ps = append(ps, Problem{Code: ProblemMinReader, Detail: l.MinReader})
	}
	if l.ContentRoot == "" {
		l.ContentRoot = "."
	}
	if p := validateRel(root, l.ContentRoot); p != nil {
		p.Code = ProblemContentRoot
		ps = append(ps, *p)
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
		path = strings.TrimSuffix(path, "/")
		for otherRole, otherPath := range seen {
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
	if l.StoreRoot != "" {
		if p := validateRel(root, l.StoreRoot); p != nil {
			p.Code = ProblemStoreRoot
			ps = append(ps, *p)
		} else {
			for role, path := range l.Stores {
				if !pathPrefix(l.StoreRoot, path) {
					ps = append(ps, Problem{Code: ProblemStoreRoot, Path: l.StoreRoot, Detail: role})
				}
			}
		}
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
	if agentsExcluded(root) {
		ps = append(ps, Problem{Code: ProblemLocalAgents, Path: ManifestRel})
	}
	return ps
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
```

`agentsExcluded` reads `<git-dir>/info/exclude` through
`repo.InfoExcludePath(root)` and reports whether an exact `/.agents/` line is
present. `ProblemContentRoot` and `ProblemPathInvalid` share `validateRel`;
the caller maps the code to the right problem so a bad `content_root` never
reports `path_invalid`.

- [ ] **Step 4: Implement version comparison in `version.go`**

```go
package layout

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct {
	major, minor, patch int
	pre                 string
}

func parseVersion(s string) (semver, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "v"))
	if s == "" || strings.EqualFold(s, "dev") {
		return semver{}, fmt.Errorf("not a release version: %q", s)
	}
	main, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("not MAJOR.MINOR.PATCH: %q", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("bad version component %q", p)
		}
		nums[i] = n
	}
	return semver{nums[0], nums[1], nums[2], pre}, nil
}

func compareVersions(a, b string) (int, error) {
	av, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]int{{av.major, bv.major}, {av.minor, bv.minor}, {av.patch, bv.patch}} {
		if pair[0] < pair[1] {
			return -1, nil
		}
		if pair[0] > pair[1] {
			return 1, nil
		}
	}
	switch {
	case av.pre == "" && bv.pre == "":
		return 0, nil
	case av.pre == "":
		return 1, nil
	case bv.pre == "":
		return -1, nil
	case av.pre < bv.pre:
		return -1, nil
	case av.pre > bv.pre:
		return 1, nil
	default:
		return 0, nil
	}
}

// Support reports whether this binary may mutate the repository. Reads are
// permitted regardless; callers that only display a layout must not use this
// as a read gate.
func Support(running string, l Layout) (bool, string) {
	if l.Schema == SchemaV1 {
		return true, ""
	}
	if l.Schema != SchemaV2 {
		return false, "unknown_schema"
	}
	if l.LayoutStatus == StatusMigrating {
		return false, "migrating"
	}
	if l.MinReader == "" {
		return true, ""
	}
	got, err := parseVersion(running)
	if err != nil {
		return false, "unreleased" // fail closed: a source build cannot prove its release
	}
	want, err := parseVersion(l.MinReader)
	if err != nil {
		return false, "invalid"
	}
	if compareValues(got, want) < 0 {
		return false, "min_reader"
	}
	return true, ""
}

func compareValues(a, b semver) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	switch {
	case a.pre == "" && b.pre == "":
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	case a.pre < b.pre:
		return -1
	case a.pre > b.pre:
		return 1
	default:
		return 0
	}
}
```

- [ ] **Step 5: Run the package tests**

Run: `go test -count=1 ./internal/layout -v`
Expected: PASS. Also run `gofmt -l agents/internal/layout` and expect no output.

- [ ] **Step 6: Commit**

```bash
git add agents/internal/layout
git commit -m "feat(layout): validate store paths and gate mutations by min_reader"
```

---

### Task 3: Add the v2 router and extend the digest catalog

**Files:**
- Modify: `agents/internal/scaffold/scaffold.go`
- Modify: `agents/internal/drift/digests.go`
- Create: `agents/internal/drift/router_test.go`

**Interfaces:**
- Consumes: `layout.Layout`, `scaffold.DefaultAgentsMD`.
- Produces: `scaffold.V2AgentsMD`, `drift.CanonicalRouterDigestFor(l)`.

- [ ] **Step 1: Write the failing router tests**

```go
func TestV2RouterNamesTheManifestNotAStorePath(t *testing.T) {
	for _, want := range []string{".agents/layout.json", "agents.layout/v2", "agents layout path"} {
		if !strings.Contains(scaffold.V2AgentsMD, want) {
			t.Errorf("v2 router does not name %q", want)
		}
	}
	if strings.Contains(scaffold.V2AgentsMD, "docs/") {
		t.Error("the v2 router must not hardcode a store path")
	}
}

func TestV1RouterIsLegacyForV2(t *testing.T) {
	v1 := layout.Resolve(t.TempDir())
	v2 := layout.Layout{Manifest: layout.Manifest{
		Schema: layout.SchemaV2, MinReader: layout.MinReaderV2,
		Profile: layout.ProfileContentVault, LayoutStatus: layout.StatusActive,
		Stores: map[string]string{
			layout.RoleDesign:  ".context/design",
			layout.RolePlans:   ".context/plans",
			layout.RoleJournal: ".context/journal",
			layout.RoleQNA:     ".context/qna",
		},
	}}
	if digest := DigestString(scaffold.DefaultAgentsMD); CanonicalRouterDigestFor(v1) != digest {
		t.Fatal("v1 must keep DefaultAgentsMD as its canonical router")
	}
	if CanonicalRouterDigestFor(v2) == digest {
		t.Fatal("v2 must not accept the v1 router as current")
	}
	if !IsLegacyRouterDigest(digest) {
		t.Fatal("the v1 router must be legacy for a v2 repository")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/drift -run 'TestV2Router|TestV1RouterIsLegacy' -v`
Expected: FAIL — `scaffold.V2AgentsMD` and `CanonicalRouterDigestFor` are
undefined.

- [ ] **Step 3: Add `V2AgentsMD` to `scaffold.go`**

Paste the router from design §4.4 as a raw string constant, exactly as the
design shows. Add a comment above it:

```go
// V2AgentsMD is the canonical router for a repository with
// .agents/layout.json. It names the manifest and the CLI's resolved view; it
// must not name a store path. DefaultAgentsMD remains the v1 router.
const V2AgentsMD = `# Agent context

Durable context for this repo is described by ` + "`.agents/layout.json`" + `
(` + "`agents.layout/v2`" + `). Read that manifest before assuming where anything lives;
it names the meta stores and their paths. This file is only the pointer.

- ` + "`.agents/layout.json`" + ` — profile, status, and one path per role
- ` + "`agents layout show`" + ` — the same layout, resolved and validated (when the CLI is installed)
- ` + "`agents layout path <role>`" + ` — one store, by role: design, plans, journal, qna

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently.
- If the ` + "`agents`" + ` CLI is installed, run ` + "`agents doctor`" + ` early and report any warnings before relying on this context.
- If ` + "`agents`" + ` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above.

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + `
skill; it is not repo-specific and is not restated here.
`
```

- [ ] **Step 4: Add the layout-aware digest selector and extend the legacy list**

```go
// CanonicalRouterDigestFor returns the digest a repository with this layout
// must match for RouterCleanCurrent.
func CanonicalRouterDigestFor(l layout.Layout) string {
	if l.Schema == layout.SchemaV2 {
		return DigestString(scaffold.V2AgentsMD)
	}
	return DigestString(scaffold.DefaultAgentsMD)
}
```

In `LegacyRouterDigests`, add `scaffold.DefaultAgentsMD` to the `templates`
slice. The canonical check runs first, so a v1 repository is unaffected; a v2
repository carrying the v1 router becomes `clean_legacy`.

- [ ] **Step 5: Run the tests**

Run: `go test -count=1 ./internal/drift ./internal/scaffold -v`
Expected: PASS. Existing `TestInspectCleanCurrentRepo` is the v1 positive
control: it must still report `clean_current`.

- [ ] **Step 6: Commit**

```bash
git add agents/internal/scaffold/scaffold.go agents/internal/drift
git commit -m "feat(drift): add the v2 router and keep the v1 router as legacy"
```

---

### Task 4: Report layout fields and resolve stores in drift

**Files:**
- Modify: `agents/internal/drift/drift.go`
- Modify: `agents/internal/drift/drift_test.go`
- Modify: `agents/cmd_drift.go`
- Modify: `agents/cmd_fleet.go`
- Modify: `agents/internal/doctor/doctor.go`

**Interfaces:**
- Consumes: `layout.Resolve`, `layout.Support`, `CanonicalRouterDigestFor`.
- Produces: `InspectRepo(root, runningVersion) (DriftReport, error)` with
  `layout_version`, `profile`, `min_reader`, `layout_status`, `stores`,
  `unsupported`, and `unsupported_detail`.

- [ ] **Step 1: Write the failing drift tests**

```go
func TestInspectV1ReportKeepsLegacyFieldsAndAddsLayoutFields(t *testing.T) {
	dir := t.TempDir()
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	rep, err := InspectRepo(dir, "v0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	if rep.LayoutVersion != "v1" || rep.Stores["qna"] != "docs/qna" {
		t.Fatalf("layout fields = %+v", rep)
	}
	if !rep.DocsStores["qna"] {
		t.Fatal("docs_stores must remain populated for v1 consumers")
	}
	if rep.Unsupported != "" {
		t.Fatalf("v1 must be supported, got %q", rep.Unsupported)
	}
}

func TestInspectV2ReportResolvesStoresAndRefusesBelowFloor(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	rep, err := InspectRepo(dir, "v0.5.99")
	if err != nil {
		t.Fatal(err)
	}
	if rep.LayoutVersion != "v2" || rep.Stores["qna"] != ".context/qna" {
		t.Fatalf("layout fields = %+v", rep)
	}
	if rep.Unsupported != "min_reader" {
		t.Fatalf("unsupported = %q, want min_reader", rep.Unsupported)
	}
	if isDriftClean(rep) {
		t.Fatal("an unsupported layout must not report clean")
	}
}

func TestInspectV2MisplacedDocsWalksDeclaredStoresNotTheVault(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	writeFile(t, filepath.Join(dir, ".context/journal/wrong-plan.md"), "# wrong\n")
	writeFile(t, filepath.Join(dir, "protein/a-plan.md"), "# vault note\n") // not an agents plan
	rep, _ := InspectRepo(dir, "v0.6.0")
	if len(rep.MisplacedDocs) != 1 || rep.MisplacedDocs[0] != ".context/journal/wrong-plan.md" {
		t.Fatalf("misplaced = %v", rep.MisplacedDocs)
	}
}
```

`writeV2Layout` writes the manifest and the four store directories; keep it in
`drift_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/drift -run TestInspectV1Report -v`
Expected: FAIL — `InspectRepo` takes one argument.

- [ ] **Step 3: Add the fields and resolve the layout**

```go
type DriftReport struct {
	RepoPath      string            `json:"repo_path"`
	RouterState   RouterState       `json:"router_state"`
	SymlinkState  string            `json:"symlink_state"`
	DomainState   string            `json:"domain_state"`
	Skills        map[string]string `json:"skills"`
	LocalSkills   []string          `json:"local_skills"`
	DocsStores    map[string]bool   `json:"docs_stores"` // deprecated alias of role presence
	MisplacedDocs []string          `json:"misplaced_docs"`
	Diff          string            `json:"diff,omitempty"`

	LayoutVersion     string            `json:"layout_version"`
	Profile           string            `json:"profile,omitempty"`
	MinReader         string            `json:"min_reader,omitempty"`
	LayoutStatus      string            `json:"layout_status,omitempty"`
	Stores            map[string]string `json:"stores"`
	Unsupported       string            `json:"unsupported,omitempty"`
	UnsupportedDetail string            `json:"unsupported_detail,omitempty"`
}
```

At the top of `InspectRepo`:

```go
l := layout.Resolve(root)
report.LayoutVersion = "v1"
if l.Schema == layout.SchemaV2 {
	report.LayoutVersion = "v2"
} else if l.Schema != layout.SchemaV1 {
	report.LayoutVersion = "unknown"
}
report.Profile = l.Profile
report.MinReader = l.MinReader
report.LayoutStatus = l.LayoutStatus
report.Stores = l.Stores
if len(l.Problems) > 0 {
	if layout.HasProblem(l.Problems, layout.ProblemSchemaUnknown) {
		report.Unsupported = "unknown_schema"
	} else {
		report.Unsupported = "invalid"
	}
	report.UnsupportedDetail = problemsText(l.Problems)
} else if ok, reason := layout.Support(runningVersion, l); !ok {
	if reason == "migrating" {
		report.LayoutStatus = layout.StatusMigrating
	} else {
		report.Unsupported = reason
		report.UnsupportedDetail = fmt.Sprintf("requires agents >= %s", l.MinReader)
	}
}
```

Router comparison uses `CanonicalRouterDigestFor(l)`. Store presence iterates
`l.Stores` and stats `filepath.Join(root, path)`, populating both `DocsStores`
and `Stores`. Misplacement dispatches:

- v1 or unknown schema: keep today's `docs/` walker byte-for-byte;
- v2: walk `store_root` when present, otherwise each declared store; skip the
  archive subtree; classify `-plan.md` outside the `plans` store and
  `-design.md` outside the `design` store.

`isDriftClean` adds:

```go
if report.Unsupported != "" || report.LayoutStatus == layout.StatusMigrating {
	return false
}
```

- [ ] **Step 4: Thread the running version through every caller**

```go
// cmd_drift.go
func runDrift(args []string, stdout io.Writer) int {
	return runDriftWithVersion(args, stdout, version)
}
func runDriftWithVersion(args []string, stdout io.Writer, running string) int { ... }
```

`runFleetUpdateWithWire` gains a `runFleetUpdateWithVersion(...)` sibling;
`doctor.Dependencies` gains `RunningVersion string`, set by `cmd_doctor.go`
from `version` and consumed by `checkScaffold(repoRoot, deps.RunningVersion)`.

- [ ] **Step 5: Run the affected packages**

Run: `go test -count=1 ./...`
Expected: PASS. The v1 positive control is the existing clean-repo test plus
`TestInspectV1ReportKeepsLegacyFieldsAndAddsLayoutFields`.

- [ ] **Step 6: Commit**

```bash
git add agents
git commit -m "feat(drift): report and resolve the repository layout"
```

---

### Task 5: Replace `docs:qna` with layout-aware doctor checks

**Files:**
- Modify: `agents/internal/doctor/doctor.go`
- Modify: `agents/internal/doctor/doctor_test.go`

**Interfaces:**
- Consumes: `layout.Resolve`, `layout.Support`, `layout.Path`.
- Produces: `layout:manifest`, `layout:stores`, and `layout:qna` checks.

- [ ] **Step 1: Write the failing doctor tests**

```go
func TestDoctorLayoutChecksResolveV1AndV2(t *testing.T) {
	for _, tc := range []struct{ name, root, qna string }{
		{"v1", newScaffoldedRepo(t), "docs/qna"},
		{"v2", newV2Repo(t, ".context"), ".context/qna"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := checkScaffold(tc.root, "v0.6.0")
			if got := findCheck(checks, "layout:manifest"); got == nil || got.Status != OK {
				t.Fatalf("layout:manifest = %+v", got)
			}
			if got := findCheck(checks, "layout:qna"); got == nil || !strings.Contains(got.Detail, tc.qna) {
				t.Fatalf("layout:qna = %+v, want path %s", got, tc.qna)
			}
		})
	}
}

func TestDoctorWarnsOnUnsupportedAndMigratingLayouts(t *testing.T) {
	root := newV2Repo(t, ".context")
	if got := findCheck(checkScaffold(root, "v0.5.99"), "layout:manifest"); got.Status != Warn {
		t.Fatalf("unsupported status = %+v", got)
	}
	setLayoutStatus(t, root, layout.StatusMigrating)
	if got := findCheck(checkScaffold(root, "v0.6.0"), "layout:manifest"); got.Status != Warn {
		t.Fatalf("migrating status = %+v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/doctor -run TestDoctorLayout -v`
Expected: FAIL — the checks do not exist.

- [ ] **Step 3: Implement the checks**

Add two checks in `checkScaffold`, and change the freshness call to
`checkQNAFreshness(repoRoot, l, now)`:

```go
func layoutManifestCheck(l layout.Layout, running string) Check {
	if len(l.Problems) > 0 {
		return Check{Name: "layout:manifest", Status: Fail,
			Detail: "layout manifest is invalid: " + problemsText(l.Problems),
			Remedy: "run `agents layout validate` and fix the named path"}
	}
	if l.Schema == layout.SchemaV1 {
		return Check{Name: "layout:manifest", Status: OK,
			Detail: "no manifest; implicit v1 docs/ layout"}
	}
	if ok, reason := layout.Support(running, l); !ok {
		if reason == "migrating" {
			return Check{Name: "layout:manifest", Status: Warn,
				Detail: "layout migration is in progress",
				Remedy: "run `agents layout migrate --resume --apply`"}
		}
		return Check{Name: "layout:manifest", Status: Warn,
			Detail: fmt.Sprintf("manifest requires agents >= %s; running %s", l.MinReader, running),
			Remedy: "upgrade the agents binary before mutating this repository"}
	}
	return Check{Name: "layout:manifest", Status: OK,
		Detail: fmt.Sprintf("agents.layout/v2 profile=%s store_root=%s", l.Profile, l.StoreRoot)}
}

func layoutStoresCheck(root string, l layout.Layout) Check {
	var missing []string
	for _, role := range layout.Roles() {
		p, _ := layout.Path(l, role)
		info, err := os.Stat(filepath.Join(root, p))
		if err != nil || !info.IsDir() {
			missing = append(missing, role+"="+p)
		}
	}
	if len(missing) > 0 {
		return Check{Name: "layout:stores", Status: Warn,
			Detail: "missing store(s): " + strings.Join(missing, ", "),
			Remedy: "run `agents init`; on a v2 repository it creates manifest-declared stores"}
	}
	return Check{Name: "layout:stores", Status: OK, Detail: "all four roles resolve to directories"}
}
```

`checkQNAFreshness` is today's `checkDocsFreshness` with its directory argument
resolved through `layout.Path(l, layout.RoleQNA)`. Its check name becomes
`layout:qna`, and its `Detail` begins with the resolved repository-relative
path (`docs/qna: ...` or `.context/qna: ...`) so the report names what it read.

- [ ] **Step 4: Run the doctor and root packages**

Run: `go test -count=1 ./internal/doctor ./...`
Expected: PASS. The v1 case must report the same `docs/qna` freshness as before,
under the new check name.

- [ ] **Step 5: Commit**

```bash
git add agents/internal/doctor
git commit -m "feat(doctor): check the manifest and resolve stores by role"
```

---

### Task 6: Ship the read-only `agents layout` commands

**Files:**
- Create: `agents/cmd_layout.go`
- Create: `agents/cmd_layout_test.go`
- Modify: `agents/commands.go`
- Modify: `agents/README.md`
- Modify: `README.md` (generated block)
- Modify: `claude/skills/agents-tool/SKILL.md`

**Interfaces:**
- Consumes: `layout.Resolve`, `layout.Validate`, `layout.Support`,
  `scaffold.DefaultAgentsMD`, `scaffold.V2AgentsMD`.
- Produces: `runLayoutShow`, `runLayoutValidate`, `runLayoutPath`, and their
  version-injectable siblings.

- [ ] **Step 1: Write the failing command tests**

```go
func TestLayoutShowResolvesV1AndV2(t *testing.T) {
	v1 := newRepoWithAgents(t)
	var out bytes.Buffer
	if code := runLayoutShowWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("v1 exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"qna": "docs/qna"`) {
		t.Fatalf("v1 json = %s", out.String())
	}
	v2 := newV2RepoForCmd(t, ".context")
	out.Reset()
	if code := runLayoutShowWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("v2 exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"qna": ".context/qna"`) {
		t.Fatalf("v2 json = %s", out.String())
	}
}

func TestLayoutPathRefusesUnsupported(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, ".context"))
	var out bytes.Buffer
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.5.99"); code != exitcode.Skip {
		t.Fatalf("exit = %d, want Skip; output=%s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("unsupported path must print nothing: %q", out.String())
	}
	out.Reset()
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0"); code != exitcode.OK ||
		strings.TrimSpace(out.String()) != ".context/qna" {
		t.Fatalf("supported path = (%d, %q)", code, out.String())
	}
}

func TestLayoutValidateReportsProblems(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	var out bytes.Buffer
	if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), layout.ProblemPathEscapes) {
		t.Fatalf("json = %s", out.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 . -run 'TestLayoutShow|TestLayoutPath|TestLayoutValidate' -v`
Expected: FAIL — the handlers do not exist.

- [ ] **Step 3: Implement the handlers**

```go
func runLayoutShow(args []string, stdout io.Writer) int {
	return runLayoutShowWithVersion(args, stdout, version)
}

func runLayoutShowWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("layout show", flag.ContinueOnError)
	fs.SetOutput(stdout)
	asJSON := fs.Bool("json", false, "emit the resolved layout as JSON")
	router := fs.Bool("router", false, "print the canonical router and nothing else")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents layout show: unexpected operand")
		}
		return exitcode.Malformed
	}
	root, _, code := layoutRepo(stdout)
	if code != exitcode.OK {
		return code
	}
	l := layout.Resolve(root)
	if len(l.Problems) > 0 {
		printProblems(stdout, l.Problems)
		return exitcode.Advisory
	}
	if *router {
		if l.Schema == layout.SchemaV2 {
			fmt.Fprint(stdout, scaffold.V2AgentsMD)
		} else {
			fmt.Fprint(stdout, scaffold.DefaultAgentsMD)
		}
		return exitcode.OK
	}
	if *asJSON {
		b, _ := json.MarshalIndent(l, "", "  ")
		stdout.Write(append(b, '\n'))
	} else {
		printLayout(stdout, l, running)
	}
	if ok, _ := layout.Support(running, l); !ok {
		return exitcode.Advisory
	}
	return exitcode.OK
}
```

`runLayoutValidate` prints problems, then checks `layout.Support`; it returns 0
for a valid supported layout, 1 for problems or an unsupported layout, and 4
outside a repository with `.agents/`. `runLayoutPath` requires exactly one role
operand, checks `layout.Path`, and returns `Malformed` for a missing/unknown
role and `Skip` for an unsupported layout.

- [ ] **Step 4: Declare the command family in `commands.go`**

Add a `layout` parent with the four leaves. Every flag named in a usage line
must be registered by its handler, and every handler's flags must appear in its
usage line (`TestEveryRegisteredFlagIsDocumented` and
`TestNoUsageLineNamesAFlagThatDoesNotExist` enforce both directions).

- [ ] **Step 5: Regenerate and update the living documents**

```bash
cd agents
go run . help --render=markdown > /tmp/layout-block.md
# Replace the block between the BEGIN/END markers in ../README.md with this output.
```

Add `agents layout`, `agents layout show`, `agents layout validate`, and
`agents layout path` to `claude/skills/agents-tool/SKILL.md`, because they are
`Audience: Agent`. Update the feature list and quickstart in
`agents/README.md`.

- [ ] **Step 6: Run the doc and help gates**

Run: `go test -count=1 -run 'TestReadme|TestHarnessSkillCovers|TestEveryRegisteredFlag|TestNoUsageLine' ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add agents README.md claude/skills/agents-tool/SKILL.md
git commit -m "feat(layout): add read-only show, validate, and path commands"
```

---

### Task 7: Guard `init` and fleet `update` against unsupported layouts

**Files:**
- Modify: `agents/cmd_init.go`
- Modify: `agents/cmd_fleet.go`
- Modify: `agents/cmd_init_test.go`
- Modify: `agents/cmd_fleet_test.go`

**Interfaces:**
- Consumes: `layout.Resolve`, `layout.Support`.
- Produces: `runInitWithVersion`, `runFleetUpdateWithVersion`, and a refusal
  helper shared by both.

- [ ] **Step 1: Write the failing guard tests**

```go
func TestInitRefusesUnsupportedV2WithoutWriting(t *testing.T) {
	root := newV2RepoForCmd(t, ".context")
	t.Chdir(root)
	before := snapshotTree(t, root)
	var out bytes.Buffer
	if code := runInitWithVersion(nil, &out, "v0.5.99"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("init wrote to an unsupported v2 repository")
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("init created a docs/ shell in a v2 repository")
	}
}

func TestFleetUpdateSkipsUnsupportedAndDoesNotRefreshSkill(t *testing.T) {
	root := newV2RepoForCmd(t, ".context")
	skill := filepath.Join(root, ".agents/skills/migrating-fleet-context/SKILL.md")
	before, _ := os.ReadFile(skill)
	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.5.99")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if wireCalled {
		t.Fatal("an unsupported repository must be skipped before wiring")
	}
	after, _ := os.ReadFile(skill)
	if !bytes.Equal(before, after) {
		t.Fatal("an unsupported repository's migration skill was downgraded")
	}
	if !strings.Contains(out.String(), "skip (layout min_reader)") {
		t.Fatalf("skip reason missing: %s", out.String())
	}
}

func TestFleetUpdateStillWiresV1PositiveControl(t *testing.T) {
	root := cleanFleetRepo(t) // existing helper
	saveFleetRegistry(t, registry.Entry{Path: root, Added: time.Unix(1, 0).UTC()})
	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.5.99")
	if code != exitcode.OK || !wireCalled {
		t.Fatalf("v1 control = (%d, wired=%v): %s", code, wireCalled, out.String())
	}
	_ = root
}
```

The fleet tests need the registry pointed at the temp repos; reuse the existing
`saveFleetRegistry` helper.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 . -run 'TestInitRefuses|TestFleetUpdateSkips|TestFleetUpdateStill' -v`
Expected: FAIL — `runInitWithVersion` does not exist yet, and the fleet handler
from Task 4 does not yet consult the manifest, so the v2 repository is written
or refreshed.

- [ ] **Step 3: Implement the refusal helper and wire it in**

```go
// layoutRefusal returns a non-empty, human-readable reason when a mutating
// command must not touch this repository.
func layoutRefusal(root, running string) (layout.Layout, string) {
	l := layout.Resolve(root)
	if len(l.Problems) > 0 {
		return l, "manifest invalid: " + problemsText(l.Problems)
	}
	if ok, reason := layout.Support(running, l); !ok {
		return l, reason
	}
	return l, ""
}
```

In `runInitWithVersion`, before `scaffold.Create`:

```go
if _, refusal := layoutRefusal(rc.Root, running); refusal != "" {
	fmt.Fprintf(stdout, "agents init: refusing to write: layout %s\n", refusal)
	return exitcode.Advisory
}
```

In `runFleetUpdateWithVersion`'s apply loop, before `wire`:

```go
if _, refusal := layoutRefusal(e.Path, running); refusal != "" {
	skipped++
	fmt.Fprintf(stdout, "skip (layout %s): %s\n", refusal, fleetPath(e.Path))
	continue
}
```

The dry-run listing reports the same skip lines. `skipped > 0` contributes
`Advisory` to the final exit code.

- [ ] **Step 4: Run the guards and the full suite**

Run: `go test -count=1 ./...`
Expected: PASS, including the v1 positive control.

- [ ] **Step 5: Commit**

```bash
git add agents
git commit -m "fix(layout): guard init and fleet update against unsupported manifests"
```

---

### Task 8: Prove the version floor with version-stamped test binaries

**Files:**
- No production file changes. The fixture lives in a `mktemp -d` directory.
- Durable coverage stays in `agents/internal/layout` and `agents/cmd_*_test.go`,
  where the running version is injected directly.

**Interfaces:**
- Consumes: Tasks 1–7, and the `version` variable in `agents/main.go`.
- Produces: a binary-level positive/negative control. An older manifest-aware
  build refuses to mutate a v2 fixture; v0.6.0 operates on it; v1 stays mutable
  for both. This is the two-version test the design requires, without two
  releases.

- [ ] **Step 1: Run the local verification gate**

```bash
cd agents
go test -count=1 ./...
go test -count=1 -race ./...
go vet ./...
```

Expected: all three pass. `-count=1` is mandatory; the repo's `verify.yml`
carries the reason.

- [ ] **Step 2: Build the two version-stamped binaries**

```bash
cd /Users/nilbot/dotfiles/agents
go build -o /tmp/agents-old -ldflags "-X main.version=v0.5.99" .
go build -o /tmp/agents-new -ldflags "-X main.version=v0.6.0" .
/tmp/agents-old version
/tmp/agents-new version
```

- [ ] **Step 3: Build a disposable v2 fixture**

Use the fixture from the 2026-09-18 probe: a temp git repo with
`.agents/layout.json` (`min_reader: 0.6.0`), the four `.context/<role>/README.md`
files, the v2 router, and a `.agents/skills/migrating-fleet-context/SKILL.md`
containing a unique marker. Redirect `XDG_STATE_HOME` to a temp directory and
commit the fixture before any command runs.

- [ ] **Step 4: Assert the old binary refuses**

```bash
XDG_STATE_HOME="$STATE" /tmp/agents-old layout show --json   # reads v2
XDG_STATE_HOME="$STATE" /tmp/agents-old layout validate      # exit 1: unsupported
XDG_STATE_HOME="$STATE" /tmp/agents-old init                 # refuses, no write
XDG_STATE_HOME="$STATE" /tmp/agents-old update --all --apply # skips, no refresh
git status --porcelain                                       # must be empty
grep -q 'MARKER' .agents/skills/migrating-fleet-context/SKILL.md
```

Expected: the reads succeed, every mutation refuses or skips, the working tree
is unchanged, and the marker survives. This is the negative control.

- [ ] **Step 5: Assert v0.6.0 operates**

```bash
XDG_STATE_HOME="$STATE" /tmp/agents-new layout validate      # exit 0
XDG_STATE_HOME="$STATE" /tmp/agents-new init                 # no-op on intact v2
XDG_STATE_HOME="$STATE" /tmp/agents-new update --all --apply # refreshes skills
```

Expected: `init` leaves the tree unchanged; `update` refreshes the migration
skill and wires the repository.

- [ ] **Step 6: Run the v1 positive control**

```bash
XDG_STATE_HOME="$STATE" /tmp/agents-old init  # in a v1 fixture, twice
XDG_STATE_HOME="$STATE" /tmp/agents-new init  # same fixture
git status --porcelain                        # must be empty after both
```

Expected: v1 is mutable and idempotent for both binaries. This is the control
that keeps the guard from being a blanket refusal.

- [ ] **Step 7: Record the evidence in the PR**

The durable automated gates remain the unit tests in Tasks 2 and 7
(`Support("v0.5.99", v2)` refuses, `Support("v0.6.0", v2)` allows, v1 allows).
The binary-level run is the end-to-end confirmation that the version stamp
reaches the guard.

---

## Part B — write support and migration (same v0.6.0 release)

### Task 9: Layout-aware scaffold, manifest writes, and `init` flags

**Files:**
- Create: `agents/internal/layout/write.go`
- Modify: `agents/internal/scaffold/scaffold.go`
- Modify: `agents/cmd_init.go`
- Modify: `agents/commands.go`
- Create: `agents/internal/scaffold/layout_scaffold_test.go`

**Interfaces:**
- Consumes: `layout.Manifest`, `layout.Layout`, `scaffold.AssetsFS`.
- Produces: `layout.MarshalManifest`, `layout.WriteManifest`,
  `scaffold.CreateWithLayout`.

- [ ] **Step 1: Write the failing scaffold tests**

```go
func TestCreateWithLayoutV2UsesManifestPathsAndLeavesNoDocsShell(t *testing.T) {
	root := t.TempDir()
	l := testLayout(".context")
	if err := CreateWithLayout(root, false, l); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		".context/design/README.md", ".context/plans/README.md",
		".context/journal/README.md", ".context/qna/README.md",
		"AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("v2 scaffold created a docs/ shell")
	}
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(got) != V2AgentsMD {
		t.Fatal("v2 scaffold did not write the v2 router")
	}
}

func TestCreateWithLayoutV2IsIdempotent(t *testing.T) {
	root := t.TempDir()
	l := testLayout(".context")
	if err := CreateWithLayout(root, false, l); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	if err := CreateWithLayout(root, false, l); err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("second scaffold changed the tree")
	}
}

func TestLayoutManifestIsVisibleToLinguist(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	if err := CreateWithLayout(root, false, testLayout(".context")); err != nil {
		t.Fatal(err)
	}
	out := gitOutput(t, root, "check-attr", "linguist-generated", "--", ".agents/layout.json")
	if !strings.Contains(out, "unset") {
		t.Fatalf("layout manifest is still linguist-generated: %s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/scaffold -run 'TestCreateWithLayout|TestLayoutManifest' -v`
Expected: FAIL — `CreateWithLayout` is undefined.

- [ ] **Step 3: Implement atomic manifest writes**

```go
func MarshalManifest(m Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func WriteManifest(root string, m Manifest) error {
	b, err := MarshalManifest(m)
	if err != nil {
		return err
	}
	path := filepath.Join(root, ManifestRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".layout-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
```

- [ ] **Step 4: Implement `CreateWithLayout`**

```go
// Create is the v1-compatible entry point. Existing callers are unchanged.
func Create(root string, local bool) error {
	return CreateWithLayout(root, local, layout.V1ForRoot(root))
}

// CreateWithLayout scaffolds .agents/ plus the stores the resolved layout
// declares. It never creates docs/ for a v2 layout.
func CreateWithLayout(root string, local bool, l layout.Layout) error {
	if local {
		linked, err := repo.IsLinkedWorktree(root)
		if err != nil {
			return err
		}
		if linked {
			return ErrLocalInLinkedWorktree
		}
	}
	agents := filepath.Join(root, ".agents")
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(agents, d), 0o755); err != nil {
			return err
		}
		keep := filepath.Join(agents, d, ".gitkeep")
		if _, err := os.Stat(keep); os.IsNotExist(err) {
			if err := os.WriteFile(keep, nil, 0o644); err != nil {
				return err
			}
		}
	}
	if l.Schema == layout.SchemaV2 {
		for _, role := range layout.Roles() {
			store, ok := layout.Path(l, role)
			if !ok {
				return fmt.Errorf("layout has no %s role", role)
			}
			if err := os.MkdirAll(filepath.Join(root, store), 0o755); err != nil {
				return err
			}
			asset := "assets/docs/" + role + "/README.md"
			if err := writeIfAbsentFromFS(filepath.Join(root, store, "README.md"), AssetsFS, asset); err != nil {
				return err
			}
		}
	} else {
		docsRoot := filepath.Join(root, "docs")
		for _, d := range docsDirs {
			if err := os.MkdirAll(filepath.Join(docsRoot, d), 0o755); err != nil {
				return err
			}
		}
	}
	for _, a := range embeddedAssets {
		if strings.HasPrefix(a.relPath, "docs/") {
			continue // written at the resolved store paths above
		}
		if err := writeIfAbsentFromFS(filepath.Join(root, a.relPath), AssetsFS, a.assetPath); err != nil {
			return err
		}
	}
	router := DefaultAgentsMD
	if l.Schema == layout.SchemaV2 {
		router = V2AgentsMD
	}
	if err := writeIfAbsent(filepath.Join(root, "AGENTS.md"), router); err != nil {
		return err
	}
	if err := linkIfAbsent(filepath.Join(root, "CLAUDE.md"), "AGENTS.md"); err != nil {
		return err
	}
	if err := appendMissingLines(filepath.Join(root, ".gitattributes"), gitattributesLines); err != nil {
		return err
	}
	lines := excludeLines
	if local {
		lines = append(append([]string{}, excludeLines...), "/.agents/")
	}
	exclude, err := repo.InfoExcludePath(root)
	if err != nil {
		return err
	}
	return appendMissingLines(exclude, lines)
}
```

`layout.V1ForRoot(root)` is the exported spelling of `v1Layout(root)`; add it so
`scaffold` does not need an unexported symbol. `gitattributesLines` gains
`.agents/layout.json -linguist-generated` after the existing `.agents/**` line,
so the manifest stays visible in PR review.

- [ ] **Step 5: Add `init` layout flags and manifest creation**

Usage:

```
agents init [--local] [--profile <p>] [--store-root <path>]
            [--content-root <path>] [--stores <role=path>] [--archive <path>]
```

Resolution order:

1. an existing manifest wins; resolve it and refuse if unsupported, invalid, or
   `migrating`;
2. layout flags present and no manifest → construct v2, write the manifest with
   `layout_status: active`, then scaffold;
3. neither → v1 behavior.

If a manifest is absent but a v1 layout already exists (`AGENTS.md` or `docs/`
is present), layout flags are refused with `agents layout migrate` as the
remedy. Adopting an existing repository is the migration command's job,
because it must move the stores and prove the router is boilerplate.

Creation is a mutation: it requires `layout.Support(running, target)` to be
true. An unstamped `make agents` dev build therefore refuses v2 creation; the
operator uses the released v0.6.0 binary, and tests inject `v0.6.0`.

`--local` with a v2 layout, or with `--profile`/`--store-root`, is refused with
the design's Decision 6 reason.

- [ ] **Step 6: Run the scaffold, init, and doc gates**

Run: `go test -count=1 ./internal/scaffold . -run 'TestCreate|TestInit|TestEveryRegisteredFlag' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add agents
git commit -m "feat(scaffold): create stores from the resolved layout"
```

---

### Task 10: Plan a v1→v2 migration without writing

**Files:**
- Create: `agents/internal/layout/migrate.go`
- Create: `agents/internal/layout/migrate_test.go`

**Interfaces:**
- Consumes: `Resolve`, `Validate`, `repo.Git`.
- Produces: `MigrateOptions`, `Plan`, `PlanMigration`.

- [ ] **Step 1: Write the failing planner tests**

```go
func TestPlanMigrationBuildsDirectoryMovesAndRecordsArchive(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/old-plan.md"), "old")
	p, err := PlanMigration(root, MigrateOptions{
		Profile: ProfileContentVault, StoreRoot: ".context",
		ContentRoot: ".", Running: "v0.6.0", RouterState: "clean_current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 0 {
		t.Fatalf("blockers = %v", p.Blockers)
	}
	if len(p.Moves) != 4 || p.Moves[0].From != "docs/design" || p.Moves[0].To != ".context/design" {
		t.Fatalf("moves = %+v", p.Moves)
	}
	if p.Archive != "docs/archive" {
		t.Fatalf("archive = %q", p.Archive)
	}
	for _, m := range p.Moves {
		if m.From == "docs/archive" || m.To == "docs/archive" {
			t.Fatal("the archive must never be a move")
		}
	}
}

func TestPlanMigrationBlocksDriftedRouterAndExistingTarget(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, ".context/design")
	p, err := PlanMigration(root, MigrateOptions{
		Profile: ProfileCodeRepo, StoreRoot: ".context",
		Running: "v0.6.0", RouterState: "drifted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlocker(p.Blockers, "router_not_clean") || !hasBlocker(p.Blockers, "target_exists") {
		t.Fatalf("blockers = %v", p.Blockers)
	}
}

func TestPlanMigrationReportsLinkCandidatesWithoutRewriting(t *testing.T) {
	root := newGitV1Repo(t)
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"),
		"See [the plan](../plans/a-plan.md).\n")
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
	p, err := PlanMigration(root, MigrateOptions{
		Profile: ProfileCodeRepo, StoreRoot: ".context",
		Running: "v0.6.0", RouterState: "clean_current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.LinkCandidates) != 1 {
		t.Fatalf("links = %+v", p.LinkCandidates)
	}
	got := p.LinkCandidates[0]
	if got.Old != "docs/plans/a-plan.md" || got.New != ".context/plans/a-plan.md" {
		t.Fatalf("candidate = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/design/a-design.md")); err != nil {
		t.Fatal("planner must not move or rewrite anything")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run TestPlanMigration -v`
Expected: FAIL — `PlanMigration` is undefined.

- [ ] **Step 3: Implement `PlanMigration`**

```go
func PlanMigration(root string, opts MigrateOptions) (Plan, error) {
	from := Resolve(root)
	if from.Schema != SchemaV1 || len(from.Problems) > 0 {
		return Plan{}, fmt.Errorf("migration source is not a valid v1 layout")
	}
	to := targetLayout(from, opts)
	if ps := Validate(root, to); len(ps) > 0 {
		return Plan{From: from, To: to, Blockers: blockersFromProblems(ps)}, nil
	}
	if ok, reason := Support(opts.Running, to); !ok {
		return Plan{From: from, To: to, Blockers: []Blocker{{
			Code: reason, Detail: "this binary must not write the target layout",
		}}}, nil
	}
	p := Plan{
		Repo: root, DryRun: true, From: from, To: to,
		RouterAction: opts.RouterState + " -> canonical v2",
		Archive:      from.Archive,
	}
	if opts.RouterState != "clean_current" && opts.RouterState != "clean_legacy" {
		p.Blockers = append(p.Blockers, Blocker{Code: "router_not_clean",
			Detail: "run the migrating-fleet-context skill to reconcile the root router first"})
	}
	for _, role := range Roles() {
		src, _ := Path(from, role)
		dst, _ := Path(to, role)
		info, err := os.Lstat(filepath.Join(root, src))
		if err != nil || !info.IsDir() {
			p.Blockers = append(p.Blockers, Blocker{Code: "store_missing", Detail: role + "=" + src})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			p.Blockers = append(p.Blockers, Blocker{Code: "store_symlink", Detail: role + "=" + src})
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, dst)); err == nil {
			p.Blockers = append(p.Blockers, Blocker{Code: "target_exists", Detail: dst})
			continue
		}
		p.Moves = append(p.Moves, Move{Role: role, From: src, To: dst, State: "pending"})
	}
	p.LinkCandidates = scanLinkCandidates(root, p.Moves, from, to)
	p.Counts = Counts{Move: len(p.Moves), Blocked: len(p.Blockers), Links: len(p.LinkCandidates)}
	return p, nil
}

func targetLayout(from Layout, opts MigrateOptions) Layout {
	stores := map[string]string{}
	if opts.StoreRoot != "" {
		for _, role := range Roles() {
			stores[role] = filepath.ToSlash(filepath.Join(opts.StoreRoot, role))
		}
	} else {
		for role, path := range opts.Stores {
			stores[role] = path
		}
	}
	contentRoot := opts.ContentRoot
	if contentRoot == "" {
		contentRoot = "."
	}
	archive := opts.Archive
	if archive == "" {
		archive = from.Archive
	}
	return Layout{Manifest: Manifest{
		Schema: SchemaV2, MinReader: MinReaderV2, Profile: opts.Profile,
		LayoutStatus: StatusActive, StoreRoot: opts.StoreRoot,
		ContentRoot: contentRoot, Archive: archive, Stores: stores,
	}}
}

func blockersFromProblems(ps []Problem) []Blocker {
	out := make([]Blocker, 0, len(ps))
	for _, p := range ps {
		out = append(out, Blocker{Code: p.Code, Detail: strings.TrimSpace(p.Path + " " + p.Detail)})
	}
	return out
}
```

`scanLinkCandidates` walks each source store, skips fenced code blocks, finds
markdown `](target)` links, resolves each target against the file's directory,
and maps a target under an old store prefix to the new prefix. It never writes.

- [ ] **Step 4: Run the planner tests and the package suite**

Run: `go test -count=1 ./internal/layout -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/internal/layout
git commit -m "feat(layout): plan v1 to v2 store moves without writing"
```

---

### Task 11: Apply and resume the migration with `git mv`

**Files:**
- Modify: `agents/internal/layout/migrate.go`
- Modify: `agents/internal/layout/migrate_test.go`

**Interfaces:**
- Consumes: `Plan`, `WriteManifest`, `repo.Git`, `scaffold.V2AgentsMD`.
- Produces: `ApplyMigration(root, p, backupTag) error`,
  `ResumeMigration(root) error`.

- [ ] **Step 1: Write the failing apply/resume tests**

```go
func TestApplyMigrationMovesBlobsAndNeverCopies(t *testing.T) {
	root := newGitV1RepoWithContent(t) // docs/design/a-design.md, docs/plans/a-plan.md
	before := trackedBlobs(t, root, "docs")
	p, err := PlanMigration(root, MigrateOptions{
		Profile: ProfileContentVault, StoreRoot: ".context",
		Running: "v0.6.0", RouterState: "clean_current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err != nil {
		t.Fatal(err)
	}
	after := trackedBlobs(t, root, ".context")
	for rel, blob := range before {
		want := strings.Replace(rel, "docs/", ".context/", 1)
		if after[want] != blob {
			t.Fatalf("blob for %s did not move to %s (got %q, want %q)", rel, want, after[want], blob)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("docs/ shell survived a migration with no archive")
	}
	router, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(router) != scaffold.V2AgentsMD {
		t.Fatal("v2 router was not written")
	}
	if out := gitOutput(t, root, "tag", "--list", "pre-layout-v2-test"); strings.TrimSpace(out) == "" {
		t.Fatal("backup tag was not created")
	}
}

func TestResumeCompletesAJournaledCrash(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, _ := PlanMigration(root, MigrateOptions{
		Profile: ProfileCodeRepo, StoreRoot: ".context",
		Running: "v0.6.0", RouterState: "clean_current",
	})
	// Simulate a crash after the manifest was written and the first move ran.
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{From: "v1", StartedAt: "2026-09-18T00:00:00Z", Moves: p.Moves}
	if err := WriteManifest(root, m); err != nil {
		t.Fatal(err)
	}
	if out, err := repo.Git(root, "mv", p.Moves[0].From, p.Moves[0].To); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	if err := ResumeMigration(root); err != nil {
		t.Fatal(err)
	}
	l := Resolve(root)
	if l.LayoutStatus != StatusActive || len(l.Problems) != 0 {
		t.Fatalf("resumed layout = %+v", l)
	}
}

func TestResumeRefusesAmbiguousAndLostMoves(t *testing.T) {
	// both present, and neither present, each produce an error naming both paths
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run 'TestApplyMigration|TestResume' -v`
Expected: FAIL — apply/resume are undefined.

- [ ] **Step 3: Implement apply**

```go
func ApplyMigration(root string, p Plan, backupTag string) error {
	if len(p.Blockers) > 0 {
		return fmt.Errorf("plan has %d blocker(s)", len(p.Blockers))
	}
	if backupTag == "" {
		return errors.New("--backup-tag is required with --apply")
	}
	if out, err := repo.Git(root, "tag", "-a", backupTag, "-m", "pre-layout-v2 backup"); err != nil {
		return fmt.Errorf("git tag: %v: %s", err, out)
	}
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{
		From: "v1", StartedAt: time.Now().UTC().Format(time.RFC3339),
		Moves: append([]Move(nil), p.Moves...),
	}
	if err := WriteManifest(root, m); err != nil {
		return err
	}
	for i := range m.Migration.Moves {
		move := &m.Migration.Moves[i]
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, move.To)), 0o755); err != nil {
			return err
		}
		if out, err := repo.Git(root, "mv", move.From, move.To); err != nil {
			return fmt.Errorf("git mv %s -> %s: %v: %s", move.From, move.To, err, out)
		}
		move.State = "done"
		if err := WriteManifest(root, m); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(scaffold.V2AgentsMD), 0o644); err != nil {
		return err
	}
	if entries, err := os.ReadDir(filepath.Join(root, "docs")); err == nil && len(entries) == 0 {
		if err := os.Remove(filepath.Join(root, "docs")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	m.LayoutStatus = StatusActive
	m.Migration = nil
	return WriteManifest(root, m)
}
```

`ResumeMigration` reads `Resolve(root)`, requires a `migrating` manifest, and
for each move compares source/destination presence:

| source | destination | action |
|---|---|---|
| present | absent | `git mv` |
| absent | present | mark done |
| present | present | error naming both |
| absent | absent | error naming both |

After all moves it writes the v2 router, removes an empty `docs/`, and writes
the active manifest.

- [ ] **Step 4: Run the migration tests**

Run: `go test -count=1 ./internal/layout -run 'TestApply|TestResume' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/internal/layout
git commit -m "feat(layout): apply and resume migrations with git mv"
```

---

### Task 12: Wire `agents layout migrate` and its output

**Files:**
- Modify: `agents/cmd_layout.go`
- Modify: `agents/cmd_layout_test.go`
- Modify: `agents/commands.go`
- Modify: `agents/README.md`
- Modify: `README.md`
- Modify: `claude/skills/agents-tool/SKILL.md`

**Interfaces:**
- Consumes: `layout.PlanMigration`, `layout.ApplyMigration`,
  `layout.ResumeMigration`, `drift.InspectRepo`.
- Produces: the `agents layout migrate` command with the design §9.2 output and
  exit codes.

- [ ] **Step 1: Write the failing CLI tests**

```go
func TestLayoutMigrateDryRunIsDefaultAndPrintsThePlan(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--profile", "content-vault", "--store-root", ".context", "--dry-run",
	}, &out, "v0.6.0")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	for _, want := range []string{"layout migrate (dry run)", "move    docs/design", "4 move"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".context")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the target")
	}
}

func TestLayoutMigrateApplyRequiresBackupTag(t *testing.T) {
	t.Chdir(newGitV1RepoWithContent(t))
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--profile", "code-repo", "--store-root", ".context", "--apply",
	}, &out, "v0.6.0")
	if code != exitcode.Malformed || !strings.Contains(out.String(), "--backup-tag") {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 . -run TestLayoutMigrate -v`
Expected: FAIL — the handler does not exist.

- [ ] **Step 3: Implement the handler**

Parse the flags from the design's usage line. Resolve the repository, run
`drift.InspectRepo(root, running)` for `RouterState`, require a clean tree and a
non-protected branch, call `layout.PlanMigration`, then:

- `--dry-run` or no apply flag → print the plan, return `Advisory`;
- `--apply` with blockers → print blockers, return `Advisory`;
- `--apply` without `--backup-tag` → print the requirement, return `Malformed`;
- `--apply` → `ApplyMigration`; on error return `NoRecord`; on success print
  the result and return `Advisory` when `len(p.LinkCandidates) > 0`, else `OK`;
- `--resume --apply` → `ResumeMigration`; the same exit rule.

Human output implements the design §9.2 shape; `--json` marshals the `Plan`.

- [ ] **Step 4: Regenerate the docs and run the gates**

```bash
cd agents
go run . help --render=markdown > /tmp/layout-block.md
go test -count=1 -run 'TestReadme|TestHarnessSkillCovers|TestEveryRegisteredFlag|TestNoUsageLine|TestLayoutMigrate' ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agents README.md claude/skills/agents-tool/SKILL.md
git commit -m "feat(layout): add the migration command and plan output"
```

---

### Task 13: Make both bundled skills role-based and add legacy digests

**Files:**
- Modify: `agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md`
- Modify: `agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md`
- Modify: `.agents/skills/recording-what-you-learn/SKILL.md` (byte-identical copy)
- Modify: `.agents/skills/migrating-fleet-context/SKILL.md` (byte-identical copy)
- Modify: `agents/internal/drift/digests.go`
- Modify: `agents/docs_test.go`

**Interfaces:**
- Consumes: `agents layout show|validate|path`, `agents layout migrate`.
- Produces: role-based prose and `clean_legacy` classification for both old
  assets.

- [ ] **Step 1: Write the failing prose and digest tests**

```go
func TestRecordingSkillNamesRolesNotDocsPaths(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(task18RepoRoot(t),
		"agents", "internal", "scaffold", "assets", "skills",
		"recording-what-you-learn", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"agents layout path qna", "agents layout path journal"} {
		if !strings.Contains(text, want) {
			t.Errorf("recording skill does not name %q", want)
		}
	}
	if strings.Contains(text, "docs/") {
		t.Error("recording skill hardcodes a docs/ path")
	}
}

func TestChangedSkillAssetsHaveLegacyDigests(t *testing.T) {
	for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
		current, err := drift.CanonicalSkillDigest(skill)
		if err != nil {
			t.Fatal(err)
		}
		legacy := drift.LegacySkillDigests(skill)
		if len(legacy) == 0 {
			t.Fatalf("%s has no legacy digest; old copies will report customized", skill)
		}
		for _, d := range legacy {
			if d == current {
				t.Fatalf("%s legacy digest equals the current asset", skill)
			}
		}
	}
}
```

Extend `TestMigrationSkillCoversItsSpecifiedProtocol`'s `required` table with:

```go
{"agents layout show", "manifest-first resolution"},
{"agents layout migrate", "the deterministic migration command"},
{"--dry-run", "plan first, apply only after approval"},
{"--apply", "the only mutation path"},
{"--resume", "resumable migration"},
{"min_reader", "below-floor refusal"},
{"layout_status", "migrating vs active"},
{"unsupported", "stop instead of guessing"},
{"archive", "immutable archive handling"},
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 . -run 'TestRecordingSkill|TestChangedSkill|TestMigrationSkillCovers' -v`
Expected: FAIL — the skills still name `docs/` and the new digest entries do
not exist.

- [ ] **Step 3: Rewrite the recording skill in roles**

Replace the store table and the grep example with:

````markdown
The repository's layout manifest is the source of truth. Resolve a role with
the tool, never by assuming a directory name:

```bash
agents layout path qna
agents layout path journal
```

On a repository with no manifest the tool resolves the v1 defaults. If the
binary is absent, read `.agents/layout.json` directly; if that is absent too,
ask before creating anything.
````

The two retrieval axes stay as prose: the Q&A role is indexed by topic, the
journal role by time. Remove every literal `docs/` path.

- [ ] **Step 4: Rewrite the migration skill for manifest-first operation**

Keep the existing structure (staleness, fleet mode, branch isolation, router
state table, traceability, approval gate, commit/PR) and add:

```markdown
## Step 1.5: Read the layout manifest

| manifest state | action |
|---|---|
| absent | v1; use `agents layout migrate --dry-run` to plan |
| active v2, supported | no layout move; reconcile prose and links only |
| `layout_status: migrating` | stop; run `agents layout migrate --resume --apply` |
| unsupported or unknown schema | stop; the binary is older than `min_reader` |
```

Name `agents layout show`, `--dry-run`, `--apply`, `--resume`, and `agents
layout path`; state that the archive is never a move source or destination.
Keep the existing pasted v1 router block for the v1 path; it remains bound to
`scaffold.DefaultAgentsMD` by `TestMigrationSkillPastesTheCanonicalRouter`. The
v2 path restores the router from `agents layout show --router` instead of
pasting a second template.

- [ ] **Step 5: Add the legacy digest texts**

Copy the exact pre-change bytes of both assets into `digests.go` as
`LegacyRecordingSkillV051` and `LegacyMigratingSkillV051`, and return them
from `LegacySkillDigests`:

```go
if skillName == "recording-what-you-learn" {
	return []string{DigestString(LegacyRecordingSkill), DigestString(LegacyRecordingSkillV051)}
}
if skillName == "migrating-fleet-context" {
	return []string{DigestString(LegacyMigratingSkillV051)}
}
return nil
```

Copy the repository's `.agents/skills/...` files from the assets so
`TestMigrationSkillMatchesEmbeddedAsset` passes.

- [ ] **Step 6: Run every prose and digest gate**

Run: `go test -count=1 -run 'TestRecordingSkill|TestChangedSkill|TestMigrationSkill|TestLivingDocuments|TestHarnessSkillCovers|TestReadme' ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add agents .agents/skills claude/skills/agents-tool/SKILL.md
git commit -m "feat(skills): resolve stores by role and keep legacy digests"
```

---

### Task 14: Refresh skills in supported v2 repositories and test the gate

**Files:**
- Modify: `agents/cmd_fleet.go`
- Modify: `agents/cmd_fleet_test.go`

**Interfaces:**
- Consumes: Task 7's `runFleetUpdateWithVersion`.
- Produces: v2-aware fleet refresh for supported repositories; skip for
  unsupported and `migrating` ones.

- [ ] **Step 1: Write the failing supported-v2 test**

```go
func TestFleetUpdateRefreshesSupportedV2AndPreservesTheGate(t *testing.T) {
	root := newV2RepoForCmd(t, ".context")
	deleteSkill(t, root) // force the refresh to have something to write
	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.6.0")
	if code != exitcode.OK || !wireCalled {
		t.Fatalf("supported v2 = (%d, wired=%v): %s", code, wireCalled, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agents/skills/migrating-fleet-context/SKILL.md")); err != nil {
		t.Fatal("supported v2 skill was not refreshed")
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("fleet update created a docs/ shell")
	}
}
```

- [ ] **Step 2: Run it to verify it fails if Task 7 skipped all v2 layouts**

Run: `go test -count=1 . -run TestFleetUpdateRefreshesSupportedV2 -v`
Expected: FAIL for the right reason — the supported v2 repository is treated
as unsupported or the skill is not refreshed.

- [ ] **Step 3: Fix the gate to distinguish unsupported from supported**

The gate is already `layoutRefusal`; the failure mode to guard against is
over-skipping. Assert the refusal reason is empty for a supported v2 layout,
not merely that the schema is v2.

- [ ] **Step 4: Run the fleet tests and the whole suite**

Run: `go test -count=1 ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agents
git commit -m "test(fleet): refresh supported v2 repositories and skip the rest"
```

---

### Task 15: End-to-end fixture, link N-in/N-out, and documentation gates

**Files:**
- Create: `agents/internal/layout/fixture_test.go`
- Modify: `.github/workflows/verify.yml`
- Modify: `docs/design/README.md`
- Modify: `agents/README.md`

**Interfaces:**
- Consumes: every earlier task.
- Produces: one fixture that migrates a repo with nested content, links, and an
  archive, and proves nothing was lost.

- [ ] **Step 1: Write the failing end-to-end fixture test**

```go
func TestMigrationFixtureLinksAndArchiveSurvive(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old.md"), "archived")
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"),
		"See [the plan](../plans/a-plan.md).\n")
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
	archiveBefore := trackedBlobs(t, root)["docs/archive/plans/2020-old.md"]
	linksBefore := countMarkdownLinks(t, root, "docs")

	p, err := PlanMigration(root, MigrateOptions{
		Profile: ProfileContentVault, StoreRoot: ".context",
		Running: "v0.6.0", RouterState: "clean_current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	if err := ApplyMigration(root, p, "pre-layout-v2-fixture"); err != nil {
		t.Fatal(err)
	}
	rewriteLinks(t, root, p.LinkCandidates)

	if got := trackedBlobs(t, root)["docs/archive/plans/2020-old.md"]; got != archiveBefore {
		t.Fatal("archive blob changed or moved")
	}
	if got := countMarkdownLinks(t, root, ".context"); got != linksBefore {
		t.Fatalf("links in = %d, links out = %d", linksBefore, got)
	}
	if bad := unresolvedLinks(t, root, ".context"); len(bad) > 0 {
		t.Fatalf("unresolved links after rewrite: %v", bad)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -count=1 ./internal/layout -run TestMigrationFixture -v`
Expected: FAIL until the helpers and the planner/apply behavior are complete.

- [ ] **Step 3: Add the fixture helpers**

`trackedBlobs` runs `git ls-files -s`, `countMarkdownLinks` counts links outside
fenced code blocks, `rewriteLinks` applies `p.LinkCandidates`' `Old`→`New`
mapping, and `unresolvedLinks` resolves every relative target and reports the
missing ones. They are test helpers, not production code.

- [ ] **Step 4: Add the new tests to the `docs` job in `verify.yml`**

Append the new named tests to the `filter` string in the `docs` job so a
missing test fails CI rather than passing a `-run` filter that matches nothing:

```
TestRecordingSkillNamesRolesNotDocsPaths
TestChangedSkillAssetsHaveLegacyDigests
TestMigrationFixtureLinksAndArchiveSurvive
TestLayoutManifestIsVisibleToLinguist
```

- [ ] **Step 5: Update the design catalog**

The design row is already in `docs/design/README.md` with status **proposed**.
When v0.6.0 ships, update that status to **implemented 2026-xx-xx** and name the
released version.

- [ ] **Step 6: Run every gate**

```bash
cd agents
go test -count=1 ./...
go test -count=1 -race ./...
go vet ./...
cd ..
agents doctor
agents drift --all
```

Expected: the Go gates pass; doctor and drift show only the pre-existing
warnings and drift. No v1 repository changes behavior.

- [ ] **Step 7: Commit**

```bash
git add agents .github/workflows/verify.yml docs/design/README.md agents/README.md
git commit -m "test(layout): cover migration fixtures, links, and prose gates"
```

---

### Task 16: Release v0.6.0, verify the deploy gate, and hand the pilot to the playbook

**Files:**
- Modify: `agents/README.md`, root `README.md`

**Interfaces:**
- Consumes: Tasks 9–15.
- Produces: v0.6.0 installed and verified on every machine, and the go/no-go
  for the paperbubble dry run.

- [ ] **Step 1: Re-run the full local gate**

```bash
cd agents
go test -count=1 ./...
go test -count=1 -race ./...
go vet ./...
cd /Users/nilbot/dotfiles
make agents
agents version
agents doctor
agents drift --all
```

Expected: all pass; doctor and drift match the baseline apart from the new
layout checks.

- [ ] **Step 2: Release `v0.6.0` (human-gated)**

```bash
git tag -a v0.6.0 -m "agents v0.6.0: layout manifests, store roots, migration"
git push origin v0.6.0
```

- [ ] **Step 3: Upgrade every machine and verify the resolved binary**

```bash
brew upgrade agents
agents version                 # expect v0.6.0
command -v agents              # record the resolved path
agents doctor                  # the `binary` check must report ok
agents update --all            # dry run: confirm v1 repos would rewire and refresh
agents drift --all --json      # expect no new v2 repositories yet
```

Repeat on every machine that can run `agents init`, `agents update --all
--apply`, or `agents save` against the fleet. Record each machine, the version,
and the resolved path. `agents doctor`'s `binary` check is the one that proves
the `agents` on `PATH` is the running executable, which is exactly the stale
binary path this gate exists to catch.

Do not run `agents update --all --apply` until the dry run names every
repository and every reason. Do not start the paperbubble migration until every
machine passes. No v2 repository exists yet.

- [ ] **Step 4: Hand off to the playbook**

Open `docs/plans/2026-09-18-layout-v2-migration-playbook.md`, execute Steps 0–4
(read-only preflight and dry run) against paperbubble, and stop at the approval
gate. Do not run `--apply` without the human's explicit approval.

---

## Self-Review

**Spec coverage.** Every numbered requirement maps to a task:

| Spec | Task |
|---|---|
| 1 manifest as the only physical source | 1, 2, 9 |
| 2 roles vs paths | 4, 5, 6, 13 |
| 3 validation rules | 2 |
| 4 v2/v1 routers and digest catalog | 3, 13 |
| 5 CLI, drift, doctor, scaffold, fleet, `layout` | 4, 5, 6, 9, 10, 11, 12, 14 |
| 6 skills | 13 |
| 7 deploy-before-flip gate and version matrix | 2, 7, 8, 16 |
| 8 migration and rollback | 10, 11, 12, and the playbook |
| 9 tests, prose gates, fixtures | 2, 3, 13, 14, 15 |

**Placeholders.** Every code step names a file, a symbol, and either full code
or the exact behavior the symbol must implement. No step says "add error
handling" without naming the error path.

**Type consistency.** `Layout`, `Manifest`, `Problem`, `Move`, `Plan`,
`MigrateOptions`, `CreateWithLayout`, `InspectRepo(root, running)`, and
`CanonicalRouterDigestFor` are defined once in "Locked Interfaces" and used with
those exact names in every task.
