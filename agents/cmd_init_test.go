package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/registry"
)

func TestInitScaffoldsWiresAndReportsTrust(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	// Advisory, not OK: the trust step is still outstanding, and an exit code
	// of 0 would report a working setup that is not yet working.
	if code := runInit(nil, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (advisory); output:\n%s", code, out.String())
	}

	for _, rel := range []string{
		".agents/memory", "CLAUDE.md", "AGENTS.md",
		".claude/settings.json", ".codex/hooks.json",
	} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err != nil {
			t.Errorf("init did not create %s: %v", rel, err)
		}
	}
	if !strings.Contains(out.String(), "trust") {
		t.Errorf("init must print the outstanding trust steps:\n%s", out.String())
	}
	// The word "trust" alone is satisfied by the section heading, so an init
	// that printed the heading and no steps under it would pass the check
	// above. The steps are the point: a wired repo that records nothing until
	// someone performs them is exactly what this output exists to prevent.
	for _, a := range harness.All() {
		steps := a.TrustSteps(root)
		if len(steps) == 0 {
			t.Errorf("%s reports no trust steps; no harness runs a freshly wired repo's hooks unattended", a.Name())
		}
		for _, s := range steps {
			if !strings.Contains(out.String(), s) {
				t.Errorf("init did not print %s's step %q:\n%s", a.Name(), s, out.String())
			}
		}
	}
}

// The --local flag has to reach scaffold.Create. Nothing else in the command
// observes it, so a dropped argument is silent: the layout is identical either
// way and only the exclude file differs.
func TestInitLocalKeepsAgentsDirOutOfTheRepo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInit([]string{"--local"}, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (advisory); output:\n%s", code, out.String())
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("exclude: %v", err)
	}
	var found bool
	for _, l := range strings.Split(string(exclude), "\n") {
		// A whole-line match: /.agents/.trace-cache/ is written unconditionally
		// and contains "/.agents/" as a substring.
		if strings.TrimSpace(l) == "/.agents/" {
			found = true
		}
	}
	if !found {
		t.Errorf("init --local must exclude the whole .agents/ directory:\n%s", exclude)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	runInit(nil, &out)
	before, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	runInit(nil, &out)
	after, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if string(before) != string(after) {
		t.Errorf("init is not idempotent:\n%s\n---\n%s", before, after)
	}
}

func TestInitOutsideRepoSkips(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	var out bytes.Buffer
	if code := runInit(nil, &out); code != 4 {
		t.Fatalf("exit = %d, want 4 (skip) outside a repo", code)
	}
}

// Kills: completing scaffold without registering the resolved repository root,
// registering into real user state, or dropping --local metadata.
func TestInitRegistersRepoInInjectedMachineState(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInit([]string{"--local"}, &out); code != 1 {
		t.Fatalf("exit = %d, want advisory; output:\n%s", code, out.String())
	}
	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 1 || r.Repos[0].Path != resolved || !r.Repos[0].Local {
		t.Fatalf("registered repos = %+v, want local %s", r.Repos, resolved)
	}
	wantRegistry := filepath.Join(state, "agents", "registry.json")
	if _, err := os.Stat(wantRegistry); err != nil {
		t.Fatalf("injected registry was not written at %s: %v", wantRegistry, err)
	}
}

// Kills: making an optional fleet cache a prerequisite for initialization, or
// echoing private corrupt cache bytes into terminal output.
func TestInitWarnsButContinuesWhenRegistryIsUnavailable(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	path := filepath.Join(state, "agents", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{private-project-name"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInit(nil, &out); code != 1 {
		t.Fatalf("exit = %d, want advisory despite unavailable cache; output:\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "memory")); err != nil {
		t.Fatalf("init did not scaffold after cache warning: %v", err)
	}
	if !strings.Contains(out.String(), "registry unavailable") {
		t.Fatalf("missing actionable registry warning:\n%s", out.String())
	}
	if strings.Contains(out.String(), "private-project-name") {
		t.Fatalf("warning exposed corrupt registry content:\n%s", out.String())
	}
}

// Kills: moving registration after wiring, which loses an initialized repo
// from fleet state whenever one harness config cannot be written.
func TestInitRegistersAfterScaffoldEvenWhenWiringFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	// A real directory at the generated symlink path makes harness wiring fail.
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInit(nil, &out); code != 5 {
		t.Fatalf("exit = %d, want NoRecord from wiring; output:\n%s", code, out.String())
	}
	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 1 || r.Repos[0].Path != resolved {
		t.Fatalf("scaffolded repo was not registered before wiring failed: %+v", r.Repos)
	}
}

// A message naming a path the tool no longer writes sends its reader to an
// empty directory to conclude that recording is broken.
func TestInitDoesNotPointAtTheRetiredTrackedTracePath(t *testing.T) {
	// t.Chdir is not decoration. runInit discovers its repository from the
	// working directory, so without this the test wired THIS repository with
	// the ephemeral test binary's path -- once per `go test` run, accumulating,
	// because stripOurs will not delete a command whose basename is
	// `agents.test`. Seven runs left 28 dead hooks erroring at every session
	// start. See TestMain, which now makes forgetting this harmless.
	t.Chdir(newRepo(t))

	var b bytes.Buffer
	runInit(nil, &b)
	if strings.Contains(b.String(), "reports/traces") {
		t.Error("init still points at .agents/reports/traces/, which nothing writes any more")
	}
}

// A freshly initialized repository must be committable. Without the generated
// indexes the pre-commit guard regenerates them, finds them unstaged, and
// blocks -- so `agents init` would produce a tree whose first commit fails.
func TestInitLeavesARepositoryThatCanCommit(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	runInit(nil, &out)

	for _, rel := range []string{
		filepath.Join(".agents", "memory", "INDEX.md"),
		filepath.Join(".agents", "reports", "handoff", "INDEX.md"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("init did not write %s: %v", rel, err)
		}
	}

	git(t, root, "add", "-A")
	cmd := exec.Command("git", "commit", "-m", "agents init")
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the first commit after `agents init` was blocked:\n%s", b)
	}
	if s := git(t, root, "status", "--porcelain"); strings.TrimSpace(s) != "" {
		t.Errorf("init left uncommittable residue:\n%s", s)
	}
}
