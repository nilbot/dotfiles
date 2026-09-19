package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// The migration fixtures build a v1 repository through scaffold itself, so the
// frozen v1 skill texts, the store READMEs, and the canonical v1 router are the
// writer's bytes rather than a copy that could silently drift. They commit the
// scaffold, because a migration plans against a committed tree and the CLI's
// preconditions (design §9.1) ask git questions about it. They call
// scaffold.Create and not `agents init`, so a fixture registers no fleet entry
// and writes no machine state: there is nothing for $XDG_STATE_HOME to isolate.

// newGitV1Repo is the migration fixture: a temp git repository on the
// non-protected branch `agents-test` carrying exactly the v1 layout
// `scaffold.Create` writes, committed, with a clean tree. The layout is written
// by scaffold itself (see scaffoldV1), so `agents drift` calls it current.
func newGitV1Repo(t *testing.T) string {
	t.Helper()
	root := newTempGitRepo(t)
	isolateGitFixture(t, root)
	scaffoldV1(t, root)
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "v1 scaffold")
	if dirty := gitOutput(t, root, "status", "--porcelain"); dirty != "" {
		t.Fatalf("v1 fixture is not committed clean:\n%s", dirty)
	}
	return root
}

// newGitV1RepoWithContent is newGitV1Repo plus one file in two stores, so a
// plan has content to carry and a link to resolve.
func newGitV1RepoWithContent(t *testing.T) string {
	t.Helper()
	root := newGitV1Repo(t)
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"), "# a design\n")
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# a plan\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "content")
	return root
}

// scaffoldV1 writes the v1 layout with scaffold.Create itself. This package
// cannot call it: scaffold imports layout, so a test file in package layout that
// imported scaffold would be the import cycle Go rejects ("import cycle not
// allowed in test"). The program under testdata/ sits on the far side of that
// edge and makes the same call `agents init` makes for a v1 repository.
func scaffoldV1(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("go", "run", "./internal/layout/testdata/scaffoldv1", root)
	cmd.Dir = moduleRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scaffold v1 fixture in %s: %v\n%s", root, err, out)
	}
}

// moduleRoot is the agents module directory, resolved from this file rather
// than from the working directory, so a TestMain that moves the working
// directory cannot break the fixture.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller cannot locate this test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// isolateGitFixture takes the machine out of the fixture: git's background
// maintenance must not race a tree snapshot (measured on macOS CI, where a
// maintenance lock appeared and vanished mid-walk), and a machine-wide
// core.hooksPath must not run this machine's hooks during a fixture commit.
func isolateGitFixture(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"config", "core.hooksPath", ".git/hooks"},
		{"config", "maintenance.auto", "false"},
		{"config", "gc.auto", "0"},
	} {
		gitOutput(t, root, args...)
	}
}

// gitOutput runs one git command in a fixture and fails the test on error.
func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, root, err, out)
	}
	return string(out)
}

// mkdirAll creates one repository-relative directory tree.
func mkdirAll(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// hasBlocker reports whether a plan's blocker list carries a code.
func hasBlocker(blockers []Blocker, code string) bool {
	for _, b := range blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}

// snapshotTree records every path under root except .git as mode plus bytes, so
// a test can prove a call wrote nothing at all -- no manifest, no directory, no
// index change.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" && d.IsDir() {
			return fs.SkipDir
		}
		switch {
		case d.IsDir():
			out[rel] = "dir"
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			out[rel] = "symlink -> " + target
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[rel] = fmt.Sprintf("file %v\n%s", d.Type(), data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// markdownFiles lists the markdown files under one directory, repository-
// relative and sorted: the same set the planner reports candidates in.
func markdownFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	var out []string
	base := filepath.Join(root, filepath.FromSlash(dir))
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isMarkdown(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// countMarkdownLinks counts `](target)` links outside fenced code blocks. It is
// half of design §9.5's N-in/N-out gate: the fixture counts links before the
// migration and again after the rewrite, and the two must be equal.
func countMarkdownLinks(t *testing.T, root, dir string) int {
	t.Helper()
	n := 0
	for _, rel := range markdownFiles(t, root, dir) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		var f fence
		for _, line := range strings.Split(string(data), "\n") {
			if !f.content(line) {
				continue
			}
			n += len(markdownLinks(line))
		}
	}
	return n
}

// rewriteLinks applies a plan's Old -> New mapping the way the
// migrating-fleet-context skill does: every markdown link in the repository
// that resolves to either side of a reported mapping is re-spelled relative to
// the file that now holds it. Resolving rather than matching text is what makes
// it work after the moves -- the file that held the link moved too, and its own
// store root may differ from the target's. It walks the repository because the
// candidates name the file's plan-time path, which the migration has since
// changed. The archive is the fixture's own concern: no candidate can point
// into it (it is never a move source), and the fixture asserts its blobs are
// byte-identical afterwards.
func rewriteLinks(t *testing.T, root string, candidates []LinkCandidate) {
	t.Helper()
	if len(candidates) == 0 {
		return
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if isMarkdown(d.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		rel = filepath.ToSlash(rel)
		dir := repoDir(rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		var f fence
		changed := false
		for i, line := range lines {
			if !f.content(line) {
				continue
			}
			var b strings.Builder
			last := 0
			for _, link := range markdownLinks(line) {
				resolved, ok := resolveLinkTarget(dir, link.target)
				if !ok {
					continue
				}
				c, ok := candidateFor(candidates, resolved, link.target)
				if !ok {
					continue
				}
				spelling, err := filepath.Rel(dir, filepath.FromSlash(c.New))
				if err != nil {
					t.Fatal(err)
				}
				b.WriteString(line[last:link.start])
				b.WriteString(filepath.ToSlash(spelling))
				last = link.end
				changed = true
			}
			b.WriteString(line[last:])
			lines[i] = b.String()
		}
		if !changed {
			continue
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

// candidateFor reports the mapping a link belongs to. Two things identify it:
// the link resolves to one side of a mapping, so it points at the store before
// or after the move; or it is spelled exactly the way the reported link was
// spelled from the file the planner read it in. The spelling is what survives
// when the file and its target land under different roots, because then the
// moved link resolves to a path that never existed in either layout.
//
// Either side of a mapping counts, so a link that already points at the new
// path is re-spelled too, and rewriting twice changes nothing.
func candidateFor(candidates []LinkCandidate, resolved, target string) (LinkCandidate, bool) {
	for _, c := range candidates {
		if resolved == c.Old || resolved == c.New {
			return c, true
		}
	}
	for _, c := range candidates {
		from, err := filepath.Rel(repoDir(c.File), c.Old)
		if err != nil {
			continue
		}
		if filepath.ToSlash(from) == target {
			return c, true
		}
	}
	return LinkCandidate{}, false
}

// unresolvedLinks lists every relative link target under dir that does not
// resolve to an existing path, as `file:line -> target`. It is the other half
// of design §9.5's gate: after the rewrite, nothing may dangle.
func unresolvedLinks(t *testing.T, root, dir string) []string {
	t.Helper()
	var out []string
	for _, rel := range markdownFiles(t, root, dir) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		fileDir := repoDir(rel)
		var f fence
		for i, line := range strings.Split(string(data), "\n") {
			if !f.content(line) {
				continue
			}
			for _, link := range markdownLinks(line) {
				resolved, ok := resolveLinkTarget(fileDir, link.target)
				if !ok {
					continue
				}
				if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(resolved))); err != nil {
					out = append(out, fmt.Sprintf("%s:%d -> %s", rel, i+1, link.target))
				}
			}
		}
	}
	return out
}

func TestPlanMigrationBuildsDirectoryMovesAndRecordsArchive(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/old-plan.md"), "old")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
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
		Running:  "v0.6.0", RouterState: "current",
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
		Running:  "v0.6.0", RouterState: "current",
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
		Running:  "v0.6.0", RouterState: "diverged",
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
		Running:  "v0.6.0", RouterState: "current",
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

// The planner writes nothing at all: no manifest, no store directory, no index
// change, not even a temporary file, and not one rewritten link byte. It is the
// whole promise of a dry run, so the proof is a whole-tree snapshot rather than
// an assertion about the paths the implementation happens to touch -- including
// for a plan that is refused. Both fixtures hold a markdown link into a moving
// store, so a planner that rewrote links in place would fail here rather than
// only contradict the absence of a write call in the source.
func TestPlanMigrationWritesNothingAtAll(t *testing.T) {
	const link = "See [the plan](../plans/a-plan.md).\n"
	refused := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(refused, "docs/design/a-design.md"), link)
	mkdirAll(t, refused, "docs/archive/plans")
	writeFile(t, filepath.Join(refused, "docs/archive/plans/2020-old-plan.md"), "archived\n")
	writeFile(t, filepath.Join(refused, "docs/notes.md"), "residue\n")
	before := snapshotTree(t, refused)
	indexBefore := gitOutput(t, refused, "status", "--porcelain", "--untracked-files=all")

	p, err := PlanMigration(refused, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlocker(p.Blockers, "docs_residue") {
		t.Fatalf("the fixture must be refused so both outcomes are covered: %v", p.Blockers)
	}
	if len(p.LinkCandidates) != 1 {
		t.Fatalf("the refused plan must report the link it did not touch: %+v", p.LinkCandidates)
	}

	clean := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(clean, "docs/design/a-design.md"), link)
	beforeClean := snapshotTree(t, clean)
	p, err = PlanMigration(clean, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) != 0 {
		t.Fatalf("clean plan = (%+v, %v)", p.Blockers, err)
	}
	if len(p.Moves) != 4 {
		t.Fatalf("moves = %+v", p.Moves)
	}
	if p.Counts.Move != 4 || p.Counts.Blocked != 0 || p.Counts.Links != 1 {
		t.Fatalf("counts = %+v", p.Counts)
	}
	if p.DryRun != true || p.Phase != PhasePlanned || p.Repo != clean {
		t.Fatalf("plan header = %+v", p)
	}
	if after := snapshotTree(t, clean); !reflect.DeepEqual(beforeClean, after) {
		t.Fatalf("a planned migration changed the tree:\nbefore: %v\nafter:  %v", beforeClean, after)
	}
	if after := snapshotTree(t, refused); !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused migration changed the tree:\nbefore: %v\nafter:  %v", before, after)
	}
	if after := gitOutput(t, refused, "status", "--porcelain", "--untracked-files=all"); after != indexBefore {
		t.Fatalf("git state changed:\nbefore: %q\nafter:  %q", indexBefore, after)
	}
	for _, rel := range []string{ManifestRel, ".context", "notes"} {
		if _, err := os.Lstat(filepath.Join(refused, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("planner created %s", rel)
		}
	}
}

// Every exit carries the header and the counts, refusals included: a JSON
// consumer reads `dry_run`, `repo`, and `blocked` before it reads the blockers,
// and a refused dry run that reported `"dry_run": false` with `"blocked": 0`
// would read as a plan already applied. The Support refusal is the routine path
// for every unstamped or below-floor binary.
func TestPlanMigrationRefusedPlansKeepTheirHeaderAndCounts(t *testing.T) {
	cases := []struct {
		name string
		opts MigrateOptions
	}{
		{"invalid target", MigrateOptions{Running: "v0.6.0", RouterState: "current"}},
		{"unsupported binary", MigrateOptions{Template: TemplateContentVault, Running: "v0.5.1", RouterState: "current"}},
		{"unclean router", MigrateOptions{Template: TemplateContentVault, Running: "v0.6.0", RouterState: "missing"}},
		{"residue", MigrateOptions{Template: TemplateContentVault, Running: "v0.6.0", RouterState: "current"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newGitV1Repo(t)
			if tc.name == "residue" {
				writeFile(t, filepath.Join(root, "docs/scratch.md"), "x\n")
			}
			p, err := PlanMigration(root, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Blockers) == 0 {
				t.Fatalf("the fixture must be refused: %+v", p)
			}
			if !p.DryRun {
				t.Fatalf("a refusal is still a dry run: %+v", p)
			}
			if p.Repo != root {
				t.Fatalf("repo = %q, want %q", p.Repo, root)
			}
			if p.Phase != PhasePlanned {
				t.Fatalf("phase = %q, want %q", p.Phase, PhasePlanned)
			}
			if p.RouterAction == "" {
				t.Fatal("a refused plan still says what it would have done to the router")
			}
			if p.From.Schema != SchemaV1 || p.To.Schema != SchemaV2 {
				t.Fatalf("from/to = %q/%q", p.From.Schema, p.To.Schema)
			}
			if p.Counts.Blocked != len(p.Blockers) || p.Counts.Move != len(p.Moves) ||
				p.Counts.Links != len(p.LinkCandidates) {
				t.Fatalf("counts = %+v for %d blockers, %d moves, %d links",
					p.Counts, len(p.Blockers), len(p.Moves), len(p.LinkCandidates))
			}
		})
	}
}

// Every refusal is a named report, one blocker per entry: the operator fixes
// them one at a time, and a refusal that collapses three entries into one line
// hides two remedies.
func TestPlanMigrationNamesEveryBlockerEntry(t *testing.T) {
	residue := newGitV1Repo(t)
	writeFile(t, filepath.Join(residue, "docs/scratch.md"), "x\n")
	mkdirAll(t, residue, "docs/notes")
	p, err := PlanMigration(residue, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	var named []string
	for _, b := range p.Blockers {
		if b.Code != "docs_residue" {
			continue
		}
		named = append(named, b.Detail)
	}
	if len(named) != 2 {
		t.Fatalf("docs_residue blockers = %v, want one per offending entry", named)
	}
	for _, want := range []string{"docs/scratch.md", "docs/notes"} {
		found := false
		for _, detail := range named {
			if strings.Contains(detail, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("no docs_residue blocker names %s: %v", want, named)
		}
	}

	// archive_not_in_source names both halves of the overlap: the archive and
	// the store whose move would drag it along (design §0.6).
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
	found := false
	for _, b := range p.Blockers {
		if b.Code == "archive_not_in_source" && strings.Contains(b.Detail, "docs/plans/archive") &&
			strings.Contains(b.Detail, "docs/plans") {
			found = true
		}
	}
	if !found {
		t.Fatalf("archive_not_in_source must name the archive and the move source: %v", p.Blockers)
	}

	// A missing store and a symlinked store are different findings with
	// different remedies, and each names role=path.
	missing := newGitV1Repo(t)
	if err := os.RemoveAll(filepath.Join(missing, "docs/qna")); err != nil {
		t.Fatal(err)
	}
	p, err = PlanMigration(missing, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !namedBlocker(p.Blockers, "store_missing", "qna=docs/qna") {
		t.Fatalf("store_missing = %v, want qna=docs/qna", p.Blockers)
	}
	for _, m := range p.Moves {
		if m.Role == RoleQNA {
			t.Fatalf("a missing store must not be planned as a move: %+v", m)
		}
	}

	symlinked := newGitV1Repo(t)
	if err := os.RemoveAll(filepath.Join(symlinked, "docs/qna")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(symlinked, "docs/qna")); err != nil {
		t.Fatal(err)
	}
	p, err = PlanMigration(symlinked, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !namedBlocker(p.Blockers, "store_symlink", "qna=docs/qna") {
		t.Fatalf("store_symlink = %v, want qna=docs/qna", p.Blockers)
	}
	for _, m := range p.Moves {
		if m.Role == RoleQNA {
			t.Fatalf("a symlinked store must not be planned as a move: %+v", m)
		}
	}
}

// namedBlocker reports whether the list carries the code with a detail naming
// the entry.
func namedBlocker(blockers []Blocker, code, detail string) bool {
	for _, b := range blockers {
		if b.Code == code && strings.Contains(b.Detail, detail) {
			return true
		}
	}
	return false
}

// The planner's error return is reserved for a source it cannot read at all: a
// repository that is not a v1 layout is not a plan with blockers, it is a plan
// that cannot be made. Every other refusal is a report.
func TestPlanMigrationErrorsOnlyWhenTheSourceCannotBeRead(t *testing.T) {
	opts := MigrateOptions{Template: TemplateContentVault, Running: "v0.6.0", RouterState: "current"}

	v2 := newTempGitRepo(t)
	writeManifest(t, v2, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {"design": ".context/design", "plans": ".context/plans",
             "journal": ".context/journal", "qna": ".context/qna"}
}`)
	if _, err := PlanMigration(v2, opts); err == nil {
		t.Fatal("a v2 source is not a v1 migration source, and must be an error")
	}

	broken := newTempGitRepo(t)
	writeManifest(t, broken, "{not json")
	if _, err := PlanMigration(broken, opts); err == nil {
		t.Fatal("an unreadable manifest must be an error, not a plan")
	}

	refused := newGitV1Repo(t)
	mkdirAll(t, refused, ".context/design")
	p, err := PlanMigration(refused, opts)
	if err != nil {
		t.Fatalf("a refusal is a plan, not an error: %v", err)
	}
	if !hasBlocker(p.Blockers, "target_exists") || p.Phase != PhasePlanned {
		t.Fatalf("refusal = %+v", p)
	}
}

// A binary below the target's floor plans but must not write: the refusal is
// the Support gate's own reason code, and no move is planned behind it.
func TestPlanMigrationRefusesAnUnsupportedBinary(t *testing.T) {
	root := newGitV1Repo(t)
	for _, running := range []string{"v0.5.1", "dev"} {
		p, err := PlanMigration(root, MigrateOptions{
			Template: TemplateContentVault,
			Running:  running, RouterState: "current",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := "below_floor"
		if running == "dev" {
			want = "unreleased"
		}
		if !hasBlocker(p.Blockers, want) {
			t.Fatalf("running %q: blockers = %v, want %s", running, p.Blockers, want)
		}
		if len(p.Moves) != 0 {
			t.Fatalf("running %q planned moves it may not apply: %+v", running, p.Moves)
		}
	}
}

// Design §0.7: a manifest under an ignored .agents/ is machine-local, and a
// clone would silently fall back to v1. The refusal is a blocker, because the
// operator's remedy is to drop the ignore rule.
func TestPlanMigrationRefusesAMachineLocalManifest(t *testing.T) {
	root := newGitV1Repo(t)
	ignoreAgents(t, root, "/.agents/")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlocker(p.Blockers, ProblemLocalAgents) {
		t.Fatalf("blockers = %v, want %s", p.Blockers, ProblemLocalAgents)
	}
}

// The link scan is a report about links that will break, so it reads markdown
// only, it ignores fenced examples, and it ignores everything that is not a
// repository path: a URL, a mailto, a bare fragment, and an absolute filesystem
// path are not moved by a store migration. A title is part of the syntax, not
// of the target, and a fence left open at end of file still hides what follows
// it.
func TestPlanMigrationSkipsFencedExamplesAndNonPaths(t *testing.T) {
	root := newGitV1Repo(t)
	body := strings.Join([]string{
		`See [the plan](../plans/a-plan.md "the plan").`,
		"",
		"```md",
		"An example: [the plan](../plans/a-plan.md).",
		"```",
		"",
		"A [site](https://example.com/plans/a-plan.md), a [mail](mailto:t@example.com),",
		"an [anchor](#section), an [absolute](/etc/hosts), and an [escape](../../outside.md).",
		"",
		"```",
		"A fence that never closes: [the plan](../plans/a-plan.md).",
		"",
	}, "\n")
	writeFile(t, filepath.Join(root, "docs/design/notes.md"), body)
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
	writeFile(t, filepath.Join(root, "docs/plans/readme.txt"), "[not markdown](../plans/a-plan.md)\n")

	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 0 {
		t.Fatalf("blockers = %v", p.Blockers)
	}
	if len(p.LinkCandidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly the titled link", p.LinkCandidates)
	}
	got := p.LinkCandidates[0]
	want := LinkCandidate{
		File: "docs/design/notes.md", Line: 1,
		Old: "docs/plans/a-plan.md", New: ".context/plans/a-plan.md",
	}
	if got != want {
		t.Fatalf("candidate = %+v, want %+v", got, want)
	}
	if p.Counts.Links != 1 {
		t.Fatalf("counts = %+v", p.Counts)
	}
	after, err := os.ReadFile(filepath.Join(root, "docs/design/notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Fatalf("the planner rewrote a file it had only reported:\n%s", after)
	}
}

// The digest is the source identity recorded at plan time (design §0.5). It
// covers directories, symlinks by target, and file contents, over sorted
// slash-separated relative paths -- and it is deliberately not a git tree oid,
// because content ignored by .gitignore is invisible to git and is still moved
// by `git mv`.
func TestDigestTreeCoversEveryEntryAndIsNotAGitOid(t *testing.T) {
	root := newGitV1Repo(t)
	base, files, size, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "sha256:") {
		t.Fatalf("digest = %q, want a spelled algorithm", base)
	}
	readme, err := os.ReadFile(filepath.Join(root, "docs/design/README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 || size != int64(len(readme)) {
		t.Fatalf("digestTree counted files=%d bytes=%d, want 1 and %d", files, size, len(readme))
	}

	// A directory, a symlink, and a file each change the digest; the symlink is
	// counted as a file and hashed by its target rather than followed.
	mkdirAll(t, root, "docs/design/nested")
	if got, _, _, err := digestTree(root, "docs/design"); err != nil || got == base {
		t.Fatalf("a new directory must change the digest: (%q, %v)", got, err)
	}
	withDir, files, _, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("directories are not files: files = %d", files)
	}
	writeFile(t, filepath.Join(root, "docs/design/nested/deep.md"), "deep\n")
	withFile, files, _, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 || withFile == withDir {
		t.Fatalf("a new file must change the digest: files=%d %q", files, withFile)
	}
	if err := os.Symlink("README.md", filepath.Join(root, "docs/design/link.md")); err != nil {
		t.Fatal(err)
	}
	withLink, files, _, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	if files != 3 || withLink == withFile {
		t.Fatalf("a symlink must be counted and hashed: files=%d %q", files, withLink)
	}
	// A symlink to a directory is one entry, not a traversal: the walk records
	// the link and never descends through it, which is what keeps a store from
	// claiming content outside itself.
	if err := os.Symlink("nested", filepath.Join(root, "docs/design/nested-link")); err != nil {
		t.Fatal(err)
	}
	withDirLink, files, _, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	if files != 4 || withDirLink == withLink {
		t.Fatalf("a directory symlink must be one entry of its own: files=%d %q", files, withDirLink)
	}
	writeFile(t, filepath.Join(root, "docs/design/README.md"), "changed\n")
	if got, _, _, err := digestTree(root, "docs/design"); err != nil || got == withDirLink {
		t.Fatalf("a changed file must change the digest: (%q, %v)", got, err)
	}

	// The git-invisible case: an ignored file is carried by `git mv`, so it has
	// to be carried by the digest too. `git ls-files` cannot see it.
	writeFile(t, filepath.Join(root, ".gitignore"), "docs/design/ignored.md\n")
	ignored, _, _, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "docs/design/ignored.md"), "machine-local\n")
	if tracked := gitOutput(t, root, "ls-files", "docs/design"); strings.Contains(tracked, "ignored.md") {
		t.Fatalf("the fixture's ignored file is tracked:\n%s", tracked)
	}
	gitOutput(t, root, "check-ignore", "-q", "--no-index", "docs/design/ignored.md")
	withIgnored, _, size, err := digestTree(root, "docs/design")
	if err != nil {
		t.Fatal(err)
	}
	if withIgnored == ignored {
		t.Fatal("the digest missed a file git cannot see, so it cannot be a git tree oid")
	}
	if size != int64(len("changed\n")+len("deep\n")+len("machine-local\n")) {
		t.Fatalf("bytes = %d, want the sum of the regular files only", size)
	}
}

// The three link helpers the end-to-end fixture needs, exercised on the shape
// the migration produces: the stores land under different roots, so a link that
// pointed at a sibling store by relative path is dangling until it is
// rewritten. The count is the N-in/N-out gate of design §9.5, and resolution is
// the post-condition.
func TestRewriteLinksKeepsEveryLinkResolving(t *testing.T) {
	root := newGitV1Repo(t)
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"), strings.Join([]string{
		"See [the plan](../plans/a-plan.md).",
		"",
		"```md",
		"An example: [the plan](../plans/a-plan.md).",
		"```",
		"",
		"A [site](https://example.com/).",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "content")

	targets := map[string]string{
		RoleDesign: ".context/design", RolePlans: "notes/plans",
		RoleJournal: ".context/journal", RoleQNA: ".context/qna",
	}
	p, err := PlanMigration(root, MigrateOptions{
		Running: "v0.6.0", RouterState: "current", Stores: targets,
	})
	if err != nil || len(p.Blockers) != 0 {
		t.Fatalf("plan = (%+v, %v)", p.Blockers, err)
	}
	if len(p.LinkCandidates) != 1 {
		t.Fatalf("candidates = %+v", p.LinkCandidates)
	}
	linksBefore := countMarkdownLinks(t, root, "docs")

	// Perform the moves the planner described, so the rewrite runs against the
	// tree the migration leaves behind.
	for _, m := range p.Moves {
		mkdirAll(t, root, repoDir(m.To))
		gitOutput(t, root, "mv", m.From, m.To)
	}
	if bad := unresolvedLinks(t, root, ".context"); len(bad) != 1 {
		t.Fatalf("the fixture must dangle before the rewrite: %v", bad)
	}
	rewriteLinks(t, root, p.LinkCandidates)

	if got := countMarkdownLinks(t, root, ".context"); got != linksBefore {
		t.Fatalf("links in = %d, links out = %d", linksBefore, got)
	}
	if bad := unresolvedLinks(t, root, ".context"); len(bad) > 0 {
		t.Fatalf("unresolved links after rewrite: %v", bad)
	}
	body, err := os.ReadFile(filepath.Join(root, ".context/design/a-design.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "(../../notes/plans/a-plan.md)") {
		t.Fatalf("the link was not re-spelled for the new store root:\n%s", body)
	}
	if !strings.Contains(string(body), "An example: [the plan](../plans/a-plan.md).") {
		t.Fatalf("the fenced example was rewritten:\n%s", body)
	}
	if !strings.Contains(string(body), "(https://example.com/)") {
		t.Fatalf("an external link was rewritten:\n%s", body)
	}

	// Rewriting twice changes nothing: the mapping matches either side.
	rewriteLinks(t, root, p.LinkCandidates)
	again, err := os.ReadFile(filepath.Join(root, ".context/design/a-design.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(body) {
		t.Fatalf("the rewrite is not idempotent:\n%s\n%s", body, again)
	}
}

// Design §0.4: --stores layers on the template rather than replacing it. One
// override replaces one role, and the template supplies the other three; a
// planner that took the override map as the whole layout would refuse a
// perfectly well-formed invocation with three role_missing blockers.
func TestPlanMigrationLayersStoresOverTheTemplate(t *testing.T) {
	root := newGitV1Repo(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Stores:   map[string]string{RoleDesign: "notes/design"},
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) != 0 {
		t.Fatalf("plan = (%+v, %v)", p.Blockers, err)
	}
	targets := map[string]string{}
	for _, m := range p.Moves {
		targets[m.Role] = m.To
	}
	want := map[string]string{
		RoleDesign: "notes/design", RolePlans: ".context/plans",
		RoleJournal: ".context/journal", RoleQNA: ".context/qna",
	}
	for role, path := range want {
		if targets[role] != path {
			t.Fatalf("stores = %v, want %v", targets, want)
		}
	}
}

// Design §9.1: only a router state the skill has provably left alone is clean.
// `known_legacy` is boilerplate the migration may replace; `diverged`, `missing`,
// and an unsaid state are each the skill's work first.
func TestPlanMigrationBlocksOnlyAnUncleanRouter(t *testing.T) {
	root := newGitV1Repo(t)
	for _, tc := range []struct {
		state   string
		blocked bool
	}{
		{"current", false},
		{"known_legacy", false},
		{"diverged", true},
		{"missing", true},
		{"", true},
	} {
		p, err := PlanMigration(root, MigrateOptions{
			Template: TemplateContentVault,
			Running:  "v0.6.0", RouterState: tc.state,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := hasBlocker(p.Blockers, "router_not_clean"); got != tc.blocked {
			t.Fatalf("router %q: router_not_clean = %v, want %v: %v", tc.state, got, tc.blocked, p.Blockers)
		}
		if !strings.HasPrefix(p.RouterAction, tc.state+" ") {
			t.Fatalf("router %q: RouterAction = %q", tc.state, p.RouterAction)
		}
	}
}

// A named pipe has no bytes to hash, no content identity to verify a move
// against, and os.ReadFile on one blocks on open until a writer appears -- so a
// planner that read every non-directory entry would hang forever on a store
// that holds one. The store is refused instead, and the refusal is timely: the
// goroutine guard turns a regression back into a failure rather than a hung
// test binary.
func TestPlanMigrationRefusesAStoreWithANamedPipe(t *testing.T) {
	root := newGitV1Repo(t)
	pipe := filepath.Join(root, "docs/design/pipe")
	if err := syscall.Mkfifo(pipe, 0o644); err != nil {
		t.Skipf("this platform cannot create a named pipe: %v", err)
	}
	type result struct {
		p   Plan
		err error
	}
	results := make(chan result, 1)
	go func() {
		p, err := PlanMigration(root, MigrateOptions{
			Template: TemplateContentVault,
			Running:  "v0.6.0", RouterState: "current",
		})
		results <- result{p, err}
	}()
	select {
	case got := <-results:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if !hasBlocker(got.p.Blockers, "store_unreadable") {
			t.Fatalf("blockers = %v, want store_unreadable", got.p.Blockers)
		}
		found := false
		for _, b := range got.p.Blockers {
			if b.Code == "store_unreadable" && strings.Contains(b.Detail, "design=docs/design") &&
				strings.Contains(b.Detail, "named pipe") {
				found = true
			}
		}
		if !found {
			t.Fatalf("the refusal must name the store and the entry: %v", got.p.Blockers)
		}
		for _, m := range got.p.Moves {
			if m.Role == RoleDesign {
				t.Fatalf("a store whose identity cannot be taken must not be planned as a move: %+v", m)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the planner blocked on a named pipe: os.ReadFile on a FIFO waits for a writer forever")
	}
}

// The archive overlap is asked of the v1 source stores, not of the moves that
// survived the per-store checks: a store that is missing is still a store the
// archive sits inside, and one run has to name both findings rather than
// withholding the overlap until the operator has fixed the first refusal.
func TestPlanMigrationReportsArchiveOverlapForAStoreItCannotMove(t *testing.T) {
	root := newGitV1Repo(t)
	if err := os.RemoveAll(filepath.Join(root, "docs/plans")); err != nil {
		t.Fatal(err)
	}
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault, Archive: "docs/plans/archive",
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !namedBlocker(p.Blockers, "store_missing", "plans=docs/plans") {
		t.Fatalf("store_missing = %v, want plans=docs/plans", p.Blockers)
	}
	if !namedBlocker(p.Blockers, "archive_not_in_source", "docs/plans") {
		t.Fatalf("the overlap must be named in the same run: %v", p.Blockers)
	}
	if len(p.Moves) != 3 {
		t.Fatalf("moves = %+v", p.Moves)
	}
	if p.Counts.Blocked != len(p.Blockers) || p.Counts.Move != len(p.Moves) {
		t.Fatalf("counts = %+v", p.Counts)
	}
}

// The archive is never a move, refused plans included: a JSON consumer reads
// moves and blockers together, and a plan that lists the overlap it refuses
// contradicts the guarantee it is reporting on. The refusal still names the
// store, so dropping the move hides nothing.
func TestPlanMigrationNeverPlansTheArchiveAsAMove(t *testing.T) {
	root := newGitV1Repo(t)
	mkdirAll(t, root, "docs/design/archive")
	writeFile(t, filepath.Join(root, "docs/design/archive/old.md"), "old\n")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault, Archive: "docs/design/archive",
		Running: "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !namedBlocker(p.Blockers, "archive_not_in_source", "docs/design") {
		t.Fatalf("blockers = %v, want archive_not_in_source naming docs/design", p.Blockers)
	}
	for _, m := range p.Moves {
		if m.Role == RoleDesign || pathPrefix(m.From, "docs/design") || pathPrefix(".context/design", m.To) {
			t.Fatalf("a refused plan still lists the archive's store as a move: %+v", m)
		}
	}
	if len(p.Moves) != 3 || p.Counts.Move != 3 || p.Counts.Blocked != len(p.Blockers) {
		t.Fatalf("moves = %+v, counts = %+v", p.Moves, p.Counts)
	}
}

// A declared archive may be nested below docs/, and the directories that exist
// only to hold it are part of the declaration: refusing docs/archive as residue
// would make `--archive docs/archive/plans` unsatisfiable, because the operator
// cannot both keep the archive and empty its parent.
func TestPlanMigrationAllowsAnArchiveNestedUnderDocs(t *testing.T) {
	for _, archive := range []string{"docs/archive", "docs/archive/plans"} {
		t.Run(archive, func(t *testing.T) {
			root := newGitV1Repo(t)
			mkdirAll(t, root, "docs/archive/plans")
			writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old-plan.md"), "old\n")
			p, err := PlanMigration(root, MigrateOptions{
				Template: TemplateContentVault, Archive: archive,
				Running: "v0.6.0", RouterState: "current",
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Blockers) != 0 {
				t.Fatalf("the declared archive's own ancestors are not residue: %v", p.Blockers)
			}
			if p.Archive != archive || len(p.Moves) != 4 {
				t.Fatalf("archive = %q, moves = %+v", p.Archive, p.Moves)
			}
		})
	}
}

// Trackedness is one question asked of git (design §0.7), and an unanswered
// question must not read as a pass: a source that is not a repository at all
// cannot be told whether .agents/ is ignored, so the plan is refused rather
// than planned against a manifest whose trackedness nobody can see.
func TestPlanMigrationRefusesWhenTrackednessCannotBeAsked(t *testing.T) {
	root := t.TempDir()
	for _, role := range Roles() {
		mkdirAll(t, root, "docs/"+role)
	}
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !namedBlocker(p.Blockers, ProblemLocalAgents, "cannot determine") {
		t.Fatalf("blockers = %v, want a fail-closed %s", p.Blockers, ProblemLocalAgents)
	}
	if len(p.Moves) != 4 {
		t.Fatalf("moves = %+v", p.Moves)
	}
	if p.Counts.Blocked != len(p.Blockers) {
		t.Fatalf("counts = %+v", p.Counts)
	}
}

// ---------------------------------------------------------------------------
// Apply, resume, and abort (design §0.5, §9.3, §9.4)
// ---------------------------------------------------------------------------

// trackedBlobs maps every index entry under dir to its blob oid, as
// `git ls-files -s` reports it. It is half of the blob-identity gate of design
// §9.3: `git mv` renames the index entry and leaves the oid alone, so comparing
// two of these maps proves the migration moved blobs instead of rewriting them,
// and reading the index at all proves the index reconciliation ran.
func trackedBlobs(t *testing.T, root, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(gitOutput(t, root, "ls-files", "-s", "--", dir), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		meta, rel, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("unexpected `git ls-files -s` line %q", line)
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			t.Fatalf("unexpected `git ls-files -s` metadata %q", meta)
		}
		out[filepath.ToSlash(rel)] = fields[1]
	}
	return out
}

// crashAt arms the reconciliation crash seam (R4) to fail once at one boundary.
// Failing once, and not always, is what makes every row of the crash matrix a
// crash followed by a resume rather than a permanently broken run.
func crashAt(t *testing.T, step string, moveIndex int) {
	t.Helper()
	fired := false
	reconcileHook = func(gotStep string, gotIndex int) error {
		if fired || gotStep != step || gotIndex != moveIndex {
			return nil
		}
		fired = true
		return fmt.Errorf("injected crash at %s/%d", step, moveIndex)
	}
	t.Cleanup(func() { reconcileHook = nil })
}

// planV2 plans the content-vault migration every v1 fixture in this file
// accepts, failing the test on a refusal: the accepted plan is the precondition
// of the apply/resume tests, never the thing under test.
func planV2(t *testing.T, root string) Plan {
	t.Helper()
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	return p
}

// countGitattributeLine counts exact lines in the repository's tracked
// .gitattributes, so a reconcile that appends its line twice is caught.
func countGitattributeLine(t *testing.T, root, want string) int {
	t.Helper()
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(attrs), "\n") {
		if strings.TrimSpace(line) == want {
			n++
		}
	}
	return n
}

func TestApplyMigrationMovesBlobsAndNeverCopies(t *testing.T) {
	root := newGitV1RepoWithContent(t) // docs/design/a-design.md, docs/plans/a-plan.md
	before := trackedBlobs(t, root, "docs")
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
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
	// N in / N out: a copy would leave the same blobs in two places, and a
	// rewrite would leave an extra entry behind.
	if len(after) != len(before) {
		t.Fatalf("moved %d blob(s), want %d: %v", len(after), len(before), after)
	}
	if left := trackedBlobs(t, root, "docs"); len(left) != 0 {
		t.Fatalf("the index still tracks something under docs/: %v", left)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("docs/ shell survived a migration with no archive")
	}
	router, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(router) != V2AgentsMD {
		t.Fatal("v2 router was not written")
	}
	if out := gitOutput(t, root, "tag", "--list", "pre-layout-v2-test"); strings.TrimSpace(out) == "" {
		t.Fatal("backup tag was not created")
	}
	l := Resolve(root)
	if l.LayoutStatus != StatusActive || l.Migration != nil || len(l.Problems) != 0 {
		t.Fatalf("resolved layout after apply = %+v", l)
	}
}

// A crash can interrupt the two halves of `git mv` itself -- the working-tree
// rename and the index update -- and then the tree is where the journal says and
// the index is not. Resume must reconcile the index without being told.
func TestResumeReconcilesAnIndexTheMoveDidNotUpdate(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	mv := p.Moves[0]
	before := trackedBlobs(t, root, mv.From)
	mkdirAll(t, root, filepath.Dir(mv.To))
	if err := os.Rename(filepath.Join(root, mv.From), filepath.Join(root, mv.To)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err != nil {
		t.Fatal(err)
	}
	if left := trackedBlobs(t, root, mv.From); len(left) != 0 {
		t.Fatalf("the index still tracks the moved source: %v", left)
	}
	after := trackedBlobs(t, root, mv.To)
	for rel, blob := range before {
		want := strings.Replace(rel, "docs/", ".context/", 1)
		if after[want] != blob {
			t.Fatalf("blob for %s did not land at %s (got %q, want %q)", rel, want, after[want], blob)
		}
	}
}

func TestResumeCompletesAJournaledCrash(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, _ := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	// Simulate a crash after the manifest was written and the first move ran.
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{
		From:      MigrationFrom{Schema: p.From.Schema, Stores: p.From.Stores, Archive: p.From.Archive},
		StartedAt: "2026-09-18T00:00:00Z", BackupTag: "pre-layout-v2-test",
		CreatedManifest: true, Phase: PhasePlanned, Moves: p.Moves,
	}
	if err := WriteManifest(root, m); err != nil {
		t.Fatal(err)
	}
	// The engine creates the destination root before its `git mv`; a move
	// simulated by hand has to create it too, or git refuses with ENOENT.
	mkdirAll(t, root, filepath.Dir(p.Moves[0].To))
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

// A journal left at `router` is the crash window between the router write and
// the active manifest (design §0.5's phase table): resume finishes it without
// redoing any move.
func TestResumeCompletesARouterPhaseJournal(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	for _, mv := range p.Moves {
		mkdirAll(t, root, filepath.Dir(mv.To))
		if out, err := repo.Git(root, "mv", mv.From, mv.To); err != nil {
			t.Fatalf("git mv: %v\n%s", err, out)
		}
	}
	writeFile(t, filepath.Join(root, "AGENTS.md"), V2AgentsMD)
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{
		From:      MigrationFrom{Schema: p.From.Schema, Stores: p.From.Stores, Archive: p.From.Archive},
		StartedAt: "2026-09-18T00:00:00Z", BackupTag: "pre-layout-v2-test",
		CreatedManifest: true, Phase: PhaseRouter, Moves: p.Moves,
	}
	if err := WriteManifest(root, m); err != nil {
		t.Fatal(err)
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
		Running:  "v0.6.0", RouterState: "current",
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
	if moved := trackedBlobs(t, root, ".context"); len(moved) != 0 {
		t.Fatalf("the crash before the first move still moved something: %v", moved)
	}
}

// The design §10 requirement "crash fixture at every move boundary" is this
// table: every move × every write boundary, every phase boundary, and one
// non-prefix case (TestResumeHandlesANonPrefixCrash).
//
// The boundaries are enumerated, never derived: both lists below are literals and
// the expected count is a third literal, so dropping a row -- from the list or
// from the loop that runs it -- fails this test instead of leaving a silent gap.
//
// The five instants are design §0.5's four, with its middle one split:
//
//	before-move    before `git mv` (instant 1)
//	after-move     after `git mv`, before the destination digest is verified (instant 2)
//	before-index   after that verification, before the index reconciliation
//	after-index    after the index agrees with the worktree, before the journal
//	               rewrite (instant 3 -- the seam inside `git mv` itself)
//	after-journal  after the journal records `done` (instant 4)
//
// The four phase rows bound the single idempotent action of each phase, and each
// fires while the journal still records the phase before it:
//
//	phase-moved   every move is done, before `moved` is recorded
//	phase-pruned  before the emptied source directories are removed
//	phase-router  before the v2 router is written (journal still `pruned`)
//	phase-active  with the router written and the journal at `router`, before it
//	              is retired as `active`
func TestResumeCrashMatrix(t *testing.T) {
	steps := []string{"before-move", "after-move", "before-index", "after-index", "after-journal"}
	phases := []string{"phase-moved", "phase-pruned", "phase-router", "phase-active"}
	moves := 4
	// 4 moves × 5 instants + 4 phase boundaries. Deliberately not computed from
	// the two lists above: that would make the assertion agree with whatever the
	// lists happen to say, which is how the previous version of this test could
	// not fail when a boundary was dropped.
	const wantBoundaries = 24
	covered := 0
	// ran records the rows that actually executed, so deleting a `t.Run` call
	// is caught as well as deleting an entry from a list.
	var ran []string

	run := func(t *testing.T, step string, moveIndex int) {
		t.Helper()
		ran = append(ran, fmt.Sprintf("%s/%d", step, moveIndex))
		root := newGitV1RepoWithContent(t)
		p, err := PlanMigration(root, MigrateOptions{
			Template: TemplateContentVault,
			Running:  "v0.6.0", RouterState: "current",
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
		t.Run(phase, func(t *testing.T) { run(t, phase, 0) })
	}
	if covered != wantBoundaries {
		t.Fatalf("crash matrix lists %d boundaries, want %d", covered, wantBoundaries)
	}
	if len(ran) != wantBoundaries {
		t.Fatalf("crash matrix ran %d boundaries, want %d: %v", len(ran), wantBoundaries, ran)
	}
}

// A later move already done while an earlier one is pending proves no "prefix of
// the list" assumption is baked into resume.
func TestResumeHandlesANonPrefixCrash(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	crashAt(t, "before-move", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	// The destination root is created by the engine before its `git mv`, so a
	// hand-simulated later move has to create it too: `git mv docs/qna
	// .context/qna` fails with ENOENT while .context/ does not exist.
	mkdirAll(t, root, filepath.Dir(p.Moves[3].To))
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
			Running:  "v0.6.0", RouterState: "current",
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
		Running:  "v0.6.0", RouterState: "current",
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

// The source is re-verified before anything moves (§9.3 step 3), so a store
// that changed between the plan and the apply is refused while it is still where
// the journal says it is -- the recovery is the restore, not a moved tree.
func TestApplyRefusesASourceThatChangedUnderTheMigration(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	writeFile(t, filepath.Join(root, p.Moves[0].From, "intruder.md"), "changed under the migration\n")
	err := ApplyMigration(root, p, "pre-layout-v2-test")
	if err == nil || !strings.Contains(err.Error(), "changed under the migration") {
		t.Fatalf("refusal = %v, want the pre-move digest mismatch", err)
	}
	var me *MoveError
	if !errors.As(err, &me) {
		t.Fatalf("refusal %v is not a *MoveError", err)
	}
	if me.Move.From != p.Moves[0].From {
		t.Fatalf("refusal names %s, want the unchanged store %s", me.Move.From, p.Moves[0].From)
	}
	if _, statErr := os.Lstat(filepath.Join(root, p.Moves[0].To)); !os.IsNotExist(statErr) {
		t.Fatal("a refused pre-move check must not have moved the tree")
	}
	if moved := trackedBlobs(t, root, ".context"); len(moved) != 0 {
		t.Fatalf("a refused apply moved blobs: %v", moved)
	}
}

// R1: every refusal names both paths and offers the non-destructive restore.
// The plan's own test asserts four substrings, which a remedy can satisfy while
// still leading with `rm -rf <destination>` -- data loss by instruction in the
// both-present and digest-mismatch rows, where the destination can hold real
// work. This test pins the shape of the remedy, not just its vocabulary.
func TestMoveRefusalRemedyNamesBothPathsAndDoesNotLeadWithADelete(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	crashAt(t, "after-move", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	// Put the source back, which is the copy case: both trees exist.
	gitOutput(t, root, "checkout", "HEAD", "--", p.Moves[0].From)
	err := ResumeMigration(root)
	if err == nil {
		t.Fatal("a copy must be refused")
	}
	var me *MoveError
	if !errors.As(err, &me) {
		t.Fatalf("refusal %v is not a *MoveError", err)
	}
	for _, want := range []string{me.Move.From, me.Move.To} {
		if !strings.Contains(me.Remedy, want) {
			t.Fatalf("remedy %q does not name %q", me.Remedy, want)
		}
	}
	restore := "git restore --source=pre-layout-v2-test -- " + me.Move.From
	if !strings.Contains(me.Remedy, restore) {
		t.Fatalf("remedy %q does not offer %q", me.Remedy, restore)
	}
	rm := strings.Index(me.Remedy, "rm -rf")
	if rm < 0 {
		t.Fatalf("remedy %q says nothing about removing a stray path", me.Remedy)
	}
	if i := strings.Index(me.Remedy, restore); i > rm {
		t.Fatalf("the remedy leads with the delete: %q", me.Remedy)
	}
	if !strings.Contains(me.Remedy[:rm], "already confirmed") {
		t.Fatalf("the delete is not fenced behind an explicit confirmation: %q", me.Remedy)
	}
	if tail := strings.TrimSuffix(me.Remedy, "`"); !strings.HasSuffix(tail, "agents layout migrate --resume --apply") {
		t.Fatalf("the remedy does not end with the retry: %q", me.Remedy)
	}
	// The same shape must hold on the lost row, where the restore is the whole
	// remedy and there is nothing to delete at all.
	lost := newGitV1RepoWithContent(t)
	lp := planV2(t, lost)
	crashAt(t, "before-move", 0)
	if err := ApplyMigration(lost, lp, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	if err := os.RemoveAll(filepath.Join(lost, lp.Moves[0].From)); err != nil {
		t.Fatal(err)
	}
	err = ResumeMigration(lost)
	if err == nil {
		t.Fatal("a lost source must be refused")
	}
	if !errors.As(err, &me) {
		t.Fatalf("refusal %v is not a *MoveError", err)
	}
	if !strings.Contains(me.Remedy, "git restore --source=pre-layout-v2-test -- "+me.Move.From) {
		t.Fatalf("lost row remedy %q does not offer the restore", me.Remedy)
	}
}

// R2: a migrated v2 repository must receive the manifest's linguist exception.
// scaffold writes it for a repository created as v2, and this migration is the
// only writer for one that becomes v2; without it `.agents/**` keeps the one
// file a reviewer has to read collapsed in every diff (design §10, Attributes).
func TestApplyAddsTheManifestLinguistExceptionOnce(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	if n := countGitattributeLine(t, root, manifestLinguistLine); n != 0 {
		t.Fatalf("a v1 repository already carries the manifest exception %d time(s)", n)
	}
	// Crash at the router boundary: the exception is written by then, and the
	// resume re-enters the same phase, which is the duplicate-append case.
	crashAt(t, "phase-router", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	if n := countGitattributeLine(t, root, manifestLinguistLine); n != 1 {
		t.Fatalf("the crashed reconcile wrote the exception %d time(s), want 1", n)
	}
	if err := ResumeMigration(root); err != nil {
		t.Fatal(err)
	}
	if n := countGitattributeLine(t, root, manifestLinguistLine); n != 1 {
		t.Fatalf("the resumed reconcile duplicated the manifest exception: %d line(s)", n)
	}
	if n := countGitattributeLine(t, root, ".agents/** linguist-generated=true"); n != 1 {
		t.Fatalf("the blanket .agents/ rule = %d line(s), want the one scaffold wrote", n)
	}
	// The exception must be effective, not merely present: gitattributes is
	// last-match-wins, and the blanket rule scaffold wrote above it would
	// otherwise keep the manifest hidden.
	if out := gitOutput(t, root, "check-attr", "linguist-generated", "--", ManifestRel); !strings.Contains(out, "unset") {
		t.Fatalf("git check-attr = %q, want the manifest unset (visible)", out)
	}
	if out := gitOutput(t, root, "check-attr", "linguist-generated", "--", ".agents/AGENTS.md"); !strings.Contains(out, "true") {
		t.Fatalf("git check-attr on .agents/AGENTS.md = %q, want the blanket rule still true", out)
	}
}

// A repository with no .gitattributes at all (a hand-built one, or one whose
// maintainers never needed the file) must still come out of the migration with
// the exception, as the only line: the append path creates the file when the
// read reports ENOENT, and it must not precede it with a stray blank line.
func TestApplyCreatesGitattributesWhenTheRepositoryHasNone(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	if err := os.Remove(filepath.Join(root, ".gitattributes")); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "drop .gitattributes")
	p := planV2(t, root)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err != nil {
		t.Fatal(err)
	}
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf(".gitattributes was not created: %v", err)
	}
	if string(attrs) != manifestLinguistLine+"\n" {
		t.Fatalf(".gitattributes = %q, want exactly the one manifest line", attrs)
	}
	if l := Resolve(root); l.LayoutStatus != StatusActive || l.Migration != nil {
		t.Fatalf("layout = %+v", l)
	}
}

// R3's third condition: abort is permitted only for a manifest this migration
// created. A `migrating` manifest that predates the run is the operator's record
// of someone else's half-finished migration, so deleting it would destroy the
// only pointer to a tag they may still need.
func TestAbortRefusesAManifestThisRunDidNotCreate(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{
		From:      MigrationFrom{Schema: p.From.Schema, Stores: p.From.Stores, Archive: p.From.Archive},
		StartedAt: "2026-09-18T00:00:00Z", BackupTag: "pre-layout-v2-test",
		CreatedManifest: false, Phase: PhasePlanned, Moves: p.Moves,
	}
	if err := WriteManifest(root, m); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	err := AbortMigration(root)
	if err == nil {
		t.Fatal("abort must refuse a manifest this run did not create")
	}
	for _, want := range []string{"phase", "did not create the manifest", "--resume --apply"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not name %q", err, want)
		}
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused abort changed the tree:\nbefore: %v\nafter:  %v", before, after)
	}
	if _, statErr := os.Stat(filepath.Join(root, ManifestRel)); statErr != nil {
		t.Fatalf("a refused abort must leave the manifest in place: %v", statErr)
	}
}

// R3: abort is the only delete, and it is limited to the state that proves
// nothing moved. It removes the manifest this run created, leaves the backup tag
// as the record, and touches no store.
func TestAbortDeletesOnlyTheManifestThisRunCreated(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	crashAt(t, "before-move", 0)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err == nil {
		t.Fatal("the injected crash must abort apply")
	}
	before := snapshotTree(t, root)
	indexBefore := gitOutput(t, root, "ls-files", "-s")
	storesBefore := trackedBlobs(t, root, "docs")
	if err := AbortMigration(root); err != nil {
		t.Fatal(err)
	}
	after := snapshotTree(t, root)
	delete(before, ManifestRel)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("abort touched more than the manifest:\nbefore: %v\nafter:  %v", before, after)
	}
	if !reflect.DeepEqual(storesBefore, trackedBlobs(t, root, "docs")) {
		t.Fatal("abort changed a store's index entries")
	}
	if got := gitOutput(t, root, "ls-files", "-s"); got != indexBefore {
		t.Fatalf("abort changed the index:\nbefore: %q\nafter:  %q", indexBefore, got)
	}
	if n := countGitattributeLine(t, root, manifestLinguistLine); n != 0 {
		t.Fatalf("a repository that ended the run as v1 carries the v2 exception: %d line(s)", n)
	}
	// A second abort has nothing to abort: the state it is limited to is gone.
	if err := AbortMigration(root); err == nil {
		t.Fatal("abort must refuse once the manifest is gone")
	}
}

func TestAbortIsLimitedToANothingMovedMigration(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
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
		Running:  "v0.6.0", RouterState: "current",
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
	if _, err := os.Stat(filepath.Join(root2, ManifestRel)); err != nil {
		t.Fatalf("a refused abort must leave the journal in place: %v", err)
	}
}

// The archive is never moved, rewritten, or walked (design §9.5). docs/ is not
// an empty shell while it holds the archive, so the migration leaves both where
// they are and reports no residue.
func TestApplyKeepsTheDeclaredArchiveWhereItIs(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old-plan.md"), "archived\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "archive")
	archiveBefore := trackedBlobs(t, root, "docs/archive")
	if len(archiveBefore) == 0 {
		t.Fatal("the fixture archive is not tracked")
	}
	p := planV2(t, root)
	if p.Archive != "docs/archive" {
		t.Fatalf("archive = %q, want the derived docs/archive", p.Archive)
	}
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err != nil {
		t.Fatal(err)
	}
	if got := trackedBlobs(t, root, "docs/archive"); !reflect.DeepEqual(got, archiveBefore) {
		t.Fatalf("the archive's blobs changed: %v -> %v", archiveBefore, got)
	}
	if out := gitOutput(t, root, "status", "--porcelain", "--", "docs/archive"); strings.TrimSpace(out) != "" {
		t.Fatalf("the archive was touched:\n%s", out)
	}
	l := Resolve(root)
	if l.LayoutStatus != StatusActive || l.Migration != nil || l.Archive != "docs/archive" {
		t.Fatalf("layout = %+v", l)
	}
}

// docs/ can gain an entry between the plan and the apply. The layout still
// completes, but the anti-shell promise does not: that is a named blocker and an
// advisory exit, never a silent success (design §9.5).
func TestApplyReportsDocsResidueAsANamedBlocker(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	writeFile(t, filepath.Join(root, "docs/notes.md"), "residue\n")
	err := ApplyMigration(root, p, "pre-layout-v2-test")
	var re *ResidueError
	if !errors.As(err, &re) {
		t.Fatalf("apply = %v, want a *ResidueError", err)
	}
	if len(re.Entries) != 1 || re.Entries[0] != "docs/notes.md" {
		t.Fatalf("residue = %v", re.Entries)
	}
	if !strings.Contains(err.Error(), "docs/notes.md") {
		t.Fatalf("refusal %q does not name the entry", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "docs/notes.md")); statErr != nil {
		t.Fatal("the residue must be reported, never deleted")
	}
	// The journal is gone: the moves and the router are complete, and only the
	// residue stands between this repository and a clean v2 layout.
	l := Resolve(root)
	if l.LayoutStatus != StatusActive || l.Migration != nil {
		t.Fatalf("layout = %+v", l)
	}
}

// R5: phases and per-move states are two frozen vocabularies (design §0.5), and
// the validator must accept exactly the four phases the planner and the engine
// write. Pinning both the constants' values and validPhase's answers is what
// makes a rename a deliberate schema change instead of a silent divergence
// between a literal in the validator and the constant the journal carries.
func TestMigrationVocabularyIsTheFourPhasesAndThreeStates(t *testing.T) {
	phaseValues := map[string]string{
		PhasePlanned: "planned",
		PhaseMoved:   "moved",
		PhasePruned:  "pruned",
		PhaseRouter:  "router",
	}
	if len(phaseValues) != 4 {
		t.Fatalf("the phase constants collide on a value: %v", phaseValues)
	}
	for constant, want := range phaseValues {
		if constant != want {
			t.Errorf("phase constant value = %q, want %q", constant, want)
		}
		if !validPhase(constant) {
			t.Errorf("validPhase(%q) = false: the validator rejects a phase the engine writes", constant)
		}
	}
	stateValues := map[string]string{
		MovePending: "pending",
		MoveMoving:  "moving",
		MoveDone:    "done",
	}
	if len(stateValues) != 3 {
		t.Fatalf("the move-state constants collide on a value: %v", stateValues)
	}
	for constant, want := range stateValues {
		if constant != want {
			t.Errorf("move-state constant value = %q, want %q", constant, want)
		}
		if validPhase(constant) {
			t.Errorf("validPhase(%q) = true: a per-move state is not a phase", constant)
		}
	}
	for _, notAPhase := range []string{"", "planned ", "Moved", "active", "migrating", "done"} {
		if validPhase(notAPhase) {
			t.Errorf("validPhase(%q) = true, want false", notAPhase)
		}
	}
}

// The engine writes only the frozen vocabulary, and it advances rather than
// jumps: a phase never goes backwards, and a move never returns to an earlier
// state. Every boundary of a complete apply is sampled through the crash seam.
func TestReconcileWritesOnlyTheFrozenVocabulary(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	ranks := map[string]int{PhasePlanned: 0, PhaseMoved: 1, PhasePruned: 2, PhaseRouter: 3}
	states := map[string]bool{}
	phases := map[string]bool{}
	samples := 0
	lastRank := 0
	reconcileHook = func(step string, moveIndex int) error {
		l := Resolve(root)
		if l.Migration == nil {
			return nil
		}
		rank, ok := ranks[l.Migration.Phase]
		if !ok {
			t.Errorf("the engine wrote phase %q, which is not in the vocabulary", l.Migration.Phase)
			return nil
		}
		if rank < lastRank {
			t.Errorf("phase went backwards: %s (%d) after rank %d", l.Migration.Phase, rank, lastRank)
		}
		lastRank = rank
		phases[l.Migration.Phase] = true
		for _, mv := range l.Migration.Moves {
			switch mv.State {
			case MovePending, MoveMoving, MoveDone:
				states[mv.State] = true
			default:
				t.Errorf("the engine wrote move state %q, which is not in the vocabulary", mv.State)
			}
		}
		samples++
		return nil
	}
	t.Cleanup(func() { reconcileHook = nil })
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err != nil {
		t.Fatal(err)
	}
	if samples == 0 {
		t.Fatal("no boundary was observed: the seam is not wired into the engine")
	}
	for _, want := range []string{PhasePlanned, PhaseMoved, PhasePruned} {
		if !phases[want] {
			t.Errorf("the engine never recorded phase %q", want)
		}
	}
	for _, want := range []string{MovePending, MoveMoving, MoveDone} {
		if !states[want] {
			t.Errorf("the engine never recorded move state %q", want)
		}
	}
}

// R4: the crash seam is a test-only knob. A complete apply runs with it nil and
// leaves it nil -- nothing on a production path arms it.
func TestApplyNeverArmsTheCrashSeam(t *testing.T) {
	if reconcileHook != nil {
		t.Fatal("another test left the crash seam armed")
	}
	root := newGitV1RepoWithContent(t)
	p := planV2(t, root)
	if err := ApplyMigration(root, p, "pre-layout-v2-test"); err != nil {
		t.Fatal(err)
	}
	if reconcileHook != nil {
		t.Fatal("a production path armed the crash seam")
	}
}
