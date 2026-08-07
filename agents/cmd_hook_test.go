package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "sq-123/payments"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "reports", "traces"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}

func readOnlyRecord(t *testing.T, root string) map[string]any {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(root, ".agents", "reports", "traces", "*.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("want exactly one trace file, got %v", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one record, got %d", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("record is not JSON: %v", err)
	}
	return rec
}

func TestHookWritesRecordWithProvenanceAndLane(t *testing.T) {
	root := newRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sub := filepath.Join(root, "payments", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","turn_id":"t1","agent_id":"a1",` +
		`"agent_type":"Explore","cwd":"` + sub + `",` +
		`"agent_transcript_path":"/tmp/agent-a1.jsonl",` +
		`"last_assistant_message":"SECRET-LEAK"}`

	var stderr bytes.Buffer
	code := runHook([]string{"subagent-stop", "--harness", "claude-code"}, strings.NewReader(payload), &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (recording hooks never disrupt a dispatch); stderr: %s", code, stderr.String())
	}

	rec := readOnlyRecord(t, root)
	if rec["harness"] != "claude-code" {
		t.Errorf("harness = %v", rec["harness"])
	}
	if rec["machine"] == "" || rec["machine"] == nil {
		t.Error("machine must never be empty: a record without it is misleading, not incomplete")
	}
	if rec["lane"] != "sq-123-payments" {
		t.Errorf("lane = %v, want sq-123-payments", rec["lane"])
	}
	if rec["cwd"] != "payments/api" {
		t.Errorf("cwd = %v, want repo-relative payments/api", rec["cwd"])
	}
	if rec["event"] != "subagent-stop" {
		t.Errorf("event = %v, want the semantic name subagent-stop", rec["event"])
	}
	if rec["session_id"] != "s1" {
		t.Errorf("session_id = %v", rec["session_id"])
	}
	if rec["turn_id"] != "t1" {
		t.Errorf("turn_id = %v", rec["turn_id"])
	}
	if rec["agent_id"] != "a1" {
		t.Errorf("agent_id = %v", rec["agent_id"])
	}
	if rec["agent_type"] != "Explore" {
		t.Errorf("agent_type = %v", rec["agent_type"])
	}
	// The pointer is what the record exists to carry. An empty transcript
	// beside pointer_verified true is a self-contradictory record.
	if rec["transcript"] != "/tmp/agent-a1.jsonl" {
		t.Errorf("transcript = %v, want /tmp/agent-a1.jsonl", rec["transcript"])
	}
	if rec["pointer_verified"] != true {
		t.Errorf("pointer_verified = %v, want true", rec["pointer_verified"])
	}
	// Records are UTC-partitioned; a local-time instant would land two records
	// from two timezones in files that cannot merge.
	if when, ok := rec["when"].(string); !ok || !strings.HasSuffix(when, "Z") {
		t.Errorf("when = %v, want an RFC3339 instant in UTC (trailing Z)", rec["when"])
	} else if ts, err := time.Parse(time.RFC3339, when); err != nil {
		t.Errorf("when = %q does not parse as RFC3339: %v", when, err)
	} else if d := time.Since(ts); d < -time.Minute || d > time.Hour {
		// A zero or stale instant still formats as valid UTC RFC3339, and it
		// picks the file the record is partitioned into.
		t.Errorf("when = %q is %v away from now, want the firing time", when, d)
	}
	// Iterate the package's own list rather than restating it, so a fourth
	// forbidden field added there is enforced here without an edit.
	for _, forbidden := range record.ForbiddenFields {
		if _, present := rec[forbidden]; present {
			t.Errorf("forbidden field %q reached the record", forbidden)
		}
	}
}

func TestHookFailsOpenOnEverything(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cases := []struct {
		name   string
		args   []string
		stdin  string
		inRepo bool
	}{
		{"malformed payload", []string{"stop", "--harness", "claude-code"}, "not json", true},
		{"unknown harness", []string{"stop", "--harness", "gemini"}, "{}", true},
		// A registered harness, deliberately: with an unregistered one this case
		// returns at harness.Get and never reaches the event guard, so deleting
		// the guard would go unnoticed.
		{"unknown event", []string{"teatime", "--harness", "claude-code"}, "{}", true},
		{"missing flag", []string{"stop"}, "{}", true},
		{"no .agents dir", []string{"stop", "--harness", "codex"}, "{}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.inRepo {
				dir = newRepo(t)
			}
			t.Chdir(dir)
			var stderr bytes.Buffer
			if code := runHook(tc.args, strings.NewReader(tc.stdin), &stderr); code != 0 {
				t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
			}
			if stderr.Len() == 0 {
				t.Error("a swallowed failure must still say something on stderr")
			}
		})
	}
}

func TestHookRecordsPayloadCwdNotShellCwd(t *testing.T) {
	root := newRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sub := filepath.Join(root, "services", "billing")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// The shell sits at the root; the payload reports where the subagent ran.
	// The record must follow the payload, which is the whole point of the field:
	// a subagent that worked in a submodule should record that submodule.
	t.Chdir(root)

	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","agent_id":"a1",` +
		`"cwd":"` + sub + `","agent_transcript_path":"/tmp/agent-a1.jsonl"}`

	var stderr bytes.Buffer
	code := runHook([]string{"subagent-stop", "--harness", "claude-code"}, strings.NewReader(payload), &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if rec := readOnlyRecord(t, root); rec["cwd"] != "services/billing" {
		t.Errorf("cwd = %v, want services/billing (the payload's cwd, not the shell's)", rec["cwd"])
	}
}

func TestHookRecordsDescriptionFromSidecar(t *testing.T) {
	root := newRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(root)

	// claudeCode.Describe reads an agent-<id>.meta.json written beside the
	// transcript at spawn time. A fixture without a sidecar cannot tell a wired
	// Description from a dropped one, because both produce "".
	transcript := filepath.Join(t.TempDir(), "agent-a1.jsonl")
	sidecar := strings.TrimSuffix(transcript, ".jsonl") + ".meta.json"
	if err := os.WriteFile(sidecar, []byte(`{"description":"Explore the payments module"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","agent_id":"a1",` +
		`"cwd":"` + root + `","agent_transcript_path":"` + transcript + `"}`

	var stderr bytes.Buffer
	code := runHook([]string{"subagent-stop", "--harness", "claude-code"}, strings.NewReader(payload), &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if rec := readOnlyRecord(t, root); rec["description"] != "Explore the payments module" {
		t.Errorf("description = %v, want the sidecar's label", rec["description"])
	}
}

func TestHookFailsOpenWithoutAgentsDir(t *testing.T) {
	root := newRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var stderr bytes.Buffer
	code := runHook([]string{"stop", "--harness", "claude-code"}, strings.NewReader(`{"session_id":"s1"}`), &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Error("a swallowed failure must still say something on stderr")
	}
	// A repo that was never `agents init`-ed must not have .agents/ conjured
	// underneath it by a hook firing.
	if _, err := os.Stat(filepath.Join(root, ".agents")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("hook created .agents/ in an unwired repo: stat err = %v", err)
	}
}

func TestHookLaneFlagOverridesBranch(t *testing.T) {
	root := newRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(root)

	payload := `{"hook_event_name":"Stop","session_id":"s1","cwd":"` + root + `"}`
	var stderr bytes.Buffer
	code := runHook([]string{"stop", "--harness", "claude-code", "--lane", "Hotfix/PAY-7"}, strings.NewReader(payload), &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	// newRepo checks out sq-123/payments; an explicit lane must win over it.
	if rec := readOnlyRecord(t, root); rec["lane"] != "hotfix-pay-7" {
		t.Errorf("lane = %v, want hotfix-pay-7 (--lane must override the branch)", rec["lane"])
	}
}
