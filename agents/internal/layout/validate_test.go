package layout

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// The layout package owns its own copies of the trackedness helpers: Go test
// helpers cannot cross a package boundary, and `repo` cannot import `layout`
// without an import cycle. The repo package defines the same helpers, plus
// gitOutput, for its own tests.

// newTempGitRepo is a temp git repository with an empty .agents/ and no commits.
func newTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "agents-test"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ignoreAgents appends one ignore rule to the file that spelling belongs in: a
// "/"-prefixed pattern is machine-local and goes to info/exclude in the common
// directory, anything else is the repository's tracked .gitignore.
func ignoreAgents(t *testing.T, root, pattern string) {
	t.Helper()
	target := filepath.Join(root, ".gitignore")
	if strings.HasPrefix(pattern, "/") {
		exclude, err := repo.InfoExcludePath(root)
		if err != nil {
			t.Fatalf("resolve info/exclude for %s: %v", root, err)
		}
		target = exclude
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(pattern + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
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

// V16 through the whole validator, in both directions: a real repository whose
// .agents/ is ignored is refused, and a plain fixture directory is not — there
// is nothing to commit outside a repository, so trackedness cannot fail a
// fixture the way it fails a repository. The rule is the glob spelling on
// purpose: it is invisible to the directory query, so only the union catches it.
func TestValidateReportsAnIgnoredManifestAndSkipsAPlainDirectory(t *testing.T) {
	root := newTempGitRepo(t)
	ignoreAgents(t, root, "/.agents/**")
	writeFile(t, filepath.Join(root, ".agents/layout.json"), "{}")
	l := mk(storesWith(RoleDesign, "context/design"))
	if got := Validate(root, l); !hasProblem(got, ProblemLocalAgents) {
		t.Fatalf("problems = %v, want %s", got, ProblemLocalAgents)
	}
	if got := Validate(t.TempDir(), l); hasProblem(got, ProblemLocalAgents) {
		t.Fatalf("a plain fixture directory must stay valid: %v", got)
	}
}

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

// Validate is only reachable for a manifest if Resolve calls it: a v2 layout
// that reads clean from Resolve would otherwise carry none of V1–V16, and every
// caller that mutates a repository goes through Resolve.
func TestResolveCarriesTheValidationProblemsOfTheManifestItRead(t *testing.T) {
	root := newTempGitRepo(t)
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": ".agents/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}`)
	l := Resolve(root)
	if !hasProblem(l.Problems, ProblemPathInAgents) {
		t.Fatalf("problems = %v, want %s", l.Problems, ProblemPathInAgents)
	}
}

// The rule each problem code stands for, for the codes the path table above
// does not reach. Every code in design §5.2 is asserted somewhere in this
// package: role_duplicate by TestResolveRejectsDuplicateStoreKeys, the rest
// here or above.
func TestValidateCoversTheRemainingProblemCodes(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Layout)
		code string
	}{
		{"missing-role", func(l *Layout) { l.Stores = map[string]string{RoleDesign: "context/design"} }, ProblemRoleMissing},
		{"empty-path", func(l *Layout) { l.Stores[RoleDesign] = "" }, ProblemPathEmpty},
		{"archive-overlap", func(l *Layout) { l.Archive = "context/design" }, ProblemArchiveOverlap},
		{"unknown-status", func(l *Layout) { l.LayoutStatus = "paused" }, ProblemStatusUnknown},
		{"journal-missing", func(l *Layout) { l.LayoutStatus = StatusMigrating }, ProblemMigration},
		{"floor-not-a-version", func(l *Layout) { l.MinMutVerFloor = "0.6" }, ProblemMinMutVerFloor},
		{"floor-required", func(l *Layout) { l.MinMutVerFloor = "" }, ProblemMinMutVerFloor},
	}
	root := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := mk(storesWith(RoleDesign, "context/design"))
			tc.edit(&l)
			if got := Validate(root, l); !hasProblem(got, tc.code) {
				t.Fatalf("problems = %v, want %s", got, tc.code)
			}
		})
	}

	// V4 with the trailing-slash spelling of the same path, repeated: map
	// iteration order decides which of the two roles is compared first, and the
	// rule ("two roles resolve to the same path") must not depend on that order.
	trailing := mk(storesWith(RoleDesign, "context/"))
	trailing.Stores[RolePlans] = "context"
	for i := 0; i < 32; i++ {
		if got := Validate(root, trailing); !hasProblem(got, ProblemDuplicatePath) {
			t.Fatalf("iteration %d: problems = %v, want %s", i, got, ProblemDuplicatePath)
		}
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
