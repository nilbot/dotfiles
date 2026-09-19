package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
		common, err := gitPath(root, "--git-common-dir")
		if err != nil {
			t.Fatalf("resolve the common git directory of %s: %v", root, err)
		}
		target = filepath.Join(common, "info", "exclude")
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

// Each rule is asked about both paths, because neither query alone covers the
// spellings. Measured with git: `.agents/`, `.agents`, and `/.agents` match the
// directory; `/.agents/**` matches the manifest path but NOT the directory.
func TestIsIgnoredMatchesEverySpellingOnThePathItMatches(t *testing.T) {
	cases := []struct {
		name, pattern string
		file, dir     bool
	}{
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

// The classification reads git's own refusal, which git translates when message
// catalogs and a non-C locale are in force. runExit forces the C locale for
// that reason; the caller's locale must not turn "not a repository" into an
// operational error that V16 then refuses.
func TestTrackednessOutsideARepositorySurvivesATranslatedLocale(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")
	if _, err := IsIgnored(t.TempDir(), ".agents/layout.json"); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("IsIgnored outside a repository under a German locale = %v, want ErrNotARepo", err)
	}
	if _, err := IsTracked(t.TempDir(), ".agents/layout.json"); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("IsTracked outside a repository under a German locale = %v, want ErrNotARepo", err)
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
//
// The failure must also not classify as ErrNotARepo: that is the one error V16
// skips, so a broken repository reported as an absent one would read as "there
// is nothing here to check".
func TestIsTrackedFailsClosedOnAGitError(t *testing.T) {
	root := newTempGitRepo(t)
	writeFile(t, filepath.Join(root, ".agents/layout.json"), "{}")
	gitOutput(t, root, "add", "-A")
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := IsTracked(root, ".agents/layout.json")
	if err == nil {
		t.Fatal("a git error must surface as an error, not as a trackedness answer")
	}
	if errors.Is(err, ErrNotARepo) {
		t.Fatalf("a corrupt index is not a missing repository: %v", err)
	}
	if !strings.Contains(err.Error(), "index") {
		t.Fatalf("the failure must carry git's own message: %v", err)
	}
}

// The same switch for the ignore question, with a failure that is not a missing
// repository: a broken .git/config fails check-ignore with exit 128 and a
// message that is not git's not-a-repository refusal.
func TestIsIgnoredFailsClosedOnAGitError(t *testing.T) {
	root := newTempGitRepo(t)
	writeFile(t, filepath.Join(root, ".git", "config"), "not a config\n")
	_, err := IsIgnored(root, ".agents/layout.json")
	if err == nil {
		t.Fatal("a git error must surface as an error, not as a trackedness answer")
	}
	if errors.Is(err, ErrNotARepo) {
		t.Fatalf("a broken git config is not a missing repository: %v", err)
	}
	if !strings.Contains(err.Error(), ".git/config") {
		t.Fatalf("the failure must carry git's own message: %v", err)
	}
}
