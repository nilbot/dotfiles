package layout

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
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
// change, not even a temporary file. It is the whole promise of a dry run, so
// the proof is a whole-tree snapshot rather than an assertion about the paths
// the implementation happens to touch -- including for a plan that is refused.
func TestPlanMigrationWritesNothingAtAll(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	mkdirAll(t, root, "docs/archive/plans")
	writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old-plan.md"), "archived\n")
	writeFile(t, filepath.Join(root, "docs/notes.md"), "residue\n")
	before := snapshotTree(t, root)
	indexBefore := gitOutput(t, root, "status", "--porcelain", "--untracked-files=all")

	p, err := PlanMigration(root, MigrateOptions{
		Template: TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlocker(p.Blockers, "docs_residue") {
		t.Fatalf("the fixture must be refused so both outcomes are covered: %v", p.Blockers)
	}

	clean := newGitV1RepoWithContent(t)
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
	if p.Counts.Move != 4 || p.Counts.Blocked != 0 || p.Counts.Links != 0 {
		t.Fatalf("counts = %+v", p.Counts)
	}
	if p.DryRun != true || p.Phase != PhasePlanned || p.Repo != clean {
		t.Fatalf("plan header = %+v", p)
	}
	if after := snapshotTree(t, clean); !reflect.DeepEqual(beforeClean, after) {
		t.Fatalf("a planned migration changed the tree:\nbefore: %v\nafter:  %v", beforeClean, after)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused migration changed the tree:\nbefore: %v\nafter:  %v", before, after)
	}
	if after := gitOutput(t, root, "status", "--porcelain", "--untracked-files=all"); after != indexBefore {
		t.Fatalf("git state changed:\nbefore: %q\nafter:  %q", indexBefore, after)
	}
	for _, rel := range []string{ManifestRel, ".context", "notes"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("planner created %s", rel)
		}
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
// repository path: a URL, a mailto, and a bare fragment are not moved by a
// store migration. A title is part of the syntax, not of the target.
func TestPlanMigrationSkipsFencedExamplesAndNonPaths(t *testing.T) {
	root := newGitV1Repo(t)
	writeFile(t, filepath.Join(root, "docs/design/notes.md"), strings.Join([]string{
		`See [the plan](../plans/a-plan.md "the plan").`,
		"",
		"```md",
		"An example: [the plan](../plans/a-plan.md).",
		"```",
		"",
		"A [site](https://example.com/plans/a-plan.md), a [mail](mailto:t@example.com),",
		"an [anchor](#section), and an [escape](../../outside.md).",
		"",
	}, "\n"))
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
	writeFile(t, filepath.Join(root, "docs/design/README.md"), "changed\n")
	if got, _, _, err := digestTree(root, "docs/design"); err != nil || got == withLink {
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
