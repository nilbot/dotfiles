# Layout Manifest and Store-Root Freedom Implementation Plan

**Status:** **Approved 2026-09-19 (design §12, gate 1); execution approved
2026-09-19 (gate 2).** Tasks 1–15 and Task 16 Steps 1–2 run task by task
against this plan. Task 16 Step 3 (tag and release) and the migration playbook
wait for gate 3, which is a separate human decision.

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
(plan/apply/resume/abort), layout-selected skill texts (v2 prose at the flat
asset paths and frozen v1 texts kept canonical, design §0.8), and per-layout
digest catalogs for both skills. The non-mutating/mutating split is a code
boundary tested with version-stamped fixtures, not two releases. Drift and
doctor consume the resolved layout instead of joining `docs/`.

**Tech Stack:** Go 1.26+ standard library (`encoding/json`, `os`, `path/filepath`,
`crypto/sha256`), `agents/internal/repo` for Git, Markdown assets embedded with
`embed.FS`, GitHub Actions release workflow.

**Spec:** [docs/design/2026-09-18-layout-manifest-and-store-root-design.md](../design/2026-09-18-layout-manifest-and-store-root-design.md)

**Playbook:** [docs/plans/2026-09-18-layout-v2-migration-playbook.md](2026-09-18-layout-v2-migration-playbook.md)

## Global Constraints

- Schema string is exactly `agents.layout/v2`; the synthesized legacy schema is
  `agents.layout/v1`.
- Manifest path is exactly `.agents/layout.json`.
- `min_mut_ver_floor` written by v0.6.0 is exactly `0.6.0`.
- CLI templates are exactly `code-repo`, `content-vault`, `custom`; they are
  creation inputs and are never persisted in the manifest.
- Roles are exactly `design`, `plans`, `journal`, `qna`; all four are required
  in v2.
- `layout_status` is exactly `active` or `migrating`.
- The single release is `v0.6.0`. There is no intermediate release; a
  simulated older version in tests is `v0.5.99`.
- Exit codes: `OK=0`, `Advisory=1`, `Block=2`, `Malformed=3`, `Skip=4`,
  `NoRecord=5` (`agents/internal/exitcode`).
- No v1 filesystem or mutation behavior changes when `.agents/layout.json` is
  absent. Drift gains additive fields and doctor renames `docs:qna` to
  `layout:qna`; both are named in the design. A v1 repository keeps the frozen
  v1 bundled-skill texts and stays `current` with no copy change (§0.8).
- Every changed embedded asset in v0.6.0 gets a legacy digest entry for its
  exact previous bytes, in the same release. Under §0.8 the canonical text is
  selected by the resolved layout, so "previous bytes" for both skills means
  the frozen v1 texts, which are also the v1 canonical assets.
- The archive is never moved, rewritten, or walked for misplaced documents.
  Immutability is content-blind: "the archive is a place, not a topic:
  everything under it is historical by definition, so nothing under it is ever
  moved, rewritten, or reclassified" (design §3, §0.6). No content classifier
  runs over the archive.
- No migration may copy a file; `git mv` is the only mechanism.
- The migration journal is a frozen plan, never a replay log: `apply` is
  plan-if-absent plus reconcile, `--resume` reconciles without re-planning, and
  both share one reconciliation function (design §0.5).
- Migration phases are exactly `planned` → `moved` → `pruned` → `router`; the
  journal is removed only when `layout_status` becomes `active`. Per-move state
  is exactly `pending` | `moving` | `done`.
- Every move carries a CLI-computed source identity — `files`, `bytes`,
  `digest` — where `digest` is a SHA-256 over the source tree's sorted
  slash-separated relative paths, sizes, and contents. Deliberately not a git
  tree oid: files ignored by `.gitignore` are invisible to git but are still
  moved by `git mv`.
- There is no `--force`. Ambiguity refuses and names the remedy; it never moves
  a tree on a guess. `--abort --apply` exists only for `phase == planned` with
  every move still `pending` and `created_manifest` true.
- Trackedness has exactly three states (design §0.7): **ignored** (a git ignore
  rule matches) refuses mutation, **untracked but not ignored** is allowed and
  carries only a `doctor` advisory, **tracked** is normal. The check is
  `repo.IsIgnored`, built on `git check-ignore -q --no-index`, asked about
  **both** `.agents/layout.json` and `.agents/`; the answer is "ignored" if
  either matches. Both paths are required, measured: a directory rule
  (`.agents/`, `.agents`, `/.agents`) matches the directory, while
  `/.agents/**` matches the manifest path but **not** the directory, so neither
  query alone is sufficient. Hand-reading `.git/info/exclude` survives only as
  remedy text. Mutation fails closed when trackedness cannot be determined.
- `known_legacy` stays non-current for `drift` on both layouts; the per-layout
  catalogs change the skill's remedy, not the currency predicate. A user-owned
  copy whose digest equals **another layout's canonical text** is replaced with
  **this layout's** canonical text — the digest proves it is not a local edit —
  and only `diverged` goes to a three-way merge or stop-and-ask (design §0.2,
  §0.8).
- A v1 repository's bundled skills are the frozen v1 texts and cannot perform a
  layout flip. `agents layout migrate` performs the flip; the migration playbook
  installs the v2 texts after Apply; `agents update --all --apply` refreshes the
  agents-owned skill only once the layout is v2 (design §0.8).
- `.gitignore` and `.agents/layout.json` are not projections of each other in
  either direction: the manifest is tracked repository state, ignore rules are
  machine state, and the only coupling is the one-way V16 check.
- `--archive` semantics are defined here, not only in a usage line: when
  omitted it defaults to the v1 `docs/archive` when that directory exists and
  to empty otherwise; when supplied it goes through the same `validateRel` and
  participates in V12 and in the `archive_not_in_source` planner blocker.
- All Go test invocations use `-count=1`; tests read tracked non-Go files.
- CLI Interface Documentation Invariant: `agents/README.md`, root `README.md`
  generated block, built-in help, `claude/skills/agents-tool/SKILL.md`, and
  affected docs update in the same change set.
- Direct pushes to `master` are forbidden; work lands through a PR that passes
  the `gate` job.
- The paperbubble repository stays frozen until the playbook is approved and
  v0.6.0 is installed and verified on every machine. No task in this plan runs
  against it.

## Documentation Checklist (every CLI change)

For every command, subcommand, flag, or output field added or changed, the
same change set updates:

1. `agents/README.md` — features, quickstart, and CLI reference.
2. root `README.md` — the generated block
   (`agents help --render=markdown`) and any prose that describes the command
   set.
3. `agents/commands.go` — built-in help usage and detail for every new flag,
   including `--template`; the manifest has no `profile` field.
4. `claude/skills/agents-tool/SKILL.md` — every command with
   `Audience: Agent`.
5. `docs/design/README.md` — the design catalog status.
6. affected `docs/qna/` entries. At minimum re-read
   `how-does-two-tier-agent-context-prevent-scaffold-drift.md` and
   `why-does-agents-init-never-update-existing-instructions.md`, and correct
   anything the layout manifest changes.
7. `docs/design/2026-08-29-two-tier-context-and-llm-migration-architecture.md`
   — a pointer amendment saying that the four `docs/` paths are the v1
   default and `.agents/layout.json` is the v2 authority.
8. Re-read every changed prose file after editing for context, logic, and tone
   consistency. The named docs tests in `.github/workflows/verify.yml` are the
   mechanical backstop, not the whole review.
9. Drift and doctor prose in living documents uses the v0.6.0 vocabulary:
   `current`, `known_legacy`, `diverged`, `missing`. The old
   `clean_current`/`clean_legacy`/`drifted`/`customized` names remain only in
   historical records (the 2026-08-29 design, the 2026-09-01 journal) and in
   explicit rename notes.
10. Manifest documentation lists exactly `schema`, `min_mut_ver_floor`,
    `layout_status`, `stores`, `archive`, and `migration`. There is no
    `profile` and no `store_root`. `--template` is documented as a
    creation-time CLI input, never as a manifest field. The `migration` object
    is documented with its inner fields: `from` (`schema`, `stores`,
    `archive`), `started_at`, `backup_tag`, `created_manifest`, `phase`
    (`planned` | `moved` | `pruned` | `router`), and `moves[]` (`role`, `from`,
    `to`, `state`, `files`, `bytes`, `digest`).
11. Release notes have a carrier that exists. The release PR adds
    `.github/release-notes/v0.6.0.md`, and `.github/workflows/release.yml`
    passes it to `softprops/action-gh-release` as
    `body_path: .github/release-notes/${{ steps.version.outputs.version }}.md`,
    with a guard step that fails the release when the file is missing or empty
    (`test -s`). `generate_release_notes: true` is **not** the carrier: it only
    builds a body from commit titles and would omit the deploy-before-flip gate
    and the not-ready repositories. The GitHub Release body is authoritative;
    the `agents/README.md` "Upgrading to v0.6.0" section (deploy-before-flip,
    the four commands/flags, and the measured fleet baseline) is the durable
    repo-side copy, not a substitute. No document may claim a release-notes
    artifact that does not exist.
12. Bundled-skill documentation names the layout-selected canonical text
    (design §0.8): the flat asset path `assets/skills/<skill>/SKILL.md` holds
    the v2 text, each skill also has the frozen v1 text at
    `assets/skills/<skill>/v1/SKILL.md`, and the two prose gates are scoped per
    layout — v2 assets must not hardcode `docs/`; v1 assets must still name
    `docs/qna` and the other v1 paths.

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

	TemplateCodeRepo     = "code-repo"
	TemplateContentVault = "content-vault"
	TemplateCustom       = "custom"

	RoleDesign  = "design"
	RolePlans   = "plans"
	RoleJournal = "journal"
	RoleQNA     = "qna"

	MinMutVerFloorV2 = "0.6.0"
)

type Problem struct {
	Code   string `json:"code"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Per-move state (design §0.5). "moving" is written immediately before
// `git mv`, so a crash inside the rename is identifiable; the filesystem stays
// the truth and every state is recoverable from it.
const (
	MovePending = "pending"
	MoveMoving  = "moving"
	MoveDone    = "done"
)

// Migration phases, each the last stage that completed. The journal is removed
// only when `layout_status` becomes `active`.
const (
	PhasePlanned = "planned" // journal written, nothing moved yet
	PhaseMoved   = "moved"   // every move is done
	PhasePruned  = "pruned"  // emptied source directories removed
	PhaseRouter  = "router"  // v2 router written
)

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

// ResidueError is a named blocker, not a crash: the layout completed but docs/
// retained entries that are neither a v1 source store nor the declared archive.
// The CLI prints one line per entry and returns Advisory.
type ResidueError struct{ Entries []string }

func (e *ResidueError) Error() string

type Manifest struct {
	Schema       string            `json:"schema"`
	MinMutVerFloor    string            `json:"min_mut_ver_floor,omitempty"`
	LayoutStatus string            `json:"layout_status"`
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
func templateDefaults(name string) (map[string]string, bool)

// agents/internal/layout/migrate.go (v0.6.0)
type MigrateOptions struct {
	Template    string
	Stores      map[string]string
	Archive     string
	Running     string
	RouterState string // drift.RouterState as a string; "current" or "known_legacy"
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
	// Phase is the migration phase this report describes: planned for a dry
	// run or a fresh apply; for --resume, the journal phase the run continued
	// from (design §7.2).
	Phase          string          `json:"phase"`
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
func ApplyMigration(root string, p Plan, backupTag string) error // plan-if-absent, then reconcile
func ResumeMigration(root string) error                          // reconcile only; never re-plans
func AbortMigration(root string) error                           // --abort --apply; phase == planned only

// Apply and resume are one reconciliation function run from the recorded
// phase (design §0.5). Internals, not exported API:
//   reconcileMigration(root string, m Manifest) (residue []string, err error)
//   digestTree(root, rel string) (digest string, files int, bytes int64, err error)
//   var reconcileHook func(step string, moveIndex int) error // nil in production;
//       the crash matrix in Task 11 is its only caller

// agents/internal/scaffold (v0.6.0)
func CreateWithLayout(root string, local bool, l layout.Layout) error

// RefreshInfrastructuralSkills writes the migrating-fleet-context text for the
// given schema (layout.SchemaV1 or layout.SchemaV2). It is a byte-identical
// no-op on an unmodified v1 repository, which is what keeps v1 out of the
// release without a fleet-wide `agents update --all --apply` (design §0.8).
func RefreshInfrastructuralSkills(root, layoutVersion string) error

// skillAssets maps schema -> skill name -> embedded asset path. The flat path
// holds the v2 text; the frozen v1 text lives under v1/SKILL.md.
var skillAssets map[string]map[string]string

// SkillAssetPath resolves one embedded skill asset for a schema, so tests and
// the prose gates can read the exact bytes a layout selects.
func SkillAssetPath(schema, skillName string) (string, error)

// agents/internal/drift
func InspectRepo(root string, runningVersion string) (DriftReport, error)
func CanonicalRouterDigestFor(l layout.Layout) string
// CanonicalSkillDigestFor selects the canonical text by resolved layout; the
// legacy catalog stays a single union, so each layout's effective legacy set is
// "every known text except this layout's canonical one" (design §0.8).
// `isCurrent` stays layout-blind — it reads the states the report resolved.
func CanonicalSkillDigestFor(skillName string, l layout.Layout) (string, error)
func LegacySkillDigests(skillName string) []string

// agents/internal/repo (v0.6.0)
// IsIgnored is the trackedness primitive: `git check-ignore -q --no-index`
// (exit 0 ignored, 1 not ignored, anything else an error). --no-index is
// required because git never reports a tracked path as ignored.
func IsIgnored(dir, relPath string) (bool, error)
// IsTracked answers the untracked-but-not-ignored case for the doctor
// advisory (`git ls-files --error-unmatch`).
func IsTracked(dir, relPath string) (bool, error)

// agents/internal/layout (v0.6.0)
// AgentsIgnored is the one question V16 and the mutation pre-flights ask. It
// asks IsIgnored about BOTH .agents/layout.json and .agents/ and reports true
// if either matches. Measured: a directory rule (.agents/, .agents, /.agents)
// matches the directory but /.agents/** matches only the manifest path, so a
// single query would miss a real spelling of "this agents directory is local".
func AgentsIgnored(root string) (bool, error)
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
| `newGitV1Repo(t)` | `layout` | temp git repo on branch `agents-test` (non-protected), `scaffold.Create` v1 layout committed, clean tree |
| `newGitV1RepoWithContent(t)` | `layout` | the above plus `docs/design/a-design.md` and `docs/plans/a-plan.md` |
| `newGitV2Repo(t, storeRoot)` | `layout` | temp git repo with an active v2 manifest, the four stores, and a clean tree |
| `newTempGitRepo(t)` | `repo` | temp git repo with `.agents/` and a clean tree, for the trackedness helpers |
| `ignoreAgents(t, root, pattern)` | `layout`, `repo`, `doctor`, `main` | appends one ignore rule and fails the test on error: a `/`-prefixed pattern goes to `<git-common-dir>/info/exclude`, any other pattern to the tracked `.gitignore` |
| `crashAt(t, step, moveIndex)` | `layout` | arms `reconcileHook` to return a sentinel error once at the named boundary, simulating a crash between two writes |
| `mkdirAll(t, root, rel)`, `writeFile(t, path, body)` | any | fail-fast filesystem helpers |
| `trackedBlobs(t, root, prefix...)` | `layout` | `git ls-files -s` restricted to prefixes: repo-relative path → blob hash |
| `gitOutput(t, root, args...)` | `layout`, `drift`, `scaffold`, `doctor`, `main` | runs Git and returns combined output, failing the test on error |
| `newRepoWithAgents(t)` | `main` | temp git repo with a v1 scaffold |
| `newV2RepoForCmd(t, storeRoot)` | `main` | temp git repo with a v2 manifest, stores, and a clean tree |
| `writeV2Layout(t, root, storeRoot)` | `drift` | writes an active v2 manifest plus the four store directories |
| `layoutRepo(stdout)` | `main` | resolves cwd → repository root with `.agents/`, printing the failure and returning an exit code |
| `testLayout(storeRoot)` | `scaffold` | the four-role `layout.Layout` used by scaffold tests |
| `snapshotTree(t, root)` | `main`, `scaffold` | sorted repository-relative path → mode + bytes, `.git` excluded |
| `findCheck(checks, name)` | `doctor` | finds a named `Check` |
| `newScaffoldedRepo(t)` / `newV2Repo(t, storeRoot)` | `doctor` | fixture repositories for the doctor tests |
| `setLayoutStatus(t, root, status)` | `doctor` | rewrites only `layout_status` in the fixture manifest |
| `skillAssetBytes(t, schema, name)` | `drift`, `main`, `scaffold` | reads one embedded skill asset through `scaffold.SkillAssetPath`, failing the test on error |
| `deleteSkill(t, root)` | `main` | removes the v2 migration skill to give refresh something to write |
| `migratingManifest(t, root, phase, moveState, created)` | `main` | writes a valid `migrating` v2 manifest with a one-move journal in the named phase, for `--abort`/`--resume` CLI tests |
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
	"strings"
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
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}`)
	l := Resolve(root)
	if l.Schema != SchemaV2 {
		t.Fatalf("layout = %+v", l)
	}
	if l.Stores[RoleDesign] != ".context/design" || l.Stores[RoleQNA] != ".context/qna" {
		t.Fatalf("stores = %v", l.Stores)
	}
	if len(l.Problems) != 0 {
		t.Fatalf("problems = %v", l.Problems)
	}
}

func TestResolveRejectsArrayStores(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": ["design", "plans", "journal", "qna"]
}`)
	l := Resolve(root)
	if !hasProblem(l.Problems, ProblemManifestJSON) {
		t.Fatalf("problems = %v, want %s", l.Problems, ProblemManifestJSON)
	}
}

func TestResolveRejectsDuplicateStoreKeys(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
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

func TestManifestJSONHasNoProfileField(t *testing.T) {
	data, err := MarshalManifest(Manifest{
		Schema: SchemaV2, MinMutVerFloor: MinMutVerFloorV2,
		LayoutStatus: StatusActive,
		Stores: map[string]string{
			RoleDesign: "docs/design", RolePlans: "docs/plans",
			RoleJournal: "docs/journal", RoleQNA: "docs/qna",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"profile"`) {
		t.Fatalf("manifest still has a profile field: %s", data)
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
	ProblemArchiveOverlap  = "archive_overlap"
	ProblemStatusUnknown   = "status_unknown"
	ProblemMigration       = "migration_missing"
	ProblemMinMutVerFloor       = "min_mut_ver_floor_invalid"
	ProblemLocalAgents     = "local_agents"
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
	MinMutVerFloor    string          `json:"min_mut_ver_floor"`
	LayoutStatus string          `json:"layout_status"`
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
			MinMutVerFloor:    raw.MinMutVerFloor,
			LayoutStatus: raw.LayoutStatus,
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
	default:
		l.Problems = append(l.Problems, Problem{Code: ProblemManifestJSON, Path: ManifestRel, Detail: "stores must be a JSON object"})
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

### Task 2: Validate every rule, define trackedness, and compare versions

**Files:**
- Modify: `agents/internal/layout/layout.go`
- Modify: `agents/internal/repo/repo.go`
- Create: `agents/internal/repo/ignore_test.go`
- Create: `agents/internal/layout/version.go`
- Create: `agents/internal/layout/validate_test.go`
- Create: `agents/internal/layout/version_test.go`

**Interfaces:**
- Consumes: `Resolve`, `Layout`, `Problem`.
- Produces: `repo.IsIgnored`, `repo.IsTracked`, `layout.AgentsIgnored`,
  `Validate(root, l) []Problem`, `Support(running, l) (bool, string)`, and the
  V1–V16 validation rules from design §5.2. V16 uses the design §0.7
  ignored-rule definition, asked about both `.agents/layout.json` and
  `.agents/`; the removed "or otherwise untracked" half was never implementable
  as a refusal.

- [ ] **Step 1: Write the failing trackedness tests**

```go
// agents/internal/repo/ignore_test.go
//
// Each rule is asked about both paths, because neither query alone covers the
// spellings. Measured with git: `.agents/`, `.agents`, and `/.agents` match the
// directory; `/.agents/**` matches the manifest path but NOT the directory.
func TestIsIgnoredMatchesEverySpellingOnThePathItMatches(t *testing.T) {
	cases := []struct{ name, pattern, file, dir bool }{
		{"local-dir", "/.agents/", true, true},
		{"local-no-slash", "/.agents", true, true},
		{"local-glob-contents", "/.agents/**", true, false},
		{"gitignore-dir", ".agents/", true, true},
		{"gitignore-no-slash", ".agents", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newTempGitRepo(t)
			ignoreAgents(t, root, tc.pattern)
			gotFile, err := IsIgnored(root, ".agents/layout.json")
			if err != nil || gotFile != tc.file {
				t.Fatalf("IsIgnored(manifest) with rule %q = (%v, %v), want %v", tc.pattern, gotFile, err, tc.file)
			}
			gotDir, err := IsIgnored(root, ".agents")
			if err != nil || gotDir != tc.dir {
				t.Fatalf("IsIgnored(dir) with rule %q = (%v, %v), want %v", tc.pattern, gotDir, err, tc.dir)
			}
		})
	}
}

// The union is the question V16 actually asks, so the glob rule — invisible to
// the directory query — must still refuse mutation.
func TestAgentsIgnoredAsksBothPaths(t *testing.T) {
	for _, pattern := range []string{"/.agents/", "/.agents", "/.agents/**", ".agents/", ".agents"} {
		t.Run(pattern, func(t *testing.T) {
			root := newTempGitRepo(t)
			ignoreAgents(t, root, pattern)
			got, err := AgentsIgnored(root)
			if err != nil || !got {
				t.Fatalf("AgentsIgnored with rule %q = (%v, %v), want true", pattern, got, err)
			}
		})
	}
	clean := newTempGitRepo(t)
	if got, err := AgentsIgnored(clean); err != nil || got {
		t.Fatalf("AgentsIgnored with no rule = (%v, %v), want false", got, err)
	}
}

func TestIsIgnoredSeesARuleEvenForATrackedPath(t *testing.T) {
	root := newTempGitRepo(t)
	writeFile(t, filepath.Join(root, ".agents/layout.json"), "{}")
	gitOutput(t, root, "add", "-A")
	if got, err := IsIgnored(root, ".agents/layout.json"); err != nil || got {
		t.Fatalf("a tracked path with no rule must not report ignored: (%v, %v)", got, err)
	}
	// --no-index is load-bearing: git never reports a tracked path as ignored,
	// so without it a repository that adds /.agents/ to info/exclude after the
	// manifest is committed would look safe while every new file under
	// .agents/ silently stops being added.
	ignoreAgents(t, root, "/.agents/")
	if got, err := IsIgnored(root, ".agents/layout.json"); err != nil || !got {
		t.Fatalf("an ignore rule must fire even for a tracked path: (%v, %v)", got, err)
	}
}

func TestTrackednessHelpersOutsideARepository(t *testing.T) {
	if _, err := IsIgnored(t.TempDir(), ".agents/layout.json"); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("IsIgnored outside a repository = %v, want ErrNotARepo", err)
	}
	if _, err := IsTracked(t.TempDir(), ".agents/layout.json"); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("IsTracked outside a repository = %v, want ErrNotARepo", err)
	}
}

func TestIsTrackedDistinguishesUntrackedFromStaged(t *testing.T) {
	root := newTempGitRepo(t)
	writeFile(t, filepath.Join(root, ".agents/layout.json"), "{}")
	if got, err := IsTracked(root, ".agents/layout.json"); err != nil || got {
		t.Fatalf("untracked file reported tracked: (%v, %v)", got, err)
	}
	gitOutput(t, root, "add", "-A")
	if got, err := IsTracked(root, ".agents/layout.json"); err != nil || !got {
		t.Fatalf("staged file reported untracked: (%v, %v)", got, err)
	}
}

// A git failure is an error, never a silent "not ignored"/"not tracked": that
// switch is what makes mutation fail closed (design §0.7). A corrupt index is
// the portable way to produce one.
func TestIsTrackedFailsClosedOnAGitError(t *testing.T) {
	root := newTempGitRepo(t)
	writeFile(t, filepath.Join(root, ".agents/layout.json"), "{}")
	gitOutput(t, root, "add", "-A")
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := IsTracked(root, ".agents/layout.json"); err == nil {
		t.Fatal("a git error must surface as an error, not as a trackedness answer")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/repo ./internal/layout -run 'TestIsIgnored|TestIsTracked|TestTrackedness|TestAgentsIgnored' -v`
Expected: FAIL — `IsIgnored`, `IsTracked`, and `AgentsIgnored` are undefined.

- [ ] **Step 3: Implement `IsIgnored`, `IsTracked`, and `AgentsIgnored`**

```go
var errExitOne = errors.New("git answered no (exit 1)")

// runExit is run plus the exit status, so a caller can tell "git answered no"
// (exit 1) from "git failed" (128 and anything else). Reading both as "no" is
// how an unresolvable question becomes a silent pass.
func runExit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Env = sanitizeEnv(os.Environ())
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return errExitOne
	}
	return err
}

// IsIgnored reports whether a git ignore rule matches relPath. It is the only
// trackedness check in this release (design §0.7): hand-reading info/exclude
// misses /.agents without a slash, /.agents/**, a tracked .gitignore entry, and
// core.excludesFile.
func IsIgnored(dir, relPath string) (bool, error) {
	if _, err := gitPath(dir, "--git-common-dir"); err != nil {
		return false, ErrNotARepo
	}
	switch err := runExit(dir, "check-ignore", "-q", "--no-index", "--", relPath); {
	case err == nil:
		return true, nil
	case errors.Is(err, errExitOne):
		return false, nil
	default:
		return false, err
	}
}

// IsTracked reports whether relPath is in the index. It answers the
// untracked-but-not-ignored window between `layout migrate --apply` and the
// migration commit, which is permitted and only carries a doctor advisory.
func IsTracked(dir, relPath string) (bool, error) {
	if _, err := gitPath(dir, "--git-common-dir"); err != nil {
		return false, ErrNotARepo
	}
	switch err := runExit(dir, "ls-files", "--error-unmatch", "--", relPath); {
	case err == nil:
		return true, nil
	case errors.Is(err, errExitOne):
		return false, nil
	default:
		return false, err
	}
}
```

In `agents/internal/layout/layout.go`, the one question V16 and both mutation
pre-flights ask:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -count=1 ./internal/repo -v`
Expected: PASS.

- [ ] **Step 5: Write the failing validation and version tests**

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
		Schema: SchemaV2, MinMutVerFloor: MinMutVerFloorV2,
		LayoutStatus: StatusActive, Stores: stores,
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
	if ok, reason := Support("v0.5.99", v2); ok || reason != "below_floor" {
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

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run 'TestValidate|TestSupport|TestCompare' -v`
Expected: FAIL — `Validate`, `Support`, and `compareVersions` are undefined.

- [ ] **Step 7: Implement validation**

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
// phases from design §0.5.
func validPhase(phase string) bool {
	switch phase {
	case PhasePlanned, PhaseMoved, PhasePruned, PhaseRouter:
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
```

V16 calls `layout.AgentsIgnored(root)` — the design §0.7 ignored-rule definition,
not a hand-read of `info/exclude`. That helper asks `repo.IsIgnored` about both
`.agents/layout.json` and `.agents/` and treats either match as ignored, because
the two queries cover different spellings (measured: a directory rule matches
the directory, `/.agents/**` matches only the manifest path). A git error other
than "not a repository" is reported as `local_agents`, because mutation must
fail closed when trackedness cannot be determined; outside a repository there is
nothing to commit, so a plain fixture stays valid. `validateRel` returns the
specific path code; there is no content-root special case. The `layout.go` import
block gains `errors` and `agents/internal/repo`.

- [ ] **Step 8: Implement version comparison in `version.go`**

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
	if l.MinMutVerFloor == "" {
		return true, ""
	}
	got, err := parseVersion(running)
	if err != nil {
		return false, "unreleased" // fail closed: a source build cannot prove its release
	}
	want, err := parseVersion(l.MinMutVerFloor)
	if err != nil {
		return false, "invalid"
	}
	if compareValues(got, want) < 0 {
		return false, "below_floor"
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

- [ ] **Step 9: Run the package tests**

Run: `go test -count=1 ./internal/layout -v`
Expected: PASS. Also run `gofmt -l agents/internal/layout` and expect no output.

- [ ] **Step 10: Commit**

```bash
git add agents/internal/layout agents/internal/repo
git commit -m "feat(layout): validate store paths and gate mutations by min_mut_ver_floor"
```

---

### Task 3: Add the v2 router, freeze the v1 skill texts, and author the v2 texts

**Files:**
- Modify: `agents/internal/scaffold/scaffold.go`
- Modify: `agents/internal/drift/digests.go`
- Modify: `agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md` (becomes the v2 text)
- Modify: `agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md` (becomes the v2 text)
- Create: `agents/internal/scaffold/assets/skills/recording-what-you-learn/v1/SKILL.md` (frozen copy of the current bytes)
- Create: `agents/internal/scaffold/assets/skills/migrating-fleet-context/v1/SKILL.md` (frozen copy of the current bytes)
- Modify: `agents/docs_test.go`
- Create: `agents/internal/drift/router_test.go`
- Modify: `agents/internal/scaffold/scaffold_test.go`

**Not modified:** `.agents/skills/**` in this repository. dotfiles is a v1
repository, so its copies stay the frozen v1 texts and remain canonical for it
(design §0.8). Only the embedded assets split.

**Interfaces:**
- Consumes: `layout.Layout`, `scaffold.DefaultAgentsMD`.
- Produces: `scaffold.V2AgentsMD`, `drift.CanonicalRouterDigestFor(l)`,
  `scaffold.skillAssets`, `scaffold.SkillAssetPath`, the frozen v1 asset files,
  the v2 flat texts, and the `LegacyRecordingSkillV051` /
  `LegacyMigratingSkillV051` byte constants.

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
		Schema: layout.SchemaV2, MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus: layout.StatusActive,
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

- ` + "`.agents/layout.json`" + ` — status and one path per role
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
repository carrying the v1 router becomes `known_legacy`.

The same layout-selected shape governs the two bundled skills: Task 4 adds the
`CanonicalSkillDigestFor(skill, l)` selection; this task's Step 5 authors the
two v2 texts and freezes the v1 texts; Task 13 wires the single legacy union
and the scoped gates. `isCurrent` stays layout-blind. This is the
layout-selected canonical-text model of design §0.8.

- [ ] **Step 5: Write the failing v2-prose, frozen-v1, and repo-copy tests**

All three land here, in the same commit as the split, because this is the step
that creates the asymmetry: until the flat asset becomes the v2 text, every
downstream assertion about `CanonicalSkillDigestFor` is green for the wrong
reason (both layouts would resolve to the same bytes).

```go
// The split itself: the flat assets are the v2 texts and v1/SKILL.md is the
// frozen v0.5.1 bytes.
func TestSkillAssetsSplitByLayout(t *testing.T) {
	v1Recording := skillAssetBytes(t, layout.SchemaV1, "recording-what-you-learn")
	v2Recording := skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn")
	v1Migrating := skillAssetBytes(t, layout.SchemaV1, "migrating-fleet-context")
	v2Migrating := skillAssetBytes(t, layout.SchemaV2, "migrating-fleet-context")

	if v1Recording == v2Recording || v1Migrating == v2Migrating {
		t.Fatal("the flat (v2) and v1 assets must differ after the split")
	}
	// The frozen half is byte-identical to the recorded v0.5.1 bytes, so the
	// constants and the files cannot drift apart unnoticed.
	if v1Recording != LegacyRecordingSkillV051 {
		t.Error("the frozen v1 recording text is not the recorded v0.5.1 bytes")
	}
	if v1Migrating != LegacyMigratingSkillV051 {
		t.Error("the frozen v1 migrating text is not the recorded v0.5.1 bytes")
	}
	// The v2 half is the new prose.
	if strings.Contains(v2Recording, "docs/") {
		t.Error("the v2 recording text still hardcodes a docs/ path")
	}
	for _, want := range []string{"agents layout path qna", "agents layout path journal", ".agents/layout.json"} {
		if !strings.Contains(v2Recording, want) {
			t.Errorf("the v2 recording text does not name %q", want)
		}
	}
	for _, want := range []string{
		"agents layout migrate", "--dry-run", "--apply", "--resume", "--abort",
		"--local", "min_mut_ver_floor", "layout_status",
		"the other layout's canonical text", "cannot perform the flip",
	} {
		if !strings.Contains(v2Migrating, want) {
			t.Errorf("the v2 migrating text does not name %q", want)
		}
	}
}

// A v1 repository's copy is the v1 asset, not the flat one: that is what keeps
// this release a no-op for dotfiles and every other v1 repository.
func TestMigrationSkillMatchesEmbeddedAsset(t *testing.T) {
	root := task18RepoRoot(t)
	l := layout.Resolve(root)
	asset, err := scaffold.SkillAssetPath(l.Schema, "migrating-fleet-context")
	if err != nil {
		t.Fatal(err)
	}
	want, err := scaffold.AssetsFS.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf(".agents/skills/migrating-fleet-context/SKILL.md is not the %s asset", l.Schema)
	}
}
```

```go
// The selector itself: both skills resolve under both layouts, to distinct
// paths, and every path is readable from AssetsFS (the v1/ subdirectories are
// embedded recursively by `//go:embed assets/*`).
func TestSkillAssetPathsResolvePerLayout(t *testing.T) {
	for _, schema := range []string{layout.SchemaV1, layout.SchemaV2} {
		for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
			path, err := SkillAssetPath(schema, skill)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := AssetsFS.ReadFile(path); err != nil {
				t.Fatalf("%s/%s: %q: %v", schema, skill, path, err)
			}
		}
	}
	v1, _ := SkillAssetPath(layout.SchemaV1, "recording-what-you-learn")
	v2, _ := SkillAssetPath(layout.SchemaV2, "recording-what-you-learn")
	if v1 == v2 {
		t.Fatal("the v1 and v2 texts must be distinct assets")
	}
}
```

`skillAssetBytes(t, schema, skill)` is the shared helper: it resolves through
`SkillAssetPath` and reads `AssetsFS`.

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/scaffold . -run 'TestSkillAssetsSplit|TestSkillAssetPaths|TestMigrationSkillMatchesEmbeddedAsset' -v`
Expected: FAIL — `SkillAssetPath`, the v1 assets, and the recorded constants do
not exist yet.

- [ ] **Step 7: Freeze the v1 bytes, author the v2 texts, and add the selector**

Freeze first, in this order: `cp` runs **before** any rewrite, so the v1 text is
a byte copy of what v0.5.1 ships. That is the point of design §0.8 — a v1
repository keeps the text that is correct for `docs/` and never becomes
non-current because of this release.

```bash
cd agents/internal/scaffold/assets/skills
for s in recording-what-you-learn migrating-fleet-context; do
  mkdir -p "$s/v1"
  cp "$s/SKILL.md" "$s/v1/SKILL.md"
done
cd /Users/nilbot/dotfiles
git status --porcelain agents/internal/scaffold/assets
```

Record the frozen bytes in `agents/internal/drift/digests.go` as
`LegacyRecordingSkillV051` and `LegacyMigratingSkillV051` (raw string constants
holding the exact text copied above). Task 13 wires them into
`LegacySkillDigests`; here they exist so the split test can pin the files.

Now rewrite the **flat** assets — and only the flat assets — to the v2 texts.

`assets/skills/recording-what-you-learn/SKILL.md`: replace the store table and
the grep example with:

````markdown
The repository's layout manifest is the source of truth. Resolve a role with
the tool, never by assuming a directory name:

```bash
agents layout path qna
agents layout path journal
```

On a repository with no manifest the tool resolves the v1 defaults. If the
binary is absent, read `.agents/layout.json` directly; if that is absent too,
the repository is v1 — follow the v1 recording skill's store names, or ask the
human before creating anything.
````

The v1 fallback is prose, not a path: a v1 repository carries the frozen v1
asset, which names the paths itself, so this text never needs a literal `docs/`.
The two retrieval axes stay as prose: the Q&A role is indexed by topic, the
journal role by time. The skill resolves paths only; it does not encode which
store humans edit or which agents mostly write — that collaboration policy is
prose in `.agents/AGENTS.md`. Remove every literal `docs/` path.

`assets/skills/migrating-fleet-context/SKILL.md`: keep the existing structure
(staleness, fleet mode, branch isolation, router-state table, traceability,
approval gate, commit/PR) and add:

```markdown
## Step 1.5: Read the layout manifest

| manifest state | action |
|---|---|
| absent | v1; use `agents layout migrate --dry-run` to plan |
| active v2, supported | no layout move; reconcile prose and links only |
| `layout_status: migrating` | stop; run `agents layout migrate --resume --apply` |
| unsupported or unknown schema | stop; the binary is older than `min_mut_ver_floor` |
| `.agents/` is ignored (`--local`) | stop; v2 is unavailable here — a manifest would be machine-local |
```

Name `agents layout show`, `--dry-run`, `--apply`, `--resume`, `--abort`, and
`agents layout path`; state that the archive is never a move source or
destination, that `--abort --apply` exists only for a `planned` phase with
nothing moved, and that any later phase resumes rather than restarts.

State the asset rule in the skill's own words, because a reader of a v1 copy
will otherwise look for a layout command that text predates:

```markdown
The v1 copy of this skill cannot perform the flip: it predates `agents layout
migrate`, and a v1 repository's bundled skills are the frozen v1 texts. The CLI
flips the layout; this skill's v2 text is installed afterwards by the migration
playbook (`agents init` on a fresh clone, or `agents update --all --apply` once
the layout is v2).
```

State the digest exit for a copy from the other layout:

```markdown
A user-owned copy whose digest equals **the other layout's canonical text** is
replaced with this layout's canonical text. The digest proves it is not a local
edit, so there is nothing to merge; only `diverged` goes to a three-way merge or
a stop-and-ask.
```

The router-state table uses the v0.6.0 names: `current`, `known_legacy`,
`diverged`, `missing`; for the skill's own embedded-asset states, use the same
names. Actions: agents-owned non-current → refresh; user-owned `known_legacy`
that is this layout's older text → leave on v1, replace on v2; a copy equal to
the other layout's canonical text → replace with this layout's canonical text;
`diverged` → three-way merge if a base is identifiable, otherwise stop and ask;
`missing` → populate this layout's canonical text. When reconciling
`.agents/AGENTS.md`, preserve per-role collaboration policy (which stores humans
edit, which agents mostly write, which are read-only for one side); if it is
missing and cannot be inferred, stop and ask. Keep the existing pasted v1 router
block for the v1 path; it remains bound to `scaffold.DefaultAgentsMD` by
`TestMigrationSkillPastesTheCanonicalRouter`. The v2 path restores the router
from `agents layout show --router` instead of pasting a second template.

Add the selector to `scaffold.go`:

```go
// skillAssets maps schema -> skill name -> embedded asset path. The flat path
// holds the v2 text; v1/SKILL.md holds the frozen v1 text. Both exist for both
// skills: the only mechanical remedy for a non-current bundled skill is a
// fleet-wide `agents update --all --apply`, and the playbook forbids exactly
// that during the pilot window, so freezing one more prose asset is cheaper
// than leaving every v1 repository red for the length of the pilot (design
// §0.8).
var skillAssets = map[string]map[string]string{
	layout.SchemaV1: {
		"recording-what-you-learn":  "assets/skills/recording-what-you-learn/v1/SKILL.md",
		"migrating-fleet-context":   "assets/skills/migrating-fleet-context/v1/SKILL.md",
	},
	layout.SchemaV2: {
		"recording-what-you-learn":  "assets/skills/recording-what-you-learn/SKILL.md",
		"migrating-fleet-context":   "assets/skills/migrating-fleet-context/SKILL.md",
	},
}

// SkillAssetPath resolves one embedded skill asset for a schema.
func SkillAssetPath(schema, skillName string) (string, error) {
	byName, ok := skillAssets[schema]
	if !ok {
		return "", fmt.Errorf("no skill assets for schema %q", schema)
	}
	path, ok := byName[skillName]
	if !ok {
		return "", fmt.Errorf("no asset for skill %q", skillName)
	}
	return path, nil
}
```

`TestMigrationSkillMatchesEmbeddedAsset` is updated in this step (Step 5 shows
the new body): the repository copy is the **v1** asset, so the comparison must
resolve the asset by the repository's layout. Without that, this step would turn
the repository's own test red. `//go:embed assets/*` already embeds the new
`v1/` subdirectories recursively, so `assets.go` is unchanged.

- [ ] **Step 8: Run the tests**

Run: `go test -count=1 ./internal/drift ./internal/scaffold . -run 'TestSkillAssetsSplit|TestSkillAssetPaths|TestMigrationSkillMatchesEmbeddedAsset|TestV2Router|TestV1RouterIsLegacy' -v`
Expected: PASS. Existing `TestInspectCleanCurrentRepo` is the v1 positive
control: it must still report `current`. Confirm
`git status --porcelain .agents/skills` is empty — the split changes embedded
assets only, never this repository's own copies.

- [ ] **Step 9: Commit**

```bash
git add agents/internal/scaffold agents/internal/drift agents/docs_test.go
git commit -m "feat(skills): freeze the v1 texts and author the v2 texts by layout"
```

---

### Task 4: Report layout fields and resolve stores in drift

**Files:**
- Modify: `agents/internal/drift/digests.go`
- Modify: `agents/internal/drift/drift.go`
- Modify: `agents/internal/drift/drift_test.go`
- Modify: `agents/cmd_drift.go`
- Modify: `agents/cmd_fleet.go`
- Modify: `agents/internal/doctor/doctor.go`

**Interfaces:**
- Consumes: `layout.Resolve`, `layout.Support`, `CanonicalRouterDigestFor`,
  `scaffold.SkillAssetPath`.
- Produces: `InspectRepo(root, runningVersion) (DriftReport, error)` with
  `layout_version`, `min_mut_ver_floor`, `layout_status`, `stores`,
  `unsupported`, and `unsupported_detail`, and `CanonicalSkillDigestFor` for the
  layout-selected skill text.

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
	if rep.Unsupported != "below_floor" {
		t.Fatalf("unsupported = %q, want below_floor", rep.Unsupported)
	}
	if isCurrent(rep) {
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

// The canonical text is chosen by the resolved layout (design §0.8). Task 3
// already split the bytes, so this proves the selection path end to end; Task 13
// adds the cross-layout `known_legacy` classification, which needs the legacy
// catalog wired there.
func TestSkillCurrencyUsesTheLayoutSelectedCanonical(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/skills/recording-what-you-learn/SKILL.md"),
		string(skillAssetBytes(t, layout.SchemaV1, "recording-what-you-learn")))
	if rep, _ := InspectRepo(dir, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "current" {
		t.Fatalf("canonical bytes = %q, want current", rep.Skills["recording-what-you-learn"])
	}
	writeFile(t, filepath.Join(dir, ".agents/skills/recording-what-you-learn/SKILL.md"), "# local edit\n")
	if rep, _ := InspectRepo(dir, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "diverged" {
		t.Fatalf("local edit = %q, want diverged", rep.Skills["recording-what-you-learn"])
	}
}
```

`skillAssetBytes(t, schema, name)` reads `scaffold.SkillAssetPath` through
`AssetsFS`; keep it in `drift_test.go`. It is the positive control that keeps
`isCurrent` layout-blind: the predicate sees only the state the selection
produced.

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
	MinMutVerFloor         string            `json:"min_mut_ver_floor,omitempty"`
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
report.MinMutVerFloor = l.MinMutVerFloor
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
		report.UnsupportedDetail = fmt.Sprintf("requires agents >= %s", l.MinMutVerFloor)
	}
}
```

Router comparison uses `CanonicalRouterDigestFor(l)`. Store presence iterates
`l.Stores` and stats `filepath.Join(root, path)`, populating both `DocsStores`
and `Stores`. Misplacement dispatches:

- v1 or unknown schema: keep today's `docs/` walker byte-for-byte;
- v2: walk each declared store; skip the
  archive subtree; classify `-plan.md` outside the `plans` store and
  `-design.md` outside the `design` store.

Skill classification selects its canonical digest by the resolved layout, the
way the router already does (§8.1, §0.8). Only the canonical lookup changes:

```go
canDigest, err := CanonicalSkillDigestFor(skillName, l)
if err == nil && h == canDigest {
	report.Skills[skillName] = string(ComponentCurrent)
} else if IsLegacySkillDigest(skillName, h) {
	report.Skills[skillName] = string(ComponentKnownLegacy)
} else {
	report.Skills[skillName] = string(ComponentDiverged)
}
```

`LegacySkillDigests(skill)` stays one union: it carries the frozen v1 canonical
bytes for a v2 repository and the v2 canonical bytes for a v1 repository, so
each layout's effective legacy set is the union minus its own canonical text.
`CanonicalSkillDigest` is replaced by `CanonicalSkillDigestFor`; update its
callers and tests. A catalog entry never makes a copy current: `known_legacy`
stays non-current for `drift` on both layouts, and the only thing the catalogs
change is the remedy the skill applies (Task 13).

`isCurrent` adds:

```go
if report.Unsupported != "" || report.LayoutStatus == layout.StatusMigrating {
	return false
}
```

The same step renames the state vocabulary in `digests.go` and `drift.go`:
`RouterCleanCurrent` → `RouterCurrent`, `RouterCleanLegacy` →
`RouterKnownLegacy`, `RouterDrifted` → `RouterDiverged`; `ComponentOK` →
`ComponentCurrent`, `ComponentCleanLegacy` → `ComponentKnownLegacy`,
`ComponentCustomized` → `ComponentDiverged`. The JSON values become
`current`, `known_legacy`, `diverged`, and `missing`. Update the doctor
skill-check tests and the drift tests to the new names; `doctor`'s own
`ok`/`warn`/`fail` statuses do not change.

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
- Consumes: `layout.Resolve`, `layout.Support`, `layout.Path`,
  `repo.IsIgnored`, `repo.IsTracked`.
- Produces: `layout:manifest`, `layout:stores`, and `layout:qna` checks, plus
  two advisories that are never failures: `layout:tracked` (a declared store
  matched by an ignore rule) and `layout:committed` (the
  untracked-but-not-ignored manifest window).

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

func TestDoctorAdvisesOnIgnoredStoresAndTheUncommittedManifestWindow(t *testing.T) {
	root := newV2Repo(t, ".context")
	initGitRepo(t, root)
	if got := findCheck(checkScaffold(root, "v0.6.0"), "layout:tracked"); got == nil || got.Status != OK {
		t.Fatalf("layout:tracked on a clean v2 repo = %+v", got)
	}
	// The untracked-but-not-ignored manifest is the normal window between
	// `layout migrate --apply` and the migration commit: advisory, never a fail.
	if got := findCheck(checkScaffold(root, "v0.6.0"), "layout:committed"); got == nil || got.Status != Warn {
		t.Fatalf("uncommitted manifest = %+v", got)
	}
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "layout")
	if got := findCheck(checkScaffold(root, "v0.6.0"), "layout:committed"); got == nil || got.Status != OK {
		t.Fatalf("committed manifest = %+v", got)
	}
	// A store matched by an ignore rule will not travel with a clone.
	ignoreAgents(t, root, ".context/qna/")
	if got := findCheck(checkScaffold(root, "v0.6.0"), "layout:tracked"); got == nil ||
		got.Status != Warn || !strings.Contains(got.Detail, ".context/qna") {
		t.Fatalf("ignored store = %+v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/doctor -run TestDoctorLayout -v`
Expected: FAIL — the checks do not exist.

- [ ] **Step 3: Implement the checks**

Add four checks in `checkScaffold`, and change the freshness call to
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
			Detail: fmt.Sprintf("manifest requires agents >= %s; running %s", l.MinMutVerFloor, running),
			Remedy: "upgrade the agents binary before mutating this repository"}
	}
	return Check{Name: "layout:manifest", Status: OK,
		Detail: fmt.Sprintf("agents.layout/v2 stores=%d", len(l.Stores))}
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

// layoutTrackedCheck is advisory: an ignored store still exists on this machine,
// but its content will not travel with a clone. Design §0.7.
func layoutTrackedCheck(root string, l layout.Layout) Check {
	var ignored []string
	for _, role := range layout.Roles() {
		p, _ := layout.Path(l, role)
		got, err := repo.IsIgnored(root, p)
		if err != nil {
			return Check{Name: "layout:tracked", Status: Warn,
				Detail: fmt.Sprintf("cannot determine whether %s is ignored: %v", p, err),
				Remedy: "run `agents layout validate`; mutation is refused while trackedness is unknown"}
		}
		if got {
			ignored = append(ignored, role+"="+p)
		}
	}
	if len(ignored) > 0 {
		return Check{Name: "layout:tracked", Status: Warn,
			Detail: "store(s) matched by an ignore rule: " + strings.Join(ignored, ", "),
			Remedy: "remove the ignore rule; content in an ignored store will not travel with a clone"}
	}
	return Check{Name: "layout:tracked", Status: OK, Detail: "no declared store is ignored"}
}

// layoutCommittedCheck is advisory: the untracked-but-not-ignored manifest is
// the normal window between `layout migrate --apply` and the migration commit.
// A clone in that window resolves v1, which is a fact to state, not to fail on.
func layoutCommittedCheck(root string, l layout.Layout) Check {
	if l.Schema == layout.SchemaV1 {
		return Check{Name: "layout:committed", Status: OK, Detail: "no manifest to commit"}
	}
	tracked, err := repo.IsTracked(root, layout.ManifestRel)
	if err != nil {
		if errors.Is(err, repo.ErrNotARepo) {
			return Check{Name: "layout:committed", Status: OK, Detail: "not a git repository"}
		}
		return Check{Name: "layout:committed", Status: Warn, Detail: err.Error()}
	}
	if !tracked {
		return Check{Name: "layout:committed", Status: Warn,
			Detail: "manifest is not committed; a clone would fall back to v1",
			Remedy: "commit .agents/layout.json with the migration commit"}
	}
	return Check{Name: "layout:committed", Status: OK, Detail: "manifest is tracked"}
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

Run the Documentation Checklist above for this change set.

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
	if !strings.Contains(out.String(), "skip (layout below_floor)") {
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

// The pre-flight asks both paths too: `/.agents/**` is invisible to a
// directory-only query, and `--local` itself writes the `/.agents/` form.
func TestInitRefusesV2FlagsWhenAgentsIsIgnored(t *testing.T) {
	for _, pattern := range []string{"/.agents/", "/.agents", "/.agents/**", ".agents/"} {
		t.Run(pattern, func(t *testing.T) {
			root := newRepoWithAgents(t)
			t.Chdir(root)
			ignoreAgents(t, root, pattern)
			before := snapshotTree(t, root)
			var out bytes.Buffer
			if code := runInitWithVersion([]string{"--template", "content-vault"}, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("init wrote a v2 layout into a repository whose .agents/ is ignored")
			}
			if !strings.Contains(out.String(), "ignored") {
				t.Fatalf("the refusal must name the ignored .agents/: %s", out.String())
			}
			// v1 is unchanged: `init --local` is the command that creates the
			// ignored .agents/ in the first place, so this pre-flight must not
			// refuse it.
			out.Reset()
			if code := runInitWithVersion([]string{"--local"}, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("--local init = %d, want the v1 trust-step Advisory: %s", code, out.String())
			}
		})
	}
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
// command must not touch this repository. v2Mutation selects the trackedness
// pre-flight from design §0.7: it applies to writes that would create or
// refresh a v2 layout, never to plain v1 or `init --local`, whose whole point
// is the ignored .agents/.
func layoutRefusal(root, running string, v2Mutation bool) (layout.Layout, string) {
	l := layout.Resolve(root)
	if len(l.Problems) > 0 {
		return l, "manifest invalid: " + problemsText(l.Problems)
	}
	if ok, reason := layout.Support(running, l); !ok {
		return l, reason
	}
	if v2Mutation {
		ignored, err := layout.AgentsIgnored(root)
		if err != nil {
			return l, "cannot determine whether .agents/ is ignored: " + err.Error()
		}
		if ignored {
			return l, ".agents/ is ignored, so a manifest here would be machine-local (drop the ignore rule, or run without --local)"
		}
	}
	return l, ""
}
```

In `runInitWithVersion`, before `scaffold.Create`, passing whether this
invocation would write a v2 layout:

```go
if _, refusal := layoutRefusal(rc.Root, running, layoutFlagsPresent); refusal != "" {
	fmt.Fprintf(stdout, "agents init: refusing to write: layout %s\n", refusal)
	return exitcode.Advisory
}
```

In `runFleetUpdateWithVersion`'s apply loop, before `wire` (no v2 write, so the
trackedness pre-flight is off and v1 `--local` repositories keep today's
behavior):

```go
if _, refusal := layoutRefusal(e.Path, running, false); refusal != "" {
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

### Task 8: Prove the version floor with version-stamped probes (intermediate build)

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
`.agents/layout.json` (`min_mut_ver_floor: 0.6.0`), the four `.context/<role>/README.md`
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

This task runs against the intermediate Part A build. Task 16 Step 1 re-runs
the same matrix against the final v0.6.0 build; the probes here only prove the
guard logic before the writer and migration land.

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
- Consumes: `layout.Manifest`, `layout.Layout`, `scaffold.AssetsFS`,
  `scaffold.SkillAssetPath`.
- Produces: `layout.MarshalManifest`, `layout.WriteManifest`,
  `scaffold.CreateWithLayout` with layout-selected skill texts.

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

// init writes both skills, and the text it writes is the one the created layout
// selects (design §0.8): a v1 repository must never receive the v2 prose.
func TestCreateWithLayoutWritesTheLayoutSelectedSkillText(t *testing.T) {
	for _, tc := range []struct {
		name string
		l    layout.Layout
	}{
		{"v1", layout.V1ForRoot(t.TempDir())},
		{"v2", testLayout(".context")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := CreateWithLayout(root, false, tc.l); err != nil {
				t.Fatal(err)
			}
			for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
				asset, err := SkillAssetPath(tc.l.Schema, skill)
				if err != nil {
					t.Fatal(err)
				}
				wantBytes, err := AssetsFS.ReadFile(asset)
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(root, ".agents/skills", skill, "SKILL.md"))
				if err != nil || !bytes.Equal(got, wantBytes) {
					t.Fatalf("%s scaffold wrote the wrong %s text: %v", tc.l.Schema, skill, err)
				}
			}
		})
	}
}

// The recording skill is user-owned: init populates it only when it is absent.
func TestCreateWithLayoutNeverOverwritesAnExistingSkill(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"), "# mine\n")
	if err := CreateWithLayout(root, false, testLayout(".context")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"))
	if string(got) != "# mine\n" {
		t.Fatal("init overwrote a user-owned skill")
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
		if strings.HasPrefix(a.relPath, ".agents/skills/") {
			continue // written at the layout-selected paths below
		}
		if err := writeIfAbsentFromFS(filepath.Join(root, a.relPath), AssetsFS, a.assetPath); err != nil {
			return err
		}
	}
	// Both skills, only when absent. The text is the one this layout selects
	// (design §0.8), and the recording skill is user-owned, so an existing file
	// is never overwritten.
	for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
		asset, err := SkillAssetPath(l.Schema, skill)
		if err != nil {
			return err
		}
		rel := filepath.Join(".agents", "skills", skill, "SKILL.md")
		if err := writeIfAbsentFromFS(filepath.Join(root, rel), AssetsFS, asset); err != nil {
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
agents init [--local] [--template <p>]
            [--stores <role=path>] [--archive <path>]
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

Implement `internal/layout/templates.go`:

```go
func templateDefaults(name string) (map[string]string, bool) {
	switch name {
	case TemplateCodeRepo:
		return map[string]string{
			RoleDesign: "docs/design", RolePlans: "docs/plans",
			RoleJournal: "docs/journal", RoleQNA: "docs/qna",
		}, true
	case TemplateContentVault:
		return map[string]string{
			RoleDesign: ".context/design", RolePlans: ".context/plans",
			RoleJournal: ".context/journal", RoleQNA: ".context/qna",
		}, true
	case TemplateCustom, "":
		return nil, true
	default:
		return nil, false // unknown template: the CLI reports an error
	}
}
```

Start from `templateDefaults(template)`, apply each `--stores role=path` as an
override, and require all four roles. Test every non-nil template's map with
`Validate`; `custom`/no template returns nil and requires all four `--stores`;
an unknown template returns `ok=false` and the CLI rejects it with a named
error rather than silently treating it as `custom`.

`--local` with a v2 layout, or with `--template`/`--stores`, is refused with
the design's Decision 6 reason, and the Task 7 trackedness pre-flight refuses
v2 creation when `.agents/` is ignored (design §0.7). Plain v1 `init` and
`init --local` are unaffected: `--local` is the command that creates the
ignored `.agents/` in the first place.

`--archive <path>` gives the manifest's `archive` field its value. When the flag
is absent the target inherits the v1 archive — `docs/archive` when that
directory exists, empty otherwise — so the recorded value never invents a path.
When it is present the path goes through the same `validateRel` as a store path,
must not equal, contain, or be contained by a store (V12), and may name a
directory that does not exist yet: V12 protects a path, it does not create one.
`--archive` is a v2-only flag, refused on an existing v1 layout with
`agents layout migrate` as the remedy, like `--template` and `--stores`.

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
- Consumes: `Resolve`, `Validate`, `repo.Git`, `repo.IsIgnored`.
- Produces: `MigrateOptions`, `Plan`, `PlanMigration`, `digestTree`, and the
  `docs_residue`, `archive_not_in_source`, and `local_agents` planner blockers.

- [ ] **Step 1: Write the failing planner tests**

```go
func TestPlanMigrationBuildsDirectoryMovesAndRecordsArchive(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/old-plan.md"), "old")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
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
	if p.Moves[0].State != MovePending {
		t.Fatalf("planned move state = %q, want %q", p.Moves[0].State, MovePending)
	}
	if p.Moves[0].Files == 0 || p.Moves[0].Bytes == 0 || p.Moves[0].Digest == "" {
		t.Fatalf("move identity must be recorded at plan time: %+v", p.Moves[0])
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

// A mixed archive needs no special case: immutability is content-blind, so a
// plan file and a design file inside docs/archive/ are planned around, never
// classified and never moved (design §0.6).
func TestPlanMigrationLeavesAMixedArchiveAlone(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old-plan.md"), "old plan\n")
	writeFile(t, filepath.Join(root, "docs/archive/2020-old-design.md"), "old design\n")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 0 {
		t.Fatalf("a content-mixed archive is not a blocker: %v", p.Blockers)
	}
	for _, m := range p.Moves {
		if strings.HasPrefix(m.From, "docs/archive") || strings.HasPrefix(m.To, "docs/archive") {
			t.Fatalf("the archive must never appear in a move: %+v", m)
		}
	}
}

func TestPlanMigrationBlocksDocsResidueAndAnArchiveInsideASource(t *testing.T) {
	residue := newGitV1Repo(t)
	writeFile(t, filepath.Join(residue, "docs/scratch.md"), "not a store\n")
	p, err := PlanMigration(residue, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlocker(p.Blockers, "docs_residue") {
		t.Fatalf("blockers = %v, want docs_residue", p.Blockers)
	}
	named := false
	for _, b := range p.Blockers {
		if b.Code == "docs_residue" && strings.Contains(b.Detail, "docs/scratch.md") {
			named = true
		}
	}
	if !named {
		t.Fatalf("docs_residue must name every offending entry: %v", p.Blockers)
	}

	// V12 validates the target layout only. An archive nested inside a source
	// store would be dragged along by `git mv docs/plans ...` while the manifest
	// kept the old path, so the planner refuses it.
	nested := newGitV1Repo(t)
	mkdirAll(t, nested, "docs/plans/archive")
	writeFile(t, filepath.Join(nested, "docs/plans/archive/old.md"), "old\n")
	p, err = PlanMigration(nested, MigrateOptions{
		Template: TemplateContentVault, Archive: "docs/plans/archive",
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlocker(p.Blockers, "archive_not_in_source") {
		t.Fatalf("blockers = %v, want archive_not_in_source", p.Blockers)
	}
}

func TestPlanMigrationBlocksDriftedRouterAndExistingTarget(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, ".context/design")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "diverged",
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
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
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

// Design §0.4/§7.2: with no template (or `custom`), --stores must supply all
// four roles; the template name itself is never persisted.
func TestPlanMigrationRequiresAllFourRolesWithoutATemplate(t *testing.T) {
	for _, template := range []string{"", TemplateCustom} {
		root := newGitV1Repo(t)
		p, err := PlanMigration(root, MigrateOptions{
			Template: template, Running: "v0.6.0", RouterState: "current",
		})
		if err != nil {
			t.Fatal(err)
		}
		missing := 0
		for _, b := range p.Blockers {
			if b.Code == "role_missing" {
				missing++
			}
		}
		if missing != 4 {
			t.Fatalf("template %q: role_missing blockers = %d, want 4: %v", template, missing, p.Blockers)
		}
	}

	root := newGitV1Repo(t)
	p, err := PlanMigration(root, MigrateOptions{
		Running: "v0.6.0", RouterState: "current",
		Stores: map[string]string{
			RoleDesign: "notes/design", RolePlans: "notes/plans",
			RoleJournal: "notes/journal", RoleQNA: "notes/qna",
		},
	})
	if err != nil || len(p.Blockers) != 0 {
		t.Fatalf("explicit --stores plan = (%+v, %v)", p.Blockers, err)
	}
	targets := map[string]string{}
	for _, m := range p.Moves {
		targets[m.Role] = m.To
	}
	if targets[RoleDesign] != "notes/design" || targets[RoleQNA] != "notes/qna" {
		t.Fatalf("targets did not come from --stores: %+v", targets)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run TestPlanMigration -v`
Expected: FAIL — `PlanMigration` is undefined.

- [ ] **Step 3: Implement `PlanMigration`**

Create `migrate.go` with the `MigrateOptions`, `Move`, `Migration`,
`MigrationFrom`, `Plan`, `LinkCandidate`, `Blocker`, and `Counts` types and the
`Phase*` and `Move*` constants exactly as listed in Locked Interfaces, then the
functions below.

```go
func PlanMigration(root string, opts MigrateOptions) (Plan, error) {
	from := Resolve(root)
	if from.Schema != SchemaV1 || len(from.Problems) > 0 {
		return Plan{}, fmt.Errorf("migration source is not a valid v1 layout")
	}
	to := targetLayout(from, opts)
	if ps := Validate(root, to); len(ps) > 0 {
		return Plan{Phase: PhasePlanned, From: from, To: to, Blockers: blockersFromProblems(ps)}, nil
	}
	if ok, reason := Support(opts.Running, to); !ok {
		return Plan{Phase: PhasePlanned, From: from, To: to, Blockers: []Blocker{{
			Code: reason, Detail: "this binary must not write the target layout",
		}}}, nil
	}
	p := Plan{
		Repo: root, DryRun: true, Phase: PhasePlanned, From: from, To: to,
		RouterAction: opts.RouterState + " -> canonical v2",
		Archive:      to.Archive,
	}
	if opts.RouterState != "current" && opts.RouterState != "known_legacy" {
		p.Blockers = append(p.Blockers, Blocker{Code: "router_not_clean",
			Detail: "run the migrating-fleet-context skill to reconcile the root router first"})
	}
	// Decision 6 and design §0.7: a manifest in an ignored .agents/ is
	// machine-local and a clone would silently fall back to v1. Both paths are
	// asked, because `/.agents/**` is invisible to the directory query alone.
	if ignored, err := AgentsIgnored(root); err != nil {
		p.Blockers = append(p.Blockers, Blocker{Code: "local_agents",
			Detail: "cannot determine whether .agents/ is ignored: " + err.Error()})
	} else if ignored {
		p.Blockers = append(p.Blockers, Blocker{Code: "local_agents",
			Detail: ".agents/ is ignored, so a manifest here would be machine-local; drop the ignore rule first"})
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
		digest, files, size, err := digestTree(root, src)
		if err != nil {
			p.Blockers = append(p.Blockers, Blocker{Code: "store_unreadable", Detail: role + "=" + src + ": " + err.Error()})
			continue
		}
		p.Moves = append(p.Moves, Move{
			Role: role, From: src, To: dst, State: MovePending,
			Files: files, Bytes: size, Digest: digest,
		})
	}
	p.Blockers = append(p.Blockers, residueBlockers(root, to.Archive)...)
	p.Blockers = append(p.Blockers, archiveInSourceBlockers(to.Archive, p.Moves)...)
	p.LinkCandidates = scanLinkCandidates(root, p.Moves, from, to)
	p.Counts = Counts{Move: len(p.Moves), Blocked: len(p.Blockers), Links: len(p.LinkCandidates)}
	return p, nil
}

// digestTree is the source identity recorded at plan time: every entry — files,
// symlinks by target, directories by name — over sorted slash-separated
// relative paths, hashing path, size, and contents. Deliberately not a git tree
// oid: a file ignored by .gitignore is invisible to git but is still carried by
// `git mv`. `files` counts non-directory entries; `bytes` counts regular-file
// bytes.
func digestTree(root, rel string) (string, int, int64, error) {
	type record struct {
		rel    string
		kind   string
		target string
		data   []byte
	}
	var records []record
	base := filepath.Join(root, rel)
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		rec := record{rel: filepath.ToSlash(relPath)}
		switch {
		case d.IsDir():
			rec.kind = "d"
		case d.Type()&os.ModeSymlink != 0:
			rec.kind = "l"
			if rec.target, err = os.Readlink(path); err != nil {
				return err
			}
		default:
			rec.kind = "f"
			if rec.data, err = os.ReadFile(path); err != nil {
				return err
			}
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].rel < records[j].rel })
	h := sha256.New()
	var files int
	var total int64
	for _, rec := range records {
		switch rec.kind {
		case "d":
			fmt.Fprintf(h, "d %s\n", rec.rel)
		case "l":
			fmt.Fprintf(h, "l %s %s\n", rec.rel, rec.target)
			files++
		default:
			fmt.Fprintf(h, "f %s %d\n", rec.rel, len(rec.data))
			h.Write(rec.data)
			files++
			total += int64(len(rec.data))
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), files, total, nil
}

// docsResidue lists entries left in docs/ that are neither a v1 source store nor
// the declared archive. The planner refuses them up front; apply re-checks the
// same list after the moves, because docs/ can gain an entry mid-flight and
// silently leaving a shell behind is the failure this design exists to prevent.
func docsResidue(root, archive string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, role := range Roles() {
		allowed[role] = true
	}
	var out []string
	for _, e := range entries {
		rel := "docs/" + e.Name()
		if allowed[e.Name()] || (archive != "" && rel == strings.TrimSuffix(archive, "/")) {
			continue
		}
		out = append(out, rel)
	}
	return out
}

// residueBlockers names every entry in docs/ that is neither one of the four v1
// source stores nor the declared archive. The migration removes docs/ only when
// it is empty, so without this the anti-shell promise fails silently and the
// operator reads a clean success with docs/ still present.
func residueBlockers(root, archive string) []Blocker {
	var out []Blocker
	for _, rel := range docsResidue(root, archive) {
		out = append(out, Blocker{Code: "docs_residue",
			Detail: rel + " is neither a v1 store nor the declared archive; move it out of docs/, delete it, or declare it, then re-run"})
	}
	return out
}

// archiveInSourceBlockers refuses when the declared or derived archive equals or
// sits inside a move source. V12 validates the target layout only, so without
// this `git mv docs/plans .context/plans` would drag the archive along and leave
// the manifest naming a path that no longer exists.
func archiveInSourceBlockers(archive string, moves []Move) []Blocker {
	if archive == "" {
		return nil
	}
	var out []Blocker
	for _, m := range moves {
		if pathPrefix(m.From, archive) || pathPrefix(archive, m.From) {
			out = append(out, Blocker{Code: "archive_not_in_source",
				Detail: archive + " overlaps the move source " + m.From + "; move the archive out of the store first, or declare a different archive"})
		}
	}
	return out
}

func targetLayout(from Layout, opts MigrateOptions) Layout {
	stores := opts.Stores
	if len(stores) == 0 {
		stores, _ = templateDefaults(opts.Template)
	}
	archive := opts.Archive
	if archive == "" {
		archive = from.Archive
	}
	return Layout{Manifest: Manifest{
		Schema: SchemaV2, MinMutVerFloor: MinMutVerFloorV2,
		LayoutStatus: StatusActive, Archive: archive, Stores: stores,
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

`templateDefaults` is the CLI template table from design §0.4. Because the
template name is never persisted, it has no schema-version compatibility
burden. The CLI rejects `ok=false` as an unknown template. `custom`/no template
returns `(nil, true)` and requires `--stores`; an empty map becomes the
existing `role_missing` blockers.

`migrate.go` imports `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `io/fs`,
`os`, `path/filepath`, `sort`, `strings`, and `agents/internal/repo` for the
code above; `Digest` values are spelled `sha256:<hex>` so a later algorithm
change is visible in the journal rather than silent. Task 11's step adds
`time` and `agents/internal/scaffold` to that list.

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
- Produces: `ApplyMigration(root, p, backupTag) error` (plan-if-absent, then
  reconcile), `ResumeMigration(root) error` (reconcile only, never re-plans),
  `AbortMigration(root) error`, `reconcileMigration`, `MoveError`,
  `ResidueError`, and the test-only `reconcileHook` crash seam.

- [ ] **Step 1: Write the failing apply/resume tests**

```go
func TestApplyMigrationMovesBlobsAndNeverCopies(t *testing.T) {
	root := newGitV1RepoWithContent(t) // docs/design/a-design.md, docs/plans/a-plan.md
	before := trackedBlobs(t, root, "docs")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
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
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	// Simulate a crash after the manifest was written and the first move ran.
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{
		From: MigrationFrom{Schema: p.From.Schema, Stores: p.From.Stores, Archive: p.From.Archive},
		StartedAt: "2026-09-18T00:00:00Z", BackupTag: "pre-layout-v2-test",
		CreatedManifest: true, Phase: PhasePlanned, Moves: p.Moves,
	}
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
	if l.LayoutStatus != StatusActive || l.Migration != nil || len(l.Problems) != 0 {
		t.Fatalf("resumed layout = %+v", l)
	}
}

// Apply is plan-if-absent plus reconcile: the journal exists before the first
// move, and it records everything resume needs — the frozen v1 source, the
// backup tag, and one identity per move (design §0.5).
func TestApplyMigrationFreezesTheJournalBeforeTheFirstMove(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	crashAt(t, "before-move", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	l := Resolve(root)
	if l.LayoutStatus != StatusMigrating || l.Migration == nil {
		t.Fatalf("journal missing after a crash before the first move: %+v", l)
	}
	if l.Migration.Phase != PhasePlanned || !l.Migration.CreatedManifest || l.Migration.BackupTag != "pre-layout-v2-test" {
		t.Fatalf("journal = %+v", l.Migration)
	}
	if l.Migration.From.Schema != SchemaV1 || len(l.Migration.From.Stores) != 4 {
		t.Fatalf("the journal must record the v1 source explicitly: %+v", l.Migration.From)
	}
	for _, m := range l.Migration.Moves {
		if m.State != MovePending || m.Digest == "" || m.Files == 0 || m.Bytes == 0 {
			t.Fatalf("move = %+v, want pending with a recorded identity", m)
		}
	}
}

// The design §10 requirement "crash fixture at every move boundary" is this
// table: every move × every write boundary, every phase boundary, and one
// non-prefix case. The boundary list is derived from the moves, so adding a
// role adds coverage instead of leaving a silent gap. Design §0.5 names three
// instants per move; `after-move` and `before-journal` are the two halves of its
// middle instant, kept separate so the index-reconciliation seam (where `git mv`
// has renamed the tree but the index is not yet updated) is its own row.
func TestResumeCrashMatrix(t *testing.T) {
	steps := []string{"before-move", "after-move", "before-journal", "after-journal"}
	phases := []string{"moved", "pruned", "router"}
	moves := 4
	covered := 0

	run := func(t *testing.T, step string, moveIndex int) {
		t.Helper()
		root := newGitV1RepoWithContent(t)
		p, err := PlanMigration(root, MigrateOptions{
			Template: TemplateContentVault,
			Running: "v0.6.0", RouterState: "current",
		})
		if err != nil || len(p.Blockers) > 0 {
			t.Fatalf("plan = (%+v, %v)", p, err)
		}
		before := trackedBlobs(t, root, "docs")
		crashAt(t, step, moveIndex)
		if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
			t.Fatal("the injected crash must abort apply")
		}
		if err := ResumeMigration(root); err != nil {
			t.Fatalf("resume after %s/%d: %v", step, moveIndex, err)
		}
		l := Resolve(root)
		if l.LayoutStatus != StatusActive || l.Migration != nil || len(l.Problems) != 0 {
			t.Fatalf("resumed layout = %+v", l)
		}
		after := trackedBlobs(t, root, ".context")
		for rel, blob := range before {
			want := strings.Replace(rel, "docs/", ".context/", 1)
			if after[want] != blob {
				t.Fatalf("blob for %s did not land at %s", rel, want)
			}
		}
	}

	for i := 0; i < moves; i++ {
		for _, step := range steps {
			covered++
			t.Run(fmt.Sprintf("%s/%d", step, i), func(t *testing.T) { run(t, step, i) })
		}
	}
	for _, phase := range phases {
		covered++
		t.Run("phase-"+phase, func(t *testing.T) { run(t, "phase-"+phase, 0) })
	}
	if covered != moves*len(steps)+len(phases) {
		t.Fatalf("crash matrix covers %d boundaries, want %d", covered, moves*len(steps)+len(phases))
	}
}

// A later move already done while an earlier one is pending proves no "prefix of
// the list" assumption is baked into resume.
func TestResumeHandlesANonPrefixCrash(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	crashAt(t, "before-move", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	if out, err := repo.Git(root, "mv", p.Moves[3].From, p.Moves[3].To); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	if err := ResumeMigration(root); err != nil {
		t.Fatal(err)
	}
	if l := Resolve(root); l.LayoutStatus != StatusActive {
		t.Fatalf("layout = %+v", l)
	}
}

func TestResumeRefusesAmbiguousAndLostMoves(t *testing.T) {
	crashed := func(t *testing.T, step string, moveIndex int) (string, Plan) {
		t.Helper()
		root := newGitV1RepoWithContent(t)
		p, err := PlanMigration(root, MigrateOptions{
			Template: TemplateContentVault,
			Running: "v0.6.0", RouterState: "current",
		})
		if err != nil || len(p.Blockers) > 0 {
			t.Fatalf("plan = (%+v, %v)", p, err)
		}
		crashAt(t, step, moveIndex)
		if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
			t.Fatal("the injected crash must abort apply")
		}
		return root, p
	}

	t.Run("both-present-equal-digests-is-a-copy", func(t *testing.T) {
		root, p := crashed(t, "after-move", 0)
		// Restore the source from HEAD: the same tree now exists twice.
		gitOutput(t, root, "checkout", "HEAD", "--", p.Moves[0].From)
		err := ResumeMigration(root)
		if err == nil {
			t.Fatal("a copy must be refused")
		}
		for _, want := range []string{p.Moves[0].From, p.Moves[0].To, "copy, not a move", "--resume --apply"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("refusal %q does not name %q", err, want)
			}
		}
	})

	t.Run("both-present-different-digests-names-the-mismatch", func(t *testing.T) {
		root, p := crashed(t, "after-move", 0)
		gitOutput(t, root, "checkout", "HEAD", "--", p.Moves[0].From)
		writeFile(t, filepath.Join(root, p.Moves[0].From, "extra.md"), "diverged\n")
		err := ResumeMigration(root)
		if err == nil || !strings.Contains(err.Error(), p.Moves[0].Digest) {
			t.Fatalf("refusal = %v, want the recorded digest named", err)
		}
	})

	t.Run("neither-present-is-lost", func(t *testing.T) {
		root, p := crashed(t, "before-move", 0)
		if err := os.RemoveAll(filepath.Join(root, p.Moves[0].From)); err != nil {
			t.Fatal(err)
		}
		err := ResumeMigration(root)
		if err == nil {
			t.Fatal("a lost source must be refused")
		}
		for _, want := range []string{p.Moves[0].From, p.Moves[0].To, "git restore --source=pre-layout-v2-test", "--resume --apply"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("refusal %q does not name %q", err, want)
			}
		}
	})
}

// The post-move digest verification is reachable in production only when the
// tree changes under the migration; the hook puts a test in exactly that state.
func TestApplyRefusesWhenTheTreeChangesUnderTheMove(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	reconcileHook = func(step string, moveIndex int) error {
		if step == "after-move" && moveIndex == 0 {
			writeFile(t, filepath.Join(root, p.Moves[0].To, "intruder.md"), "changed under the migration\n")
		}
		return nil
	}
	t.Cleanup(func() { reconcileHook = nil })
	err = ApplyMigration(root, p, "pre-layout-v2-test")
	if err == nil || !strings.Contains(err.Error(), "changed under the migration") {
		t.Fatalf("refusal = %v, want the post-move digest mismatch", err)
	}
}

func TestAbortIsLimitedToANothingMovedMigration(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	crashAt(t, "before-move", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	if err := AbortMigration(root); err != nil {
		t.Fatalf("abort with nothing moved = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(root, ManifestRel)); !os.IsNotExist(err) {
		t.Fatal("abort must delete the manifest it created")
	}
	if out := gitOutput(t, root, "tag", "--list", "pre-layout-v2-test"); strings.TrimSpace(out) == "" {
		t.Fatal("abort must leave the backup tag")
	}

	root2 := newGitV1RepoWithContent(t)
	p2, _ := PlanMigration(root2, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	crashAt(t, "after-move", 0)
	if err := ApplyMigration(root2, p2, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	err = AbortMigration(root2)
	if err == nil {
		t.Fatal("abort after a move has landed must refuse")
	}
	for _, want := range []string{"phase", "--resume --apply"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not name %q", err, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/layout -run 'TestApplyMigration|TestResume' -v`
Expected: FAIL — apply/resume are undefined.

- [ ] **Step 3: Implement apply, resume, and abort as one reconciliation engine**

Add the `MoveError` and `ResidueError` types from Locked Interfaces to
`migrate.go` (they are first used here), then the functions below.
`docsResidue`, `digestTree`, and the plan types already exist from Task 10.

```go
// reconcileHook is the crash-matrix seam: nil in production, set by tests to
// fail once between two writes. It exists so the crash boundaries are
// enumerable rather than asserted.
var reconcileHook func(step string, moveIndex int) error

func hook(step string, moveIndex int) error {
	if reconcileHook == nil {
		return nil
	}
	return reconcileHook(step, moveIndex)
}

// MoveError is a per-move refusal: both paths, the reason, and the remedy.
// There is no --force; the operator resolves the filesystem and re-runs.
type MoveError struct {
	Move   Move
	Reason string
	Remedy string
}

func (e *MoveError) Error() string {
	return fmt.Sprintf("%s -> %s: %s\n  remedy: %s", e.Move.From, e.Move.To, e.Reason, e.Remedy)
}

// ResidueError is a named blocker, not a crash: the layout completed but docs/
// retained entries that are neither a v1 source store nor the declared archive.
func (e *ResidueError) Error() string {
	return fmt.Sprintf("docs/ still holds %d entr(y/ies) after the migration: %s",
		len(e.Entries), strings.Join(e.Entries, ", "))
}

// ApplyMigration is plan-if-absent plus reconcile: it creates the backup tag,
// freezes the plan into the journal, and then runs the same reconciliation
// resume runs. There is no second code path to drift out of step.
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
		From: MigrationFrom{
			Schema:  p.From.Schema,
			Stores:  cloneStores(p.From.Stores),
			Archive: p.From.Archive,
		},
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		BackupTag:       backupTag,
		CreatedManifest: true,
		Phase:           PhasePlanned,
		Moves:           append([]Move(nil), p.Moves...),
	}
	if err := WriteManifest(root, m); err != nil {
		return err
	}
	return reconcileMigration(root)
}

// ResumeMigration reconciles an existing journal. It never re-plans: the frozen
// plan in the manifest is the only authority on what moves where.
func ResumeMigration(root string) error {
	l := Resolve(root)
	if l.LayoutStatus != StatusMigrating || l.Migration == nil {
		return errors.New("no migrating layout to resume")
	}
	if len(l.Problems) > 0 {
		return fmt.Errorf("manifest or journal is invalid: %d problem(s); run `agents layout validate`", len(l.Problems))
	}
	return reconcileMigration(root)
}

// AbortMigration deletes a manifest this migration created while nothing has
// moved. Every later phase refuses: there the remedy is --resume --apply, not a
// delete that would strand moved trees. Aborting is a mutation, so the CLI
// applies the same version guard as every other write before calling this; a
// binary below min_mut_ver_floor never reaches it, and the safe-by-construction
// state is what lets the human delete the manifest by hand instead.
func AbortMigration(root string) error {
	l := Resolve(root)
	if l.LayoutStatus != StatusMigrating || l.Migration == nil {
		return errors.New("no migrating layout to abort")
	}
	m := *l.Migration
	if m.Phase != PhasePlanned {
		return fmt.Errorf("abort refused: phase is %s, not %s; run `agents layout migrate --resume --apply`", m.Phase, PhasePlanned)
	}
	if !m.CreatedManifest {
		return errors.New("abort refused: this run did not create the manifest; restore it from the backup tag instead")
	}
	for _, mv := range m.Moves {
		if mv.State != MovePending {
			return fmt.Errorf("abort refused: move %s is %s; run `agents layout migrate --resume --apply`", mv.From, mv.State)
		}
	}
	return os.Remove(filepath.Join(root, ManifestRel))
}

// reconcileMigration is the single mutation engine for apply and resume. Each
// iteration advances exactly one phase, and every step is idempotent, so a crash
// between any two writes is recoverable by running the same function again.
func reconcileMigration(root string) error {
	for {
		l := Resolve(root)
		if l.Migration == nil {
			return errors.New("no migration journal")
		}
		m := l.Manifest
		switch l.Migration.Phase {
		case PhasePlanned:
			if err := runMoves(root, &m); err != nil {
				return err
			}
		case PhaseMoved:
			if err := pruneSources(root, &m); err != nil {
				return err
			}
		case PhasePruned:
			if err := writeV2Router(root); err != nil {
				return err
			}
			m.Migration.Phase = PhaseRouter
			if err := WriteManifest(root, m); err != nil {
				return err
			}
		case PhaseRouter:
			m.LayoutStatus = StatusActive
			m.Migration = nil
			if err := WriteManifest(root, m); err != nil {
				return err
			}
			// The layout is complete; the anti-shell promise may not be. A named
			// blocker with an Advisory exit, never a clean success.
			if residue := docsResidue(root, m.Archive); len(residue) > 0 {
				return &ResidueError{Entries: residue}
			}
			return nil
		default:
			return fmt.Errorf("manifest phase %q is not one of planned|moved|pruned|router", l.Migration.Phase)
		}
	}
}

// runMoves reconciles every move against the filesystem, in journal order but
// independently: a later move that already landed does not imply an earlier one
// did, and no prefix of the list is assumed (design §0.5).
func runMoves(root string, m *Manifest) error {
	for i := range m.Migration.Moves {
		mv := &m.Migration.Moves[i]
		src := filepath.Join(root, mv.From)
		dst := filepath.Join(root, mv.To)
		srcPresent, dstPresent := pathExists(src), pathExists(dst)

		switch {
		case srcPresent && !dstPresent:
			if err := hook("before-move", i); err != nil {
				return err
			}
			mv.State = MoveMoving
			if err := WriteManifest(root, *m); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if out, err := repo.Git(root, "mv", mv.From, mv.To); err != nil {
				return fmt.Errorf("git mv %s -> %s: %v: %s", mv.From, mv.To, err, out)
			}
			if err := hook("after-move", i); err != nil {
				return err
			}
			// The recorded identity is verified on both sides of the move: a
			// rename is atomic on one filesystem, so a mismatch here means the tree
			// changed under the migration and the journal must not record `done`.
			got, _, _, err := digestTree(root, mv.To)
			if err != nil {
				return fmt.Errorf("%s: %w", mv.To, err)
			}
			if got != mv.Digest {
				return &MoveError{Move: *mv, Reason: fmt.Sprintf(
					"after `git mv`, %s has %s but the journal recorded %s: the tree changed under the migration",
					mv.To, got, mv.Digest), Remedy: moveRemedy(m, mv)}
			}
			if err := hook("before-journal", i); err != nil {
				return err
			}
			if err := stageRename(root, mv); err != nil {
				return err
			}
			mv.State = MoveDone
			if err := WriteManifest(root, *m); err != nil {
				return err
			}
			if err := hook("after-journal", i); err != nil {
				return err
			}

		case !srcPresent && dstPresent:
			got, _, _, err := digestTree(root, mv.To)
			if err != nil {
				return fmt.Errorf("%s: %w", mv.To, err)
			}
			if got != mv.Digest {
				return &MoveError{Move: *mv, Reason: fmt.Sprintf(
					"digest mismatch: the journal recorded %s (%d file(s)) but %s has %s",
					mv.Digest, mv.Files, mv.To, got), Remedy: moveRemedy(m, mv)}
			}
			if err := stageRename(root, mv); err != nil {
				return err
			}
			if mv.State != MoveDone {
				mv.State = MoveDone
				if err := WriteManifest(root, *m); err != nil {
					return err
				}
			}

		case srcPresent && dstPresent:
			fromDigest, _, _, err := digestTree(root, mv.From)
			if err != nil {
				return err
			}
			toDigest, _, _, err := digestTree(root, mv.To)
			if err != nil {
				return err
			}
			reason := fmt.Sprintf("both %s and %s exist", mv.From, mv.To)
			if fromDigest == toDigest {
				reason += "; their contents are identical, which is a copy, not a move"
			} else {
				reason += fmt.Sprintf("; their contents differ (%s vs %s)", fromDigest, toDigest)
			}
			return &MoveError{Move: *mv, Reason: reason, Remedy: moveRemedy(m, mv)}

		default:
			return &MoveError{Move: *mv, Reason: fmt.Sprintf("neither %s nor %s exists: the tree was lost or already reconciled", mv.From, mv.To), Remedy: moveRemedy(m, mv)}
		}
	}
	if err := hook("phase-moved", 0); err != nil {
		return err
	}
	m.Migration.Phase = PhaseMoved
	return WriteManifest(root, *m)
}

// stageRename makes the index agree with the working tree for one move. `git mv`
// is itself two operations — a filesystem rename and an index update — so a
// crash between them leaves the tree moved and the index stale. `git add -A`
// over both paths is idempotent and repeatable, and rename detection happens at
// diff/commit time, so the displayed rename and the recorded history are
// unaffected.
func stageRename(root string, mv *Move) error {
	if out, err := repo.Git(root, "add", "-A", "--", mv.From, mv.To); err != nil {
		return fmt.Errorf("git add -A -- %s %s: %v: %s", mv.From, mv.To, err, out)
	}
	return nil
}

// moveRemedy is the per-refusal remedy line. It names which path to remove and
// which command restores the other from the migration's own backup tag.
func moveRemedy(m *Manifest, mv *Move) string {
	tag := m.Migration.BackupTag
	return fmt.Sprintf(
		"keep the tree you want and remove the stray one (`rm -rf %s` or `rm -rf %s`); `git restore --source=%s -- %s` restores the source from the backup tag; then re-run `agents layout migrate --resume --apply`",
		mv.From, mv.To, tag, mv.From)
}

// pruneSources removes directories the moves emptied. ENOENT and ENOTEMPTY are
// not failures: the first is a re-run of this step, the second means the
// directory still holds something, which the residue check reports rather than
// deletes.
func pruneSources(root string, m *Manifest) error {
	if err := hook("phase-pruned", 0); err != nil {
		return err
	}
	for _, mv := range m.Migration.Moves {
		_ = os.Remove(filepath.Join(root, filepath.Dir(mv.From))) // empty directories only
	}
	if entries, err := os.ReadDir(filepath.Join(root, "docs")); err == nil && len(entries) == 0 {
		if err := os.Remove(filepath.Join(root, "docs")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	m.Migration.Phase = PhasePruned
	return WriteManifest(root, *m)
}

// writeV2Router is idempotent: a router already equal to the v2 text is left
// alone, so re-running after a crash writes nothing new.
func writeV2Router(root string) error {
	if err := hook("phase-router", 0); err != nil {
		return err
	}
	path := filepath.Join(root, "AGENTS.md")
	if current, err := os.ReadFile(path); err == nil && string(current) == scaffold.V2AgentsMD {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(scaffold.V2AgentsMD), 0o644)
}

// docsResidue is defined with the planner in Task 10; apply reuses it so the
// pre-flight and the post-move check cannot disagree about what a residue is.

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func cloneStores(stores map[string]string) map[string]string {
	out := make(map[string]string, len(stores))
	for role, path := range stores {
		out[role] = path
	}
	return out
}
```

Resume classification, filesystem first, per move:

| source | destination | meaning | action |
|---|---|---|---|
| present | absent | pending | `git mv`, stage the rename, mark `done` |
| absent | present | done, or copied in | verify `digest`: equal → stage and mark `done`; differ → refuse naming the mismatch |
| present | present | ambiguous | refuse; when both digests are equal, say explicitly "a copy, not a move" |
| absent | absent | lost or already reconciled | refuse; restore the source from the backup tag |

The `state` field is a hint that keeps the common case fast; the filesystem and
the recorded `digest` are the truth. `--abort` is the only delete, and it is
limited to `phase == planned` with every move still `pending` and
`created_manifest` true. There is no `--force`.

- [ ] **Step 4: Run the migration tests**

Run: `go test -count=1 ./internal/layout -run 'TestApply|TestResume|TestAbort' -v`
Expected: PASS. `TestResumeCrashMatrix` names every boundary it covers; the
count assertion fails if a boundary is ever dropped.

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
  `layout.ResumeMigration`, `layout.AbortMigration`, `drift.InspectRepo`,
  `repo.Git`, `repo.InProgress`.
- Produces: the `agents layout migrate` command with the design §9.2 output, the
  `--abort` flag (§0.5), the §9.1 preconditions, and the exit-code table below.

- [ ] **Step 1: Write the failing CLI tests**

```go
func TestLayoutMigrateDryRunIsDefaultAndPrintsThePlan(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--dry-run",
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

// Design §7.2: --json carries the migration phase. A fresh dry run reports the
// phase it would start from (`planned`) even though no journal exists yet.
func TestLayoutMigrateJSONCarriesThePhase(t *testing.T) {
	t.Chdir(newGitV1RepoWithContent(t))
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--dry-run", "--json",
	}, &out, "v0.6.0")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	var payload struct {
		DryRun bool   `json:"dry_run"`
		Phase  string `json:"phase"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DryRun || payload.Phase != layout.PhasePlanned {
		t.Fatalf("payload = %+v, want dry_run=true phase=%q", payload, layout.PhasePlanned)
	}
}

// Design §9.1: a multi-step git operation is checked first, because a merge,
// rebase, cherry-pick, revert, am, or bisect moves or discards HEAD on its own
// and would strand the migration's backup tag. repo.InProgress already names
// all six (agents/internal/repo/repo.go:263); the handler must consume it.
func TestLayoutMigrateRefusesDirtyTreeBranchAndInProgressGit(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)

	writeFile(t, filepath.Join(root, "docs/design/scratch.md"), "dirty\n")
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory || !strings.Contains(out.String(), "not clean") {
		t.Fatalf("dirty tree = (%d, %s)", code, out.String())
	}
	if err := os.Remove(filepath.Join(root, "docs/design/scratch.md")); err != nil {
		t.Fatal(err)
	}

	gitOutput(t, root, "switch", "-c", "main")
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory || !strings.Contains(out.String(), "main") {
		t.Fatalf("protected branch = (%d, %s)", code, out.String())
	}
	gitOutput(t, root, "switch", "agents-test")

	// A real merge, not a marker file, so the guard is exercised against git's
	// own state the way cmd_save_test.go does for repo.InProgress.
	gitOutput(t, root, "switch", "-c", "side")
	writeFile(t, filepath.Join(root, "side.txt"), "side\n")
	gitOutput(t, root, "add", "side.txt")
	gitOutput(t, root, "commit", "-m", "side")
	gitOutput(t, root, "switch", "agents-test")
	writeFile(t, filepath.Join(root, "agents-side.txt"), "agents\n")
	gitOutput(t, root, "add", "agents-side.txt")
	gitOutput(t, root, "commit", "-m", "agents")
	gitOutput(t, root, "merge", "--no-ff", "--no-commit", "side")
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory || !strings.Contains(out.String(), "merge") {
		t.Fatalf("merge in progress = (%d, %s)", code, out.String())
	}
}

func TestLayoutMigrateApplyRequiresBackupTag(t *testing.T) {
	t.Chdir(newGitV1RepoWithContent(t))
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--template", "code-repo", "--apply",
	}, &out, "v0.6.0")
	if code != exitcode.Malformed || !strings.Contains(out.String(), "--backup-tag") {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
}

// The planner names every offending entry, one line each, before anything runs.
func TestLayoutMigrateDryRunNamesResidue(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(root, "docs/scratch.md"), "stray\n")
	t.Chdir(root)
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0")
	if code != exitcode.Advisory || !strings.Contains(out.String(), "docs/scratch.md") {
		t.Fatalf("residue = (%d, %s)", code, out.String())
	}
}

// --abort is the only verb that deletes a manifest, and it succeeds only in the
// state design §0.5 allows: phase planned, every move pending, created_manifest.
func TestLayoutMigrateAbortDeletesAPlannedManifest(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhasePlanned, layout.MovePending, true)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("abort exit = %d, want OK: %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agents/layout.json")); !os.IsNotExist(err) {
		t.Fatal("abort did not delete the manifest")
	}
	if got := gitOutput(t, root, "tag", "--list"); !strings.Contains(got, "pre-layout-v2-test") {
		t.Fatalf("abort must leave the backup tag: %s", got)
	}
}

func TestLayoutMigrateAbortRefusesAfterAMoveAndWithLayoutFlags(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhaseMoved, layout.MoveDone, true)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("abort after a move = %d, want Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--resume --apply") {
		t.Fatalf("the refusal must name the remedy: %s", out.String())
	}
	out.Reset()
	code := runLayoutMigrateWithVersion([]string{"--abort", "--apply", "--template", "content-vault"}, &out, "v0.6.0")
	if code != exitcode.Malformed {
		t.Fatalf("abort with layout flags = %d, want Malformed: %s", code, out.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 . -run TestLayoutMigrate -v`
Expected: FAIL — the handler does not exist.

- [ ] **Step 3: Implement the handler**

Usage:

```
agents layout migrate [--template <p>] [--stores <role=path> ...]
                       [--archive <path>]
                       [--dry-run | --apply | --resume --apply | --abort --apply]
                       [--backup-tag <name>] [--json]
```

Parse the flags from that usage line. Resolve the repository, run
`drift.InspectRepo(root, running)` for `RouterState`, then enforce the three
preconditions of design §9.1 that the plan's own validation cannot see:

- `repo.InProgress` names no operation — this check runs first, because a
  merge, rebase, cherry-pick, revert, am, or bisect moves or discards HEAD on
  its own and would strand the backup tag; refuse naming the operation;
- the tree is clean (`git status --porcelain` empty) — refuse with "working
  tree is not clean";
- the current branch (`git branch --show-current`) is not `master` or `main` —
  refuse naming the branch.

Each refusal exits `Advisory` (1). Then call `layout.PlanMigration` and apply
the table below. `--abort`, `--resume`, and layout flags are mutually
exclusive; `--abort` and `--resume` require `--apply`.

| Invocation | Outcome | Exit |
|---|---|---|
| `--dry-run` or no apply flag | the plan is printed and nothing is written | `Advisory` (1) |
| `--dry-run` or `--apply` | dirty tree, `master`/`main`, or a `repo.InProgress` operation | `Advisory` (1) |
| `--apply` | applied; no blockers, no residue, no link candidates | `OK` (0) |
| `--apply` | plan has blockers, or `docs_residue` appeared after the moves | `Advisory` (1) |
| `--apply` | applied, but link candidates remain for the skill | `Advisory` (1) |
| `--apply` | missing `--backup-tag`, conflicting flags, or malformed input | `Malformed` (3) |
| `--apply` | a `MoveError` or any other mid-flight failure; the manifest stays `migrating` | `NoRecord` (5) |
| `--resume --apply` | the same rules as `--apply` | as above |
| `--abort --apply` | manifest deleted, backup tag left | `OK` (0) |
| `--abort --apply` | refused: any phase but `planned`, a move not `pending`, `created_manifest` false, or no `migrating` manifest | `Advisory` (1) |
| any | not inside a repository with `.agents/` | `Skip` (4) |

This is design §7.2 as amended by §0.5. `--abort --apply` is a mutation and
passes the same version guard as every other write: a binary below
`min_mut_ver_floor` refuses it with `Advisory` (1), and the human deletes the
manifest by hand — safe precisely because the permitted state proves nothing has
moved. A `MoveError` prints its per-move remedy line and leaves the journal in
place; a `ResidueError` prints one line per offending entry and leaves the
layout `active`, because the moves did complete and only the shell promise
failed. Human output implements the design §9.2 shape. `--json` marshals the
`Plan`: a dry run or a fresh `--apply` emits the plan from `PlanMigration`
with `phase: planned`; `--resume --apply` emits the journal's own plan — the
journal phase the resume continued from, the journal's frozen `from`, the
manifest's `to`, and the journal's `moves` — with the per-move
`files`/`bytes`/`digest` unchanged.

- [ ] **Step 4: Regenerate the docs and run the gates**

Run the Documentation Checklist above for this change set.

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

### Task 13: Wire the per-layout skill digest catalogs and run the scoped prose gates

**Files:**
- Modify: `agents/internal/drift/digests.go`
- Modify: `agents/docs_test.go`

**Not modified:** any asset and `.agents/skills/**`. Task 3 authored the v2 flat
texts, froze the v1 texts, and recorded the v0.5.1 byte constants; this task only
wires the catalogs and adds the gates. dotfiles is a v1 repository, so it holds
the frozen v1 texts, and those are its canonical assets: this release changes no
v1 copy, because the only mechanical remedy for a non-current bundled skill is a
fleet-wide `agents update --all --apply` and the playbook forbids exactly that
during the pilot window.

**Interfaces:**
- Consumes: `agents layout show|validate|path`, `agents layout migrate`,
  `scaffold.SkillAssetPath`, `drift.CanonicalSkillDigestFor`, and Task 3's
  `LegacyRecordingSkillV051` / `LegacyMigratingSkillV051`.
- Produces: `known_legacy` classification in both directions, the pinned frozen
  v1 assets, and the layout-scoped prose gates.

- [ ] **Step 1: Write the failing prose, pinning, and digest tests**

```go
// The flat path is the v2 asset; v1/SKILL.md is the frozen v1 asset (Task 3).
func TestRecordingSkillNamesRolesNotDocsPaths(t *testing.T) {
	text := skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn")
	for _, want := range []string{
		"agents layout path qna", "agents layout path journal", ".agents/layout.json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the v2 recording skill does not name %q", want)
		}
	}
	if strings.Contains(text, "docs/") {
		t.Error("the v2 recording skill hardcodes a docs/ path")
	}
}

// The other half of the gate: the v1 assets must keep speaking v1, so a v1
// repository never silently loses the paths its layout uses.
func TestFrozenV1SkillAssetsStillSpeakV1Paths(t *testing.T) {
	for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
		v1 := skillAssetBytes(t, layout.SchemaV1, skill)
		if !strings.Contains(v1, "docs/") {
			t.Errorf("the frozen v1 %s asset no longer names the v1 docs/ paths", skill)
		}
		if strings.Contains(v1, "agents layout path") {
			t.Errorf("the frozen v1 %s asset carries v2-only prose", skill)
		}
	}
}

// The v1 constants are not merely legacy: they are the v1 canonical text, so the
// pin doubles as the freeze. Nobody "improves" a v1 repository by accident.
func TestFrozenV1AssetsEqualTheRecordedV051Bytes(t *testing.T) {
	for _, tc := range []struct{ skill, want string }{
		{"recording-what-you-learn", LegacyRecordingSkillV051},
		{"migrating-fleet-context", LegacyMigratingSkillV051},
	} {
		path, err := scaffold.SkillAssetPath(layout.SchemaV1, tc.skill)
		if err != nil {
			t.Fatal(err)
		}
		data, err := scaffold.AssetsFS.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != tc.want {
			t.Fatalf("the v1 asset for %s no longer equals the recorded v0.5.1 bytes; v1 is frozen", tc.skill)
		}
	}
}

// The canonical text is selected by the resolved layout, so the same bytes are
// current in one layout and known_legacy in the other (design §0.8).
func TestSkillCurrencyIsSelectedByResolvedLayout(t *testing.T) {
	v1Text := skillAssetBytes(t, layout.SchemaV1, "recording-what-you-learn")
	v2Text := skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn")
	if v1Text == v2Text {
		t.Fatal("the v1 and v2 texts must differ after the Task 3 split; the selection is untestable otherwise")
	}
	write := func(root, text string) {
		t.Helper()
		writeFile(t, filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"), text)
	}

	v1 := t.TempDir()
	write(v1, v1Text)
	if rep, _ := InspectRepo(v1, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "current" {
		t.Fatalf("v1 repo + v1 text = %q, want current", rep.Skills["recording-what-you-learn"])
	}
	write(v1, v2Text)
	if rep, _ := InspectRepo(v1, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "known_legacy" {
		t.Fatalf("v1 repo + v2 text = %q, want known_legacy", rep.Skills["recording-what-you-learn"])
	}

	v2 := t.TempDir()
	writeV2Layout(t, v2, ".context")
	write(v2, v1Text)
	if rep, _ := InspectRepo(v2, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "known_legacy" {
		t.Fatalf("v2 repo + v1 text = %q, want known_legacy", rep.Skills["recording-what-you-learn"])
	}
	write(v2, v2Text)
	if rep, _ := InspectRepo(v2, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "current" {
		t.Fatalf("v2 repo + v2 text = %q, want current", rep.Skills["recording-what-you-learn"])
	}
}

// The v2 prose resolves roles through the binary, and on a v1 repository the
// binary answers the v1 paths. This is the positive control that lets one text
// serve both layouts; without it "the v2 skill also works on v1" is an assertion.
func TestV2RecordingSkillFallbackResolvesV1Paths(t *testing.T) {
	t.Chdir(newRepoWithAgents(t))
	for role, want := range map[string]string{"qna": "docs/qna", "journal": "docs/journal"} {
		var out bytes.Buffer
		if code := runLayoutPathWithVersion([]string{role}, &out, "v0.6.0"); code != exitcode.OK ||
			strings.TrimSpace(out.String()) != want {
			t.Fatalf("layout path %s on a v1 fixture = (%d, %q), want %q", role, code, out.String(), want)
		}
	}
}

func TestChangedSkillAssetsHaveLegacyDigests(t *testing.T) {
	for _, schema := range []string{layout.SchemaV1, layout.SchemaV2} {
		other := layout.SchemaV2
		if schema == layout.SchemaV2 {
			other = layout.SchemaV1
		}
		for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
			current, err := drift.CanonicalSkillDigestFor(skill, layout.Layout{Manifest: layout.Manifest{Schema: schema}})
			if err != nil {
				t.Fatal(err)
			}
			otherCanonical, err := drift.CanonicalSkillDigestFor(skill, layout.Layout{Manifest: layout.Manifest{Schema: other}})
			if err != nil {
				t.Fatal(err)
			}
			legacy := drift.LegacySkillDigests(skill)
			if len(legacy) == 0 {
				t.Fatalf("%s/%s has no legacy digest; the other layout's copies would report diverged", schema, skill)
			}
			for _, d := range legacy {
				if d == current {
					t.Fatalf("%s/%s: the legacy catalog contains this layout's canonical text", schema, skill)
				}
			}
			// Each layout's effective legacy set is the union minus its own
			// canonical text, so the other layout's canonical text must be there:
			// a repository that flips layout without the skill reports
			// known_legacy, never diverged.
			found := false
			for _, d := range legacy {
				if d == otherCanonical {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s/%s: the other layout's canonical text is missing from the legacy catalog", schema, skill)
			}
		}
	}
}
```

The repository copy of each bundled skill is the **v1** text, so
`TestMigrationSkillCoversItsSpecifiedProtocol` and
`TestMigrationSkillNamesNoRepoLocalDocuments` now read both assets and scope
their assertions. The existing required table (agents ls, drift --json, doctor,
`clean_current`, …) is asserted against the **v1** text, because every one of
those requirements is true of the frozen v0.5.1 prose and the repository copy is
that text. The layout-era requirements are asserted against the **v2** text:

```go
// v1: the existing table, unchanged, asserted against the v1 asset.
// v2: the layout-era requirements. None of these commands or states exists in
// v0.5.1, so they belong to the text Task 3 authored.
layoutRequired := []struct{ substr, why string }{
	{"agents layout show", "manifest-first resolution"},
	{"agents layout migrate", "the deterministic migration command"},
	{"--dry-run", "plan first, apply only after approval"},
	{"--apply", "the only mutation path"},
	{"--resume", "resumable migration"},
	{"--abort", "the nothing-moved escape from a planned migration"},
	{"min_mut_ver_floor", "below-floor refusal"},
	{"layout_status", "migrating vs active"},
	{"unsupported", "stop instead of guessing"},
	{"archive", "immutable archive handling"},
	{"--local", "v2 is unavailable where .agents/ is ignored"},
	{"the other layout's canonical text", "a copy from another layout is replaced with this layout's canonical text, never merged"},
	{"cannot perform the flip", "the v1 text is frozen; only `agents layout migrate` flips a layout"},
}
```

```go
// The test reads three texts: the repository copy, the v1 asset it must equal,
// and the v2 asset the layout-era requirements live in.
repoCopy, err := os.ReadFile(filepath.Join(task18RepoRoot(t), ".agents", "skills", "migrating-fleet-context", "SKILL.md"))
if err != nil {
	t.Fatal(err)
}
v1 := skillAssetBytes(t, layout.SchemaV1, "migrating-fleet-context")
v2 := skillAssetBytes(t, layout.SchemaV2, "migrating-fleet-context")
if string(repoCopy) != v1 {
	t.Fatal("the repository copy is not the v1 canonical text")
}
// legacyRequired is the existing table, unchanged, asserted against v1.
// layoutRequired holds the layout-era rows above, asserted against v2.
// assertContains(t, text, substr, why) is the existing check, factored out.
```

The v2 text also uses the v0.6.0 state vocabulary, and the test forbids the old
backticked names **there** (the v1 text keeps `clean_current`,
`clean_legacy`, `drifted` — that is its canonical vocabulary and this release
does not touch it):

```go
for _, old := range []string{"`clean_current`", "`clean_legacy`", "`drifted`", "`customized`"} {
	if strings.Contains(v2, old) {
		t.Errorf("the v2 skill still uses the v0.5.1 state name %s", old)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 . -run 'TestRecordingSkill|TestFrozenV1|TestChangedSkill|TestMigrationSkill|TestV2RecordingSkill' -v`
Expected: FAIL — the per-layout digest checks do not exist yet, so a copy from
the other layout still classifies as `diverged` instead of `known_legacy`.

- [ ] **Step 3: Wire the frozen v1 texts into the legacy digest catalog**

Task 3 already recorded the frozen bytes in `digests.go` as
`LegacyRecordingSkillV051` and `LegacyMigratingSkillV051` and authored the v2
texts at the flat paths. This step returns them from `LegacySkillDigests`:

```go
if skillName == "recording-what-you-learn" {
	return []string{DigestString(LegacyRecordingSkill), DigestString(LegacyRecordingSkillV051)}
}
if skillName == "migrating-fleet-context" {
	return []string{DigestString(LegacyMigratingSkillV051)}
}
return nil
```

The flat `assets/skills/<skill>/SKILL.md` paths hold the v2 texts and are the
canonical bytes for a v2 repository; each layout's effective legacy set is the
union minus its own canonical text, which is why a v1 repository carrying the v2
text reports `known_legacy` rather than `diverged` — the state the skill's
replacement rule acts on.

`known_legacy` stays non-current for `drift` in both directions. Nothing in this
step changes `isCurrent`: the catalogs change the skill's remedy, not the
currency predicate.

- [ ] **Step 4: Run every prose and digest gate**

Run: `go test -count=1 -run 'TestRecordingSkill|TestFrozenV1|TestSkillCurrency|TestV2RecordingSkill|TestChangedSkill|TestMigrationSkill|TestLivingDocuments|TestHarnessSkillCovers|TestReadme' ./...`
Expected: PASS. Confirm `git status --porcelain .agents/skills` is empty: this
task must change no file in this repository's own skill directory, because
dotfiles is v1 and its copies are already canonical.

- [ ] **Step 5: Commit**

```bash
git add agents/internal/drift agents/docs_test.go
git commit -m "feat(drift): classify both layouts' skill texts as known legacy"
```

---

### Task 14: Refresh the agents-owned skill by resolved layout and test the gate

**Files:**
- Modify: `agents/internal/scaffold/scaffold.go`
- Modify: `agents/internal/scaffold/scaffold_test.go`
- Modify: `agents/cmd_fleet.go`
- Modify: `agents/cmd_fleet_test.go`

**Interfaces:**
- Consumes: Task 7's `runFleetUpdateWithVersion`, `scaffold.SkillAssetPath`.
- Produces: `scaffold.RefreshInfrastructuralSkills(root, layoutVersion string)`
  — scoped to the agents-owned `migrating-fleet-context` only, and writing only
  when the bytes differ — and v2-aware fleet refresh for supported repositories;
  skip for unsupported and `migrating` ones. A v1 repository's skill cannot
  perform a flip (only `agents layout migrate` does), so this refresh is not how
  a v1 repository becomes v2; it is how a v2 repository keeps its agents-owned
  skill current.

- [ ] **Step 1: Write the failing refresh and gate tests**

```go
// Refresh writes the text for the resolved layout, only when it differs, and
// touches only the agents-owned skill. On a v1 repository it is a literal
// no-op — no write at all, so no mtime churn — which is what lets v0.6.0 leave
// the fleet alone (design §0.8).
func TestRefreshInfrastructuralSkillsIsLayoutSelectedAndUserSkillSafe(t *testing.T) {
	root := t.TempDir()
	if err := Create(root, false); err != nil {
		t.Fatal(err)
	}
	migrating := filepath.Join(root, ".agents/skills/migrating-fleet-context/SKILL.md")

	// The v1 text is already in place, so refresh must not write. An identical
	// rewrite is indistinguishable by content, so the observable is the mtime.
	frozen := time.Unix(1000000000, 0)
	if err := os.Chtimes(migrating, frozen, frozen); err != nil {
		t.Fatal(err)
	}
	if err := RefreshInfrastructuralSkills(root, layout.SchemaV1); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(migrating)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(frozen) {
		t.Fatal("refresh rewrote identical bytes on a v1 repository")
	}

	if err := RefreshInfrastructuralSkills(root, layout.SchemaV2); err != nil {
		t.Fatal(err)
	}
	asset, err := SkillAssetPath(layout.SchemaV2, "migrating-fleet-context")
	if err != nil {
		t.Fatal(err)
	}
	want, err := AssetsFS.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(migrating)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("refresh wrote the wrong text for v2: %v", err)
	}

	// The user-owned recording skill is never written by refresh, in either
	// layout, and the v1 copy of it is untouched.
	recAsset, err := SkillAssetPath(layout.SchemaV1, "recording-what-you-learn")
	if err != nil {
		t.Fatal(err)
	}
	recWant, _ := AssetsFS.ReadFile(recAsset)
	recGot, err := os.ReadFile(filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"))
	if err != nil || !bytes.Equal(recGot, recWant) {
		t.Fatal("refresh touched the user-owned recording skill")
	}
}

// The pre-existing scaffold test asserts the same rule from the other side: a
// v1 repository's migrating copy is refreshed to the **v1** asset, because a
// repository whose layout is v1 must be left with the text its layout uses.
// Update TestRefreshInfrastructuralSkills to read SkillAssetPath(layout.SchemaV1,
// "migrating-fleet-context") instead of the flat path.
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

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/scaffold . -run 'TestRefreshInfrastructural|TestFleetUpdateRefreshesSupportedV2' -v`
Expected: FAIL — `RefreshInfrastructuralSkills` takes one argument, writes the
flat asset regardless of layout, and writes unconditionally (so the mtime
assertion fails even after the signature is fixed), and the fleet handler either
treats the supported v2 repository as unsupported or does not refresh it.

- [ ] **Step 3: Select the refresh text by layout and skip the write when it matches**

```go
// RefreshInfrastructuralSkills refreshes the 100% agents-owned infrastructural
// skill (migrating-fleet-context) to the text for the repository's resolved
// layout. It never writes recording-what-you-learn or a repository-specific
// skill (design §0.8).
//
// It reads first and writes only when the bytes differ, so an unmodified v1
// repository is a literal no-op rather than an identical rewrite: the design
// promises "byte-identical", and an unconditional write would leave mtime churn
// in every v1 repository on every `agents update --all --apply`.
//
// This is not how a repository becomes v2. A v1 repository's skill copy is the
// frozen v1 text and does not know `agents layout migrate`; the CLI performs
// the flip, and this refresh only keeps the agents-owned skill current once the
// layout is v2.
func RefreshInfrastructuralSkills(root, layoutVersion string) error {
	assetPath, err := SkillAssetPath(layoutVersion, "migrating-fleet-context")
	if err != nil {
		return err
	}
	content, err := AssetsFS.ReadFile(assetPath)
	if err != nil {
		return err
	}
	target := filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md")
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, content, 0o644)
}
```

In `cmd_fleet.go`, keep the layout `layoutRefusal` already returned and pass its
schema to the refresh:

```go
l, refusal := layoutRefusal(e.Path, running, false)
if refusal != "" {
	skipped++
	fmt.Fprintf(stdout, "skip (layout %s): %s\n", refusal, fleetPath(e.Path))
	continue
}
...
if err := scaffold.RefreshInfrastructuralSkills(e.Path, l.Schema); err != nil {
	...
}
```

- [ ] **Step 4: Run the fleet tests and the whole suite**

Run: `go test -count=1 ./...`
Expected: PASS, including Task 7's "an unsupported repository's migration skill
was downgraded" negative test, the v1 positive control, and the updated
`TestRefreshInfrastructuralSkills` (a v1 repository's migrating copy is
refreshed to the **v1** asset, not the flat one).

- [ ] **Step 5: Commit**

```bash
git add agents
git commit -m "feat(fleet): refresh the agents-owned skill from the resolved layout"
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
	// A content-mixed archive: immutability is content-blind, so these are never
	// classified, never reported as misplaced, and never moved (design §0.6).
	writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old-plan.md"), "archived plan")
	writeFile(t, filepath.Join(root, "docs/archive/2020-old-design.md"), "archived design")
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"),
		"See [the plan](../plans/a-plan.md).\n")
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
	archiveBefore := trackedBlobs(t, root)
	linksBefore := countMarkdownLinks(t, root, "docs")

	// drift agrees: nothing under the archive is ever "misplaced".
	rep, err := drift.InspectRepo(root, "v0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.MisplacedDocs {
		if strings.HasPrefix(m, "docs/archive/") {
			t.Fatalf("archive file reported as misplaced: %s", m)
		}
	}

	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	if err := ApplyMigration(root, p, "pre-layout-v2-fixture"); err != nil {
		t.Fatal(err)
	}
	rewriteLinks(t, root, p.LinkCandidates)

	for _, archived := range []string{
		"docs/archive/plans/2020-old-plan.md",
		"docs/archive/2020-old-design.md",
	} {
		if got := trackedBlobs(t, root)[archived]; got != archiveBefore[archived] {
			t.Fatalf("archive blob changed or moved: %s", archived)
		}
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
missing test fails CI rather than passing a `-run` filter that matches nothing
(the job runs `go test -run "^(...)$" ./...`, so tests in any package count):

```
TestResumeCrashMatrix
TestResumeHandlesANonPrefixCrash
TestResumeRefusesAmbiguousAndLostMoves
TestApplyMigrationFreezesTheJournalBeforeTheFirstMove
TestAbortIsLimitedToANothingMovedMigration
TestApplyRefusesWhenTheTreeChangesUnderTheMove
TestPlanMigrationBlocksDocsResidueAndAnArchiveInsideASource
TestPlanMigrationLeavesAMixedArchiveAlone
TestIsIgnoredMatchesEverySpellingOnThePathItMatches
TestIsIgnoredSeesARuleEvenForATrackedPath
TestAgentsIgnoredAsksBothPaths
TestTrackednessHelpersOutsideARepository
TestIsTrackedDistinguishesUntrackedFromStaged
TestIsTrackedFailsClosedOnAGitError
TestDoctorAdvisesOnIgnoredStoresAndTheUncommittedManifestWindow
TestInitRefusesV2FlagsWhenAgentsIsIgnored
TestRefreshInfrastructuralSkillsIsLayoutSelectedAndUserSkillSafe
TestSkillAssetsSplitByLayout
TestSkillAssetPathsResolvePerLayout
TestMigrationSkillMatchesEmbeddedAsset
TestRecordingSkillNamesRolesNotDocsPaths
TestFrozenV1SkillAssetsStillSpeakV1Paths
TestFrozenV1AssetsEqualTheRecordedV051Bytes
TestSkillCurrencyIsSelectedByResolvedLayout
TestV2RecordingSkillFallbackResolvesV1Paths
TestChangedSkillAssetsHaveLegacyDigests
TestMigrationFixtureLinksAndArchiveSurvive
TestLayoutManifestIsVisibleToLinguist
```

- [ ] **Step 5: Update the design catalog**

The design row in `docs/design/README.md` records the gate-1 approval
(**approved 2026-09-19 — not approved for execution**). When v0.6.0 ships,
update that status to **implemented 2026-xx-xx** and name the released version.

- [ ] **Step 6: Run every gate**

```bash
cd agents
go test -count=1 ./...
go test -count=1 -race ./...
go vet ./...
cd ..
agents doctor
agents drift --all
agents drift --all --json > /tmp/drift-after-v060.json
```

Expected: the Go gates pass; doctor and drift report exactly the states the
release notes name, and no v1 repository's files change.

The accurate fleet sentence, and the only one any document may use: **v0.6.0
adds no new v1 advisory; `dotfiles`, `paperbubble`, `cowork`, and `lewm-mlx`
stay `current`; `autogo-mlx` and `desktop_pet` continue to exit 1 for
pre-existing 2026-08-29 two-tier reasons.** Measured read-only on 2026-09-18
with v0.5.1: `autogo-mlx` (router missing, recording skill missing, 1/4 stores,
2 misplaced docs) and `desktop_pet` (router diverged, domain missing, recording
skill missing, 0/4 stores) were already non-current, so `agents drift --all`
already exited 1 before this release; the other four were individually current.
A new `known_legacy` or `diverged` on any of the four in this run is a
regression in the v1 asset selection, not an expected advisory. Keep
`/tmp/drift-after-v060.json` for Task 16 Step 4.

Run the Documentation Checklist above before this step is complete; every item
must be checked or explicitly marked N/A with the reason.

- [ ] **Step 7: Commit**

```bash
git add agents .github/workflows/verify.yml docs/design/README.md agents/README.md
git commit -m "test(layout): cover migration fixtures, links, and prose gates"
```

---

### Task 16: Release v0.6.0, verify the deploy gate, and hand the pilot to the playbook

**Files:**
- Modify: `agents/README.md`, root `README.md`
- Modify: `.github/workflows/release.yml`
- Create: `.github/release-notes/v0.6.0.md`

**Interfaces:**
- Consumes: Tasks 9–15.
- Produces: v0.6.0 installed and verified on every machine, a release-notes
  carrier that exists and carries the custom prose, and the go/no-go for the
  paperbubble dry run.

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

Expected: all pass; doctor and drift match the baseline recorded in Task 15
Step 6, apart from the new layout checks. Re-run the Task 8 version-stamped
matrix against the final v0.6.0 build before tagging.

- [ ] **Step 2: Give the release notes a carrier that carries the prose**

The design requires the deploy-before-flip gate and the not-ready repositories
to be named in the release notes (design §0 row 7, §6.3). Today no such artifact
exists: `.github/workflows/release.yml` creates the GitHub Release with no
`body`/`body_path`. `generate_release_notes: true` would not fix that — it only
builds a body from commit titles, with none of this prose — so the carrier is a
committed notes file that the workflow must find.

1. Create `.github/release-notes/v0.6.0.md` in the release PR, containing: the
   deploy-before-flip prerequisite (every machine that can run `agents init`,
   `agents update --all --apply`, or `agents save` must resolve v0.6.0 on `PATH`
   before any repository flips); the four commands/flags of the migration path
   (`agents layout migrate --dry-run`, `--apply --backup-tag`,
   `--resume --apply` after a crash, `--abort --apply` only while nothing has
   moved); and the measured fleet baseline — v0.6.0 adds no new v1 advisory,
   `dotfiles` / `paperbubble` / `cowork` / `lewm-mlx` stay `current`, and
   `autogo-mlx` / `desktop_pet` continue to exit 1 for pre-existing 2026-08-29
   two-tier reasons (design §0.8).
2. `.github/workflows/release.yml` — pass that file as the Release body and make
   a missing or empty one fail the release rather than shipping nothing:

```yaml
      - name: release notes must exist and be non-empty
        run: test -s ".github/release-notes/${{ steps.version.outputs.version }}.md"

      - name: Create GitHub Release
        uses: softprops/action-gh-release@fe965f7af51af5f2602596916f38a38df2e33de0 # v3.0.2
        with:
          tag_name: ${{ steps.version.outputs.version }}
          body_path: .github/release-notes/${{ steps.version.outputs.version }}.md
          files: |
            dist/agents_*_*.tar.gz
            dist/checksums.txt
```

3. `agents/README.md` — add an **"Upgrading to v0.6.0"** section with the same
   three things. The GitHub Release body is the authoritative carrier; the
   README section is the durable repo-side copy, not a substitute for it, and
   neither may claim an artifact the other does not produce.

No document may claim a release-notes artifact that does not exist.

- [ ] **Step 3: Release `v0.6.0` (human-gated)**

```bash
git tag -a v0.6.0 -m "agents v0.6.0: layout manifests, store roots, migration"
git push origin v0.6.0
```

Confirm the published GitHub Release body contains the deploy-before-flip gate
and the fleet baseline, not just commit titles. If the release step failed on
the `test -s` guard, the tag exists without a body: write the notes file, fix
the guard, and re-run the release job rather than accepting an empty body.

- [ ] **Step 4: Upgrade every machine and verify the resolved binary**

```bash
brew upgrade agents
agents version                 # expect v0.6.0
command -v agents              # record the resolved path
agents doctor                  # the `binary` check must report ok
agents update --all            # dry run: confirm v1 repos would rewire and refresh
agents drift --all --json      # expect no new v2 repositories yet, and no new v1 advisory
```

Repeat on every machine that can run `agents init`, `agents update --all
--apply`, or `agents save` against the fleet. Record each machine, the version,
and the resolved path. `agents doctor`'s `binary` check is the one that proves
the `agents` on `PATH` is the running executable, which is exactly the stale
binary path this gate exists to catch.

Compare `agents drift --all --json` against `/tmp/drift-after-v060.json` from
Task 15 Step 6. The expected result is the same sentence as there: v0.6.0 adds
no new v1 advisory, `dotfiles` / `paperbubble` / `cowork` / `lewm-mlx` stay
`current`, and `autogo-mlx` / `desktop_pet` continue to exit 1 for pre-existing
2026-08-29 two-tier reasons. A new `known_legacy` or `diverged` on any of the
four means the v1 asset selection regressed: stop and fix it before the
playbook.

Do not run `agents update --all --apply` until the dry run names every
repository and every reason. Do not start the paperbubble migration until every
machine passes. No v2 repository exists yet.

- [ ] **Step 5: Hand off to the playbook**

Open `docs/plans/2026-09-18-layout-v2-migration-playbook.md`, execute its
preconditions through the approval gate (§2–§6) against paperbubble, and stop
there. Do not run `--apply` without the human's explicit approval.

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
| §0.5 migration state machine, phases, identity, `--abort`, crash matrix | 10, 11, 12, 15 |
| §0.6 content-blind archive and `archive_not_in_source` | 10, 15, and the playbook |
| §0.7 trackedness (`IsIgnored`/`IsTracked`/`AgentsIgnored`), three-state model, doctor advisories | 2, 5, 7, 10, 12, 15 |
| §0.8 layout-selected canonical skill texts, the other-layout exit, and who flips a repository | 3, 4, 9, 13, 14, 15, 16, and the playbook |
| §9.1 migration preconditions (clean tree, non-protected branch, `repo.InProgress`, stores, router, trackedness) | 10, 12 |

**Placeholders.** Every code step names a file, a symbol, and either full code
or the exact behavior the symbol must implement. No step says "add error
handling" without naming the error path.

**Type consistency.** `Layout`, `Manifest`, `Problem`, `Move`, `Migration`,
`MigrationFrom`, `Plan`, `MigrateOptions`, `MoveError`, `ResidueError`,
`CreateWithLayout`, `SkillAssetPath`, `RefreshInfrastructuralSkills`,
`AgentsIgnored`, `InspectRepo(root, running)`, `CanonicalRouterDigestFor`,
`CanonicalSkillDigestFor`, `repo.IsIgnored`, and `repo.IsTracked` are defined
once in "Locked Interfaces" and used with those exact names in every task. The
`Phase*` and `Move*` constants are defined in Task 10 and used verbatim in Tasks
11–12, 15, and the playbook. `LegacyRecordingSkillV051` and
`LegacyMigratingSkillV051` are defined in Task 3 (they pin the frozen v1 assets)
and consumed in Task 13 (they are the legacy digest catalog). `repo.InProgress`
is the existing helper in `agents/internal/repo/repo.go` (merge, rebase,
cherry-pick, revert, am, bisect); Task 12 consumes it and does not redefine it.
