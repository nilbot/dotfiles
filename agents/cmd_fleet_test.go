package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/registry"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func cleanFleetRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
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
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fleetRepo(t *testing.T, withAgents bool) string {
	t.Helper()
	root := t.TempDir()
	if withAgents {
		if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func saveFleetRegistry(t *testing.T, entries ...registry.Entry) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	r := &registry.Registry{Repos: entries}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	return registry.Path()
}

// Kills: reporting a missing cache entry as success, omitting Local metadata,
// or letting a repository-controlled newline forge another output row.
func TestFleetLSReportsRegisteredPresentAndMissingRepos(t *testing.T) {
	present := fleetRepo(t, true)
	missing := filepath.Join(t.TempDir(), "gone\nforged")
	saveFleetRegistry(t,
		registry.Entry{Path: present, Added: time.Unix(1, 0).UTC(), Local: true},
		registry.Entry{Path: missing, Added: time.Unix(2, 0).UTC()},
	)

	var out bytes.Buffer
	if code := runFleetLS(nil, &out); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory; output:\n%s", code, out.String())
	}
	body := out.String()
	if !strings.Contains(body, strconv.QuoteToASCII(present)) || !strings.Contains(body, "(local)") {
		t.Fatalf("present local repo not reported: %q", body)
	}
	if !strings.Contains(body, strconv.QuoteToASCII(missing)) || !strings.Contains(body, "no .agents/") {
		t.Fatalf("missing repo not reported actionably: %q", body)
	}
	if strings.Contains(body, missing) {
		t.Fatalf("raw repository path forged an output line: %q", body)
	}
}

// Kills: pruning an earlier in-memory snapshot after a concurrent registration,
// which would silently erase the newly registered repository.
func TestFleetLSPruneReloadsUnderLockBeforeMutation(t *testing.T) {
	missing := fleetRepo(t, false)
	path := saveFleetRegistry(t, registry.Entry{Path: missing, Added: time.Unix(1, 0).UTC()})
	concurrent := fleetRepo(t, true)

	var out bytes.Buffer
	code := runFleetLSWithBeforePrune([]string{"--prune"}, &out, func() {
		if _, err := registry.Register(concurrent, true); err != nil {
			t.Fatal(err)
		}
	})
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want OK; output:\n%s", code, out.String())
	}
	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 1 || r.Repos[0].Path != concurrent {
		t.Fatalf("pruned registry = %+v, lost concurrent registration", r.Repos)
	}
	if !strings.Contains(out.String(), "pruned 1") {
		t.Fatalf("prune result missing: %q", out.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("registry disappeared: %v", err)
	}
}

// Kills: printing the pre-lock classification while pruning a different,
// freshly locked classification of the same entry.
func TestFleetLSPruneDisplaysAndPrunesTheSameFreshSnapshot(t *testing.T) {
	repo := fleetRepo(t, false)
	saveFleetRegistry(t, registry.Entry{Path: repo, Added: time.Unix(1, 0).UTC()})

	var out bytes.Buffer
	code := runFleetLSWithBeforePrune([]string{"--prune"}, &out, func() {
		if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want OK; output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), strconv.QuoteToASCII(repo)) || strings.Contains(out.String(), "no .agents/") || !strings.Contains(out.String(), "pruned 0") {
		t.Fatalf("prune output did not use its fresh classification: %q", out.String())
	}
	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 1 || r.Repos[0].Path != repo {
		t.Fatalf("freshly present repo was pruned: %+v", r.Repos)
	}
}

// Kills: treating an indeterminate .agents stat failure as confirmed absence,
// pruning it, or allowing a hostile path to forge advisory output.
func TestFleetLSPruneRetainsAndReportsUnknownEntry(t *testing.T) {
	unknown := filepath.Join(t.TempDir(), "unknown\nforged")
	if err := os.MkdirAll(unknown, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".agents", filepath.Join(unknown, ".agents")); err != nil {
		t.Fatal(err)
	}
	saveFleetRegistry(t, registry.Entry{Path: unknown, Added: time.Unix(1, 0).UTC()})

	var out bytes.Buffer
	code := runFleetLS([]string{"--prune"}, &out)
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory; output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), strconv.QuoteToASCII(unknown)) || !strings.Contains(out.String(), "could not inspect") || !strings.Contains(out.String(), "pruned 0") || strings.Contains(out.String(), unknown) {
		t.Fatalf("unknown entry was not reported safely: %q", out.String())
	}
	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 1 || r.Repos[0].Path != unknown {
		t.Fatalf("unknown entry was pruned: %+v", r.Repos)
	}
}

// Kills: a dry run invoking rewiring, rewriting the cache, omitting a missing
// entry, or incorrectly returning success for a command that applied nothing.
func TestFleetUpdateDryRunChangesNothing(t *testing.T) {
	present := fleetRepo(t, true)
	missing := fleetRepo(t, false)
	path := saveFleetRegistry(t,
		registry.Entry{Path: present, Added: time.Unix(1, 0).UTC()},
		registry.Entry{Path: missing, Added: time.Unix(2, 0).UTC()},
	)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all"}, &out, func(string, io.Writer) int {
		calls++
		return exitcode.OK
	})
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory; output:\n%s", code, out.String())
	}
	if calls != 0 {
		t.Fatalf("dry run invoked wire %d times", calls)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("dry run changed the registry")
	}
	for _, want := range []string{"would rewire 1", strconv.QuoteToASCII(present), strconv.QuoteToASCII(missing), "skip (missing)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("dry-run output missing %q: %q", want, out.String())
		}
	}
}

// Kills: stopping at the first wire failure, invoking wire for a missing repo,
// or returning success while either failure or registered drift remains.
func TestFleetUpdateApplyContinuesAfterFailureAndSkipsMissing(t *testing.T) {
	first := fleetRepo(t, true)
	second := fleetRepo(t, true)
	missing := fleetRepo(t, false)
	saveFleetRegistry(t,
		registry.Entry{Path: first, Added: time.Unix(1, 0).UTC()},
		registry.Entry{Path: second, Added: time.Unix(2, 0).UTC()},
		registry.Entry{Path: missing, Added: time.Unix(3, 0).UTC()},
	)

	var called []string
	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all", "--apply"}, &out, func(path string, _ io.Writer) int {
		called = append(called, path)
		if path == first {
			return exitcode.NoRecord
		}
		return exitcode.OK
	})
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory; output:\n%s", code, out.String())
	}
	if len(called) != 2 || called[0] != first || called[1] != second {
		t.Fatalf("wire calls = %q, want both present repos in registry order", called)
	}
	for _, want := range []string{strconv.QuoteToASCII(first), strconv.QuoteToASCII(missing), "failed", "missing"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("apply output missing %q: %q", want, out.String())
		}
	}
}

func TestFleetUpdateApplySuccess(t *testing.T) {
	present := cleanFleetRepo(t)
	saveFleetRegistry(t, registry.Entry{Path: present, Added: time.Unix(1, 0).UTC()})
	var called []string
	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all", "--apply"}, &out, func(path string, _ io.Writer) int {
		called = append(called, path)
		return exitcode.OK
	})
	if code != exitcode.OK || len(called) != 1 || called[0] != present {
		t.Fatalf("exit=%d calls=%q output=%q", code, called, out.String())
	}
	if strings.Contains(out.String(), "notice:") || strings.Contains(out.String(), "context drift") {
		t.Fatalf("clean repo should not emit drift notice: %q", out.String())
	}
}

func TestFleetUpdateApplyDriftAdvisoryOnDivergedRepo(t *testing.T) {
	diverged := cleanFleetRepo(t)
	if err := os.WriteFile(filepath.Join(diverged, "AGENTS.md"), []byte("# Diverged Agent Context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveFleetRegistry(t, registry.Entry{Path: diverged, Added: time.Unix(1, 0).UTC()})

	var called []string
	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all", "--apply"}, &out, func(path string, _ io.Writer) int {
		called = append(called, path)
		return exitcode.OK
	})
	if code != exitcode.Advisory {
		t.Fatalf("exit=%d want Advisory; output=%q", code, out.String())
	}
	if len(called) != 1 || called[0] != diverged {
		t.Fatalf("calls=%q want [%s]", called, diverged)
	}
	wantNotice := fmt.Sprintf("notice: %s has context drift; run 'migrating-fleet-context' agent skill to migrate", strconv.QuoteToASCII(diverged))
	if !strings.Contains(out.String(), wantNotice) {
		t.Fatalf("output missing drift notice %q; got: %q", wantNotice, out.String())
	}
	if !strings.Contains(out.String(), "rewired 1 registered repo(s)") {
		t.Fatalf("output missing rewired count; got: %q", out.String())
	}
}

func TestFleetUpdateApplyRefreshesInfrastructuralSkills(t *testing.T) {
	repo := cleanFleetRepo(t)
	migratingPath := filepath.Join(repo, ".agents", "skills", "migrating-fleet-context", "SKILL.md")
	if err := os.WriteFile(migratingPath, []byte("stale migrating skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveFleetRegistry(t, registry.Entry{Path: repo, Added: time.Unix(1, 0).UTC()})

	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all", "--apply"}, &out, func(string, io.Writer) int {
		return exitcode.OK
	})
	if code != exitcode.OK {
		t.Fatalf("exit=%d want OK; output=%q", code, out.String())
	}
	// This repository has no manifest, so it resolves v1 and the canonical text
	// for the refresh is the frozen v1 asset, not the flat (v2) one. Resolve it
	// the way the code under test does rather than hardcoding a path: the
	// assertion still proves the exact bytes written.
	assetPath, err := scaffold.SkillAssetPath(layout.Resolve(repo).Schema, "migrating-fleet-context")
	if err != nil {
		t.Fatal(err)
	}
	expectedContent, err := scaffold.AssetsFS.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	gotContent, err := os.ReadFile(migratingPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotContent) != string(expectedContent) {
		t.Fatalf("migrating-fleet-context was not refreshed")
	}
}

// deleteSkill removes the agents-owned skill copy, so the refresh under test
// has something to write and its assertion cannot pass on a file that was
// already there.
func deleteSkill(t *testing.T, root string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, ".agents", "skills", "migrating-fleet-context")); err != nil {
		t.Fatal(err)
	}
}

// A supported v2 repository is wired and its agents-owned skill is rewritten
// from the v2 asset -- the half of §7.5's update paragraph that Task 7's gate
// must not swallow -- and the run leaves it current, so no drift notice is
// printed. docs/ stays absent: a v2 repository has no v1 stores, and nothing in
// update creates them.
func TestFleetUpdateRefreshesSupportedV2AndPreservesTheGate(t *testing.T) {
	root := newV2RepoForCmd(t, ".context")
	// Complete the fixture into a repository update can leave current: the
	// user-owned recording skill, the domain file, and the root symlink
	// (drift's currency predicate weighs all three). The agents-owned copy is
	// deleted, because writing it is the refresh's job.
	writeFile(t, filepath.Join(root, ".agents", "skills", "recording-what-you-learn", "SKILL.md"),
		skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn"))
	writeFile(t, filepath.Join(root, ".agents", "AGENTS.md"), "# Domain rules\n")
	if err := os.Symlink("AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	deleteSkill(t, root)
	saveFleetRegistry(t, registry.Entry{Path: root, Added: time.Unix(1, 0).UTC()})

	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.6.0")
	if code != exitcode.OK || !wireCalled {
		t.Fatalf("supported v2 = (%d, wired=%v): %s", code, wireCalled, out.String())
	}
	assetPath, err := scaffold.SkillAssetPath(layout.SchemaV2, "migrating-fleet-context")
	if err != nil {
		t.Fatal(err)
	}
	want, err := scaffold.AssetsFS.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("supported v2 skill was not refreshed to the v2 text: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("fleet update created a docs/ shell")
	}
}

func TestFleetUpdateApplySkipsAndReportsUnknownEntry(t *testing.T) {
	unknown := fleetRepo(t, false)
	if err := os.Symlink(".agents", filepath.Join(unknown, ".agents")); err != nil {
		t.Fatal(err)
	}
	saveFleetRegistry(t, registry.Entry{Path: unknown, Added: time.Unix(1, 0).UTC()})
	calls := 0
	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all", "--apply"}, &out, func(string, io.Writer) int {
		calls++
		return exitcode.OK
	})
	if code != exitcode.Advisory || calls != 0 {
		t.Fatalf("exit=%d calls=%d, want Advisory and no wire; output=%q", code, calls, out.String())
	}
	if !strings.Contains(out.String(), strconv.QuoteToASCII(unknown)) || !strings.Contains(out.String(), "could not inspect") {
		t.Fatalf("unknown entry not reported: %q", out.String())
	}
}

// An unsupported repository is skipped before wiring and before the migration
// skill refresh, so fleet update cannot downgrade a v2 repository's skill.
func TestFleetUpdateSkipsUnsupportedAndDoesNotRefreshSkill(t *testing.T) {
	root := newV2RepoForCmd(t, ".context")
	skill := filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliberately not the canonical v2 text: this is what an older, pre-v2
	// binary last wrote, and a refresh that ran would replace it.
	original := []byte("v0.5.1-era migrating-fleet-context text\n")
	if err := os.WriteFile(skill, original, 0o644); err != nil {
		t.Fatal(err)
	}
	// saveFleetRegistry points XDG_STATE_HOME at a temp directory, so this
	// entry never reaches the machine's own fleet cache.
	saveFleetRegistry(t, registry.Entry{Path: root, Added: time.Unix(1, 0).UTC()})

	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.5.99")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if wireCalled {
		t.Fatal("an unsupported repository must be skipped before wiring")
	}
	after, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("the skipped repository's migration skill was removed: %v", err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("an unsupported repository's migration skill was downgraded")
	}
	if !strings.Contains(out.String(), "skip (layout below_floor): "+strconv.QuoteToASCII(root)) {
		t.Fatalf("skip reason missing or unnamed: %s", out.String())
	}
}

// A dry run names every repository and every reason before anyone applies, so
// the first time an operator learns a repository will be skipped is not the
// apply that would have written to it.
func TestFleetUpdateSkipsUnsupportedInDryRun(t *testing.T) {
	unsupported := newV2RepoForCmd(t, ".context")
	supported := cleanFleetRepo(t)
	saveFleetRegistry(t,
		registry.Entry{Path: unsupported, Added: time.Unix(1, 0).UTC()},
		registry.Entry{Path: supported, Added: time.Unix(2, 0).UTC()},
	)

	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.5.99")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if wireCalled {
		t.Fatal("a dry run must not wire")
	}
	if !strings.Contains(out.String(), "skip (layout below_floor): "+strconv.QuoteToASCII(unsupported)) {
		t.Fatalf("dry run did not name the unsupported repository: %s", out.String())
	}
	if !strings.Contains(out.String(), "  "+strconv.QuoteToASCII(supported)) {
		t.Fatalf("dry run did not list the supported repository: %s", out.String())
	}
	// The count names what the run would rewrite. A skipped repository is not
	// one of them, exactly as a missing entry is not.
	if !strings.Contains(out.String(), "would rewire 1 registered repo(s)") {
		t.Fatalf("dry run counted a repository it would skip: %s", out.String())
	}
}

// The gate skips every reason a manifest refuses, not only the version floor:
// an invalid manifest and a migration in flight are equally unsafe to wire, and
// equally unsafe to refresh the migration skill in.
func TestFleetUpdateSkipsInvalidAndMigratingRepositories(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		root func(*testing.T) string
	}{
		{"invalid", "skip (layout manifest invalid:", func(t *testing.T) string {
			return newManifestRepoForCmd(t, "{ not json")
		}},
		{"migrating", "skip (layout migrating): ", newMigratingRepoForCmd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.root(t)
			saveFleetRegistry(t, registry.Entry{Path: root, Added: time.Unix(1, 0).UTC()})
			wireCalled := false
			var out bytes.Buffer
			code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
				func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.6.0")
			if code != exitcode.Advisory {
				t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
			}
			if wireCalled {
				t.Fatal("a repository skipped for its manifest must not be wired")
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("skip reason missing %q: %s", tc.want, out.String())
			}
			if !strings.Contains(out.String(), strconv.QuoteToASCII(root)) {
				t.Fatalf("skip line does not name the repository %s: %s", root, out.String())
			}
		})
	}
}

// The v1 control: no manifest, so the guard has no opinion and the repository
// is wired exactly as before, even for a binary that predates the floor.
func TestFleetUpdateStillWiresV1PositiveControl(t *testing.T) {
	root := cleanFleetRepo(t) // existing helper
	saveFleetRegistry(t, registry.Entry{Path: root, Added: time.Unix(1, 0).UTC()})
	wireCalled := false
	var out bytes.Buffer
	code := runFleetUpdateWithVersion([]string{"--all", "--apply"}, &out,
		func(string, io.Writer) int { wireCalled = true; return exitcode.OK }, "v0.5.99")
	if code != exitcode.OK || !wireCalled {
		t.Fatalf("v1 control = (%d, wired=%v): %s", code, wireCalled, out.String())
	}
}

func TestFleetCommandsRejectMalformedArguments(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cases := []struct {
		name string
		run  func([]string, io.Writer) int
		args []string
	}{
		{"ls flag", runFleetLS, []string{"--unknown"}},
		{"ls operand", runFleetLS, []string{"extra"}},
		{"update missing all", runFleetUpdate, nil},
		{"update operand", runFleetUpdate, []string{"--all", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := tc.run(tc.args, &out); code != exitcode.Malformed {
				t.Fatalf("exit=%d want Malformed; output=%q", code, out.String())
			}
		})
	}
}

// A complete command implementation is still dead if the binary dispatcher
// never registers it.
func TestMainRegistersFleetCommands(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if code := run([]string{"ls"}); code != exitcode.OK {
		t.Fatalf("run(ls) = %d, want OK", code)
	}
	if code := run([]string{"update", "--all"}); code != exitcode.Advisory {
		t.Fatalf("run(update --all) = %d, want Advisory", code)
	}
}
