package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/registry"
	"github.com/nilbot/dotfiles/agents/internal/repo"
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
		".agents/skills", "CLAUDE.md", "AGENTS.md",
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
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills")); err != nil {
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	runInit(nil, &out)

	// The generated indexes went with the stores they described. What init
	// still owes a repository is the scaffolding that makes it usable at all.
	for _, rel := range []string{
		"CLAUDE.md",
		filepath.Join(".agents", "skills"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("init did not create %s: %v", rel, err)
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

// A test that registers into the machine's real fleet registry is a test that
// escaped its sandbox. machine.StateDir() reads XDG_STATE_HOME first and
// os.UserHomeDir() otherwise, so a test that sets neither writes to the
// developer's own ~/.local/state/agents/registry.json -- measured 2026-08-14 at
// 56 entries on this machine, of which 2 were real repositories.
//
// This asserts containment, not portability. The suite already PASSES under a
// synthetic HOME; passing was never the question.
func TestInitDoesNotTouchTheAmbientStateDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	if code := runInit(nil, &out); code != exitcode.OK && code != exitcode.Advisory {
		t.Fatalf("runInit = %d; %s", code, out.String())
	}

	registry := filepath.Join(state, "agents", "registry.json")
	if _, err := os.Stat(registry); err != nil {
		t.Fatalf("init did not use XDG_STATE_HOME; registry absent at %s: %v", registry, err)
	}
	data, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), root) {
		t.Errorf("the registry under XDG_STATE_HOME does not name %s:\n%s", root, data)
	}
}

// ignoreAgents appends one ignore rule to the file that spelling belongs in: a
// "/"-prefixed pattern is machine-local and goes to info/exclude in the common
// directory, anything else is the repository's tracked .gitignore.
//
// The layout package and repo package each carry a private copy of this helper
// because Go test helpers cannot cross a package boundary. This is the main
// package's copy, for the commands that must refuse a machine-local manifest
// (design §0.7).
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

// snapshotTree records every path under root with its mode and bytes, so a test
// can assert that a refused command wrote nothing at all -- including a file it
// rewrote with the bytes it already had, which a content-only comparison would
// call unchanged.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			snap[rel] = "dir " + info.Mode().String()
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "symlink " + target
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = info.Mode().String() + " " + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

// The guard approves a supported v2 layout, and the binary that may write it
// must then write *that* layout: `init` used to create the v1 docs/ shell even
// here, which is the same anti-shell failure design §1.2 records for v0.5.1.
// The manifest declares the stores, so init creates no layout (design §7.5).
// The machine exclude file is not layout, and is written anyway.
func TestInitOnSupportedV2WritesNoV1Shell(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newV2RepoForCmd(t, ".context")
	t.Chdir(root)
	before := snapshotTree(t, root)

	var out bytes.Buffer
	if code := runInitWithVersion(nil, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want the v1 trust-step Advisory this command already returns: %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("init created a docs/ shell in a supported v2 repository")
	}
	// Every file that was already there is untouched: the manifest, the four
	// declared stores and their READMEs, the router, .gitattributes. `init`
	// still wires harnesses -- that is its other job, and the only thing it may
	// add to a repository whose layout the manifest already declares.
	//
	// .git is machine state, not the tree this asserts about: the exclude file
	// there is expected to change, and the assertions below cover it.
	after := snapshotTree(t, root)
	for rel, want := range before {
		if isGitDirEntry(rel) {
			continue
		}
		if got, ok := after[rel]; !ok || got != want {
			t.Fatalf("init changed %s", rel)
		}
	}
	for rel := range after {
		if _, existed := before[rel]; existed || isGitDirEntry(rel) {
			continue
		}
		// Machine wiring is the one thing `init` may add here; it is what this
		// command is for, and these are the paths scaffold already treats as
		// machine-local. Nothing else may appear: not docs/, not a store, not
		// a skill.
		if rel == ".claude" || rel == ".codex" ||
			strings.HasPrefix(rel, ".claude"+string(filepath.Separator)) ||
			strings.HasPrefix(rel, ".codex"+string(filepath.Separator)) ||
			rel == filepath.Join(".agents", "hooks.json") ||
			rel == filepath.Join(".agents", ".agents-wire.lock") {
			continue
		}
		t.Fatalf("init created %s in a repository whose layout it must not write", rel)
	}

	// The machine wiring init just wrote is excluded, so `git add .` in this
	// repository cannot commit one machine's settings.json -- and the manifest
	// is not hidden, because that rule is only --local's.
	exclude := readExcludeFile(t, root)
	for _, want := range []string{
		"/.claude/settings.json", "/.codex/hooks.json",
		"/.agents/hooks.json", "/.agents/.agents-wire.lock",
	} {
		if !hasExcludeLine(exclude, want) {
			t.Errorf("exclude file is missing %q:\n%s", want, exclude)
		}
	}
	if hasExcludeLine(exclude, "/.agents/") {
		t.Errorf("init hid the whole .agents/ directory, manifest included:\n%s", exclude)
	}
}

// Decision 6, design §0.7: --local's one mechanism is an ignore rule for the
// whole .agents/ directory, and on a v2 repository that rule hides the manifest
// -- the file that says which stores are the repository's. A clone would then
// resolve v1 while the tracked stores sat at the v2 paths, so the command
// refuses instead of writing machine state that breaks the repository elsewhere.
func TestInitRefusesLocalOnASupportedV2Repository(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newV2RepoForCmd(t, ".context")
	t.Chdir(root)
	before := snapshotTree(t, root)

	var out bytes.Buffer
	if code := runInitWithVersion([]string{"--local"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--local is not supported") {
		t.Fatalf("the refusal must name the flag: %s", out.String())
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("--local init wrote into a supported v2 repository")
	}
	if exclude := readExcludeFile(t, root); hasExcludeLine(exclude, "/.agents/") {
		t.Fatalf("--local wrote the .agents/ rule that hides the manifest:\n%s", exclude)
	}
}

func isGitDirEntry(rel string) bool {
	return rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator))
}

// readExcludeFile reads the repository's machine exclude file, empty when git
// has not created it yet: the assertions are about which rules are present.
func readExcludeFile(t *testing.T, root string) string {
	t.Helper()
	path, err := repo.InfoExcludePath(root)
	if err != nil {
		t.Fatalf("resolve info/exclude for %s: %v", root, err)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func hasExcludeLine(exclude, want string) bool {
	for _, line := range strings.Split(exclude, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// A manifest this binary may not write is refused before anything is written:
// v1 `init` creates docs/{design,plans,journal,qna}, which in a v2 repository
// are the wrong stores entirely.
func TestInitRefusesUnsupportedV2WithoutWriting(t *testing.T) {
	// The refusal happens before registration. A guard that regressed would
	// register this fixture, so the fleet cache is kept out of the machine's
	// own state directory.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
	// The reason is the bare token the fleet listing prints, not a sentence:
	// `skip (layout below_floor)` and this refusal must name the same thing.
	if !strings.Contains(out.String(), "refusing to write: layout below_floor") {
		t.Fatalf("the refusal must name the reason: %s", out.String())
	}
}

// The guard covers every reason a manifest refuses, not only the version floor.
// An invalid manifest is a repository whose stores cannot be resolved, and a
// migration in flight is one whose stores are being moved right now; v1 `init`
// would write docs/{design,plans,journal,qna} into both.
func TestInitRefusesInvalidAndMigratingManifestsWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		root func(*testing.T) string
	}{
		{"invalid", "manifest invalid:", func(t *testing.T) string {
			return newManifestRepoForCmd(t, "{ not json")
		}},
		{"migrating", "refusing to write: layout migrating", newMigratingRepoForCmd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := tc.root(t)
			t.Chdir(root)
			before := snapshotTree(t, root)
			var out bytes.Buffer
			if code := runInitWithVersion(nil, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("init wrote into a repository whose manifest it must not touch")
			}
			if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
				t.Fatal("init created a docs/ shell in a repository with a manifest")
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("refusal output missing %q: %s", tc.want, out.String())
			}
		})
	}
}

// The pre-flight asks both paths too: `/.agents/**` is invisible to a
// directory-only query, and `--local` itself writes the `/.agents/` form.
//
// Task 9 flips `layoutFlagsPresent` from this flag, so the invocation below now
// reaches the same guard the direct call above does: Advisory, tree untouched,
// and the reason names the ignored .agents/ rather than the malformed input the
// flag used to be.
func TestInitRefusesV2FlagsWhenAgentsIsIgnored(t *testing.T) {
	for _, pattern := range []string{"/.agents/", "/.agents", "/.agents/**", ".agents/"} {
		t.Run(pattern, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := newRepoWithAgents(t)
			t.Chdir(root)
			ignoreAgents(t, root, pattern)
			before := snapshotTree(t, root)

			// The v2 pre-flight: a manifest here would be machine-local.
			if _, refusal := layoutRefusal(root, "v0.6.0", true); !strings.Contains(refusal, "ignored") {
				t.Fatalf("v2 pre-flight reason = %q, want it to name the ignored .agents/", refusal)
			}

			var out bytes.Buffer
			if code := runInitWithVersion([]string{"--template", "content-vault"}, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("exit = %d, want the Advisory refusal of the trackedness pre-flight: %s", code, out.String())
			}
			if !strings.Contains(out.String(), "ignored") {
				t.Fatalf("the refusal must name the ignored .agents/: %s", out.String())
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("init wrote a v2 layout into a repository whose .agents/ is ignored")
			}

			// v1 is unchanged: `init --local` is the command that creates the
			// ignored .agents/ in the first place, so this pre-flight must not
			// refuse it.
			if _, refusal := layoutRefusal(root, "v0.6.0", false); refusal != "" {
				t.Fatalf("the v1 pre-flight refused %s: %q", pattern, refusal)
			}
			out.Reset()
			if code := runInitWithVersion([]string{"--local"}, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("--local init = %d, want the v1 trust-step Advisory: %s", code, out.String())
			}
		})
	}
}

// readManifestForCmd reads the manifest a command wrote. A missing or
// unparseable document is a failure rather than an empty result: every
// assertion built on it is about bytes the command actually produced.
func readManifestForCmd(t *testing.T, root string) layout.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(layout.ManifestRel)))
	if err != nil {
		t.Fatal(err)
	}
	var m layout.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// R1, and the trap Task 7's implementer flagged: the layout written to the
// manifest must be the one the scaffold receives. Re-resolving after the write
// would return the document just written, the §7.5 no-op keys on a manifest
// that is present and active, and the repository would be left with a manifest
// and no stores at all. The assertions are about the stores, not the document.
//
// The template name is a creation input and has no wire identity, so this also
// pins that the expanded map -- never the name -- is what lands on disk.
func TestInitLayoutFlagsCreateTheStoresTheyRecord(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want map[string]string
		// absent names paths whose existence would mean an earlier --stores
		// value survived instead of being overridden.
		absent []string
	}{
		{
			"content-vault",
			[]string{"--template", "content-vault"},
			map[string]string{
				layout.RoleDesign: ".context/design", layout.RolePlans: ".context/plans",
				layout.RoleJournal: ".context/journal", layout.RoleQNA: ".context/qna",
			},
			nil,
		},
		{
			// code-repo is the v2 layout whose stores are the v1 paths: a
			// repository can adopt v2 without moving a document (design §0).
			"code-repo",
			[]string{"--template", "code-repo"},
			map[string]string{
				layout.RoleDesign: "docs/design", layout.RolePlans: "docs/plans",
				layout.RoleJournal: "docs/journal", layout.RoleQNA: "docs/qna",
			},
			nil,
		},
		{
			// No template: --stores supplies every role, which is what custom
			// and the empty template mean (design §0.4).
			"stores-only",
			[]string{
				"--stores", "design=architecture/design",
				"--stores", "plans=architecture/plans",
				"--stores", "journal=notes/log",
				"--stores", "qna=notes/qna",
			},
			map[string]string{
				layout.RoleDesign: "architecture/design", layout.RolePlans: "architecture/plans",
				layout.RoleJournal: "notes/log", layout.RoleQNA: "notes/qna",
			},
			nil,
		},
		{
			"override",
			[]string{"--template", "content-vault", "--stores", "qna=notes/qna"},
			map[string]string{
				layout.RoleDesign: ".context/design", layout.RolePlans: ".context/plans",
				layout.RoleJournal: ".context/journal", layout.RoleQNA: "notes/qna",
			},
			nil,
		},
		{
			// The flag is applied in the order it was given, so a role named
			// twice is last-wins: the value the operator typed last is the one
			// they meant. The first path must never reach the tree.
			"repeated-role-last-wins",
			[]string{
				"--template", "content-vault",
				"--stores", "qna=notes/first",
				"--stores", "qna=notes/second",
			},
			map[string]string{
				layout.RoleDesign: ".context/design", layout.RolePlans: ".context/plans",
				layout.RoleJournal: ".context/journal", layout.RoleQNA: "notes/second",
			},
			[]string{"notes/first"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := newRepo(t)
			t.Chdir(root)

			var out bytes.Buffer
			if code := runInitWithVersion(tc.args, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("exit = %d, want the trust-step Advisory: %s", code, out.String())
			}

			m := readManifestForCmd(t, root)
			if m.Schema != layout.SchemaV2 || m.LayoutStatus != layout.StatusActive ||
				m.MinMutVerFloor != layout.MinMutVerFloorV2 {
				t.Fatalf("manifest header = %+v", m)
			}
			if !reflect.DeepEqual(m.Stores, tc.want) {
				t.Fatalf("manifest stores = %v, want %v", m.Stores, tc.want)
			}
			for role, store := range tc.want {
				rel := filepath.Join(filepath.FromSlash(store), "README.md")
				if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
					t.Errorf("the recorded %s store %s was not created: %v", role, store, err)
				}
			}
			for _, rel := range tc.absent {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
					t.Errorf("%s exists, so an earlier --stores value was not overridden: %v", rel, err)
				}
			}
			// docs/ is v1's store shell. A v2 layout whose stores are not under
			// docs/ must not leave that shell behind; code-repo legitimately
			// puts its stores there, so the check follows the layout asked for.
			usesDocs := false
			for _, store := range tc.want {
				usesDocs = usesDocs || strings.HasPrefix(store, "docs/")
			}
			if _, err := os.Stat(filepath.Join(root, "docs")); !usesDocs && !os.IsNotExist(err) {
				t.Error("a v2 creation whose stores are elsewhere left the v1 docs/ shell behind")
			}
			router, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			if string(router) != layout.V2AgentsMD {
				t.Error("a v2 creation wrote the v1 router")
			}
		})
	}
}

// Creation is a mutation, and the layout it must be gated on is the one being
// created -- not the one this repository resolves to. For a manifest-less
// repository the resolved layout is the implicit v1 one, which layout.Support
// always accepts; asking it would let an unstamped dev build write a v0.6.0
// manifest it then refuses to manage (design §5.3, §7.5). The paired control is
// the same invocation with a release version, which must still create.
func TestInitCreationIsGatedOnTheRunningVersion(t *testing.T) {
	args := []string{"--template", "content-vault"}

	for _, tc := range []struct{ running, reason string }{
		{"dev", "unreleased"},      // a source build cannot prove its release
		{"v0.5.99", "below_floor"}, // older than the manifest it would write
	} {
		t.Run(tc.running+" refuses without writing", func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := newRepo(t)
			t.Chdir(root)
			before := snapshotTree(t, root)

			var out bytes.Buffer
			if code := runInitWithVersion(args, &out, tc.running); code != exitcode.Advisory {
				t.Fatalf("exit = %d, want the Advisory an unsupported layout gets: %s", code, out.String())
			}
			if !strings.Contains(out.String(), tc.reason) {
				t.Fatalf("the refusal must name the reason %q: %s", tc.reason, out.String())
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(layout.ManifestRel))); !os.IsNotExist(err) {
				t.Fatal("an unsupported build wrote the manifest")
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("an unsupported build wrote a layout")
			}
		})
	}

	t.Run("v0.6.0 creates", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		root := newRepo(t)
		t.Chdir(root)

		var out bytes.Buffer
		if code := runInitWithVersion(args, &out, "v0.6.0"); code != exitcode.Advisory {
			t.Fatalf("exit = %d, want the trust-step Advisory: %s", code, out.String())
		}
		if got := readManifestForCmd(t, root).MinMutVerFloor; got != layout.MinMutVerFloorV2 {
			t.Fatalf("min_mut_ver_floor = %q, want %q", got, layout.MinMutVerFloorV2)
		}
		for _, role := range layout.Roles() {
			if _, err := os.Stat(filepath.Join(root, ".context", role, "README.md")); err != nil {
				t.Errorf("the release version did not create .context/%s: %v", role, err)
			}
		}
	})
}

// Resolution order 1: an existing manifest wins, so the layout flags cannot
// apply. Ignoring them silently would leave an operator who typed
// `--stores qna=notes/qna` believing the override took effect.
func TestInitNamesLayoutFlagsAnExistingManifestOverrides(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newV2RepoForCmd(t, ".context")
	t.Chdir(root)
	manifest := filepath.Join(root, filepath.FromSlash(layout.ManifestRel))
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runInitWithVersion([]string{"--stores", "qna=notes/qna"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want the trust-step Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--stores") || !strings.Contains(out.String(), "not applied") {
		t.Fatalf("the output must name the ignored flag: %s", out.String())
	}
	after, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("init rewrote a manifest that already declares the layout")
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "qna")); !os.IsNotExist(err) {
		t.Fatal("an ignored --stores override created its store")
	}
}

// The template name has no wire identity (design §0.4): no profile, no
// store_root, no template field, and the name itself must not appear.
func TestInitManifestCarriesNoTemplateName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInitWithVersion(
		[]string{"--template", "content-vault", "--stores", "qna=notes/qna"},
		&out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want the trust-step Advisory: %s", code, out.String())
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(layout.ManifestRel)))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"profile", "template", "store_root", "content-vault"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("the manifest names %q:\n%s", forbidden, data)
		}
	}
}

// R2: --archive records the path and does not create it. The archive is a place
// the manifest protects (V12); V12 does not make a directory.
func TestInitRecordsTheArchiveWithoutCreatingIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInitWithVersion(
		[]string{"--template", "content-vault", "--archive", ".context/archive"},
		&out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want the trust-step Advisory: %s", code, out.String())
	}
	if got := readManifestForCmd(t, root).Archive; got != ".context/archive" {
		t.Fatalf("archive = %q, want .context/archive", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".context", "archive")); !os.IsNotExist(err) {
		t.Error("init created the archive directory, which V12 protects rather than makes")
	}
}

// R2's default: with no --archive, the target inherits the v1 archive the tree
// already has, and never invents a path.
//
// This default is unreachable through the CLI with docs/archive present: a
// repository that has docs/ is refused by design §7.5's existing-layout rule
// before any flag is applied. The rule still governs the layout this command
// constructs, so it is pinned at the seam that implements it.
func TestInitArchiveDefaultsToTheV1Archive(t *testing.T) {
	withArchive := newRepo(t)
	if err := os.MkdirAll(filepath.Join(withArchive, "docs", "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := archiveForInit(withArchive, false, "")
	if err != nil || got != "docs/archive" {
		t.Errorf("archive default = (%q, %v), want docs/archive", got, err)
	}
	got, err = archiveForInit(newRepo(t), false, "")
	if err != nil || got != "" {
		t.Errorf("archive default with no docs/archive = (%q, %v), want empty", got, err)
	}
	got, err = archiveForInit(withArchive, true, "notes/archive")
	if err != nil || got != "notes/archive" {
		t.Errorf("supplied archive = (%q, %v), want notes/archive", got, err)
	}
	// An explicitly empty --archive is an input the operator supplied, not the
	// absence of one, so it is refused rather than recorded as "no archive".
	if got, err := archiveForInit(withArchive, true, ""); err == nil {
		t.Errorf(`--archive "" = (%q, nil), want a refusal`, got)
	}
}

// Every malformed layout flag is a CLI typo, not a manifest to write: exit 3,
// a message naming the offending value, and nothing on disk.
func TestInitRejectsMalformedLayoutFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--bogus"}, "bogus"},
		{"unknown template", []string{"--template", "prose-vault"}, "prose-vault"},
		{"store without an equals sign", []string{"--stores", "design"}, "design"},
		{"store without a role", []string{"--stores", "=docs/design"}, "=docs/design"},
		{"store without a path", []string{"--stores", "design="}, "design="},
		{"role outside the four", []string{"--template", "content-vault", "--stores", "changelog=notes/changelog"}, "changelog"},
		{"missing role without a template", []string{"--stores", "qna=notes/qna"}, "plans"},
		{"custom with no stores", []string{"--template", "custom"}, "design"},
		{"absolute store path", []string{"--template", "content-vault", "--stores", "design=/tmp/design"}, "/tmp/design"},
		{"escaping store path", []string{"--template", "content-vault", "--stores", "design=../design"}, "../design"},
		{"overlapping stores", []string{"--template", "content-vault", "--stores", "qna=.context/design"}, ".context/design"},
		{"escaping archive", []string{"--template", "content-vault", "--archive", "../archive"}, "../archive"},
		{"archive equal to a store", []string{"--template", "content-vault", "--archive", ".context/design"}, ".context/design"},
		{"archive containing a store", []string{"--template", "content-vault", "--archive", ".context"}, ".context"},
		{"archive contained by a store", []string{"--template", "content-vault", "--archive", ".context/design/archive"}, ".context/design/archive"},
		{"empty archive", []string{"--template", "content-vault", "--archive="}, "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := newRepo(t)
			t.Chdir(root)
			before := snapshotTree(t, root)

			var out bytes.Buffer
			if code := runInitWithVersion(tc.args, &out, "v0.6.0"); code != exitcode.Malformed {
				t.Fatalf("exit = %d, want Malformed (3): %s", code, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("the rejection must name %q: %s", tc.want, out.String())
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
				t.Error("a malformed invocation wrote into the repository")
			}
		})
	}
}

// design §7.5: with no manifest and a v1 layout already in the tree, adopting
// the repository is `agents layout migrate`'s job -- it moves the stores and
// proves the router is boilerplate. Every layout flag is refused, before
// anything is written, whichever marker the tree carries.
func TestInitRefusesLayoutFlagsOnAnExistingV1Layout(t *testing.T) {
	markers := []struct {
		name string
		root func(*testing.T) string
	}{
		{"agents-md", func(t *testing.T) string {
			root := newRepo(t)
			if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# existing\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return root
		}},
		{"docs", func(t *testing.T) string {
			root := newRepo(t)
			if err := os.MkdirAll(filepath.Join(root, "docs", "design"), 0o755); err != nil {
				t.Fatal(err)
			}
			return root
		}},
		{"scaffolded", newRepoWithAgents},
	}
	flags := []struct {
		name string
		args []string
	}{
		{"template", []string{"--template", "content-vault"}},
		{"stores", []string{"--stores", "design=architecture/design"}},
		{"archive", []string{"--archive", "notes/archive"}},
	}
	for _, m := range markers {
		for _, f := range flags {
			t.Run(m.name+"/"+f.name, func(t *testing.T) {
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				root := m.root(t)
				t.Chdir(root)
				before := snapshotTree(t, root)

				var out bytes.Buffer
				if code := runInitWithVersion(f.args, &out, "v0.6.0"); code != exitcode.Advisory {
					t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
				}
				if !strings.Contains(out.String(), "--"+f.name) {
					t.Errorf("the refusal must name --%s: %s", f.name, out.String())
				}
				if !strings.Contains(out.String(), "agents layout migrate") {
					t.Errorf("the refusal must name the remedy: %s", out.String())
				}
				if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
					t.Error("a refused init wrote into the repository")
				}
			})
		}
	}
}

// Decision 6, design §0.7: a manifest a clone cannot see would leave that clone
// resolving v1 while the stores sat at the v2 paths. --local is refused with
// the layout flags exactly as it is on a repository whose manifest already
// resolves v2, and the check runs before the manifest exists.
func TestInitRefusesLocalWithLayoutFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"template", []string{"--local", "--template", "content-vault"}},
		{"stores", []string{"--local", "--stores", "design=architecture/design"}},
		{"archive", []string{"--local", "--archive", "notes/archive"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := newRepo(t)
			t.Chdir(root)
			before := snapshotTree(t, root)

			var out bytes.Buffer
			if code := runInitWithVersion(tc.args, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
			}
			if !strings.Contains(out.String(), "--local is not supported") {
				t.Errorf("the refusal must name --local and the reason: %s", out.String())
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
				t.Error("a refused --local init wrote into the repository")
			}
		})
	}
}
