package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/harness"
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
// The manifest declares the stores, so init creates nothing (design §7.5).
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
	after := snapshotTree(t, root)
	for rel, want := range before {
		if got, ok := after[rel]; !ok || got != want {
			t.Fatalf("init changed %s", rel)
		}
	}
	for rel := range after {
		if _, existed := before[rel]; existed {
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
// Controller ruling R4 keeps the layout flags (`--template`, `--stores`,
// `--archive`) out of this task, so no `init` invocation can reach a v2 write
// yet. The refusal is therefore asked of the guard directly, and the CLI half
// pins what init does today: it refuses the flag it does not define without
// writing anything. Task 9 flips `layoutFlagsPresent` and this call reaches the
// same guard, at which point the Malformed assertion becomes the Advisory one.
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
			if code := runInitWithVersion([]string{"--template", "content-vault"}, &out, "v0.6.0"); code != exitcode.Malformed {
				t.Fatalf("exit = %d, want Malformed while the layout flags do not exist: %s", code, out.String())
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
