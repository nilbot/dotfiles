package drift

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// ---------------------------------------------------------------------------
// The end-to-end migration fixture (design §0.6, §9.5, §10)
//
// The link helpers below -- countMarkdownLinks, rewriteLinks, candidateFor,
// unresolvedLinks, and the two path spellings they need (within, storePath) --
// are a deliberate per-package duplication of the ones in
// internal/layout/migrate_test.go, where the plan's helper table puts them.
// Test helpers do not cross packages, and the production spellings they would
// otherwise need (fence, markdownLinks, markdownLink, resolveLinkTarget,
// repoDir, isMarkdown, pathPrefix, mapStorePath) are unexported in package
// layout, which is exactly the property that keeps them out of this package's
// reach. They are minimal on purpose: this fixture reads its own markdown, so
// they count `](...)` links outside fenced blocks, apply an Old->New mapping the
// way the migration skill does -- including the new repository-relative `New`
// form for a file that does not move -- and list relative targets that do not
// resolve. Everything else here is the v1 repository builder and the index
// reader the archive-immutability gate needs.
// ---------------------------------------------------------------------------

// newGitV1Repo is the fixture's repository: the layout scaffold.Create writes
// for a v1 repository -- the v1 router, both frozen v1 skill texts, and the
// four docs/ stores with their READMEs -- committed with a clean tree. It calls
// the writer directly rather than `agents init`, so no fleet entry is
// registered; XDG_STATE_HOME is redirected anyway, because a fixture that
// reached this machine's registry would judge the fleet against scratch
// repositories.
//
// The branch is `main`, from newRepo: the CLI refuses a protected branch, but
// PlanMigration and ApplyMigration ask git nothing about it -- that precondition
// belongs to the command, and this fixture is the library-level end-to-end.
func newGitV1Repo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	// The same isolation the layout migration fixtures use: git's background
	// maintenance must not race a tree snapshot, and a machine-wide
	// core.hooksPath must not run this machine's hooks during a fixture commit.
	gitIn(t, root, "config", "core.hooksPath", ".git/hooks")
	gitIn(t, root, "config", "maintenance.auto", "false")
	gitIn(t, root, "config", "gc.auto", "0")
	if err := scaffold.Create(root, false); err != nil {
		t.Fatal(err)
	}
	commitFixture(t, root, "v1 scaffold")
	if dirty := gitIn(t, root, "status", "--porcelain"); dirty != "" {
		t.Fatalf("v1 fixture is not committed clean:\n%s", dirty)
	}
	return root
}

// gitIn runs one git command in a fixture and fails the test on error.
func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, root, err, out)
	}
	return string(out)
}

// commitFixture stages everything and commits it, so the planner meets a clean
// tree of the kind design §9.1's preconditions describe.
func commitFixture(t *testing.T, root, message string) {
	t.Helper()
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-m", message)
}

// trackedBlobs maps every index entry to its mode and blob oid, as
// `git ls-files -s` reports them. Two readings of it are the archive gate of
// design §9.3: a blob oid is content-addressed, so an unchanged oid under an
// unchanged path is the same bytes in the same place, and the mode is read too
// because a chmod is a change to a place the repository calls immutable.
func trackedBlobs(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(gitIn(t, root, "ls-files", "-s"), "\n") {
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
		out[filepath.ToSlash(rel)] = fields[0] + " " + fields[1]
	}
	return out
}

// archiveBlobs narrows a trackedBlobs reading to docs/archive/, so comparing
// two of them catches a file that appeared in the archive or vanished from it
// as well as one whose bytes or mode changed.
func archiveBlobs(blobs map[string]string) map[string]string {
	out := map[string]string{}
	for rel, id := range blobs {
		if withinArchive(rel) {
			out[rel] = id
		}
	}
	return out
}

// withinArchive reports whether a repository-relative path is the archive or
// lives inside it.
func withinArchive(rel string) bool {
	return rel == "docs/archive" || strings.HasPrefix(rel, "docs/archive/")
}

// within reports whether a repository-relative path is a directory or lives
// inside it. It is the fixture's copy of the production pathPrefix, which is
// unexported in package layout: the two must agree, or the fixture would judge a
// tree the planner described by different rules.
func within(parent, child string) bool {
	parent = strings.TrimSuffix(path.Clean(parent), "/")
	child = strings.TrimSuffix(path.Clean(child), "/")
	if parent == "." {
		return true
	}
	return parent == child || strings.HasPrefix(child, parent+"/")
}

// storePath re-spells a path that lives under a moving store with that store's
// new root; it mirrors the production mapStorePath, and is what turns a
// candidate's plan-time path into the path it has after the move.
func storePath(from, to, rel string) string {
	from = strings.TrimSuffix(path.Clean(from), "/")
	to = strings.TrimSuffix(path.Clean(to), "/")
	rel = strings.TrimSuffix(path.Clean(rel), "/")
	if rel == from {
		return to
	}
	return to + strings.TrimPrefix(rel, from)
}

// inlineLink matches one inline markdown link's target -- the `](target)` form
// the planner reports. Angle-bracketed and titled forms are not what this
// fixture writes, and every reading below uses this one scanner on both sides
// of the migration, so two counts cannot disagree about what they counted.
var inlineLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// isMarkdown reports whether a file name is markdown, the only prose a link
// report reads.
func isMarkdown(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

// linkFence keeps fenced code blocks out of the readings below: a link shown as
// an example is not a link, and counting or rewriting one would make the
// N-in/N-out gate compare two different things.
type linkFence struct{ marker string }

func (f *linkFence) content(line string) bool {
	trimmed := strings.TrimSpace(line)
	if f.marker != "" {
		if strings.HasPrefix(trimmed, f.marker) {
			f.marker = ""
		}
		return false
	}
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			f.marker = marker
			return false
		}
	}
	return true
}

// resolveLinkTarget names the repository path a markdown target points at,
// resolved against the directory of the file that holds it. A URL, an absolute
// path, and a bare fragment name no repository path and are not candidates.
func resolveLinkTarget(dir, target string) (string, bool) {
	if target == "" || strings.HasPrefix(target, "#") || filepath.IsAbs(target) {
		return "", false
	}
	switch {
	case strings.HasPrefix(target, "http://"),
		strings.HasPrefix(target, "https://"),
		strings.HasPrefix(target, "mailto:"):
		return "", false
	}
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return "", false
	}
	return path.Join(dir, target), true
}

// markdownFiles lists the markdown under one repository-relative directory,
// sorted: the file set every reading below walks. The argument may be a single
// file, and "." is the whole repository except .git, which is what lets a
// reading cover tracked prose outside every store -- a root README -- as well as
// the stores themselves.
func markdownFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)),
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			if !isMarkdown(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
	if err != nil {
		t.Fatalf("walking %s in %s: %v", dir, root, err)
	}
	sort.Strings(out)
	return out
}

// countMarkdownLinks counts `](target)` links outside fenced code blocks under
// one directory tree. It is half of design §9.5's N-in/N-out gate: the fixture
// counts the links the v1 stores hold before the migration and the links the v2
// stores hold after it, and the two counts must be equal.
func countMarkdownLinks(t *testing.T, root, dir string) int {
	t.Helper()
	n := 0
	for _, rel := range markdownFiles(t, root, dir) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		var f linkFence
		for _, line := range strings.Split(string(data), "\n") {
			if f.content(line) {
				n += len(inlineLink.FindAllString(line, -1))
			}
		}
	}
	return n
}

// rewriteLinks applies the plan's Old -> New mapping the way the
// migrating-fleet-context skill does: it walks the repository after the moves
// and writes each reported candidate's New spelling into the link the plan
// reported, wherever that file now lives.
//
// The spelling written back depends on where the containing file ended up, which
// is why the moves are passed in. A file inside a move source carries New already
// spelled from its new directory, so New is written verbatim; a file that did
// not move takes New as the target's repository-relative path and re-spells it
// from the directory the file still lives in. Matching goes through the
// plan-time path of the file being written, because the migration has since
// renamed the file that held the link. The archive is the fixture's own concern
// here: no file under it is ever scanned, so no candidate is reported there, and
// the fixture asserts its blobs are identical afterwards.
func rewriteLinks(t *testing.T, root string, moves []layout.Move, candidates []layout.LinkCandidate) {
	t.Helper()
	if len(candidates) == 0 {
		return
	}
	// planTime reverses the moves for one post-move repository path and reports
	// whether the file that now holds it is one of the moved files.
	planTime := func(rel string) (string, bool) {
		for _, m := range moves {
			if within(m.To, rel) {
				return storePath(m.To, m.From, rel), true
			}
		}
		return rel, false
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		rel = filepath.ToSlash(rel)
		planFile, moved := planTime(rel)
		dir := path.Dir(rel)
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		var f linkFence
		changed := false
		for i, line := range lines {
			if !f.content(line) {
				continue
			}
			out := inlineLink.ReplaceAllStringFunc(line, func(match string) string {
				ix := inlineLink.FindStringSubmatchIndex(match)
				target := match[ix[2]:ix[3]]
				resolved, ok := resolveLinkTarget(dir, target)
				if !ok {
					return match
				}
				c, ok := candidateFor(candidates, planFile, dir, resolved, target)
				if !ok {
					return match
				}
				spelling := c.New
				if !moved {
					rel, err := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(c.New))
					if err != nil {
						t.Fatalf("spelling %s from %s: %v", c.New, dir, err)
					}
					spelling = filepath.ToSlash(rel)
				}
				return match[:ix[2]] + spelling + match[ix[3]:]
			})
			if out != line {
				lines[i] = out
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

// candidateFor reports the mapping a link in one file belongs to. A candidate is
// a property of a file and a spelling, and both rules are scoped to the file the
// planner read it in: the link is spelled exactly the way the plan recorded it
// (Old is a spelling, so it means nothing beside its own file), or the link
// already resolves, from that file's directory, to wherever New points.
//
// The first rule is the one that survives when the containing file and its
// target land under different roots: there the moved link resolves to a path
// that existed in neither layout, so resolution alone cannot recognise it. The
// second makes a second application of the mapping change nothing.
func candidateFor(candidates []layout.LinkCandidate, file, dir, resolved, target string) (layout.LinkCandidate, bool) {
	for _, c := range candidates {
		if c.File == file && target == c.Old {
			return c, true
		}
	}
	for _, c := range candidates {
		if c.File != file {
			continue
		}
		if newPath, ok := resolveLinkTarget(dir, c.New); ok && newPath == resolved {
			return c, true
		}
	}
	return layout.LinkCandidate{}, false
}

// unresolvedLinks lists every relative link target under one directory tree
// that does not resolve to an existing path, as `file:line -> target`. It is
// the other half of design §9.5's gate: after the rewrite, nothing may dangle.
func unresolvedLinks(t *testing.T, root, dir string) []string {
	t.Helper()
	var out []string
	for _, rel := range markdownFiles(t, root, dir) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		fileDir := path.Dir(rel)
		var f linkFence
		for i, line := range strings.Split(string(data), "\n") {
			if !f.content(line) {
				continue
			}
			for _, match := range inlineLink.FindAllStringSubmatch(line, -1) {
				resolved, ok := resolveLinkTarget(fileDir, match[1])
				if !ok {
					continue
				}
				if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(resolved))); err != nil {
					out = append(out, fmt.Sprintf("%s:%d -> %s", rel, i+1, match[1]))
				}
			}
		}
	}
	return out
}

// assertDocsHoldsOnlyTheArchive proves docs/ is not a shell after a migration
// that declared an archive: the four v1 stores are gone, and every path left
// under docs/ is inside the archive the migration kept in place.
func assertDocsHoldsOnlyTheArchive(t *testing.T, root string) {
	t.Helper()
	var left []string
	err := filepath.WalkDir(filepath.Join(root, "docs"),
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel != "docs" && !withinArchive(rel) {
				left = append(left, rel)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("docs/ did not survive as the archive's parent: %v", err)
	}
	if len(left) > 0 {
		t.Fatalf("docs/ is a shell after the migration: %v", left)
	}
}

// TestMigrationFixtureLinksAndArchiveSurvive is the end-to-end gate of design
// §0.6, §9.3, §9.5, and §10: a real v1 repository carrying nested content,
// markdown links of every class the amended scan reports, and a content-mixed
// archive goes through the whole migration -- plan, apply with `git mv`, and the
// skill's link rewrite -- and every promise the design makes about what survives
// is checked against the bytes on disk afterwards.
//
// The link classes are the ones design §9.2 (amended 2026-09-19) names: a link
// from one moved store into another, a link from a moved store into the archive
// the migration keeps in place, and a link in tracked prose that never moves -- a
// root README -- pointing into a store that does. The last two were reported by
// nothing while the scan was source-scoped, so the old fixture passed over a
// migration that left both dangling.
//
// It lives in package drift, not in package layout where the plan put it. The
// direction is what decides it: drift imports layout, so an in-package layout
// test calling drift.InspectRepo would be the import cycle Go rejects.
func TestMigrationFixtureLinksAndArchiveSurvive(t *testing.T) {
	root := newGitV1Repo(t)
	writeFile(t, filepath.Join(root, "docs/archive/plans/2020-old-plan.md"), "archived plan")
	writeFile(t, filepath.Join(root, "docs/archive/2020-old-design.md"), "archived design")
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"), strings.Join([]string{
		"See [the plan](../plans/a-plan.md).",
		"",
		"See [the old design](../archive/2020-old-design.md).",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
	writeFile(t, filepath.Join(root, "README.md"), "See [the plan](docs/plans/a-plan.md).\n")
	commitFixture(t, root, "content, links, and a mixed archive")

	archiveBefore := archiveBlobs(trackedBlobs(t, root))
	// Whole-repository, not just the stores: the prose that never moves is part
	// of the gate now, and a count that skipped it could not see the README
	// link at all.
	linksBefore := countMarkdownLinks(t, root, ".")
	if linksBefore == 0 {
		t.Fatal("the v1 fixture holds no markdown link: the N-in/N-out gate would be vacuous")
	}

	// drift's own answer, before anything moves. The archive is a place, not a
	// topic (design §0.6): a `*-plan.md` and a `*-design.md` inside it are
	// neither classified nor reported, because a report is an instruction to
	// move a file that must never move.
	rep, err := InspectRepo(root, "v0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.MisplacedDocs {
		if strings.HasPrefix(m, "docs/archive/") {
			t.Fatalf("archive file reported as misplaced: %s", m)
		}
	}
	if len(rep.MisplacedDocs) != 0 {
		t.Fatalf("the v1 fixture is not drift-clean: misplaced = %v", rep.MisplacedDocs)
	}
	if rep.LayoutVersion != "v1" {
		t.Fatalf("layout version = %q, want v1", rep.LayoutVersion)
	}
	if !IsCurrent(rep) {
		t.Fatalf("the v1 fixture is not current before the migration: %+v", rep)
	}

	p, err := layout.PlanMigration(root, layout.MigrateOptions{
		Template: layout.TemplateContentVault,
		Running:  "v0.6.0", RouterState: "current",
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan = (%+v, %v)", p, err)
	}
	if p.Archive != "docs/archive" {
		t.Fatalf("plan archive = %q, want docs/archive", p.Archive)
	}
	if len(p.Moves) != 4 {
		t.Fatalf("moves = %+v, want the four stores", p.Moves)
	}
	for _, m := range p.Moves {
		if withinArchive(m.From) || withinArchive(m.To) {
			t.Fatalf("the archive was planned as a move: %+v", m)
		}
	}
	if len(p.LinkCandidates) == 0 {
		t.Fatal("no link candidate reported: the rewrite gate would be vacuous")
	}
	// Every class, named: the store-to-store link, the archive-bound link, and
	// the link in prose that does not move. `Old` is the link as written in its
	// own file; `New` is the spelling that resolves after the migration, which
	// for a moved file is relative to its new directory and for a file that
	// stays put is the target's new repository-relative path.
	wantLinks := []layout.LinkCandidate{
		{File: "README.md", Line: 1,
			Old: "docs/plans/a-plan.md", New: ".context/plans/a-plan.md"},
		{File: "docs/design/a-design.md", Line: 1,
			Old: "../plans/a-plan.md", New: "../plans/a-plan.md"},
		{File: "docs/design/a-design.md", Line: 3,
			Old: "../archive/2020-old-design.md", New: "../../docs/archive/2020-old-design.md"},
	}
	if !reflect.DeepEqual(p.LinkCandidates, wantLinks) {
		t.Fatalf("candidates = %+v, want %+v", p.LinkCandidates, wantLinks)
	}

	if err := layout.ApplyMigration(root, p, "pre-layout-v2-fixture"); err != nil {
		t.Fatal(err)
	}
	rewriteLinks(t, root, p.Moves, p.LinkCandidates)

	// The archive is never moved and its blobs are byte-identical. The
	// comparison covers the whole archive subtree, so a file that appeared in
	// it, vanished from it, or changed mode fails as well as one that changed
	// content.
	archiveAfter := archiveBlobs(trackedBlobs(t, root))
	if !reflect.DeepEqual(archiveAfter, archiveBefore) {
		t.Fatalf("archive changed:\nbefore = %v\nafter  = %v", archiveBefore, archiveAfter)
	}
	for _, archived := range []string{
		"docs/archive/plans/2020-old-plan.md",
		"docs/archive/2020-old-design.md",
	} {
		if got := archiveAfter[archived]; got == "" {
			t.Fatalf("archive blob changed or moved: %s", archived)
		} else if got != archiveBefore[archived] {
			t.Fatalf("archive blob changed or moved: %s", archived)
		}
	}
	// The index comparison would not notice a worktree deletion, so ask git
	// whether what is on disk under the archive still matches what it recorded.
	if dirty := gitIn(t, root, "status", "--porcelain", "--", "docs/archive"); dirty != "" {
		t.Fatalf("the archive's worktree no longer matches the index:\n%s", dirty)
	}

	// docs/ is not a shell: every v1 store is gone, and what is left under
	// docs/ is exactly the archive this migration declared and kept (design
	// §9.5's counter-case is the subtest below, where no archive is declared).
	assertDocsHoldsOnlyTheArchive(t, root)

	// N in / N out over the whole repository, and none of them dangling -- the
	// store-to-store link, the archive-bound link, and the README link alike.
	if got := countMarkdownLinks(t, root, "."); got != linksBefore {
		t.Fatalf("links in = %d, links out = %d", linksBefore, got)
	}
	if bad := unresolvedLinks(t, root, "."); len(bad) > 0 {
		t.Fatalf("unresolved links after rewrite: %v", bad)
	}
	// The two fixed classes are checked by name as well as by resolution: a
	// rewrite that had left them spelled as they were would keep the counts
	// equal and still dangle.
	design, err := os.ReadFile(filepath.Join(root, ".context/design/a-design.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(design), "(../plans/a-plan.md)") {
		t.Fatalf("the store-to-store link was not re-spelled:\n%s", design)
	}
	if !strings.Contains(string(design), "(../../docs/archive/2020-old-design.md)") {
		t.Fatalf("the link into the kept archive was not re-spelled from the new directory:\n%s", design)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "(.context/plans/a-plan.md)") {
		t.Fatalf("the root README was not re-spelled onto the moved store:\n%s", readme)
	}
	// The archive is a place, not a rewrite target: no file under it was edited
	// (the blob comparison above proves it), and the report never named one.
	for _, c := range p.LinkCandidates {
		if withinArchive(c.File) {
			t.Fatalf("a link inside the archive was reported: %+v", c)
		}
	}

	// `agents layout validate`'s equivalent, in process: the migrated layout
	// resolves, has no problems, and this binary supports it.
	l := layout.Resolve(root)
	if l.Schema != layout.SchemaV2 {
		t.Fatalf("resolved schema = %q, want %q", l.Schema, layout.SchemaV2)
	}
	if len(l.Problems) > 0 {
		t.Fatalf("migrated layout problems = %+v", l.Problems)
	}
	if ok, reason := layout.Support("v0.6.0", l); !ok {
		t.Fatalf("migrated layout unsupported: %s", reason)
	}
	if l.Archive != "docs/archive" {
		t.Fatalf("manifest archive = %q, want docs/archive", l.Archive)
	}

	// §9.5's anti-shell promise, which the archive fixture above cannot show:
	// there the archive is exactly what keeps docs/ alive. The same migration on
	// a v1 repository with no docs/archive/ leaves no docs/ at all.
	t.Run("docs_is_gone_without_an_archive", func(t *testing.T) {
		root := newGitV1Repo(t)
		writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# plan\n")
		commitFixture(t, root, "content")

		p, err := layout.PlanMigration(root, layout.MigrateOptions{
			Template: layout.TemplateContentVault,
			Running:  "v0.6.0", RouterState: "current",
		})
		if err != nil || len(p.Blockers) > 0 {
			t.Fatalf("plan = (%+v, %v)", p, err)
		}
		if p.Archive != "" {
			t.Fatalf("archive = %q, want none", p.Archive)
		}
		if err := layout.ApplyMigration(root, p, "pre-layout-v2-no-archive"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
			t.Fatalf("docs/ survived a migration that declared no archive: %v", err)
		}
		l := layout.Resolve(root)
		if len(l.Problems) > 0 {
			t.Fatalf("migrated layout problems = %+v", l.Problems)
		}
		if ok, reason := layout.Support("v0.6.0", l); !ok {
			t.Fatalf("migrated layout unsupported: %s", reason)
		}
	})
}
