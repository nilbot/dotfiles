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

func TestFleetUpdateApplyDriftAdvisoryOnDriftedRepo(t *testing.T) {
	drifted := cleanFleetRepo(t)
	if err := os.WriteFile(filepath.Join(drifted, "AGENTS.md"), []byte("# Drifted Agent Context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveFleetRegistry(t, registry.Entry{Path: drifted, Added: time.Unix(1, 0).UTC()})

	var called []string
	var out bytes.Buffer
	code := runFleetUpdateWithWire([]string{"--all", "--apply"}, &out, func(path string, _ io.Writer) int {
		called = append(called, path)
		return exitcode.OK
	})
	if code != exitcode.Advisory {
		t.Fatalf("exit=%d want Advisory; output=%q", code, out.String())
	}
	if len(called) != 1 || called[0] != drifted {
		t.Fatalf("calls=%q want [%s]", called, drifted)
	}
	wantNotice := fmt.Sprintf("notice: %s has context drift; run 'migrating-fleet-context' agent skill to migrate", strconv.QuoteToASCII(drifted))
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
	expectedContent, err := scaffold.AssetsFS.ReadFile("assets/skills/migrating-fleet-context/SKILL.md")
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
