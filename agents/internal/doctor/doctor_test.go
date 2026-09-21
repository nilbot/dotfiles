package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/harness"
)

// These tests replace the ones that exercised the trace store, the layout
// manifest and the four `scaffold:*` checks. Those subjects are gone; what
// remains is three questions this package still answers, and each gets a test
// because each was rewritten rather than merely deleted.

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A repository with no generated config is the target state now, so the check
// must be OK rather than a failure with `agents wire` as its remedy. This is
// the inversion the refactor turned on, and a check that still demanded the
// config would report every correct repository as broken.
func TestWiringIsOKWhenThereIsNoConfig(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("claude-code")
	got := checkWiring(a, root)
	if got.Status != OK {
		t.Errorf("missing config = %q, want %q: %s", got.Status, OK, got.Detail)
	}
}

// A config with no entry of ours is likewise OK. The old check failed here --
// it wanted one entry per event.
func TestWiringIsOKWhenNoEntryIsOurs(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("claude-code")
	writeJSON(t, a.WireConfigPath(root), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/my/own/audit.sh"}]}]}}`)
	got := checkWiring(a, root)
	if got.Status != OK {
		t.Errorf("foreign-only config = %q, want %q: %s", got.Status, OK, got.Detail)
	}
}

// The failure this check exists for: an entry an earlier version wrote, calling
// a subcommand this binary no longer answers. Left in place, the harness runs
// it at the start of every session and fails.
func TestWiringFailsOnARetiredEntry(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("claude-code")
	writeJSON(t, a.WireConfigPath(root), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}]}}`)
	got := checkWiring(a, root)
	if got.Status != Fail {
		t.Fatalf("a retired entry = %q, want %q: %s", got.Status, Fail, got.Detail)
	}
	if got.Remedy == "" {
		t.Error("a refusal that does not say what to do is a dead end")
	}
}

// Antigravity keeps its entries under a named group rather than per event, so
// the walk is different code and needs its own case.
func TestWiringFindsARetiredAntigravityGroup(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("antigravity")
	writeJSON(t, a.WireConfigPath(root), `{"agents":{"Stop":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness antigravity"}]}}`)
	if got := checkWiring(a, root); got.Status != Fail {
		t.Errorf("retired antigravity group = %q, want %q: %s", got.Status, Fail, got.Detail)
	}
}

// A malformed config is still a fault: it is a file this tool either wrote or
// will need to write, and silently ignoring it would hide that.
func TestWiringFailsOnMalformedConfig(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("claude-code")
	writeJSON(t, a.WireConfigPath(root), "{not json")
	if got := checkWiring(a, root); got.Status != Fail {
		t.Errorf("malformed config = %q, want %q: %s", got.Status, Fail, got.Detail)
	}
}

// The skill is written once and then belongs to the repository: a local edit is
// the tool being used as intended, so it must be reported without a warning.
// Only a missing skill is a gap.
func TestSkillsReportsMissingCustomizedAndPresent(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		got := checkSkills(t.TempDir())
		if len(got) != 1 || got[0].Status != Warn {
			t.Fatalf("missing skill = %+v, want one Warn", got)
		}
		if got[0].Remedy == "" {
			t.Error("a missing skill needs a remedy")
		}
	})

	t.Run("customized", func(t *testing.T) {
		root := t.TempDir()
		writeJSON(t, filepath.Join(root, ".agents", "skills", "recording-what-you-learn", "SKILL.md"), "# my own version\n")
		got := checkSkills(root)
		if len(got) != 1 || got[0].Status != OK {
			t.Fatalf("customized skill = %+v, want one OK; a repository's own edits are not a fault", got)
		}
	})
}

// The freshness check reads one fixed store path. An absent store is not
// applicable rather than a failure, because most repositories have not adopted
// it and a check that fails everywhere teaches people to ignore the report.
func TestQNAFreshnessTreatsAnAbsentStoreAsNotAFailure(t *testing.T) {
	got := checkQNAFreshness(t.TempDir(), time.Now())
	if got.Status != OK {
		t.Errorf("absent store = %q, want %q: %s", got.Status, OK, got.Detail)
	}
}

func TestQNAFreshnessReadsTheNewestEntry(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "docs", "qna", "an-entry.md"), "# an entry\n")
	got := checkQNAFreshness(root, time.Now())
	if got.Status != OK {
		t.Errorf("store with an entry = %q, want %q: %s", got.Status, OK, got.Detail)
	}
}

// The rendered report carries a status and a name per check, and the caller
// keys off both. A check with neither would render as a blank line.
func TestRunWithDepsNamesEveryCheck(t *testing.T) {
	root := t.TempDir()
	checks, err := RunWithDeps(root, "/nonexistent/agents", DependenciesFor(""))
	if err != nil {
		t.Fatalf("RunWithDeps: %v", err)
	}
	if len(checks) == 0 {
		t.Fatal("RunWithDeps returned no checks; the report would be empty")
	}
	for _, c := range checks {
		if c.Name == "" {
			t.Errorf("a check has no name: %+v", c)
		}
		if c.Status == "" {
			t.Errorf("%s has no status", c.Name)
		}
		if c.Status != OK && c.Status != Warn && c.Status != Fail {
			t.Errorf("%s has status %q, which is not one of the three", c.Name, c.Status)
		}
	}
	// Every harness must appear, because a harness missing from the report is
	// the failure mode this check replaced.
	for _, a := range harness.All() {
		found := false
		for _, c := range checks {
			if c.Name == "wiring:"+a.Name() {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no wiring check in the report", a.Name())
		}
	}
}

// A check's JSON rendering must survive the round trip the report promises;
// this guards the shape rather than any one field.
func TestRunWithDepsIsObservationalOnly(t *testing.T) {
	root := t.TempDir()
	before, _ := os.ReadDir(root)
	_, err := RunWithDeps(root, "/nonexistent/agents", DependenciesFor(""))
	if err != nil {
		t.Fatalf("RunWithDeps: %v", err)
	}
	after, _ := os.ReadDir(root)
	if len(before) != len(after) {
		t.Errorf("doctor created %d entries in the repository it inspected", len(after)-len(before))
	}
}

// The lookalike branch exists because this repository really did leave entries
// behind in the generated shape under a binary this tool does not own: an
// ephemeral `agents.test` binary wired it, `wire` could not remove them -- its
// delete predicate requires the binary to be this tool -- and doctor reported
// the wiring exact while the harness ran the failing hooks at every session
// start. The failure this branch detects is therefore "an entry that looks like
// ours but is not", and the property that matters is the remedy: telling the
// operator to run `agents wire` prints a command that provably does nothing.
func TestWiringWarnsOnALookalikeUnderAnotherBinary(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("claude-code")
	const lookalike = "/opt/agents.test hook stop --harness claude-code"
	writeJSON(t, a.WireConfigPath(root),
		`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+lookalike+`"}]}]}}`)

	got := checkWiring(a, root)
	if got.Status != Warn {
		t.Errorf("a lookalike entry = %q, want %q: %s", got.Status, Warn, got.Detail)
	}
	if strings.Contains(got.Remedy, "agents wire") {
		t.Errorf("the remedy sends the operator to `agents wire`, which cannot delete this entry: %s", got.Remedy)
	}
	if !strings.Contains(got.Detail, lookalike) {
		t.Errorf("the report must name the entry an operator has to remove by hand; %q does not mention %q",
			got.Detail, lookalike)
	}
	if got.Remedy == "" {
		t.Error("a warning with nothing to do about it is a dead end")
	}
}

// The two findings are reported in order rather than merged: an entry this tool
// owns is one `agents wire` removes, so offering the hand edit a lookalike needs
// would be sending the operator to repair something the tool can repair itself.
func TestWiringPrefersAnOwnedEntryOverALookalike(t *testing.T) {
	root := t.TempDir()
	a, _ := harness.Get("claude-code")
	writeJSON(t, a.WireConfigPath(root), `{"hooks":{"Stop":[{"hooks":[
		{"type":"command","command":"/opt/agents.test hook stop --harness claude-code"},
		{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}]}}`)

	got := checkWiring(a, root)
	if got.Status != Fail {
		t.Fatalf("an owned entry beside a lookalike = %q, want %q: %s", got.Status, Fail, got.Detail)
	}
	if !strings.Contains(got.Remedy, "agents wire") {
		t.Errorf("an owned entry is exactly what `agents wire` removes, and the remedy must say so: %s", got.Remedy)
	}
}

// ---------------------------------------------------------------------------
// The checks that run on every invocation and had no test after the refactor.
//
// Each is driven through the Dependencies it is handed rather than through a
// real git binary, a real PATH or a real installation: the question a test has
// to answer is whether the check fires for the failure it names, and a stub
// answers it exactly on any machine. Where a check reads the filesystem that is
// a real directory, because the failures that matter here -- a dangling link, a
// link pointing at an older binary -- are filesystem shapes.
// ---------------------------------------------------------------------------

// The exact questions the git-backed checks ask. Spelled once so that the stub
// answers the question the code asks and not a neighbouring one.
const (
	gitGlobalHooksQuestion      = "config --global --includes --null --show-origin --get-all core.hooksPath"
	gitLocalHooksQuestion       = "config --local --get-all core.hooksPath"
	gitEffectiveHooksQuestion   = "config --get core.hooksPath"
	gitGlobalAttributesQuestion = "config --global --includes --null --show-origin --get-all core.attributesFile"
)

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// findCheck selects one check out of a family by name: asserting on a slice
// index starts passing for the wrong reason the moment a check is added.
func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", name, checks)
	return Check{}
}

// gitAnswers is the Git dependency of a check, answered from a table keyed by
// the arguments after "git". An unanticipated invocation fails the test instead
// of returning an empty success: a check that stopped asking its question would
// otherwise go on passing.
func gitAnswers(t *testing.T, answers map[string]GitResult) func(string, ...string) GitResult {
	t.Helper()
	return func(dir string, args ...string) GitResult {
		question := strings.Join(args, " ")
		answer, ok := answers[question]
		if !ok {
			t.Errorf("the check asked git %q, which this fixture does not answer", question)
			return GitResult{Code: 127}
		}
		return answer
	}
}

// gitConfigOriginOutput renders what `git config --show-origin --null` prints:
// one origin-then-value pair per entry, each NUL-terminated.
func gitConfigOriginOutput(entries ...string) string {
	var b strings.Builder
	for _, entry := range entries {
		b.WriteString(entry)
		b.WriteByte(0)
	}
	return b.String()
}

// checkBinary answers "is the `agents` on PATH the executable that is running?".
// The failure it detects is a second install -- a package manager's copy, a
// stale symlink earlier on PATH, a `go run` shadowed by ~/go/bin -- and the cost
// of missing it is a report that certifies a machine whose `agents` is not the
// one being run. Each case is a different way the question comes back wrong.
func TestCheckBinaryComparesPathAgainstTheRunningExecutable(t *testing.T) {
	newExecutable := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "agents")
		writeFixture(t, path, "#!/bin/sh\n")
		return path
	}
	missing := func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") }

	cases := []struct {
		name     string
		binary   string
		lookPath func(string) (string, error)
		want     string
	}{
		{
			name:   "no lookup available",
			binary: "/usr/local/bin/agents",
			want:   Fail,
		},
		{
			name:   "not on PATH",
			binary: "/usr/local/bin/agents",
			want:   Fail,
			lookPath: func(string) (string, error) {
				return "", errors.New("executable file not found in $PATH")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkBinary(tc.binary, tc.lookPath)
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q: %s", got.Status, tc.want, got.Detail)
			}
			if got.Remedy == "" {
				t.Error("a failure with nothing to do about it is a dead end")
			}
		})
	}

	t.Run("the running executable does not exist", func(t *testing.T) {
		got := checkBinary(missing(t), func(string) (string, error) { return "/usr/local/bin/agents", nil })
		if got.Status != Fail {
			t.Errorf("an unresolvable running executable = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("the PATH entry is a dangling symlink", func(t *testing.T) {
		running := newExecutable(t)
		link := filepath.Join(t.TempDir(), "agents")
		if err := os.Symlink(missing(t), link); err != nil {
			t.Fatal(err)
		}
		got := checkBinary(running, func(string) (string, error) { return link, nil })
		if got.Status != Fail {
			t.Errorf("a dangling PATH entry = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("PATH resolves to another install", func(t *testing.T) {
		running := newExecutable(t)
		onPath := newExecutable(t)
		got := checkBinary(running, func(string) (string, error) { return onPath, nil })
		if got.Status != Fail {
			t.Fatalf("a second install on PATH = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "not the running executable") {
			t.Errorf("the report must say which of the two situations this is: %s", got.Detail)
		}
	})

	t.Run("PATH is a symlink to the running executable", func(t *testing.T) {
		running := newExecutable(t)
		link := filepath.Join(t.TempDir(), "agents")
		if err := os.Symlink(running, link); err != nil {
			t.Fatal(err)
		}
		got := checkBinary(running, func(string) (string, error) { return link, nil })
		if got.Status != OK {
			t.Errorf("a symlinked install = %q, want %q: %s; resolving links is what keeps a normal install from failing", got.Status, OK, got.Detail)
		}
	})
}

// checkGitleaks is the only check on a tool this repository does not install,
// so its whole job is to say plainly that the secret scanner is absent. Silence
// here reads as "scanned", which is the one wrong answer.
func TestCheckGitleaksReportsAMissingScanner(t *testing.T) {
	cases := []struct {
		name     string
		lookPath func(string) (string, error)
		want     string
	}{
		{name: "no lookup available", lookPath: nil, want: Warn},
		{
			name: "not installed",
			lookPath: func(string) (string, error) {
				return "", errors.New("executable file not found in $PATH")
			},
			want: Warn,
		},
		{
			name:     "installed",
			lookPath: func(string) (string, error) { return "/opt/homebrew/bin/gitleaks", nil },
			want:     OK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkGitleaks(tc.lookPath)
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q: %s", got.Status, tc.want, got.Detail)
			}
			if tc.want != OK && got.Remedy == "" {
				t.Error("a missing scanner needs the line that installs it")
			}
		})
	}
}

// checkAntigravityTrust decides whether the repository root is the exact string
// the CLI's trustedWorkspaces list holds. The failure it detects is a repository
// the CLI will refuse to run in, and the one it must not invent is a neighbour
// whose path merely starts the same way: a prefix comparison would report a
// repository as trusted that is not.
func TestCheckAntigravityTrustMatchesTheRepositoryRootExactly(t *testing.T) {
	const repoRoot = "/Users/someone/src/repo"
	newConfig := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "settings.json")
		writeFixture(t, path, body)
		return path
	}

	t.Run("no CLI config path", func(t *testing.T) {
		got := checkAntigravityTrust("", repoRoot)
		if got.Status != OK {
			t.Errorf("status = %q, want %q: %s", got.Status, OK, got.Detail)
		}
		if got.Remedy == "" {
			t.Error("the manual trust step is the whole content of this check")
		}
	})

	t.Run("config not written yet", func(t *testing.T) {
		got := checkAntigravityTrust(filepath.Join(t.TempDir(), "settings.json"), repoRoot)
		if got.Status != OK {
			t.Errorf("an absent CLI config = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("config is malformed", func(t *testing.T) {
		got := checkAntigravityTrust(newConfig(t, "{not json"), repoRoot)
		if got.Status != Warn {
			t.Errorf("a malformed CLI config = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
	})

	t.Run("this repository is listed", func(t *testing.T) {
		got := checkAntigravityTrust(newConfig(t, `{"trustedWorkspaces":["/Users/someone/other","`+repoRoot+`"]}`), repoRoot)
		if got.Status != OK || !strings.Contains(got.Detail, "confirmed") {
			t.Errorf("a listed repository = %q (%s), want OK and a confirmed entry", got.Status, got.Detail)
		}
		if got.Remedy != "" {
			t.Errorf("a trusted repository needs no remedy, got %q", got.Remedy)
		}
	})

	t.Run("listed with a trailing separator", func(t *testing.T) {
		got := checkAntigravityTrust(newConfig(t, `{"trustedWorkspaces":["`+repoRoot+`/"]}`), repoRoot)
		if !strings.Contains(got.Detail, "confirmed") {
			t.Errorf("a pasted path with a trailing slash names the same directory: %s", got.Detail)
		}
	})

	t.Run("a sibling with the same prefix is not this repository", func(t *testing.T) {
		got := checkAntigravityTrust(newConfig(t, `{"trustedWorkspaces":["`+repoRoot+`-other"]}`), repoRoot)
		if !strings.Contains(got.Detail, "not in the CLI's trustedWorkspaces") {
			t.Errorf("a sibling checkout must not satisfy the trust check: %s", got.Detail)
		}
		// The step must be named, and it must be named where a reader will
		// actually see it. A remedy on an ok check is never printed -- the
		// caller renders remedies only for checks that are not ok -- so the
		// instruction belongs in the detail. Measured before this assertion
		// existed: the advice was attached to Remedy and reached no report.
		if !strings.Contains(got.Detail, "trustedWorkspaces") {
			t.Errorf("an untrusted repository must be told the manual step in the detail it prints: %s", got.Detail)
		}
		if got.Status != OK {
			t.Errorf("status = %q, want %q: the Desktop App has no trust gate, so this is advice rather than a fault", got.Status, OK)
		}
	})

	t.Run("another repository is listed", func(t *testing.T) {
		got := checkAntigravityTrust(newConfig(t, `{"trustedWorkspaces":["/Users/someone/src/elsewhere"]}`), repoRoot)
		if !strings.Contains(got.Detail, "not in the CLI's trustedWorkspaces") {
			t.Errorf("an unrelated list = %s, want this repository reported as untrusted", got.Detail)
		}
		// The step must be named where a reader actually sees it. The status
		// stays OK -- the Desktop App has no trust gate, so an untrusted root is
		// advice rather than a fault -- and the caller prints a remedy only for
		// checks that are not OK. So a remedy attached to this check reaches no
		// report, which is what it used to do: measured, the instruction lived
		// in Remedy and was never rendered. It now travels in the detail.
		if !strings.Contains(got.Detail, "trustedWorkspaces") {
			t.Errorf("an untrusted repository must be told the manual step in the detail it prints: %s", got.Detail)
		}
		if got.Status != OK {
			t.Errorf("status = %q, want %q: the Desktop App has no trust gate, so this is advice rather than a fault", got.Status, OK)
		}
	})
}

// rootChecks exists because every other check compares one configured path with
// another: a binary stamped to a deleted worktree produced a byte-identical
// report before it was added, and the personal hook chain had already stopped
// running at exit 0. So the property is not only that it fails, but that the
// failure names the path that is gone -- no other line in the report does.
func TestRootChecksReportsAStampedCheckoutThatIsGone(t *testing.T) {
	t.Run("an unstamped binary has no root to report", func(t *testing.T) {
		if got := rootChecks(Dependencies{}); len(got) != 0 {
			t.Errorf("an empty Root = %+v, want no check at all; `go run` has no checkout to have lost", got)
		}
	})

	t.Run("the stamped checkout is gone", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "deleted-worktree")
		got := rootChecks(Dependencies{Root: root})
		if len(got) != 1 || got[0].Status != Fail {
			t.Fatalf("a deleted stamped checkout = %+v, want one Fail", got)
		}
		if !strings.Contains(got[0].Detail, root) {
			t.Errorf("the report must name the path nothing else mentions: %s", got[0].Detail)
		}
		if got[0].Remedy == "" {
			t.Error("a failure with nothing to do about it is a dead end")
		}
	})

	t.Run("the stamped checkout is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-a-checkout")
		writeFixture(t, path, "#!/bin/sh\n")
		got := rootChecks(Dependencies{Root: path})
		if len(got) != 1 || got[0].Status != Fail {
			t.Fatalf("a stamped path that is not a directory = %+v, want one Fail", got)
		}
	})

	t.Run("the stamped checkout exists", func(t *testing.T) {
		got := rootChecks(Dependencies{Root: t.TempDir()})
		if len(got) != 1 || got[0].Status != OK {
			t.Fatalf("an intact checkout = %+v, want one OK", got)
		}
	})
}

// hooksFixture is a machine provisioned the way the installer provisions one: a
// checkout carrying the reviewed git config paths, a hooks directory of four
// links, and the binary those links resolve to.
type hooksFixture struct {
	root   string
	binary string
	deps   Dependencies
}

func newHooksFixture(t *testing.T) hooksFixture {
	t.Helper()
	root := t.TempDir()
	f := hooksFixture{
		root:   root,
		binary: filepath.Join(root, "bin", "agents"),
	}
	f.deps = Dependencies{
		Root:            root,
		HooksDir:        filepath.Join(root, "git", "hooks.d"),
		GlobalGitConfig: filepath.Join(root, ".gitconfig"),
		SharedGitConfig: filepath.Join(root, "git", "gitconfig.shared"),
		LegacyHooksPath: func(string) (string, error) { return filepath.Join(root, "git", "hooks"), nil },
	}
	writeFixture(t, f.binary, "#!/bin/sh\n")
	if err := os.MkdirAll(f.deps.HooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range installedHookNames {
		if err := os.Symlink(f.binary, filepath.Join(f.deps.HooksDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// exactGitAnswers answers the three questions checkGitHooks asks, as a correctly
// provisioned machine answers them. The global setting is expected to come from
// the primary global config specifically: a hooksPath set from an included or
// shared file is a different finding even when the value is right.
func (f hooksFixture) exactGitAnswers() map[string]GitResult {
	return map[string]GitResult{
		gitGlobalHooksQuestion:    {Output: gitConfigOriginOutput("file:"+f.deps.GlobalGitConfig, f.deps.HooksDir)},
		gitLocalHooksQuestion:     {Code: 1},
		gitEffectiveHooksQuestion: {Output: f.deps.HooksDir + "\n"},
	}
}

// A machine provisioned exactly right must clear every one of these checks.
// Without this, every other case here can pass while one of them is inverted.
func TestCheckGitHooksClearsAnExactInstallation(t *testing.T) {
	f := newHooksFixture(t)
	f.deps.Git = gitAnswers(t, f.exactGitAnswers())

	checks := checkGitHooks(f.root, f.binary, f.deps)
	for _, name := range []string{
		"git-hooks:global",
		"git-hooks:local",
		"git-hooks:effective",
		"git-hooks:links",
		"git-hooks:unmanaged",
		"git-hooks:legacy",
	} {
		if got := findCheck(t, checks, name); got.Status != OK {
			t.Errorf("%s = %q, want %q: %s %s", name, got.Status, OK, got.Detail, got.Remedy)
		}
	}
}

// The failure the local and effective checks exist for: a repository-local (or
// otherwise closer) core.hooksPath takes the personal hook chain out of the
// path, and git then runs a hook directory that has none of this tool's hooks in
// it. Nothing about the global setting looks wrong, which is why the effective
// value is checked separately.
func TestCheckGitHooksReportsAHooksPathThatShadowsTheHooks(t *testing.T) {
	f := newHooksFixture(t)
	shadow := filepath.Join(f.root, "git", "hooks")
	answers := f.exactGitAnswers()
	answers[gitLocalHooksQuestion] = GitResult{Output: shadow + "\n"}
	answers[gitEffectiveHooksQuestion] = GitResult{Output: shadow + "\n"}
	f.deps.Git = gitAnswers(t, answers)

	checks := checkGitHooks(f.root, f.binary, f.deps)
	local := findCheck(t, checks, "git-hooks:local")
	if local.Status != Warn {
		t.Errorf("a repository-local override = %q, want %q: %s", local.Status, Warn, local.Detail)
	}
	if local.Remedy == "" {
		t.Error("an operator cannot unshadow a hook directory the report does not name")
	}
	effective := findCheck(t, checks, "git-hooks:effective")
	if effective.Status != Warn {
		t.Errorf("an effective hooksPath pointing elsewhere = %q, want %q: %s", effective.Status, Warn, effective.Detail)
	}
	// The links themselves are intact; the report must not blame them.
	if got := findCheck(t, checks, "git-hooks:links"); got.Status != OK {
		t.Errorf("the installed links are untouched by a shadowing config: %+v", got)
	}
}

// The three ways the global setting can be wrong, told apart because they end
// differently: unset, set in a file this review does not own, or set twice by
// includes that disagree. All three leave the machine with hooks that do not
// run.
func TestCheckGitHooksReportsAGlobalHooksPathItDoesNotOwn(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		f := newHooksFixture(t)
		answers := f.exactGitAnswers()
		answers[gitGlobalHooksQuestion] = GitResult{Code: 1}
		f.deps.Git = gitAnswers(t, answers)

		got := findCheck(t, checkGitHooks(f.root, f.binary, f.deps), "git-hooks:global")
		if got.Status != Fail {
			t.Fatalf("an unset global hooksPath = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Remedy, "git/install-hooks.sh") {
			t.Errorf("the remedy must be the reviewed installer, with its paths: %s", got.Remedy)
		}
	})

	t.Run("set from a file this review does not own", func(t *testing.T) {
		f := newHooksFixture(t)
		answers := f.exactGitAnswers()
		answers[gitGlobalHooksQuestion] = GitResult{
			Output: gitConfigOriginOutput("file:"+f.deps.SharedGitConfig, f.deps.HooksDir),
		}
		f.deps.Git = gitAnswers(t, answers)

		got := findCheck(t, checkGitHooks(f.root, f.binary, f.deps), "git-hooks:global")
		if got.Status != Fail {
			t.Fatalf("the right value from the wrong file = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("set twice", func(t *testing.T) {
		f := newHooksFixture(t)
		answers := f.exactGitAnswers()
		answers[gitGlobalHooksQuestion] = GitResult{Output: gitConfigOriginOutput(
			"file:"+f.deps.GlobalGitConfig, f.deps.HooksDir,
			"file:"+f.deps.SharedGitConfig, filepath.Join(f.root, "git", "hooks"),
		)}
		f.deps.Git = gitAnswers(t, answers)

		got := findCheck(t, checkGitHooks(f.root, f.binary, f.deps), "git-hooks:global")
		if got.Status != Fail {
			t.Fatalf("two global hooksPath values = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "2 values") {
			t.Errorf("the report must say how many values it found: %s", got.Detail)
		}
	})

	t.Run("no git runner at all", func(t *testing.T) {
		f := newHooksFixture(t)
		checks := checkGitHooks(f.root, f.binary, f.deps)
		got := findCheck(t, checks, "git-hooks:global")
		if got.Status != Fail {
			t.Fatalf("an unavailable git = %q, want %q: a report that cannot look must not pass", got.Status, Fail)
		}
	})
}

// A binary that was never stamped to a checkout -- `go run`, a test binary -- has
// no HooksDir to compare against, and asking the global questions anyway would
// compare a configured path with the empty string. The stub is what proves the
// narrowing: it fails the test on any question but the repository-local one.
func TestCheckGitHooksWithoutACheckoutAsksOnlyWhatItCan(t *testing.T) {
	f := newHooksFixture(t)
	f.deps.Root = ""
	f.deps.HooksDir = ""
	f.deps.Git = gitAnswers(t, map[string]GitResult{gitLocalHooksQuestion: {Code: 1}})

	checks := checkGitHooks(f.root, f.binary, f.deps)
	if len(checks) != 2 {
		t.Fatalf("an unstamped binary produced %d checks, want the local and legacy pair: %+v", len(checks), checks)
	}
	if got := findCheck(t, checks, "git-hooks:local"); got.Status != OK {
		t.Errorf("git-hooks:local = %q, want %q: %s", got.Status, OK, got.Detail)
	}
	if got := findCheck(t, checks, "git-hooks:legacy"); got.Status != OK {
		t.Errorf("git-hooks:legacy = %q, want %q: %s", got.Status, OK, got.Detail)
	}
}

// checkInstalledLinks watches the four symlinks git will actually execute. The
// case worth a fixture is the one a package upgrade leaves behind: the link is
// still ours and still a link, but it names the previous version's binary, and
// the installer refuses to repoint a link it did not just write unless it is
// handed --adopt-owned. A remedy without the flag is a command that fails.
func TestCheckInstalledLinksReportsWhatOnlyAFlagCanRepair(t *testing.T) {
	t.Run("all four resolve to the current binary", func(t *testing.T) {
		f := newHooksFixture(t)
		if got := checkInstalledLinks(f.deps, f.binary); got.Status != OK {
			t.Errorf("an intact link set = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("a link names an older binary", func(t *testing.T) {
		f := newHooksFixture(t)
		older := filepath.Join(f.root, "bin", "agents-0.5.1")
		writeFixture(t, older, "#!/bin/sh\n")
		if err := os.Remove(filepath.Join(f.deps.HooksDir, "commit-msg")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(older, filepath.Join(f.deps.HooksDir, "commit-msg")); err != nil {
			t.Fatal(err)
		}

		got := checkInstalledLinks(f.deps, f.binary)
		if got.Status != Fail {
			t.Fatalf("a link to the previous version = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "commit-msg") {
			t.Errorf("the report must name the link that needs repointing: %s", got.Detail)
		}
		if !strings.Contains(got.Remedy, "--adopt-owned") {
			t.Errorf("the installer refuses an existing link without the flag, so the remedy must carry it: %s", got.Remedy)
		}
	})

	t.Run("a foreign file sits under a managed hook name", func(t *testing.T) {
		f := newHooksFixture(t)
		path := filepath.Join(f.deps.HooksDir, "pre-commit")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, path, "#!/bin/sh\nexit 0\n")

		got := checkInstalledLinks(f.deps, f.binary)
		if got.Status != Fail {
			t.Fatalf("a foreign hook where ours belongs = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "not an owned symlink") {
			t.Errorf("the report must not describe a foreign file as a missing link: %s", got.Detail)
		}
	})

	t.Run("a hook link is missing", func(t *testing.T) {
		f := newHooksFixture(t)
		if err := os.Remove(filepath.Join(f.deps.HooksDir, "post-merge")); err != nil {
			t.Fatal(err)
		}
		got := checkInstalledLinks(f.deps, f.binary)
		if got.Status != Fail {
			t.Fatalf("a missing hook link = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("the current binary cannot be inspected", func(t *testing.T) {
		f := newHooksFixture(t)
		got := checkInstalledLinks(f.deps, filepath.Join(f.root, "bin", "gone"))
		if got.Status != Fail {
			t.Fatalf("an unstattable binary = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})
}

// checkUnmanagedLinks exists because git silently ignores names it does not
// know: a dangling link left by an older install dangles forever and no other
// check names it, since git-hooks:links looks only at the four managed names.
// The other half of the property is what it must not claim -- a managed name is
// already reported by that other check, and a link that resolves is the
// human's to keep.
func TestCheckUnmanagedLinksReportsOnlyDanglingUnownedLinks(t *testing.T) {
	t.Run("nothing dangling", func(t *testing.T) {
		f := newHooksFixture(t)
		if got := checkUnmanagedLinks(f.deps); got.Status != OK {
			t.Errorf("a clean directory = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("an unowned link dangles", func(t *testing.T) {
		f := newHooksFixture(t)
		if err := os.Symlink(filepath.Join(f.root, "bin", "agents-0.4.0"), filepath.Join(f.deps.HooksDir, "pre-push")); err != nil {
			t.Fatal(err)
		}
		got := checkUnmanagedLinks(f.deps)
		if got.Status != Warn {
			t.Fatalf("a dangling unowned link = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
		if !strings.Contains(got.Detail, "pre-push") {
			t.Errorf("the report must name the link to delete: %s", got.Detail)
		}
	})

	t.Run("an unowned link resolves", func(t *testing.T) {
		f := newHooksFixture(t)
		if err := os.Symlink(f.binary, filepath.Join(f.deps.HooksDir, "pre-push")); err != nil {
			t.Fatal(err)
		}
		if got := checkUnmanagedLinks(f.deps); got.Status != OK {
			t.Errorf("an unowned link that works = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("a managed link dangles", func(t *testing.T) {
		f := newHooksFixture(t)
		path := filepath.Join(f.deps.HooksDir, "post-checkout")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.root, "bin", "agents-0.4.0"), path); err != nil {
			t.Fatal(err)
		}
		// Reported once, by git-hooks:links, which owns those names.
		if got := checkUnmanagedLinks(f.deps); got.Status != OK {
			t.Errorf("a dangling managed link = %q, want %q: it is git-hooks:links' finding, not this check's: %s",
				got.Status, OK, got.Detail)
		}
		if got := checkInstalledLinks(f.deps, f.binary); got.Status != Fail {
			t.Errorf("the check that owns managed names must report it: %+v", got)
		}
	})
}

// checkLocalHooks is the cheap half of the shadowing question: it reads the
// repository-local value alone. Exit 1 is git's "no such setting" and must stay
// OK, while an unreadable answer is a failure -- reading "could not ask" as
// "nothing set" is how a shadowed hook chain passes silently.
func TestCheckLocalHooksClassifiesTheRepositoryOverride(t *testing.T) {
	cases := []struct {
		name   string
		answer GitResult
		want   string
		remedy bool
	}{
		{name: "unset", answer: GitResult{Code: 1}, want: OK},
		{name: "unreadable", answer: GitResult{Code: 128, Output: "fatal: bad config line 1\n"}, want: Fail, remedy: true},
		// Git answered, with nothing: not the same as unset, and not a pass.
		{name: "empty value", answer: GitResult{}, want: Fail},
		{name: "set", answer: GitResult{Output: "/repo/.git/hooks\n"}, want: Warn, remedy: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked []string
			got := checkLocalHooks("/repo", func(dir string, args ...string) GitResult {
				asked = append(asked, strings.Join(args, " "))
				return tc.answer
			})
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q: %s", got.Status, tc.want, got.Detail)
			}
			if tc.remedy && got.Remedy == "" {
				t.Error("the report tells the operator something is wrong and not what to do")
			}
			if len(asked) != 1 || asked[0] != gitLocalHooksQuestion {
				t.Errorf("asked git %v, want the repository-local question only", asked)
			}
		})
	}
}

// checkLegacyHooks looks for the retired dispatcher shim in the repository's own
// hooks directory -- the one a global core.hooksPath shadows. Its whole safety
// property is that it matches exact bytes: a shim left behind still runs and
// fails, while a hook the human wrote must never be named as removable.
func TestCheckLegacyHooksDetectsOnlyTheExactRetiredShim(t *testing.T) {
	shim := func(t *testing.T) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "githook", "testdata", "retired-run-hooks.sh"))
		if err != nil {
			t.Fatalf("the canonical retired shim fixture: %v", err)
		}
		return b
	}
	newLegacy := func(t *testing.T, names ...string) (string, Dependencies) {
		t.Helper()
		dir := t.TempDir()
		for _, name := range names {
			writeFixture(t, filepath.Join(dir, name), string(shim(t)))
		}
		return dir, Dependencies{LegacyHooksPath: func(string) (string, error) { return dir, nil }}
	}

	t.Run("nothing there", func(t *testing.T) {
		_, deps := newLegacy(t)
		if got := checkLegacyHooks("/repo", deps); got.Status != OK {
			t.Errorf("an empty hooks directory = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("a foreign hook that is not the retired shim", func(t *testing.T) {
		dir, deps := newLegacy(t)
		writeFixture(t, filepath.Join(dir, "pre-commit"), "#!/bin/sh\nexit 0\n")
		if got := checkLegacyHooks("/repo", deps); got.Status != OK {
			t.Errorf("a hand-written hook = %q, want %q: naming it would tell the operator to delete their own hook: %s",
				got.Status, OK, got.Detail)
		}
	})

	t.Run("the retired shim is still installed", func(t *testing.T) {
		_, deps := newLegacy(t, "pre-commit", "commit-msg")
		got := checkLegacyHooks("/repo", deps)
		if got.Status != Warn {
			t.Fatalf("two retired shims = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
		for _, name := range []string{"pre-commit", "commit-msg"} {
			if !strings.Contains(got.Detail, name) {
				t.Errorf("the report must name each shim it found; %q omits %q", got.Detail, name)
			}
		}
	})

	t.Run("the hooks directory cannot be resolved", func(t *testing.T) {
		got := checkLegacyHooks("/repo", Dependencies{})
		if got.Status != Fail {
			t.Errorf("no resolver = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("the resolver returns a relative path", func(t *testing.T) {
		deps := Dependencies{LegacyHooksPath: func(string) (string, error) { return ".git/hooks", nil }}
		if got := checkLegacyHooks("/repo", deps); got.Status != Fail {
			t.Errorf("a relative hooks directory = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("the resolver fails", func(t *testing.T) {
		deps := Dependencies{LegacyHooksPath: func(string) (string, error) { return "", errors.New("no git dir") }}
		if got := checkLegacyHooks("/repo", deps); got.Status != Fail {
			t.Errorf("a failed resolution = %q, want %q: an unresolvable question must not read as clean: %s",
				got.Status, Fail, got.Detail)
		}
	})
}

// attributesFixture is the global half of the attributes setup: a tracked source
// in the checkout, the reviewed symlink at ~/.gitattributes, and a repository
// carrying its own copy of the rule.
type attributesFixture struct {
	root   string
	repo   string
	link   string
	source string
	deps   Dependencies
}

func newAttributesFixture(t *testing.T) attributesFixture {
	t.Helper()
	root := t.TempDir()
	f := attributesFixture{
		root:   root,
		repo:   filepath.Join(root, "repo"),
		source: filepath.Join(root, "dotfiles", "git", "gitattributes"),
		link:   filepath.Join(root, "home", ".gitattributes"),
	}
	f.deps = Dependencies{
		Root:                  root,
		AttributesLink:        f.link,
		AttributesSource:      f.source,
		AttributesConfigValue: "~/.gitattributes",
		GlobalGitConfig:       filepath.Join(root, "home", ".gitconfig"),
		SharedGitConfig:       filepath.Join(root, "dotfiles", "git", "gitconfig.shared"),
	}
	writeFixture(t, f.source, ".agents/** linguist-generated=true\n")
	writeFixture(t, filepath.Join(f.repo, ".gitattributes"), ".agents/** linguist-generated=true\n")
	if err := os.MkdirAll(filepath.Dir(f.link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.source, f.link); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f attributesFixture) exactGitAnswers() map[string]GitResult {
	return map[string]GitResult{
		gitGlobalAttributesQuestion: {Output: gitConfigOriginOutput(
			"file:"+f.deps.SharedGitConfig, f.deps.AttributesConfigValue)},
	}
}

func TestCheckGitAttributesInGlobalMode(t *testing.T) {
	cases := []struct {
		name    string
		want    string
		detail  string
		noGit   bool // leave deps.Git nil: no runner at all
		prepare func(t *testing.T, f *attributesFixture)
	}{
		{
			name: "exact",
			want: OK,
		},
		{
			name: "the global setting is unset",
			want: Fail,
			prepare: func(t *testing.T, f *attributesFixture) {
				answers := f.exactGitAnswers()
				answers[gitGlobalAttributesQuestion] = GitResult{Code: 1}
				f.deps.Git = gitAnswers(t, answers)
			},
		},
		{
			name: "the global setting is set twice",
			want: Fail,
			prepare: func(t *testing.T, f *attributesFixture) {
				answers := f.exactGitAnswers()
				answers[gitGlobalAttributesQuestion] = GitResult{Output: gitConfigOriginOutput(
					"file:"+f.deps.SharedGitConfig, f.deps.AttributesConfigValue,
					"file:"+f.deps.GlobalGitConfig, "~/.gitattributes",
				)}
				f.deps.Git = gitAnswers(t, answers)
			},
		},
		{
			name: "the global setting has the wrong value",
			want: Fail,
			prepare: func(t *testing.T, f *attributesFixture) {
				answers := f.exactGitAnswers()
				answers[gitGlobalAttributesQuestion] = GitResult{Output: gitConfigOriginOutput(
					"file:"+f.deps.SharedGitConfig, "~/.config/git/attributes")}
				f.deps.Git = gitAnswers(t, answers)
			},
		},
		{
			// The failure a content comparison cannot see: the bytes are
			// identical today, but the rules in force belong to a copy, so the
			// reviewed source stops being the thing git reads.
			name:   "the link resolves to a copy rather than the tracked source",
			want:   Fail,
			detail: "tracked source",
			prepare: func(t *testing.T, f *attributesFixture) {
				stale := filepath.Join(f.root, "stale-gitattributes")
				writeFixture(t, stale, ".agents/** linguist-generated=true\n")
				if err := os.Remove(f.link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(stale, f.link); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "the link is a regular file",
			want:   Fail,
			detail: "not a symlink",
			prepare: func(t *testing.T, f *attributesFixture) {
				if err := os.Remove(f.link); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, f.link, ".agents/** linguist-generated=true\n")
			},
		},
		{
			name:   "the link is missing",
			want:   Fail,
			detail: "missing or not a symlink",
			prepare: func(t *testing.T, f *attributesFixture) {
				if err := os.Remove(f.link); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "the link dangles",
			want:   Fail,
			detail: "broken",
			prepare: func(t *testing.T, f *attributesFixture) {
				if err := os.Remove(f.link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.root, "gone"), f.link); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "the repository carries only a near miss",
			want:   Fail,
			detail: "exact agents rule",
			prepare: func(t *testing.T, f *attributesFixture) {
				// `.agents/*` leaves every nested file unmarked, which is the
				// difference the check exists to see.
				writeFixture(t, filepath.Join(f.repo, ".gitattributes"), ".agents/* linguist-generated=true\n")
			},
		},
		{
			name:   "there is no git runner",
			want:   Fail,
			detail: "runner is unavailable",
			noGit:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAttributesFixture(t)
			if !tc.noGit {
				f.deps.Git = gitAnswers(t, f.exactGitAnswers())
			}
			if tc.prepare != nil {
				tc.prepare(t, &f)
			}
			got := checkGitAttributes(f.repo, f.deps)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q: %s", got.Status, tc.want, got.Detail)
			}
			if tc.detail != "" && !strings.Contains(got.Detail, tc.detail) {
				t.Errorf("detail = %q, want it to mention %q: two faults this different should not read the same", got.Detail, tc.detail)
			}
			if tc.want == Fail && got.Remedy == "" {
				t.Error("a failure with nothing to do about it is a dead end")
			}
		})
	}
}

// Without a global attributes file to check, the check narrows to the
// repository's own copy -- the half that travels with the repository and is
// therefore the half a clone can be missing.
func TestCheckGitAttributesInRepositoryOnlyMode(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		write  bool
		deps   Dependencies
		want   string
		remedy bool
	}{
		{name: "the exact rule", body: ".agents/** linguist-generated=true\n", write: true, want: OK},
		{
			name:  "user rules plus ours",
			body:  "*.lock -diff\n.agents/** linguist-generated=true\n",
			write: true, want: OK,
		},
		{name: "the file is missing", want: Fail, remedy: true},
		{
			name: "only a near miss", body: ".agents/** linguist-generated\n",
			write: true, want: Fail, remedy: true,
		},
		{
			// A stamped binary with no attributes source configured: the global
			// half cannot be checked, and the repository half still must be.
			name: "no git runner", body: ".agents/** linguist-generated=true\n", write: true,
			deps: Dependencies{Root: "/somewhere/else", AttributesSource: "/somewhere/gitattributes"},
			want: Fail, remedy: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.write {
				writeFixture(t, filepath.Join(root, ".gitattributes"), tc.body)
			}
			deps := tc.deps
			got := checkGitAttributes(root, deps)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q: %s", got.Status, tc.want, got.Detail)
			}
			if tc.remedy && got.Remedy == "" {
				t.Error("a failure with nothing to do about it is a dead end")
			}
		})
	}
}
