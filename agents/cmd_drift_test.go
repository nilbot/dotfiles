package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/registry"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func newTestRepo(t *testing.T) string {
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
	return dir
}

func TestCmdDriftCleanRepo(t *testing.T) {
	dir := newTestRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runDrift([]string{"--repo", dir}, &out)
	if code != exitcode.OK {
		t.Fatalf("runDrift exit=%d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	body := out.String()
	if !strings.Contains(body, "clean_current") {
		t.Errorf("output missing clean_current: %s", body)
	}
	if !strings.Contains(body, "recording-what-you-learn:") {
		t.Errorf("output missing recording skill: %s", body)
	}
	if !strings.Contains(body, "migrating-fleet-context:") {
		t.Errorf("output missing migration skill: %s", body)
	}
}

func TestCmdDriftDriftedRepo(t *testing.T) {
	dir := newTestRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	// Append custom rule to root AGENTS.md to trigger drift
	agentsPath := filepath.Join(dir, "AGENTS.md")
	f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n## Custom Domain Rules\n- Use Python uv\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var out bytes.Buffer
	code := runDrift([]string{"--repo", dir}, &out)
	if code != exitcode.Advisory {
		t.Fatalf("runDrift exit=%d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	body := out.String()
	if !strings.Contains(body, "drifted") {
		t.Errorf("output missing 'drifted': %s", body)
	}
	if !strings.Contains(body, "--- canonical/AGENTS.md") || !strings.Contains(body, "+## Custom Domain Rules") {
		t.Errorf("output missing unified diff: %s", body)
	}
}

func TestCmdDriftMissingAGENTS(t *testing.T) {
	dir := newTestRepo(t)
	var out bytes.Buffer
	code := runDrift([]string{"--repo", dir}, &out)
	if code != exitcode.Advisory {
		t.Fatalf("runDrift exit=%d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	body := out.String()
	if !strings.Contains(body, "missing") {
		t.Errorf("output missing 'missing' status: %s", body)
	}
}

func TestCmdDriftJSONOutput(t *testing.T) {
	dir := newTestRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runDrift([]string{"--repo", dir, "--json"}, &out)
	if code != exitcode.OK {
		t.Fatalf("runDrift exit=%d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	var r drift.DriftReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, out.Bytes())
	}
	if r.RouterState != "clean_current" {
		t.Errorf("r.RouterState = %q, want clean_current", r.RouterState)
	}
	if r.SymlinkState != "ok" {
		t.Errorf("r.SymlinkState = %q, want ok", r.SymlinkState)
	}
	if r.DomainState != "ok" {
		t.Errorf("r.DomainState = %q, want ok", r.DomainState)
	}
	if r.Skills["recording-what-you-learn"] != "ok" {
		t.Errorf("skill recording = %q, want ok", r.Skills["recording-what-you-learn"])
	}
	if r.Skills["migrating-fleet-context"] != "ok" {
		t.Errorf("skill migrating = %q, want ok", r.Skills["migrating-fleet-context"])
	}
	if !r.DocsStores["design"] || !r.DocsStores["plans"] || !r.DocsStores["journal"] || !r.DocsStores["qna"] {
		t.Errorf("docs stores = %+v, want all true", r.DocsStores)
	}
}

func TestCmdDriftJSONOutputDrifted(t *testing.T) {
	dir := newTestRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(dir, "AGENTS.md")
	f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n## Custom Domain Rules\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var out bytes.Buffer
	code := runDrift([]string{"--repo", dir, "--json"}, &out)
	if code != exitcode.Advisory {
		t.Fatalf("runDrift exit=%d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	var r drift.DriftReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, out.Bytes())
	}
	if r.RouterState != "drifted" {
		t.Errorf("r.RouterState = %q, want drifted", r.RouterState)
	}
	if r.Diff == "" {
		t.Error("r.Diff is empty, want unified diff")
	}
}

func TestCmdDriftPositionalArgumentsMalformed(t *testing.T) {
	var out bytes.Buffer
	code := runDrift([]string{"unexpected"}, &out)
	if code != exitcode.Malformed {
		t.Fatalf("runDrift exit=%d, want Malformed (%d)", code, exitcode.Malformed)
	}
}

func TestCmdDriftAllAndRepoMutuallyExclusive(t *testing.T) {
	dir := newTestRepo(t)
	var out bytes.Buffer
	code := runDrift([]string{"--all", "--repo", dir}, &out)
	if code != exitcode.Malformed {
		t.Fatalf("runDrift exit=%d, want Malformed (%d)", code, exitcode.Malformed)
	}
}

func TestCmdDriftAllFleetInspection(t *testing.T) {
	cleanDir := newTestRepo(t)
	if err := scaffold.Create(cleanDir, false); err != nil {
		t.Fatal(err)
	}
	driftedDir := newTestRepo(t)
	if err := scaffold.Create(driftedDir, false); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(filepath.Join(driftedDir, "AGENTS.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\n## Drift\n")
	f.Close()

	saveFleetRegistry(t,
		registry.Entry{Path: cleanDir, Added: time.Unix(1, 0).UTC()},
		registry.Entry{Path: driftedDir, Added: time.Unix(2, 0).UTC()},
	)

	var out bytes.Buffer
	code := runDrift([]string{"--all"}, &out)
	if code != exitcode.Advisory {
		t.Fatalf("runDrift --all exit=%d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	body := out.String()
	if !strings.Contains(body, cleanDir) || !strings.Contains(body, driftedDir) {
		t.Fatalf("output missing registered repos: %s", body)
	}
	if !strings.Contains(body, "clean_current") || !strings.Contains(body, "drifted") {
		t.Fatalf("output missing router states: %s", body)
	}
}

func TestCmdDriftAllFleetJSON(t *testing.T) {
	cleanDir := newTestRepo(t)
	if err := scaffold.Create(cleanDir, false); err != nil {
		t.Fatal(err)
	}

	saveFleetRegistry(t,
		registry.Entry{Path: cleanDir, Added: time.Unix(1, 0).UTC()},
	)

	var out bytes.Buffer
	code := runDrift([]string{"--all", "--json"}, &out)
	if code != exitcode.OK {
		t.Fatalf("runDrift --all --json exit=%d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	var reports []drift.DriftReport
	if err := json.Unmarshal(out.Bytes(), &reports); err != nil {
		t.Fatalf("failed to parse JSON slice: %v\n%s", err, out.Bytes())
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].RepoPath != cleanDir || reports[0].RouterState != "clean_current" {
		t.Errorf("unexpected report: %+v", reports[0])
	}
}

func TestCmdDriftDefaultCurrentDir(t *testing.T) {
	dir := newTestRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	var out bytes.Buffer
	code := runDrift(nil, &out)
	if code != exitcode.OK {
		t.Fatalf("runDrift exit=%d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
}

func TestMainRegistersDriftCommand(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := newTestRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var stdout, stderr string
	var code int
	stdout, stderr = captureStdoutAndStderr(t, func() {
		code = run([]string{"drift"})
	})
	if code != exitcode.OK {
		t.Fatalf("run(drift) = %d, want OK; stdout=%q, stderr=%q", code, stdout, stderr)
	}
}
