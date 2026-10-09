package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// The rendered report carries a status and a name per check, and the caller
// keys off both. A check with neither would render as a blank line.
func TestRunWithDepsNamesEveryCheck(t *testing.T) {
	root := t.TempDir()
	checks, err := RunWithDeps(root, "/nonexistent/agents", DependenciesFor())
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
	_, err := RunWithDeps(root, "/nonexistent/agents", DependenciesFor())
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
	gitGlobalAttributesQuestion = "config --global --includes --null --show-origin --get-all core.attributesFile"
	gitGlobalIncludesQuestion   = "config --global --includes --get-all include.path"
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

// ---------------------------------------------------------------------------
// The chain: the directory core.hooksPath names, the record inside it, and the
// four entries Git executes.
//
// The fixture is a machine the installer provisioned, and every case breaks one
// thing about it. Nothing here reads a compiled checkout root -- there is none
// any more -- so the chain is found the way doctor finds it: through the global
// setting.
// ---------------------------------------------------------------------------

// chainFixture is a machine wired the way git/install-hooks.sh wires one: the
// chain at <root>/git/hooks.d holding a record and four generated entries, the
// binary the record names, a checkout supplying two personal stages, and the
// global setting that points Git at it.
type chainFixture struct {
	root     string
	chain    string
	binary   string
	checkout string
	deps     Dependencies
}

func newChainFixture(t *testing.T) chainFixture {
	t.Helper()
	root := t.TempDir()
	f := chainFixture{
		root:     root,
		chain:    filepath.Join(root, "git", "hooks.d"),
		binary:   filepath.Join(root, "bin", "agents"),
		checkout: filepath.Join(root, "personal checkout"),
	}
	f.deps = Dependencies{
		GlobalGitConfig:       filepath.Join(root, "home", ".gitconfig"),
		AttributesLink:        filepath.Join(root, "home", ".gitattributes"),
		AttributesConfigValue: "~/.gitattributes",
		LegacyHooksPath:       func(string) (string, error) { return filepath.Join(root, "git", "hooks"), nil },
	}
	writeFixture(t, f.binary, "#!/bin/sh\n")
	if err := os.Chmod(f.binary, 0o755); err != nil {
		t.Fatal(err)
	}
	// The installer has to be where the remedy's shape says it is, or the
	// remedy falls back to the sentence that names no path.
	writeFixture(t, filepath.Join(root, "git", "install-hooks.sh"), "#!/bin/sh\n")
	for _, name := range installedHookNames {
		path := filepath.Join(f.chain, name)
		writeFixture(t, path, generatedEntry(f.checkout, name))
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, filepath.Join(f.chain, chainRecordName), generatedChainRecord(f.binary, f.checkout))
	// Two personal stages, the shape this repository's own git/hooks carries.
	for _, name := range []string{"recent.post-merge", "recent.post-checkout"} {
		path := filepath.Join(f.checkout, "git", "hooks", name)
		writeFixture(t, path, "#!/bin/sh\n")
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// generatedEntry is the entry git/install-hooks.sh writes, reduced to the three
// things doctor recognises: line 1, the generated header, and the exec line for
// this hook name.
func generatedEntry(checkout, hook string) string {
	return "#!/bin/sh\n" +
		"# Written by git/install-hooks.sh for " + checkout + ".\n" +
		"# The checkout is read from our own header, so a broken record is still repairable.\n" +
		"exec \"$binary\" githook " + hook + " --checkout \"$checkout\" \"$@\"\n"
}

func generatedChainRecord(binary, checkout string) string {
	return "# Written by git/install-hooks.sh. Re-run the installer to change it.\n" +
		"format=1\nbinary=" + binary + "\ncheckout=" + checkout + "\n"
}

// exactGitAnswers answers the two questions the chain checks ask, as a machine
// the installer provisioned answers them.
func (f chainFixture) exactGitAnswers() map[string]GitResult {
	return map[string]GitResult{
		gitGlobalHooksQuestion: {Output: gitConfigOriginOutput("file:"+f.deps.GlobalGitConfig, f.chain)},
		gitLocalHooksQuestion:  {Code: 1},
	}
}

func (f chainFixture) checks(t *testing.T) []Check {
	t.Helper()
	f.deps.Git = gitAnswers(t, f.exactGitAnswers())
	return chainChecks(f.root, f.binary, f.deps)
}

// A machine provisioned exactly right must clear every chain check. Without
// this, every other case here can pass while one of them is inverted.
func TestChainClearsAnExactInstallation(t *testing.T) {
	f := newChainFixture(t)
	checks := f.checks(t)
	for _, name := range []string{
		"chain:hooks-path",
		"chain:entries",
		"chain:record",
		"chain:unmanaged",
		"chain:local",
		"chain:legacy",
		"chain:running",
		"chain:checkout",
	} {
		if got := findCheck(t, checks, name); got.Status != OK {
			t.Errorf("%s = %q, want %q: %s %s", name, got.Status, OK, got.Detail, got.Remedy)
		}
	}
}

// chain:hooks-path reads the one setting that says where the chain is. The
// value must come from the machine-local primary global config -- a path set by
// an included file is a different finding even when it is right -- and an unset
// setting is a report, not a fault: §8 row 15 pins that, because failing here
// would fail doctor on every machine that never installed the hooks.
func TestChainHooksPathClassifiesTheGlobalSetting(t *testing.T) {
	t.Run("unset means no chain is installed", func(t *testing.T) {
		f := newChainFixture(t)
		f.deps.Git = gitAnswers(t, map[string]GitResult{
			gitGlobalHooksQuestion: {Code: 1},
			gitLocalHooksQuestion:  {Code: 1},
		})
		checks := chainChecks(f.root, f.binary, f.deps)
		if got := findCheck(t, checks, "chain:hooks-path"); got.Status != OK {
			t.Fatalf("an unset core.hooksPath = %q, want %q: %s", got.Status, OK, got.Detail)
		}
		if len(checks) != 3 {
			t.Fatalf("with no chain installed doctor produced %d checks, want hooks-path, local and legacy: %+v", len(checks), checks)
		}
	})

	t.Run("set from a file this review does not own", func(t *testing.T) {
		f := newChainFixture(t)
		answers := f.exactGitAnswers()
		answers[gitGlobalHooksQuestion] = GitResult{Output: gitConfigOriginOutput(
			"file:"+filepath.Join(f.root, "elsewhere", "config"), f.chain)}
		f.deps.Git = gitAnswers(t, answers)

		got := findCheck(t, chainChecks(f.root, f.binary, f.deps), "chain:hooks-path")
		if got.Status != Fail {
			t.Fatalf("the right value from the wrong file = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("set twice", func(t *testing.T) {
		f := newChainFixture(t)
		answers := f.exactGitAnswers()
		answers[gitGlobalHooksQuestion] = GitResult{Output: gitConfigOriginOutput(
			"file:"+f.deps.GlobalGitConfig, f.chain,
			"file:"+filepath.Join(f.root, "elsewhere"), filepath.Join(f.root, "git", "hooks"),
		)}
		f.deps.Git = gitAnswers(t, answers)

		got := findCheck(t, chainChecks(f.root, f.binary, f.deps), "chain:hooks-path")
		if got.Status != Fail {
			t.Fatalf("two global hooksPath values = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "2 values") {
			t.Errorf("the report must say how many values it found: %s", got.Detail)
		}
	})

	t.Run("the setting cannot be read", func(t *testing.T) {
		f := newChainFixture(t)
		answers := f.exactGitAnswers()
		answers[gitGlobalHooksQuestion] = GitResult{Code: 128, Output: "fatal: bad config line 1\n"}
		f.deps.Git = gitAnswers(t, answers)

		if got := findCheck(t, chainChecks(f.root, f.binary, f.deps), "chain:hooks-path"); got.Status != Fail {
			t.Errorf("an unreadable setting = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("no git runner at all", func(t *testing.T) {
		f := newChainFixture(t)
		if got := findCheck(t, chainChecks(f.root, f.binary, f.deps), "chain:hooks-path"); got.Status != Fail {
			t.Errorf("an unavailable git = %q, want %q: a report that cannot look must not pass", got.Status, Fail)
		}
	})
}

// chain:entries watches the four files Git will actually execute. The failure
// §8 row 13 names is a mode bit: a non-executable entry is skipped with a hint
// and exit 0, so the guard is off and only the hint says so.
func TestChainEntriesReportsWhatGitWouldSkip(t *testing.T) {
	t.Run("an entry is not executable", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Chmod(filepath.Join(f.chain, "pre-commit"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:entries")
		if got.Status != Fail {
			t.Fatalf("a non-executable entry = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "not executable") {
			t.Errorf("the report must say the mode is what stops it: %s", got.Detail)
		}
	})

	t.Run("a managed name holds a foreign file", func(t *testing.T) {
		f := newChainFixture(t)
		writeFixture(t, filepath.Join(f.chain, "commit-msg"), "#!/bin/sh\nexit 0\n")
		got := findCheck(t, f.checks(t), "chain:entries")
		if got.Status != Fail {
			t.Fatalf("a foreign file where our entry belongs = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		if !strings.Contains(got.Detail, "generated header") {
			t.Errorf("the report must name what is missing from the file: %s", got.Detail)
		}
	})

	t.Run("a managed name is missing", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Remove(filepath.Join(f.chain, "post-merge")); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:entries")
		if got.Status != Fail || !strings.Contains(got.Detail, "post-merge") {
			t.Fatalf("a missing entry = %q (%s), want a Fail naming post-merge", got.Status, got.Detail)
		}
	})

	t.Run("the chain directory is gone", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.RemoveAll(f.chain); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:entries")
		if got.Status != Fail {
			t.Fatalf("a deleted chain directory = %q, want %q: this is the one silence doctor exists to break", got.Status, Fail)
		}
		if !strings.Contains(got.Detail, f.chain) {
			t.Errorf("the report must name the directory that is gone: %s", got.Detail)
		}
		if got.Remedy == "" {
			t.Error("a failure with nothing to do about it is a dead end")
		}
	})
}

// chain:record parses chain.env under the entry's allow-list. Every case here
// is a record the entry would refuse too, which is the point: doctor and the
// entry must not disagree about what is readable.
func TestChainRecordRefusesMalformedRecords(t *testing.T) {
	cases := []struct {
		name   string
		record string
		detail string
	}{
		{
			name:   "an unknown key",
			record: "format=1\nbinary=%s\ncheckout=%s\npwned=1\n",
			detail: "pwned",
		},
		{
			name:   "a wrong format",
			record: "format=2\nbinary=%s\ncheckout=%s\n",
			detail: "not format 1",
		},
		{
			name:   "a relative binary",
			record: "format=1\nbinary=bin/agents\ncheckout=%s\n",
			detail: "not an absolute path",
		},
		{
			name:   "a checkout that is neither - nor absolute",
			record: "format=1\nbinary=%s\ncheckout=somewhere\n",
			detail: "neither - nor an absolute path",
		},
		{
			name:   "a truncated record",
			record: "format=1\nbinary=%s\n",
			detail: "checkout",
		},
		{
			name:   "a line that is a shell command",
			record: "format=1\nbinary=%s\ncheckout=%s\n: > /tmp/pwned\n",
			detail: "unknown key",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newChainFixture(t)
			body := tc.record
			if strings.Count(body, "%s") == 2 {
				body = strings.Replace(body, "%s", f.binary, 1)
				body = strings.Replace(body, "%s", f.checkout, 1)
			} else if strings.Count(body, "%s") == 1 {
				body = strings.Replace(body, "%s", f.binary, 1)
			}
			writeFixture(t, filepath.Join(f.chain, chainRecordName), body)

			got := findCheck(t, f.checks(t), "chain:record")
			if got.Status != Fail {
				t.Fatalf("%s = %q, want %q: %s", tc.name, got.Status, Fail, got.Detail)
			}
			if !strings.Contains(got.Detail, tc.detail) {
				t.Errorf("detail = %q, want it to mention %q", got.Detail, tc.detail)
			}
			if got.Remedy == "" {
				t.Error("a failure with nothing to do about it is a dead end")
			}
		})
	}

	t.Run("the record is missing", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Remove(filepath.Join(f.chain, chainRecordName)); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:record")
		if got.Status != Fail || !strings.Contains(got.Detail, chainRecordName) {
			t.Fatalf("a missing record = %q (%s), want a Fail naming chain.env", got.Status, got.Detail)
		}
	})

	t.Run("the binary it names is not executable", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Chmod(f.binary, 0o644); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:record")
		if got.Status != Fail || !strings.Contains(got.Detail, f.binary) {
			t.Fatalf("a record naming a non-executable binary = %q (%s), want a Fail naming it", got.Status, got.Detail)
		}
	})
}

// chain:running is the check that caught the upgrade incident, and it stays a
// failure: §8 row 14's mutation is reporting both paths as facts with no
// failure, which would print ok while the binary you run is not the binary your
// commits run.
func TestChainRunningFailsWhenTheRecordNamesAnotherBinary(t *testing.T) {
	t.Run("the record names the running binary", func(t *testing.T) {
		f := newChainFixture(t)
		if got := findCheck(t, f.checks(t), "chain:running"); got.Status != OK {
			t.Errorf("the record names the running binary = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("the record names an older binary", func(t *testing.T) {
		f := newChainFixture(t)
		older := filepath.Join(f.root, "bin", "agents-0.5.1")
		writeFixture(t, older, "#!/bin/sh\n")
		writeFixture(t, filepath.Join(f.chain, chainRecordName), generatedChainRecord(older, f.checkout))

		got := findCheck(t, f.checks(t), "chain:running")
		if got.Status != Fail {
			t.Fatalf("a record naming another binary = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
		for _, want := range []string{older, f.binary} {
			if !strings.Contains(got.Detail, want) {
				t.Errorf("the failure must print both paths; %q omits %q", got.Detail, want)
			}
		}
	})

	t.Run("the record cannot be read", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Remove(filepath.Join(f.chain, chainRecordName)); err != nil {
			t.Fatal(err)
		}
		if got := findCheck(t, f.checks(t), "chain:running"); got.Status != Fail {
			t.Errorf("no record to compare with = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})
}

// chain:checkout reports the personal stages, and it reports a COUNT: this
// repository's git/hooks carries two tracked files, so "not empty" is true of
// any checkout and zero is the state the check exists to notice. It warns where
// root:exists failed, because the built-in guard still runs.
func TestChainCheckoutReportsThePersonalStageCount(t *testing.T) {
	t.Run("two stages are found", func(t *testing.T) {
		f := newChainFixture(t)
		got := findCheck(t, f.checks(t), "chain:checkout")
		if got.Status != OK {
			t.Fatalf("two personal stages = %q, want %q: %s", got.Status, OK, got.Detail)
		}
		if !strings.Contains(got.Detail, "2 personal stage") {
			t.Errorf("the report must give the count it found: %s", got.Detail)
		}
	})

	t.Run("the record declares none", func(t *testing.T) {
		f := newChainFixture(t)
		writeFixture(t, filepath.Join(f.chain, chainRecordName), generatedChainRecord(f.binary, "-"))
		if got := findCheck(t, f.checks(t), "chain:checkout"); got.Status != OK {
			t.Errorf("checkout=- = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("the recorded checkout is gone", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.RemoveAll(f.checkout); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:checkout")
		if got.Status != Warn {
			t.Fatalf("a deleted checkout = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
		if !strings.Contains(got.Detail, f.checkout) {
			t.Errorf("the report must name the checkout that is gone: %s", got.Detail)
		}
	})

	t.Run("the checkout has been emptied", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.RemoveAll(filepath.Join(f.checkout, "git", "hooks")); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:checkout")
		if got.Status != Warn {
			t.Fatalf("an emptied checkout = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
		if !strings.Contains(got.Detail, "0 executable") {
			t.Errorf("the report must give the count it found, and it is zero: %s", got.Detail)
		}
	})
}

// chain:unmanaged reports the complementary case to chain:entries: names Git
// never runs. A dangling link left by an earlier installer, and a generated
// entry under a name that is not a hook -- which looks installed and is not.
func TestChainUnmanagedReportsDanglingLinksAndLookalikes(t *testing.T) {
	t.Run("nothing to report", func(t *testing.T) {
		f := newChainFixture(t)
		if got := findCheck(t, f.checks(t), "chain:unmanaged"); got.Status != OK {
			t.Errorf("a clean chain = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("an unowned link dangles", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Symlink(filepath.Join(f.root, "bin", "agents-0.4.0"), filepath.Join(f.chain, "pre-push")); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:unmanaged")
		if got.Status != Warn {
			t.Fatalf("a dangling unowned link = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
		if !strings.Contains(got.Detail, "pre-push") {
			t.Errorf("the report must name the link to delete: %s", got.Detail)
		}
	})

	t.Run("an unowned link resolves", func(t *testing.T) {
		f := newChainFixture(t)
		if err := os.Symlink(f.binary, filepath.Join(f.chain, "pre-push")); err != nil {
			t.Fatal(err)
		}
		if got := findCheck(t, f.checks(t), "chain:unmanaged"); got.Status != OK {
			t.Errorf("an unowned link that works = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("a generated entry sits under a name git never runs", func(t *testing.T) {
		f := newChainFixture(t)
		path := filepath.Join(f.chain, "pre-push")
		writeFixture(t, path, generatedEntry(f.checkout, "pre-push"))
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
		got := findCheck(t, f.checks(t), "chain:unmanaged")
		if got.Status != Warn {
			t.Fatalf("a look-alike entry = %q, want %q: %s", got.Status, Warn, got.Detail)
		}
		if !strings.Contains(got.Detail, "pre-push") {
			t.Errorf("the report must name the file that will never run: %s", got.Detail)
		}
	})

	t.Run("a managed name dangles", func(t *testing.T) {
		f := newChainFixture(t)
		path := filepath.Join(f.chain, "post-checkout")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.root, "bin", "agents-0.4.0"), path); err != nil {
			t.Fatal(err)
		}
		// Reported once, by chain:entries, which owns those names.
		if got := findCheck(t, f.checks(t), "chain:unmanaged"); got.Status != OK {
			t.Errorf("a dangling managed name = %q, want %q: it is chain:entries' finding, not this check's: %s",
				got.Status, OK, got.Detail)
		}
		if got := findCheck(t, f.checks(t), "chain:entries"); got.Status != Fail {
			t.Errorf("the check that owns managed names must report it: %+v", got)
		}
	})
}

// chain:local is the cheap half of the shadowing question: it reads the
// repository-local value alone. Exit 1 is git's "no such setting" and must stay
// OK, while an unreadable answer is a failure -- reading "could not ask" as
// "nothing set" is how a shadowed hook chain passes silently.
func TestChainLocalClassifiesTheRepositoryOverride(t *testing.T) {
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
			got := checkChainLocal("/repo", func(dir string, args ...string) GitResult {
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

	t.Run("no git runner", func(t *testing.T) {
		if got := checkChainLocal("/repo", nil); got.Status != Fail {
			t.Errorf("no runner = %q, want %q: a report that cannot look must not pass", got.Status, Fail)
		}
	})
}

// chain:legacy looks for the retired dispatcher shim in the repository's own
// hooks directory -- the one a global core.hooksPath shadows. Its whole safety
// property is that it matches exact bytes: a shim left behind still runs and
// fails, while a hook the human wrote must never be named as removable.
func TestChainLegacyDetectsOnlyTheExactRetiredShim(t *testing.T) {
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
		if got := checkChainLegacy("/repo", deps); got.Status != OK {
			t.Errorf("an empty hooks directory = %q, want %q: %s", got.Status, OK, got.Detail)
		}
	})

	t.Run("a foreign hook that is not the retired shim", func(t *testing.T) {
		dir, deps := newLegacy(t)
		writeFixture(t, filepath.Join(dir, "pre-commit"), "#!/bin/sh\nexit 0\n")
		if got := checkChainLegacy("/repo", deps); got.Status != OK {
			t.Errorf("a hand-written hook = %q, want %q: naming it would tell the operator to delete their own hook: %s",
				got.Status, OK, got.Detail)
		}
	})

	t.Run("the retired shim is still installed", func(t *testing.T) {
		_, deps := newLegacy(t, "pre-commit", "commit-msg")
		got := checkChainLegacy("/repo", deps)
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
		got := checkChainLegacy("/repo", Dependencies{})
		if got.Status != Fail {
			t.Errorf("no resolver = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("the resolver returns a relative path", func(t *testing.T) {
		deps := Dependencies{LegacyHooksPath: func(string) (string, error) { return ".git/hooks", nil }}
		if got := checkChainLegacy("/repo", deps); got.Status != Fail {
			t.Errorf("a relative hooks directory = %q, want %q: %s", got.Status, Fail, got.Detail)
		}
	})

	t.Run("the resolver fails", func(t *testing.T) {
		deps := Dependencies{LegacyHooksPath: func(string) (string, error) { return "", errors.New("no git dir") }}
		if got := checkChainLegacy("/repo", deps); got.Status != Fail {
			t.Errorf("a failed resolution = %q, want %q: an unresolvable question must not read as clean: %s",
				got.Status, Fail, got.Detail)
		}
	})
}

// The record's three keys are the whole format, and doctor's parser must agree
// with the entry's: the same bytes are read by both, and a disagreement would
// mean a machine whose commits work and whose doctor fails, or the reverse.
func TestChainRecordParserAcceptsTheEntryFormat(t *testing.T) {
	record, err := parseChainRecord("/chain.env", []byte(
		"# Written by git/install-hooks.sh. Re-run the installer to change it.\n"+
			"format=1\nbinary=/opt/homebrew/bin/agents\ncheckout=/Users/nilbot/dotfiles\n"))
	if err != nil {
		t.Fatalf("the generated record did not parse: %v", err)
	}
	if record.Format != "1" || record.Binary != "/opt/homebrew/bin/agents" || record.Checkout != "/Users/nilbot/dotfiles" {
		t.Errorf("parsed %+v, want the three generated values", record)
	}

	t.Run("comments and blank lines are skipped", func(t *testing.T) {
		record, err := parseChainRecord("/chain.env", []byte(
			"# comment\n\nformat=1\nbinary=/bin/agents\ncheckout=-\n"))
		if err != nil {
			t.Fatalf("a record with a comment and a blank line did not parse: %v", err)
		}
		if record.Checkout != "-" {
			t.Errorf("checkout = %q, want -", record.Checkout)
		}
	})

	t.Run("nothing after the first = is interpreted", func(t *testing.T) {
		record, err := parseChainRecord("/chain.env", []byte(
			"format=1\nbinary=/bin/agents\ncheckout=/checkout=a=b\n"))
		if err != nil {
			t.Fatalf("a checkout containing = did not parse: %v", err)
		}
		if record.Checkout != "/checkout=a=b" {
			t.Errorf("checkout = %q, want the value taken literally", record.Checkout)
		}
	})
}

// ---------------------------------------------------------------------------
// attributes:global
// ---------------------------------------------------------------------------

// attributesFixture is the machine's attributes setup: a link at
// ~/.gitattributes resolving to a file that exists, and a repository carrying
// its own copy of the rule.
//
// The recorded link is no longer compared against <checkout>/git/gitattributes:
// with no compiled checkout root there is no independent statement of where it
// should point, so the fixture only has to be a real file.
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
		AttributesLink:        f.link,
		AttributesConfigValue: "~/.gitattributes",
		GlobalGitConfig:       filepath.Join(root, "home", ".gitconfig"),
	}
	writeFixture(t, f.source, "")
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
		// The setting lives in the tracked shared file, which the machine's
		// global config includes -- the shape the real machine has.
		gitGlobalAttributesQuestion: {Output: gitConfigOriginOutput(
			"file:"+f.source, f.deps.AttributesConfigValue)},
		gitGlobalIncludesQuestion: {Output: f.source + "\n"},
	}
}

func TestCheckAttributesGlobal(t *testing.T) {
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
					"file:"+f.deps.GlobalGitConfig, f.deps.AttributesConfigValue,
					"file:"+filepath.Join(f.root, "elsewhere"), "~/.gitattributes",
				)}
				f.deps.Git = gitAnswers(t, answers)
			},
		},
		{
			name: "the global setting comes from a file this review does not own",
			want: Fail,
			prepare: func(t *testing.T, f *attributesFixture) {
				answers := f.exactGitAnswers()
				answers[gitGlobalAttributesQuestion] = GitResult{Output: gitConfigOriginOutput(
					"file:"+filepath.Join(f.root, "elsewhere"), f.deps.AttributesConfigValue)}
				f.deps.Git = gitAnswers(t, answers)
			},
		},
		{
			name: "the global setting has the wrong value",
			want: Fail,
			prepare: func(t *testing.T, f *attributesFixture) {
				answers := f.exactGitAnswers()
				answers[gitGlobalAttributesQuestion] = GitResult{Output: gitConfigOriginOutput(
					"file:"+f.deps.GlobalGitConfig, "~/.config/git/attributes")}
				f.deps.Git = gitAnswers(t, answers)
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
			name: "the repository carries user rules beside ours",
			want: OK,
			prepare: func(t *testing.T, f *attributesFixture) {
				writeFixture(t, filepath.Join(f.repo, ".gitattributes"),
					"*.lock -diff\n.agents/** linguist-generated=true\n")
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
			got := checkAttributesGlobal(f.repo, f.deps)
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

// The repository half is checked before the machine half, because it is the one
// that works on a machine that never installed anything: a repository whose
// .gitattributes is missing must be told to run `agents init` whatever the
// global link looks like.
func TestCheckAttributesGlobalReportsTheRepositoryHalfFirst(t *testing.T) {
	f := newAttributesFixture(t)
	f.deps.Git = gitAnswers(t, f.exactGitAnswers())
	if err := os.Remove(filepath.Join(f.repo, ".gitattributes")); err != nil {
		t.Fatal(err)
	}
	got := checkAttributesGlobal(f.repo, f.deps)
	if got.Status != Fail || !strings.Contains(got.Detail, "repository .gitattributes") {
		t.Fatalf("a missing repository rule = %q (%s), want a Fail naming the repository file", got.Status, got.Detail)
	}
	if !strings.Contains(got.Remedy, "agents init") {
		t.Errorf("the remedy for the repository half is `agents init`: %s", got.Remedy)
	}
}

// The remedy is the installer command, and it is only printable when the
// installer is where the chain's shape says it is. A checkout that is not at
// <chain>/../../git/install-hooks.sh must not produce a command that cannot run.
func TestHookInstallerRemedyNamesTheInstallerOnlyWhenItExists(t *testing.T) {
	root := t.TempDir()
	chain := filepath.Join(root, "git", "hooks.d")
	writeFixture(t, filepath.Join(root, "git", "install-hooks.sh"), "#!/bin/sh\n")
	deps := Dependencies{GlobalGitConfig: filepath.Join(root, "home", ".gitconfig")}

	got := hookInstallerRemedy(chain, deps, true)
	for _, want := range []string{filepath.Join(root, "git", "install-hooks.sh"), "--adopt-owned", root} {
		if !strings.Contains(got, want) {
			t.Errorf("the remedy must carry %q: %s", want, got)
		}
	}

	elsewhere := filepath.Join(t.TempDir(), "not-a-checkout", "git", "hooks.d")
	if got := hookInstallerRemedy(elsewhere, deps, false); got != "run the reviewed global hook installer" {
		t.Errorf("a chain with no installer beside it = %q, want the sentence that names no path", got)
	}
}

// The attributes origin guard survives the loss of the compiled root by asking
// a question that needs no root: is the file that set core.attributesFile one
// the machine's own global config reads? Both spellings git allows are covered,
// because a guard that only recognises one of them fails a correct machine.
func TestAttributesOriginIsReviewed(t *testing.T) {
	home := t.TempDir()
	deps := Dependencies{GlobalGitConfig: filepath.Join(home, ".gitconfig")}
	deps.Git = func(string, ...string) GitResult {
		return GitResult{Output: "~/dotfiles/git/gitconfig.shared\nrelative/shared\n"}
	}

	cases := []struct {
		name   string
		origin string
		want   bool
	}{
		{"the primary global config", "file:" + deps.GlobalGitConfig, true},
		{"an include named with ~", "file:" + filepath.Join(home, "dotfiles/git/gitconfig.shared"), true},
		{"an include named relatively", "file:" + filepath.Join(home, "relative/shared"), true},
		{"a file nothing includes", "file:" + filepath.Join(home, "elsewhere/config"), false},
		{"an origin that is not a file", "command line", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attributesOriginIsReviewed("/repo", deps, tc.origin); got != tc.want {
				t.Errorf("attributesOriginIsReviewed(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}
