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
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// --template with an empty directory isolates this test from any Git
	// templates configured on the machine running it. The local relative
	// core.hooksPath also isolates it from a machine-wide hook chain while
	// keeping .git/hooks/ live for tests that deliberately install a hook there.
	empty := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "sq-123/payments", "--template=" + empty},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
		{"config", "core.hooksPath", ".git/hooks"},
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

// Every repository fixture must be independent of the developer machine's
// global hook installation while keeping .git/hooks available to tests that
// deliberately install a repository hook there.
func TestNewRepoKeepsRepositoryHooksLocal(t *testing.T) {
	root := newRepo(t)
	cmd := exec.Command("git", "config", "--local", "--get", "core.hooksPath")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read fixture core.hooksPath: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != ".git/hooks" {
		t.Fatalf("fixture core.hooksPath = %q, want .git/hooks", got)
	}
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

// Caching at the hook is what closes the window the whole feature exists for.
//
// A subagent transcript is complete when the child stops and is deleted later,
// unpredictably, while the session that made it is still running -- 25 of 111
// in one measured session, scattered rather than oldest-first. `agents trace
// cache` run afterwards therefore arrives too late for whatever has already
// gone, and no schedule fixes that, because the order is not one anything can
// anticipate. The earliest moment a complete child transcript exists on disk is
// this hook.
//
// Only subagent-stop. A session transcript is 12.9 MB against a subagent's
// 424 KB mean, is still growing, and is named by ~30 stop events a day: copying
// it here would be quadratic in session length on a path a harness is blocked
// on.
func TestHookCachesTheSubagentTranscriptItJustRecorded(t *testing.T) {
	thisMachine(t)
	root := newRepo(t)
	t.Chdir(root)

	transcript := filepath.Join(t.TempDir(), "agent-a9f1.jsonl")
	const body = `{"type":"assistant","text":"what the subagent found"}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","agent_id":"a9f1",` +
		`"cwd":"` + root + `","agent_transcript_path":"` + transcript + `"}`

	var stderr bytes.Buffer
	if code := runHook([]string{"subagent-stop", "--harness", "claude-code"}, strings.NewReader(payload), &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}

	cacheRoot, err := repo.TraceCacheDir(root)
	if err != nil {
		t.Fatal(err)
	}
	cached := trace.CachedPath(cacheRoot, record.Record{
		Harness: "claude-code", Transcript: transcript,
	})
	b, err := os.ReadFile(cached)
	if err != nil {
		t.Fatalf("the hook recorded the pointer but did not cache the transcript (%v); "+
			"by the time anyone runs `agents trace cache` it may be gone", err)
	}
	if string(b) != body {
		t.Errorf("cached content = %q, want the transcript", b)
	}
}

// The session transcript is the one that must NOT be copied here.
func TestHookDoesNotCacheOnStopOrSessionStart(t *testing.T) {
	for _, event := range []string{"stop", "session-start"} {
		t.Run(event, func(t *testing.T) {
			thisMachine(t)
			root := newRepo(t)
			t.Chdir(root)

			// The session id must appear in the basename, because that is how the
			// adapter verifies the pointer. Named otherwise, the record is
			// unverified and Cache declines to copy it for that reason -- so the
			// test passed with the event guard deleted, proving nothing. Measured:
			// it did.
			transcript := filepath.Join(t.TempDir(), "session-s1.jsonl")
			if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			payload := `{"hook_event_name":"Stop","session_id":"s1","cwd":"` + root + `",` +
				`"transcript_path":"` + transcript + `"}`

			var stderr bytes.Buffer
			if code := runHook([]string{event, "--harness", "claude-code"}, strings.NewReader(payload), &stderr); code != 0 {
				t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
			}
			// The precondition, asserted rather than assumed: an unverified
			// pointer would make the assertion below true for the wrong reason.
			if rec := readOnlyRecord(t, root); rec["pointer_verified"] != true {
				t.Fatalf("fixture is not discriminating: pointer_verified = %v, so Cache "+
					"would decline this transcript whatever the event guard does; record: %v",
					rec["pointer_verified"], rec)
			}
			cacheRoot, err := repo.TraceCacheDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(trace.CachedPath(cacheRoot, record.Record{
				Harness: "claude-code", Transcript: transcript,
			})); err == nil {
				t.Errorf("%s copied the session transcript: it is still growing, so the "+
					"copy is a prefix that skip-if-exists would freeze, and it is named "+
					"by every turn", event)
			}
		})
	}
}

// The hook is on a path the harness is blocked on, and its contract is that a
// trace is worth strictly less than the dispatch. A cache failure must be
// reported and must not change the exit code -- and the record must still be
// there, because the pointer is the more important of the two.
func TestHookStillRecordsWhenCachingCannotHappen(t *testing.T) {
	thisMachine(t)
	root := newRepo(t)
	t.Chdir(root)

	// Named but absent: the pointer is worth keeping even when the bytes are not
	// there to copy.
	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","agent_id":"a9f2",` +
		`"cwd":"` + root + `","agent_transcript_path":"` + filepath.Join(t.TempDir(), "never-existed.jsonl") + `"}`

	var stderr bytes.Buffer
	if code := runHook([]string{"subagent-stop", "--harness", "claude-code"}, strings.NewReader(payload), &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0 even when the transcript cannot be copied; stderr: %s",
			code, stderr.String())
	}
	if rec := readOnlyRecord(t, root); rec["agent_id"] != "a9f2" {
		t.Errorf("the record must survive a failed cache: got %v", rec)
	}
}

// Writing to disk on every subagent completion is not everyone's trade.
func TestHookAutoCacheCanBeTurnedOff(t *testing.T) {
	thisMachine(t)
	root := newRepo(t)
	t.Chdir(root)
	t.Setenv("AGENTS_NO_AUTO_CACHE", "1")

	transcript := filepath.Join(t.TempDir(), "agent-a9f3.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","agent_id":"a9f3",` +
		`"cwd":"` + root + `","agent_transcript_path":"` + transcript + `"}`

	var stderr bytes.Buffer
	if code := runHook([]string{"subagent-stop", "--harness", "claude-code"}, strings.NewReader(payload), &stderr); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr.String())
	}
	cacheRoot, err := repo.TraceCacheDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trace.CachedPath(cacheRoot, record.Record{
		Harness: "claude-code", Transcript: transcript,
	})); err == nil {
		t.Error("AGENTS_NO_AUTO_CACHE was set and the transcript was cached anyway")
	}
	// The pointer is not the expensive part and is not what was opted out of.
	if rec := readOnlyRecord(t, root); rec["agent_id"] != "a9f3" {
		t.Errorf("opting out of caching must not stop recording: got %v", rec)
	}
}
