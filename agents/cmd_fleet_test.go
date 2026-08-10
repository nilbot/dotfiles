package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/registry"
)

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
	present := fleetRepo(t, true)
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
