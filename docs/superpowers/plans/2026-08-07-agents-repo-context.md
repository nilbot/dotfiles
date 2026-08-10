# `agents` — repo-tracked agent context Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a single Go binary, `agents`, that populates and maintains a tracked `.agents/` directory in any git repo, records where agent transcripts live without ever copying their contents, and replaces the dotfiles git-hook subsystem with a chained multicall dispatcher.

**Architecture:** One Go module at `~/dotfiles/agents/`, built by `make agents` to `~/bin/agents`. Harness-specific behaviour lives behind a `harness.Adapter` interface with two implementations (Claude Code, Codex); everything else is harness-agnostic. Hook payloads arrive on stdin as JSON and are decoded into a fixed struct that has no field capable of holding message content — redaction is structural, not filtered. Git hooks reach the same binary through `core.hooksPath` and a directory of symlinks, dispatching on `basename(argv[0])`.

**Tech Stack:** Go 1.26 (stdlib only, plus `gopkg.in/yaml.v3` for memory frontmatter). Shells out to `git` for repository facts. No test framework beyond `go test`.

**Spec:** [docs/superpowers/specs/agents/2026-08-07-agents-repo-context-design.md](../specs/agents/2026-08-07-agents-repo-context-design.md). Section references below (§3.5, §8.3, …) point there.

## Global Constraints

- **Module path:** `github.com/nilbot/dotfiles/agents`, rooted at `~/dotfiles/agents/`. Go directive `go 1.26`.
- **Only one external dependency is permitted:** `gopkg.in/yaml.v3`. Anything else needs a decision from the repo owner first.
- **The record type must never gain a field capable of carrying `last_assistant_message`, `tool_input`, or `tool_response`** (§3.2). This is enforced by a test that reflects over the struct, not by a grep on output.
- **Recording hooks exit 0 on every path.** A failed record must never disrupt a dispatch. `agents guard` is the sole deliberate exception (§6).
- **Shared exit codes across every subcommand:** 0 ok · 1 advisory · 2 block · 3 malformed input · 4 not applicable/skip · 5 could not record.
- **Harness identity comes from the explicit `--harness` flag, never from the environment** (§5). Env detection was measured to fail in both directions.
- **Never symlink `~/.claude` or `~/.codex` wholesale**; link individual subdirectories only (§8.4).
- **The fleet registry lives at `~/.local/state/agents/` and is never tracked.** The dotfiles repo is public on GitHub (§10).
- **Commit messages must not contain `Co-Authored-By: Claude` or `🤖 Generated with [Claude Code]`.** This is a standing rule from `~/.claude/CLAUDE.md`; Task 17 makes it mechanical.
- **Nothing in this plan may write to `git/gitconfig.symlink`'s identity section** — identity lives solely in `~/etc/extras.secret/gitconfig`.

---

## File Structure

```
agents/
├── go.mod                          module github.com/nilbot/dotfiles/agents
├── go.sum
├── main.go                         multicall check, then subcommand table
├── cmd_init.go                     agents init
├── cmd_wire.go                     agents wire
├── cmd_hook.go                     agents hook <event> --harness <name>
├── cmd_trace.go                    agents trace ls|cache
├── cmd_index.go                    agents index
├── cmd_handoff.go                  agents handoff write|prune
├── cmd_save.go                     agents save
├── cmd_guard.go                    agents guard --staged
├── cmd_fleet.go                    agents ls, agents update
├── cmd_doctor.go                   agents doctor
└── internal/
    ├── exitcode/exitcode.go        the six shared codes, one place
    ├── machine/machine.go          stable machine id in XDG state
    ├── repo/repo.go                git root, branch, worktree, repo-relative cwd
    ├── lane/lane.go                lane resolution + slugify
    ├── pointer/pointer.go          derive-and-verify transcript resolution (§3.5)
    ├── record/record.go            Record type, redaction guarantee, JSONL writer
    ├── harness/
    │   ├── harness.go              Payload, Event, Capabilities, Adapter, registry
    │   ├── claudecode.go           Claude Code adapter + settings.json merge
    │   └── codex.go                Codex adapter + hooks.json generation
    ├── scaffold/scaffold.go        .agents/ layout, CLAUDE.md, .gitattributes
    ├── trace/trace.go              query + cache
    ├── memory/memory.go            frontmatter parse, INDEX.md generation
    ├── handoff/handoff.go          lane-scoped handoff files + INDEX.md
    ├── guard/guard.go              staged-content checks
    ├── githook/githook.go          multicall dispatch + chain execution
    ├── registry/registry.go        fleet cache in ~/.local/state/agents/
    └── doctor/doctor.go            checks + report rendering
```

Fixtures already committed at `docs/superpowers/specs/agents/fixtures/2026-08-07-codex-hook-payloads/` are the Codex golden inputs. Tests reference them by relative path from the package under test.

Outside the module:

```
git/hooks.d/                        symlinks named for git hooks → ~/bin/agents (Task 18)
git/gitattributes                   linked to ~/.gitattributes (Task 18)
Makefile                            gains an `agents` target (Task 8)
```

---

## Phase 1 — The record loop, end to end on Claude Code

Ends with: a real Claude Code session in a real repo writing real trace records. Everything after this is breadth.

### Task 1: Module skeleton, exit codes, machine identity

**Files:**
- Create: `agents/go.mod`
- Create: `agents/main.go`
- Create: `agents/internal/exitcode/exitcode.go`
- Create: `agents/internal/machine/machine.go`
- Test: `agents/internal/machine/machine_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `exitcode.OK/Advisory/Block/Malformed/Skip/NoRecord` (untyped int constants); `machine.ID() (string, error)`; `machine.StateDir() string`.

**Design decision this task settles** (spec left it open): machine identity is a stable id generated once into `~/.local/state/agents/machine-id`, seeded as `<slug-of-short-hostname>-<4 hex>`. It is never re-derived from the live hostname, because hostnames change and a changed hostname would silently split one machine's records into two identities. The 4 hex characters exist because short hostnames repeat across machines (`macbook-pro` is not unique).

- [ ] **Step 1: Initialize the module**

```bash
cd ~/dotfiles/agents 2>/dev/null || mkdir -p ~/dotfiles/agents && cd ~/dotfiles/agents
go mod init github.com/nilbot/dotfiles/agents
```

Then edit `go.mod` so the go directive reads exactly `go 1.26` (drop any patch version `go mod init` wrote).

- [ ] **Step 2: Write the exit codes**

`agents/internal/exitcode/exitcode.go`:

```go
// Package exitcode holds the process exit codes shared by every agents
// subcommand. A caller -- a git hook, a harness, a shell -- can act on the code
// without knowing which subcommand produced it.
package exitcode

const (
	OK        = 0 // did the thing
	Advisory  = 1 // finished, but the caller should look at the output
	Block     = 2 // the only code that stops work
	Malformed = 3 // input could not be parsed
	Skip      = 4 // not applicable here (not a repo, no .agents/, unknown event)
	NoRecord  = 5 // wanted to record and could not
)
```

- [ ] **Step 3: Write the failing test for machine identity**

`agents/internal/machine/machine_test.go`:

```go
package machine

import (
	"os"
	"regexp"
	"testing"
)

var idPattern = regexp.MustCompile(`^[a-z0-9-]+-[0-9a-f]{4}$`)

func TestIDIsGeneratedThenStable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	first, err := ID()
	if err != nil {
		t.Fatalf("ID() error: %v", err)
	}
	if !idPattern.MatchString(first) {
		t.Fatalf("ID() = %q, want <slug>-<4 hex>", first)
	}

	second, err := ID()
	if err != nil {
		t.Fatalf("second ID() error: %v", err)
	}
	if second != first {
		t.Fatalf("ID() not stable: %q then %q", first, second)
	}
}

func TestIDHonoursExistingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := os.MkdirAll(dir+"/agents", 0o755); err != nil {
		t.Fatal(err)
	}
	// Trailing newline and surrounding space must be tolerated: this file is
	// meant to be editable by hand.
	if err := os.WriteFile(dir+"/agents/machine-id", []byte("  m1-mbp-a7f3\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ID()
	if err != nil {
		t.Fatalf("ID() error: %v", err)
	}
	if got != "m1-mbp-a7f3" {
		t.Fatalf("ID() = %q, want m1-mbp-a7f3", got)
	}
}
```

- [ ] **Step 4: Run it and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/machine/ -v`
Expected: FAIL — `undefined: ID`.

- [ ] **Step 5: Implement**

`agents/internal/machine/machine.go`:

```go
// Package machine resolves this computer's stable identity.
//
// Every trace record carries it, because both harnesses write $HOME-relative
// transcript paths that look identical on every machine you own. A record
// without provenance is not merely incomplete elsewhere -- it resolves to a
// different session that happens to occupy the same path.
package machine

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// StateDir is where machine-local, never-tracked state lives.
func StateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "agents")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "state", "agents")
	}
	return filepath.Join(home, ".local", "state", "agents")
}

// ID returns the stable identifier for this machine, generating it on first
// call. It is deliberately NOT derived from the live hostname: hostnames change,
// and a changed hostname would split one machine's history into two identities
// with no way to notice after the fact.
func ID() (string, error) {
	path := filepath.Join(StateDir(), "machine-id")
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}

	id, err := generate()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func generate() (string, error) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	host, _, _ = strings.Cut(host, ".") // strip .local and any domain
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(host), "-"), "-")
	if slug == "" {
		slug = "unknown"
	}

	// Short hostnames repeat across machines; two random bytes make the id
	// unique without making it unreadable.
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return slug + "-" + hex.EncodeToString(b[:]), nil
}
```

- [ ] **Step 6: Write a placeholder main so the module builds**

`agents/main.go`:

```go
// Command agents maintains repo-tracked agent context: the .agents/ directory,
// the harness wiring that feeds it, and the git hooks that guard it.
package main

import (
	"fmt"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return exitcode.Malformed
	}
	switch args[0] {
	default:
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		usage()
		return exitcode.Malformed
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: agents <command> [flags]

commands are registered in this file as they are implemented.
`)
}
```

- [ ] **Step 7: Verify build and tests**

Run: `cd ~/dotfiles/agents && go build ./... && go test ./... -v`
Expected: build succeeds, `internal/machine` tests PASS.

- [ ] **Step 8: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): module skeleton, shared exit codes, stable machine id"
```

---

### Task 2: The record type and its structural redaction guarantee

**Files:**
- Create: `agents/internal/record/record.go`
- Test: `agents/internal/record/record_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `record.Record` (struct, fields listed below); `record.Record.Line() ([]byte, error)`; `record.NewWriter(agentsDir string) *record.Writer`; `(*record.Writer).Append(r Record) error`; `record.ForbiddenFields` (`[]string`).

**Why this task exists on its own:** §3.2's promise — that message content cannot reach the tracked tree — is a property of this type. A reviewer should be able to accept or reject that promise without reading anything else.

- [ ] **Step 1: Write the failing tests**

`agents/internal/record/record_test.go`:

```go
package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The whole redaction argument in spec 3.2 rests on this: the type has no field
// capable of holding message content, so no writer can emit it by mistake. This
// asserts the structure, not the output of one code path.
func TestRecordCannotCarryForbiddenFields(t *testing.T) {
	rt := reflect.TypeOf(Record{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		for _, bad := range ForbiddenFields {
			if strings.EqualFold(tag, bad) || strings.EqualFold(f.Name, bad) {
				t.Errorf("Record has field %s (json:%q) matching forbidden %q", f.Name, tag, bad)
			}
		}
	}
}

func TestLineIsOneJSONObjectPerLine(t *testing.T) {
	r := Record{
		When:            time.Date(2026, 8, 7, 15, 41, 14, 0, time.UTC),
		Harness:         "codex",
		Machine:         "m1-mbp-a7f3",
		Event:           "subagent_stop",
		Lane:            "sq-123-payments",
		Cwd:             "payments/api",
		SessionID:       "019fdcab-9733-72e3-ba7c-d2e0cc7fb334",
		AgentID:         "019fdcab-ac94-7502-a322-d01f047c274a",
		AgentType:       "default",
		Transcript:      "/Users/nilbot/.codex/sessions/2026/08/07/rollout-x.jsonl",
		PointerVerified: true,
	}

	line, err := r.Line()
	if err != nil {
		t.Fatalf("Line() error: %v", err)
	}
	if strings.Count(string(line), "\n") != 1 || !strings.HasSuffix(string(line), "\n") {
		t.Fatalf("Line() must be exactly one trailing newline, got %q", line)
	}

	var back map[string]any
	if err := json.Unmarshal(line, &back); err != nil {
		t.Fatalf("Line() is not valid JSON: %v", err)
	}
	if back["when"] != "2026-08-07T15:41:14Z" {
		t.Fatalf("when = %v, want RFC3339 UTC", back["when"])
	}
	if back["pointer_verified"] != true {
		t.Fatalf("pointer_verified = %v, want true", back["pointer_verified"])
	}
}

func TestAppendWritesDatePartitionedJSONL(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	when := time.Date(2026, 8, 7, 23, 59, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := w.Append(Record{When: when, Harness: "claude-code", Machine: "m", Event: "stop"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	path := filepath.Join(dir, "reports", "traces", "2026-08-07.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	if got := strings.Count(string(b), "\n"); got != 2 {
		t.Fatalf("got %d lines, want 2", got)
	}
}

// Partitioning is by UTC date, not local date, so records from two machines in
// different timezones land in the same file and merge=union works as intended.
func TestAppendPartitionsByUTC(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// 2026-08-08T01:30Z is still 2026-08-07 in most of the Americas.
	when := time.Date(2026, 8, 8, 1, 30, 0, 0, time.FixedZone("UTC-8", -8*3600))
	if err := w.Append(Record{When: when, Harness: "codex", Machine: "m", Event: "stop"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reports", "traces", "2026-08-08.jsonl")); err != nil {
		t.Fatalf("expected UTC-dated file: %v", err)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/record/ -v`
Expected: FAIL — `undefined: Record`.

- [ ] **Step 3: Implement**

`agents/internal/record/record.go`:

```go
// Package record defines the one thing this tool writes into a tracked
// repository: a pointer to where a transcript lives, and enough provenance to
// know whether that pointer means anything from here.
//
// It never writes what the transcript says.
package record

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ForbiddenFields names the payload fields that must never reach a tracked
// file. All three routinely quote command output, which routinely contains
// credentials -- the captured Codex fixtures carry encrypted task blobs in
// tool_input.message. The guarantee is structural: Record has no field able to
// hold them, and the payload decoder (package harness) names only the fields it
// wants, so everything else is discarded at the JSON boundary.
var ForbiddenFields = []string{
	"last_assistant_message",
	"tool_input",
	"tool_response",
}

// Record is one line of .agents/reports/traces/YYYY-MM-DD.jsonl.
//
// Adding a field here is a decision about what this repository publishes.
// Before adding one, check it against ForbiddenFields and against spec 3.2.
type Record struct {
	When    time.Time `json:"when"`
	Harness string    `json:"harness"`
	Machine string    `json:"machine"`
	Event   string    `json:"event"`

	// Lane and Cwd exist to make retrieval mechanical rather than semantic.
	// Cwd is repo-relative and identifies the module in a multi-module repo.
	Lane string `json:"lane"`
	Cwd  string `json:"cwd"`

	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`

	// Description is empty for harnesses that do not supply one. Codex is such
	// a harness; the adapter declares the gap rather than the format pretending
	// both harnesses are equal.
	Description string `json:"description"`

	Transcript      string `json:"transcript"`
	PointerVerified bool   `json:"pointer_verified"`
}

// Line renders the record as exactly one JSONL line, newline included.
func (r Record) Line() ([]byte, error) {
	type alias Record // avoid recursing through a custom marshaller later
	out := struct {
		When string `json:"when"`
		alias
	}{
		When:  r.When.UTC().Format(time.RFC3339),
		alias: alias(r),
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Writer appends records under an .agents directory.
type Writer struct{ agentsDir string }

func NewWriter(agentsDir string) *Writer { return &Writer{agentsDir: agentsDir} }

// Append adds one record to the UTC-dated file for its timestamp.
//
// Partitioning is by UTC so that records written from two timezones land in the
// same file, which is what makes the merge=union gitattribute do the right
// thing on a merge.
func (w *Writer) Append(r Record) error {
	dir := filepath.Join(w.agentsDir, "reports", "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	line, err := r.Line()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, r.When.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	// One write syscall per record. Concurrent agents appending to the same
	// file rely on O_APPEND atomicity, which holds for writes of this size.
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("append record: %w", err)
	}
	return nil
}
```

Note the `When` shadowing in `Line`: the anonymous struct's `When string` takes precedence over the embedded alias's `When time.Time`, so the timestamp serializes as `2026-08-07T15:41:14Z` rather than with nanoseconds. Do not "simplify" this away — the test asserts the exact format.

- [ ] **Step 4: Run tests**

Run: `cd ~/dotfiles/agents && go test ./internal/record/ -v`
Expected: all four PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dotfiles && git add agents/internal/record/ && git commit -m "feat(agents): trace record type with structural redaction"
```

---

### Task 3: Repository context and lane resolution

**Files:**
- Create: `agents/internal/repo/repo.go`
- Create: `agents/internal/lane/lane.go`
- Test: `agents/internal/repo/repo_test.go`
- Test: `agents/internal/lane/lane_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `repo.Context` (`Root, Branch, Worktree, RelCwd string`); `repo.Discover(cwd string) (*repo.Context, error)`; `repo.ErrNotARepo`; `repo.AgentsDir(root string) string`; `lane.Resolve(explicit string, rc *repo.Context) string`; `lane.Slugify(s string) string`.

- [ ] **Step 1: Write the failing lane tests**

`agents/internal/lane/lane_test.go`:

```go
package lane

import (
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"SQ-123/payments":  "sq-123-payments",
		"feature/Add Auth": "feature-add-auth",
		"---weird---":      "weird",
		"":                 "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyTruncatesWithoutTrailingDash(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "ab-"
	}
	got := Slugify(long)
	if len(got) > 64 {
		t.Fatalf("len = %d, want <= 64", len(got))
	}
	if got[len(got)-1] == '-' {
		t.Fatalf("Slugify left a trailing dash: %q", got)
	}
}

func TestResolvePrecedence(t *testing.T) {
	rc := &repo.Context{Branch: "SQ-123/payments", Worktree: "myrepo"}

	if got := Resolve("Explicit Lane", rc); got != "explicit-lane" {
		t.Errorf("explicit should win: got %q", got)
	}
	if got := Resolve("", rc); got != "sq-123-payments" {
		t.Errorf("branch should be next: got %q", got)
	}

	detached := &repo.Context{Branch: "", Worktree: "My Repo"}
	if got := Resolve("", detached); got != "my-repo" {
		t.Errorf("worktree should be next: got %q", got)
	}

	nothing := &repo.Context{}
	if got := Resolve("", nothing); got != "default" {
		t.Errorf("default should be last: got %q", got)
	}
	if got := Resolve("", nil); got != "default" {
		t.Errorf("nil context should be default: got %q", got)
	}
}
```

- [ ] **Step 2: Write the failing repo test**

`agents/internal/repo/repo_test.go`:

```go
package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestDiscoverFindsRootBranchAndRelativeCwd(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "payments", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	rc, err := Discover(sub)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// macOS puts TempDir under /var, a symlink to /private/var; git reports the
	// resolved path. Compare resolved forms or this test fails for the wrong
	// reason.
	wantRoot, _ := filepath.EvalSymlinks(dir)
	if rc.Root != wantRoot {
		t.Errorf("Root = %q, want %q", rc.Root, wantRoot)
	}
	if rc.Branch != "main" {
		t.Errorf("Branch = %q, want main", rc.Branch)
	}
	if rc.RelCwd != "payments/api" {
		t.Errorf("RelCwd = %q, want payments/api", rc.RelCwd)
	}
	if rc.Worktree != filepath.Base(wantRoot) {
		t.Errorf("Worktree = %q, want %q", rc.Worktree, filepath.Base(wantRoot))
	}
}

func TestDiscoverAtRootReportsDot(t *testing.T) {
	dir := initRepo(t)
	rc, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if rc.RelCwd != "." {
		t.Errorf("RelCwd = %q, want .", rc.RelCwd)
	}
}

func TestDiscoverOutsideRepo(t *testing.T) {
	// A directory that is definitively not inside a git repo.
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	outside := filepath.Join(dir, "nested")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Discover(outside); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}
```

- [ ] **Step 3: Run both and watch them fail**

Run: `cd ~/dotfiles/agents && go test ./internal/repo/ ./internal/lane/ -v`
Expected: FAIL — undefined identifiers in both packages.

- [ ] **Step 4: Implement repo**

`agents/internal/repo/repo.go`:

```go
// Package repo answers the questions this tool asks of git: where the worktree
// root is, what branch is checked out, and where the caller is standing
// relative to the root.
//
// It shells out to git rather than reimplementing repository discovery. git is
// already a hard dependency of everything here, and .git layouts (worktrees,
// submodules, alternates) have more edge cases than are worth owning.
package repo

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotARepo means the given directory is not inside a git worktree. Callers
// that are hooks should treat this as exitcode.Skip, not as a failure.
var ErrNotARepo = errors.New("not inside a git repository")

type Context struct {
	Root     string // absolute, symlinks resolved, as git reports it
	Branch   string // empty when HEAD is detached
	Worktree string // basename of Root
	RelCwd   string // slash-separated, "." at the root
}

// AgentsDir is the tracked context directory for a repo root.
func AgentsDir(root string) string { return filepath.Join(root, ".agents") }

func Discover(cwd string) (*Context, error) {
	root, err := run(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}

	rel := "."
	if abs, err := filepath.Abs(cwd); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if r, err := filepath.Rel(root, abs); err == nil {
			rel = filepath.ToSlash(r)
		}
	}

	// A detached HEAD has no branch. That is normal (bisect, CI, a rebase in
	// progress), so it is not an error -- lane resolution falls through.
	branch, _ := run(cwd, "symbolic-ref", "--short", "-q", "HEAD")

	return &Context{
		Root:     root,
		Branch:   branch,
		Worktree: filepath.Base(root),
		RelCwd:   rel,
	}, nil
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
```

- [ ] **Step 5: Implement lane**

`agents/internal/lane/lane.go`:

```go
// Package lane resolves the unit of work-in-progress that handoffs and traces
// are grouped by.
//
// The branch is the strongest default because it already exists, already tracks
// the ticket, and requires nothing of the user. Everything else is a fallback
// for when there is no branch to read.
package lane

import (
	"regexp"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

const maxLen = 64

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify makes a string safe as a directory name and stable as a join key.
func Slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > maxLen {
		s = strings.TrimRight(s[:maxLen], "-")
	}
	return s
}

// Resolve applies the precedence in spec 4: explicit flag, then branch, then
// worktree name, then "default".
func Resolve(explicit string, rc *repo.Context) string {
	candidates := []string{explicit}
	if rc != nil {
		candidates = append(candidates, rc.Branch, rc.Worktree)
	}
	for _, c := range candidates {
		if s := Slugify(c); s != "" {
			return s
		}
	}
	return "default"
}
```

- [ ] **Step 6: Run tests**

Run: `cd ~/dotfiles/agents && go test ./internal/repo/ ./internal/lane/ -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/internal/repo/ agents/internal/lane/ && git commit -m "feat(agents): repository context discovery and lane resolution"
```

---

### Task 4: Pointer resolution — derive and verify

**Files:**
- Create: `agents/internal/pointer/pointer.go`
- Test: `agents/internal/pointer/pointer_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `pointer.Resolve(candidates []string, key string) (path string, verified bool)`.

**Why not a per-event field map** (§3.5): Codex's own semantics are inconsistent — at `SubagentStart`, `transcript_path` is the *child's* and `agent_transcript_path` is absent; at `SubagentStop`, `transcript_path` is the *parent's* and `agent_transcript_path` is the child's. A lookup table encodes today's inconsistency. The invariant that actually holds across both harnesses is that the transcript's path contains the id it belongs to, so derive from that and record whether the derivation succeeded.

- [ ] **Step 1: Write the failing test**

`agents/internal/pointer/pointer_test.go`:

```go
package pointer

import "testing"

const (
	codexParent = "/Users/n/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-00-019fdcab-9733-72e3-ba7c-d2e0cc7fb334.jsonl"
	codexChild  = "/Users/n/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-06-019fdcab-ac94-7502-a322-d01f047c274a.jsonl"
	ccChild     = "/Users/n/.claude/projects/-Users-n-work/019f-sess/subagents/agent-a4e4a1bc424b2047f.jsonl"
	ccSession   = "/Users/n/.claude/projects/-Users-n-work/019fdcab-9733-72e3-ba7c-d2e0cc7fb334.jsonl"
)

func TestPicksChildAtCodexSubagentStop(t *testing.T) {
	// SubagentStop hands over both paths, parent first in field order.
	got, verified := Resolve([]string{codexParent, codexChild}, "019fdcab-ac94-7502-a322-d01f047c274a")
	if got != codexChild || !verified {
		t.Fatalf("Resolve = (%q, %v), want child and verified", got, verified)
	}
}

func TestPicksChildAtCodexSubagentStart(t *testing.T) {
	// SubagentStart supplies only one path, and it is already the child's.
	got, verified := Resolve([]string{codexChild}, "019fdcab-ac94-7502-a322-d01f047c274a")
	if got != codexChild || !verified {
		t.Fatalf("Resolve = (%q, %v), want child and verified", got, verified)
	}
}

func TestClaudeCodeSubagentBasename(t *testing.T) {
	got, verified := Resolve([]string{ccChild}, "a4e4a1bc424b2047f")
	if got != ccChild || !verified {
		t.Fatalf("Resolve = (%q, %v), want child and verified", got, verified)
	}
}

// Claude Code puts the session id in a directory component, not the basename.
// The invariant is "the key appears in the path", so this must still verify.
func TestClaudeCodeSessionIDInPathComponent(t *testing.T) {
	got, verified := Resolve([]string{ccSession}, "019fdcab-9733-72e3-ba7c-d2e0cc7fb334")
	if got != ccSession || !verified {
		t.Fatalf("Resolve = (%q, %v), want session path and verified", got, verified)
	}
}

// A basename match beats a path-component match, so a parent whose directory
// happens to contain the child's id cannot win.
func TestBasenameBeatsPathComponent(t *testing.T) {
	decoy := "/Users/n/.claude/projects/p/a4e4a1bc424b2047f/session.jsonl"
	got, _ := Resolve([]string{decoy, ccChild}, "a4e4a1bc424b2047f")
	if got != ccChild {
		t.Fatalf("Resolve = %q, want basename match %q", got, ccChild)
	}
}

// Degrade, never drop. An unverified pointer is still a lead; a missing record
// is nothing.
func TestUnverifiedWhenNoCandidateMatches(t *testing.T) {
	got, verified := Resolve([]string{codexParent}, "some-other-id")
	if got != codexParent {
		t.Fatalf("Resolve = %q, want the best candidate anyway", got)
	}
	if verified {
		t.Fatal("verified = true, want false")
	}
}

func TestEmptyKeyIsNeverVerified(t *testing.T) {
	if _, verified := Resolve([]string{codexParent}, ""); verified {
		t.Fatal("an empty key cannot verify anything")
	}
}

func TestNoCandidates(t *testing.T) {
	got, verified := Resolve([]string{"", "  "}, "id")
	if got != "" || verified {
		t.Fatalf("Resolve = (%q, %v), want empty and unverified", got, verified)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/pointer/ -v`
Expected: FAIL — `undefined: Resolve`.

- [ ] **Step 3: Implement**

`agents/internal/pointer/pointer.go`:

```go
// Package pointer resolves which transcript path in a hook payload belongs to
// the thing the record is about.
//
// It derives rather than looks up. The measured invariant across both harnesses
// is that a transcript's path contains the id of what it transcribes:
//
//	Codex        agent_id=019fdcab-ac94...  ->  rollout-...-019fdcab-ac94-....jsonl
//	Claude Code  agent_id=a4e4a1bc424b2047f ->  subagents/agent-a4e4a1bc424b2047f.jsonl
//
// A per-event field map would encode today's inconsistency in a fast-moving
// vendor contract. Deriving and reporting whether the derivation held survives
// the contract moving.
package pointer

import (
	"path/filepath"
	"strings"
)

// Resolve picks the candidate belonging to key -- an agent_id for subagent
// events, a session_id otherwise -- and reports whether key was actually found
// in the path it chose.
//
// When nothing matches it returns the first usable candidate with verified
// false. Recording an unverified pointer beats dropping the record: the pointer
// is still a lead, and pointer_verified says how much to trust it.
func Resolve(candidates []string, key string) (string, bool) {
	var usable []string
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c != "" {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return "", false
	}
	if key == "" {
		return usable[0], false
	}

	// A basename match is the strong signal; a match anywhere in the path is
	// the weaker one that Claude Code's session layout needs. Both verify, but
	// the strong one wins when both are present.
	var pathMatch string
	for _, c := range usable {
		if strings.Contains(filepath.Base(c), key) {
			return c, true
		}
		if pathMatch == "" && strings.Contains(c, key) {
			pathMatch = c
		}
	}
	if pathMatch != "" {
		return pathMatch, true
	}
	return usable[0], false
}
```

- [ ] **Step 4: Run tests**

Run: `cd ~/dotfiles/agents && go test ./internal/pointer/ -v`
Expected: all eight PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dotfiles && git add agents/internal/pointer/ && git commit -m "feat(agents): derive-and-verify transcript pointer resolution"
```

---

### Task 5: Harness adapter interface and the Claude Code adapter

**Files:**
- Create: `agents/internal/harness/harness.go`
- Create: `agents/internal/harness/claudecode.go`
- Test: `agents/internal/harness/harness_test.go`
- Test: `agents/internal/harness/claudecode_test.go`

**Interfaces:**
- Consumes: `pointer.Resolve`.
- Produces: `harness.Payload`; `harness.Event{Semantic, Vendor, Matcher string}`; `harness.Capabilities{Description bool}`; `harness.Trace`; `harness.Adapter` (interface); `harness.Decode(io.Reader) (Payload, error)`; `harness.Build(a Adapter, semantic string, p Payload) Trace`; `harness.Get(name string) (Adapter, bool)`; `harness.All() []Adapter`; semantic event constants `harness.SessionStart`, `SubagentStart`, `SubagentStop`, `Stop`.

**Note on `Decode` being shared rather than per-adapter:** both harnesses use the same payload key names — verified against the captured Codex fixtures and against the Claude Code payload shape recorded in the spec. One decoder is also what makes the redaction guarantee cheap to audit: there is exactly one place where JSON becomes Go, and it names only the fields it wants.

- [ ] **Step 1: Write the failing interface tests**

`agents/internal/harness/harness_test.go`:

```go
package harness

import (
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

// The decoder is the boundary where untrusted JSON becomes Go. Fields the
// record must never carry have to die here, not later.
func TestDecodeDiscardsForbiddenFields(t *testing.T) {
	raw := `{
	  "hook_event_name": "SubagentStop",
	  "session_id": "sess-1",
	  "agent_id": "agent-1",
	  "last_assistant_message": "SECRET-LEAK",
	  "tool_input": {"message": "gAAAAABSECRET"},
	  "tool_response": {"stdout": "SECRET-LEAK"}
	}`

	p, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.SessionID != "sess-1" || p.AgentID != "agent-1" {
		t.Fatalf("Decode dropped wanted fields: %+v", p)
	}

	// Nothing in the decoded value may reference the forbidden content.
	rendered := fmtPayload(p)
	for _, bad := range []string{"SECRET-LEAK", "gAAAAABSECRET"} {
		if strings.Contains(rendered, bad) {
			t.Fatalf("decoded payload retained %q: %s", bad, rendered)
		}
	}
	_ = record.ForbiddenFields
}

func fmtPayload(p Payload) string {
	return strings.Join([]string{
		p.HookEventName, p.SessionID, p.TurnID, p.AgentID, p.AgentType,
		p.Cwd, p.TranscriptPath, p.AgentTranscriptPath, p.Source,
	}, "|")
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode(strings.NewReader("not json")); err == nil {
		t.Fatal("want an error for malformed input")
	}
}

func TestGetIsCaseInsensitiveAndFailsClosed(t *testing.T) {
	if _, ok := Get("claude-code"); !ok {
		t.Fatal("claude-code must be registered")
	}
	if _, ok := Get("Claude-Code"); !ok {
		t.Fatal("Get must be case-insensitive")
	}
	if _, ok := Get("gemini"); ok {
		t.Fatal("unregistered harness must not resolve")
	}
}
```

- [ ] **Step 2: Write the failing Claude Code adapter tests**

`agents/internal/harness/claudecode_test.go`:

```go
package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Constructed from the payload shape recorded in spec "Measured facts >
// Claude Code". Task 8 captures a live payload and reconciles this against it.
const ccSubagentStopPayload = `{
  "hook_event_name": "SubagentStop",
  "session_id": "019fdcab-9733-72e3-ba7c-d2e0cc7fb334",
  "agent_id": "a4e4a1bc424b2047f",
  "agent_type": "Explore",
  "cwd": "/Users/n/work/myrepo",
  "agent_transcript_path": "PLACEHOLDER",
  "last_assistant_message": "SECRET-LEAK"
}`

func TestClaudeCodeBuildsVerifiedSubagentTrace(t *testing.T) {
	// A transcript on disk with the sidecar the harness writes at spawn time.
	dir := t.TempDir()
	sub := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(sub, "agent-a4e4a1bc424b2047f.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(sub, "agent-a4e4a1bc424b2047f.meta.json")
	meta, _ := json.Marshal(map[string]string{"description": "Find the retry window"})
	if err := os.WriteFile(sidecar, meta, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Decode(strings.NewReader(strings.Replace(ccSubagentStopPayload, "PLACEHOLDER", transcript, 1)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, p)

	if tr.Transcript != transcript {
		t.Errorf("Transcript = %q, want %q", tr.Transcript, transcript)
	}
	if !tr.PointerVerified {
		t.Error("PointerVerified = false, want true")
	}
	if tr.AgentID != "a4e4a1bc424b2047f" || tr.AgentType != "Explore" {
		t.Errorf("agent fields wrong: %+v", tr)
	}
	if tr.Description != "Find the retry window" {
		t.Errorf("Description = %q, want it read from the sidecar", tr.Description)
	}
	if strings.Contains(tr.Description+tr.Transcript, "SECRET-LEAK") {
		t.Error("forbidden content reached the trace")
	}
}

func TestClaudeCodeDescriptionEmptyWithoutSidecar(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "agent-deadbeef.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, Payload{AgentID: "deadbeef", AgentTranscriptPath: transcript})

	if tr.Description != "" {
		t.Fatalf("Description = %q, want empty when there is no sidecar", tr.Description)
	}
	if !tr.PointerVerified {
		t.Fatal("a missing sidecar must not invalidate the pointer")
	}
}

func TestClaudeCodeSessionEventsKeyOnSessionID(t *testing.T) {
	a, _ := Get("claude-code")
	tr := Build(a, SessionStart, Payload{
		SessionID:      "019fdcab-9733",
		TranscriptPath: "/Users/n/.claude/projects/p/019fdcab-9733.jsonl",
	})
	if !tr.PointerVerified {
		t.Fatal("session events must verify against session_id")
	}
	if tr.AgentID != "" {
		t.Fatalf("AgentID = %q, want empty for a session event", tr.AgentID)
	}
}

func TestClaudeCodeDeclaresDescriptionCapability(t *testing.T) {
	a, _ := Get("claude-code")
	if !a.Capabilities().Description {
		t.Fatal("Claude Code supplies descriptions via the spawn-time sidecar")
	}
}
```

- [ ] **Step 3: Run and watch both fail**

Run: `cd ~/dotfiles/agents && go test ./internal/harness/ -v`
Expected: FAIL — undefined identifiers.

- [ ] **Step 4: Implement the interface and registry**

`agents/internal/harness/harness.go`:

```go
// Package harness isolates everything that differs between coding-agent
// runtimes: what a hook payload is called, which events exist, what the
// generated config looks like, and what each runtime can and cannot tell us.
//
// Harness identity is always passed in explicitly. It is never inferred from
// the environment: Codex publishes no identifying variables of its own, and a
// Codex hook launched from a Claude Code session inherits ~14 CLAUDE_CODE_* and
// ANTHROPIC_* variables. Detection has false positives available and no true
// positives.
package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/pointer"
)

// Semantic event names. These are what the command line and the record use;
// each adapter maps them to its own vendor spelling for wiring.
const (
	SessionStart  = "session-start"
	SubagentStart = "subagent-start"
	SubagentStop  = "subagent-stop"
	Stop          = "stop"
)

// Payload is the entire subset of a hook payload this tool will decode.
//
// It is deliberately exhaustive: encoding/json discards keys with no
// destination field, so anything absent here -- last_assistant_message,
// tool_input, tool_response -- cannot reach any writer, whatever a future
// harness decides to send.
type Payload struct {
	HookEventName       string `json:"hook_event_name"`
	SessionID           string `json:"session_id"`
	TurnID              string `json:"turn_id"`
	AgentID             string `json:"agent_id"`
	AgentType           string `json:"agent_type"`
	Cwd                 string `json:"cwd"`
	TranscriptPath      string `json:"transcript_path"`
	AgentTranscriptPath string `json:"agent_transcript_path"`
	Source              string `json:"source"`
}

// Event maps a semantic event to one harness's spelling of it.
type Event struct {
	Semantic string
	Vendor   string
	Matcher  string // emitted only when non-empty; empty means match everything
}

// Capabilities states what a harness can supply, so the record format does not
// have to pretend they are equal.
type Capabilities struct {
	Description bool // supplies a human label for a subagent
}

// Trace is everything a harness can determine from a payload on its own,
// knowing nothing about repositories, machines, or lanes.
type Trace struct {
	Event           string
	SessionID       string
	TurnID          string
	AgentID         string
	AgentType       string
	Description     string
	Transcript      string
	PointerVerified bool
	Cwd             string // absolute, as the harness reported it
}

type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Events() []Event

	// Describe returns a human label for a subagent, or "" when the harness
	// cannot supply one. transcript is the already-resolved path, because the
	// only harness that can answer reads a sidecar next to it.
	Describe(p Payload, transcript string) string

	// WireConfigPath is the generated config file for a repo.
	WireConfigPath(repoRoot string) string

	// Wire writes that config, merging into whatever is already there.
	Wire(repoRoot, binary string) error

	// TrustSteps are the manual steps left after wiring. No harness lets a
	// freshly wired repo's hooks fire unattended, and defeating that gate is an
	// explicit non-goal.
	TrustSteps(repoRoot string) []string
}

var registry = map[string]Adapter{}

func register(a Adapter) { registry[a.Name()] = a }

func Get(name string) (Adapter, bool) {
	a, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return a, ok
}

// All returns every adapter in a stable order.
func All() []Adapter {
	names := []string{"claude-code", "codex"}
	var out []Adapter
	for _, n := range names {
		if a, ok := registry[n]; ok {
			out = append(out, a)
		}
	}
	return out
}

// Decode reads one hook payload. It is the single place where hook JSON becomes
// Go values, which is what makes the redaction guarantee auditable.
func Decode(r io.Reader) (Payload, error) {
	var p Payload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return Payload{}, fmt.Errorf("decode hook payload: %w", err)
	}
	return p, nil
}

// Build assembles the harness-determined part of a record.
func Build(a Adapter, semantic string, p Payload) Trace {
	tr := Trace{
		Event:     semantic,
		SessionID: p.SessionID,
		TurnID:    p.TurnID,
		Cwd:       p.Cwd,
	}

	// The key whose presence in a transcript path verifies the pointer: the
	// agent for subagent events, the session otherwise.
	key := p.SessionID
	if semantic == SubagentStart || semantic == SubagentStop {
		tr.AgentID = p.AgentID
		tr.AgentType = p.AgentType
		key = p.AgentID
	}

	tr.Transcript, tr.PointerVerified = pointer.Resolve(
		[]string{p.AgentTranscriptPath, p.TranscriptPath}, key,
	)
	if a.Capabilities().Description {
		tr.Description = a.Describe(p, tr.Transcript)
	}
	return tr
}
```

- [ ] **Step 5: Implement the Claude Code adapter (recording half)**

`agents/internal/harness/claudecode.go` — the wiring half lands in Task 7; write this much now:

```go
package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func init() { register(claudeCode{}) }

type claudeCode struct{}

func (claudeCode) Name() string { return "claude-code" }

func (claudeCode) Capabilities() Capabilities {
	// Claude Code writes an agent-<id>.meta.json sidecar at spawn time, so a
	// human label is available. Codex has no equivalent.
	return Capabilities{Description: true}
}

func (claudeCode) Events() []Event {
	return []Event{
		{Semantic: SessionStart, Vendor: "SessionStart"},
		{Semantic: SubagentStart, Vendor: "SubagentStart"},
		{Semantic: SubagentStop, Vendor: "SubagentStop"},
		{Semantic: Stop, Vendor: "Stop"},
	}
}

// Describe reads the spawn-time sidecar that sits beside the transcript.
// Best effort by design: a missing or unreadable sidecar costs a label, and a
// record without a label is still a usable pointer.
func (claudeCode) Describe(p Payload, transcript string) string {
	if transcript == "" || !strings.HasSuffix(transcript, ".jsonl") {
		return ""
	}
	sidecar := strings.TrimSuffix(transcript, ".jsonl") + ".meta.json"
	b, err := os.ReadFile(sidecar)
	if err != nil {
		return ""
	}
	var meta struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return ""
	}
	return meta.Description
}

func (claudeCode) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".claude", "settings.json")
}

func (claudeCode) TrustSteps(repoRoot string) []string {
	return []string{
		"Claude Code: open a session in " + repoRoot + " and accept the project-trust prompt once.",
		"Claude Code: hooks are re-read when the settings file changes; if they do not fire, open /hooks once in an interactive session.",
	}
}
```

Leave `Wire` unimplemented for now by adding this stub in the same file, and delete it in Task 7:

```go
func (claudeCode) Wire(repoRoot, binary string) error { return nil }
```

- [ ] **Step 6: Run tests**

Run: `cd ~/dotfiles/agents && go test ./internal/harness/ -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/internal/harness/ && git commit -m "feat(agents): harness adapter interface and Claude Code recording"
```

---

### Task 6: `agents hook` — the fail-open recording entrypoint

**Files:**
- Create: `agents/cmd_hook.go`
- Modify: `agents/main.go` (register the `hook` command)
- Test: `agents/cmd_hook_test.go`

**Interfaces:**
- Consumes: `harness.Decode/Build/Get`, `record.NewWriter`, `repo.Discover`, `lane.Resolve`, `machine.ID`, `exitcode.*`.
- Produces: `runHook(args []string, stdin io.Reader, stderr io.Writer) int` in package `main`.

**The contract this task implements:** recording hooks exit 0 on every path (§6). A missing `.agents/`, an unreadable payload, a full disk, an unknown harness — none of them may disrupt the dispatch that is waiting on this process. Diagnostics go to stderr; the exit code stays 0.

- [ ] **Step 1: Write the failing test**

`agents/cmd_hook_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	payload := `{"hook_event_name":"SubagentStop","session_id":"s1","agent_id":"a1",` +
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
	if rec["agent_id"] != "a1" {
		t.Errorf("agent_id = %v", rec["agent_id"])
	}
	if rec["pointer_verified"] != true {
		t.Errorf("pointer_verified = %v, want true", rec["pointer_verified"])
	}
	for k := range rec {
		if k == "last_assistant_message" || k == "tool_input" || k == "tool_response" {
			t.Errorf("forbidden field %q reached the record", k)
		}
	}
}

func TestHookFailsOpenOnEverything(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cases := []struct {
		name    string
		args    []string
		stdin   string
		inRepo  bool
	}{
		{"malformed payload", []string{"stop", "--harness", "claude-code"}, "not json", true},
		{"unknown harness", []string{"stop", "--harness", "gemini"}, "{}", true},
		{"unknown event", []string{"teatime", "--harness", "codex"}, "{}", true},
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
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test . -run TestHook -v`
Expected: FAIL — `undefined: runHook`.

- [ ] **Step 3: Implement**

`agents/cmd_hook.go`:

```go
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/lane"
	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// runHook records one hook firing.
//
// It returns 0 on every path, deliberately. A harness is blocked on this
// process; a trace record is worth strictly less than the dispatch it would
// interrupt. Every reason for not recording is reported on stderr instead,
// where `agents doctor` and the user can see it.
func runHook(args []string, stdin io.Reader, stderr io.Writer) int {
	if err := recordHook(args, stdin); err != nil {
		fmt.Fprintf(stderr, "agents hook: not recorded: %v\n", err)
	}
	return 0
}

var validEvents = map[string]bool{
	harness.SessionStart:  true,
	harness.SubagentStart: true,
	harness.SubagentStop:  true,
	harness.Stop:          true,
}

func recordHook(args []string, stdin io.Reader) error {
	if len(args) == 0 {
		return errors.New("usage: agents hook <event> --harness <name>")
	}
	event := args[0]
	if !validEvents[event] {
		return fmt.Errorf("unknown event %q", event)
	}

	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	harnessName := fs.String("harness", "", "harness that is calling (required)")
	laneFlag := fs.String("lane", "", "override lane resolution")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *harnessName == "" {
		return errors.New("--harness is required; harness identity is never inferred")
	}
	adapter, ok := harness.Get(*harnessName)
	if !ok {
		return fmt.Errorf("unknown harness %q", *harnessName)
	}

	p, err := harness.Decode(stdin)
	if err != nil {
		return err
	}

	// The payload's cwd is the harness's view; fall back to ours when absent.
	cwd := p.Cwd
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return err
		}
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		return err
	}
	agentsDir := repo.AgentsDir(rc.Root)
	if fi, err := os.Stat(agentsDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("no %s; run `agents init` here first", filepath.Base(agentsDir))
	}

	mid, err := machine.ID()
	if err != nil {
		return err
	}

	tr := harness.Build(adapter, event, p)
	return record.NewWriter(agentsDir).Append(record.Record{
		When:            time.Now().UTC(),
		Harness:         adapter.Name(),
		Machine:         mid,
		Event:           tr.Event,
		Lane:            lane.Resolve(*laneFlag, rc),
		Cwd:             rc.RelCwd,
		SessionID:       tr.SessionID,
		TurnID:          tr.TurnID,
		AgentID:         tr.AgentID,
		AgentType:       tr.AgentType,
		Description:     tr.Description,
		Transcript:      tr.Transcript,
		PointerVerified: tr.PointerVerified,
	})
}
```

Note: `repo.Discover` is given the *payload's* cwd, but `rc.RelCwd` is computed against it too — so a subagent that ran in a submodule directory records that directory, not the shell's. That is the point of the field.

- [ ] **Step 4: Register the command in `main.go`**

Replace the empty `switch` in `run` with:

```go
	switch args[0] {
	case "hook":
		return runHook(args[1:], os.Stdin, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		usage()
		return exitcode.Malformed
	}
```

- [ ] **Step 5: Run tests**

Run: `cd ~/dotfiles/agents && go test . -run TestHook -v`
Expected: all PASS, including all five fail-open subtests.

- [ ] **Step 6: Commit**

```bash
cd ~/dotfiles && git add agents/cmd_hook.go agents/cmd_hook_test.go agents/main.go && git commit -m "feat(agents): fail-open hook recording entrypoint"
```

---

### Task 7: `agents init` and `agents wire` for Claude Code

**Files:**
- Create: `agents/internal/scaffold/scaffold.go`
- Create: `agents/internal/scaffold/scaffold_test.go`
- Create: `agents/cmd_init.go`
- Create: `agents/cmd_wire.go`
- Modify: `agents/internal/harness/claudecode.go` (replace the `Wire` stub)
- Test: `agents/internal/harness/wire_claudecode_test.go`
- Test: `agents/cmd_init_test.go`
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `repo.Discover`, `repo.AgentsDir`, `harness.All`, `harness.Adapter.Wire/TrustSteps/WireConfigPath`.
- Produces: `scaffold.Create(repoRoot string, local bool) error`; `scaffold.ClaudeMD` (`string` constant); `runInit(args []string, stdout io.Writer) int`; `runWire(args []string, stdout io.Writer) int`.

**Decision:** generated harness configs are ignored via `.git/info/exclude`, not the repo's tracked `.gitignore`. They are machine-specific generated files, and a repo's tracked ignore list belongs to that repo's maintainers, not to this tool. The tracked `.gitattributes` is the exception — it is a deliberate, shared, repo-level statement (§3.7) and is appended to.

- [ ] **Step 1: Write the failing scaffold test**

`agents/internal/scaffold/scaffold_test.go`:

```go
package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateBuildsLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, rel := range []string{
		".agents/memory",
		".agents/reports/handoff",
		".agents/reports/specs",
		".agents/reports/plans",
		".agents/reports/analysis",
		".agents/reports/traces",
		".agents/skills",
	} {
		if fi, err := os.Stat(filepath.Join(root, rel)); err != nil || !fi.IsDir() {
			t.Errorf("missing directory %s", rel)
		}
	}

	// AGENTS.md is a symlink, not a copy: two byte-identical files silently
	// diverge, which is what the prior art actually did.
	fi, err := os.Lstat(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("AGENTS.md must be a symlink to CLAUDE.md")
	}
	target, _ := os.Readlink(filepath.Join(root, "AGENTS.md"))
	if target != "CLAUDE.md" {
		t.Fatalf("AGENTS.md -> %q, want CLAUDE.md", target)
	}

	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf(".gitattributes: %v", err)
	}
	if !strings.Contains(string(attrs), "merge=union") {
		t.Error("traces need merge=union or a concurrent append produces invalid JSON")
	}
	if !strings.Contains(string(attrs), "linguist-generated=true") {
		t.Error(".agents/** should collapse in diffs")
	}
}

func TestCreatePreservesExistingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.png binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(claude) != "# mine\n" {
		t.Error("an existing CLAUDE.md must not be overwritten")
	}
	attrs, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if !strings.Contains(string(attrs), "*.png binary") {
		t.Error("existing gitattributes lines must survive")
	}
	if !strings.Contains(string(attrs), "merge=union") {
		t.Error("our lines must still be appended")
	}

	// Idempotent: a second Create must not duplicate the appended lines.
	if err := Create(root, false); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	attrs2, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if strings.Count(string(attrs2), "merge=union") != 1 {
		t.Errorf("gitattributes duplicated on re-run:\n%s", attrs2)
	}
}

func TestCreateLocalExcludesAgentsDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, true); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("exclude: %v", err)
	}
	if !strings.Contains(string(exclude), "/.agents/") {
		t.Errorf("--local must exclude .agents/:\n%s", exclude)
	}
}

func TestCreateAlwaysExcludesGeneratedHarnessConfigs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude, _ := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	for _, want := range []string{"/.claude/settings.json", "/.codex/hooks.json", "/.agents/.trace-cache/"} {
		if !strings.Contains(string(exclude), want) {
			t.Errorf("exclude missing %q:\n%s", want, exclude)
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/scaffold/ -v`
Expected: FAIL — `undefined: Create`.

- [ ] **Step 3: Implement scaffold**

`agents/internal/scaffold/scaffold.go`:

```go
// Package scaffold creates the tracked .agents/ layout in a repository and the
// two thin files that make a harness notice it.
package scaffold

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeMD is the trigger, not the payload.
//
// It is the only file every harness loads automatically, so it costs context in
// every session -- including the ones that never touch .agents/. Keep it short.
const ClaudeMD = `# Agent context

Durable context for this repo lives in ` + "`.agents/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`.agents/memory/INDEX.md`" + ` — curated knowledge about this codebase (generated)
- ` + "`.agents/reports/handoff/INDEX.md`" + ` — work in flight, by lane (generated)
- ` + "`.agents/reports/`" + ` — specs, plans, analysis, and trace pointers
- ` + "`.agents/skills/`" + ` — procedures specific to this repo

Run ` + "`agents doctor`" + ` early and surface what it says. A hook cannot install
itself and a missing hook fails silently, so an unreported failure here means
nothing is being recorded.

Write handoffs with ` + "`agents handoff write`" + `, not by hand. Commit ` + "`.agents/`" + `
changes with ` + "`agents save`" + ` so they do not ride along with code changes.
`

// gitattributesLines are tracked on purpose: they are a statement about how this
// repository merges and renders, which belongs to the repository.
//
// merge=union is load-bearing, not cosmetic. Two branches appending traces on
// the same day otherwise produce conflict markers that are not valid JSON, and a
// line-oriented reader silently drops those lines.
var gitattributesLines = []string{
	".agents/reports/traces/*.jsonl merge=union",
	".agents/** linguist-generated=true",
}

// excludeLines are machine-specific generated paths. They go in
// .git/info/exclude rather than the repo's tracked .gitignore: an ignore list
// belongs to the repository's maintainers, not to this tool.
var excludeLines = []string{
	"/.claude/settings.json",
	"/.claude/skills",
	"/.codex/hooks.json",
	"/.codex/skills",
	"/.agents/.trace-cache/",
}

var dirs = []string{
	"memory",
	"reports/handoff",
	"reports/specs",
	"reports/plans",
	"reports/analysis",
	"reports/traces",
	"skills",
}

// Create is idempotent. Running it on an initialized repo must change nothing.
func Create(root string, local bool) error {
	agents := filepath.Join(root, ".agents")
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(agents, d), 0o755); err != nil {
			return err
		}
	}
	// git does not track empty directories, and an .agents/ that vanishes on
	// clone is worse than one with placeholder files.
	for _, d := range dirs {
		keep := filepath.Join(agents, d, ".gitkeep")
		if _, err := os.Stat(keep); os.IsNotExist(err) {
			if err := os.WriteFile(keep, nil, 0o644); err != nil {
				return err
			}
		}
	}

	if err := writeIfAbsent(filepath.Join(root, "CLAUDE.md"), ClaudeMD); err != nil {
		return err
	}
	if err := linkIfAbsent(filepath.Join(root, "AGENTS.md"), "CLAUDE.md"); err != nil {
		return err
	}

	if err := appendMissingLines(filepath.Join(root, ".gitattributes"), gitattributesLines); err != nil {
		return err
	}

	lines := excludeLines
	if local {
		// --local: the whole directory stays out of the repo, for repos where
		// committing agent artifacts is not acceptable. Same layout either way.
		lines = append(append([]string{}, excludeLines...), "/.agents/")
	}
	return appendMissingLines(filepath.Join(root, ".git", "info", "exclude"), lines)
}

func writeIfAbsent(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func linkIfAbsent(path, target string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return os.Symlink(target, path)
}

// appendMissingLines adds only the lines that are not already present, so
// running init twice does not duplicate anything and a hand-edited file keeps
// its edits.
func appendMissingLines(path string, want []string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(l)] = true
	}

	var add []string
	for _, l := range want {
		if !have[l] {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	var b strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	for _, l := range add {
		b.WriteString(l + "\n")
	}
	_, err = f.WriteString(b.String())
	return err
}
```

- [ ] **Step 4: Write the failing Claude Code wiring test**

`agents/internal/harness/wire_claudecode_test.go`:

```go
package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings is not JSON: %v\n%s", err, b)
	}
	return m
}

func commandsFor(t *testing.T, settings map[string]any, event string) []string {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	groups, _ := hooks[event].([]any)
	var out []string
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if c, ok := hm["command"].(string); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func TestClaudeCodeWireWritesEveryEvent(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")

	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}

	settings := readSettings(t, a.WireConfigPath(root))
	for _, ev := range []string{"SessionStart", "SubagentStart", "SubagentStop", "Stop"} {
		cmds := commandsFor(t, settings, ev)
		if len(cmds) != 1 {
			t.Fatalf("%s: got %d commands, want 1: %v", ev, len(cmds), cmds)
		}
		if !strings.HasPrefix(cmds[0], "/Users/n/bin/agents hook ") {
			t.Errorf("%s: command = %q", ev, cmds[0])
		}
		if !strings.Contains(cmds[0], "--harness claude-code") {
			t.Errorf("%s: command must name the harness explicitly: %q", ev, cmds[0])
		}
	}

	// The skills symlink is how a repo-specific procedure written once is
	// loaded by both harnesses.
	link := filepath.Join(root, ".claude", "skills")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf(".claude/skills must be a symlink: %v", err)
	}
	if target != filepath.Join("..", ".agents", "skills") {
		t.Errorf(".claude/skills -> %q", target)
	}
}

func TestClaudeCodeWireMergesAndDoesNotDuplicate(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	existing := `{
	  "effortLevel": "high",
	  "permissions": {"allow": ["Bash(git *)"]},
	  "hooks": {
	    "PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"my-audit.sh"}]}],
	    "SubagentStop": [{"hooks":[{"type":"command","command":"my-notify.sh"}]}]
	  }
	}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}
	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("second Wire: %v", err)
	}

	settings := readSettings(t, path)
	if settings["effortLevel"] != "high" {
		t.Error("unrelated settings must survive wiring")
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions must survive wiring")
	}
	if got := commandsFor(t, settings, "PreToolUse"); len(got) != 1 || got[0] != "my-audit.sh" {
		t.Errorf("foreign hooks on other events must survive: %v", got)
	}

	cmds := commandsFor(t, settings, "SubagentStop")
	var mine, ours int
	for _, c := range cmds {
		if c == "my-notify.sh" {
			mine++
		}
		if strings.Contains(c, "--harness claude-code") {
			ours++
		}
	}
	if mine != 1 {
		t.Errorf("a foreign hook on an event we own must survive: %v", cmds)
	}
	if ours != 1 {
		t.Errorf("re-wiring must replace, not duplicate: %v", cmds)
	}
}

// A binary that moved must still be recognised as ours and replaced, or every
// `make agents` to a new location would leave a dead hook behind.
func TestClaudeCodeWireReplacesAMovedBinary(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	if err := a.Wire(root, "/old/path/agents"); err != nil {
		t.Fatal(err)
	}
	if err := a.Wire(root, "/new/path/agents"); err != nil {
		t.Fatal(err)
	}

	cmds := commandsFor(t, readSettings(t, a.WireConfigPath(root)), "Stop")
	if len(cmds) != 1 || !strings.HasPrefix(cmds[0], "/new/path/agents") {
		t.Fatalf("commands = %v, want exactly the new path", cmds)
	}
}
```

- [ ] **Step 5: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/harness/ -run Wire -v`
Expected: FAIL — the `Wire` stub writes nothing.

- [ ] **Step 6: Implement Claude Code wiring**

Delete the `Wire` stub from `agents/internal/harness/claudecode.go` and add:

```go
func (c claudeCode) Wire(repoRoot, binary string) error {
	if err := writeHooksJSON(c.WireConfigPath(repoRoot), c.Name(), c.Events(), binary); err != nil {
		return err
	}
	// Neither harness discovers .agents/skills on its own: Claude Code reads
	// .claude/skills, Codex reads .codex/skills. One directory, two names.
	return linkSkills(filepath.Join(repoRoot, ".claude", "skills"))
}
```

and put the shared helpers in `agents/internal/harness/harness.go` — Codex uses the identical file format, verified against the working probe config:

```go
// hookCommand renders the invocation that a harness will run. Harness identity
// is on the command line because it cannot be read from the environment.
func hookCommand(binary, harnessName, semantic string) string {
	return fmt.Sprintf("%s hook %s --harness %s", binary, semantic, harnessName)
}

// isOurs reports whether a configured command was generated by `agents wire`.
//
// It matches on the shape of the invocation rather than on an absolute path, so
// rebuilding the binary to a new location replaces the old entry instead of
// leaving a dead one beside a new one.
func isOurs(cmd string) bool {
	return strings.Contains(cmd, " hook ") && strings.Contains(cmd, " --harness ")
}

// writeHooksJSON merges our hook entries into a config file both harnesses
// share the schema of, preserving every key and every foreign hook.
//
// Merging rather than owning matters: this file also carries settings that have
// nothing to do with us (permissions, effort level, a project's own hooks), and
// silently dropping someone's audit hook would be a serious misbehaviour.
func writeHooksJSON(path, harnessName string, events []Event, binary string) error {
	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON; fix or remove it: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	for _, ev := range events {
		groups, _ := hooks[ev.Vendor].([]any)
		kept := stripOurs(groups)

		entry := map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": hookCommand(binary, harnessName, ev.Semantic),
			}},
		}
		if ev.Matcher != "" {
			entry["matcher"] = ev.Matcher
		}
		hooks[ev.Vendor] = append(kept, entry)
	}
	settings["hooks"] = hooks

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// stripOurs removes previously generated entries, at the level of individual
// hooks rather than whole groups, so a group that mixes a foreign hook with
// ours keeps the foreign one.
func stripOurs(groups []any) []any {
	var kept []any
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			kept = append(kept, g)
			continue
		}
		inner, _ := gm["hooks"].([]any)
		var innerKept []any
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); isOurs(cmd) {
				continue
			}
			innerKept = append(innerKept, h)
		}
		if len(innerKept) == 0 {
			continue
		}
		gm["hooks"] = innerKept
		kept = append(kept, gm)
	}
	return kept
}

// linkSkills points a harness's skills directory at the tracked one. It never
// replaces a real directory -- only a stale symlink -- because a real directory
// there is somebody's content.
func linkSkills(link string) error {
	target := filepath.Join("..", ".agents", "skills")
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink; move it aside to wire skills", link)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, link)
}
```

Add `encoding/json`, `os`, `path/filepath` to `harness.go`'s imports.

- [ ] **Step 7: Write the failing init test**

`agents/cmd_init_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitScaffoldsWiresAndReportsTrust(t *testing.T) {
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
}

func TestInitIsIdempotent(t *testing.T) {
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
```

- [ ] **Step 8: Implement `init` and `wire`**

`agents/cmd_init.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func runInit(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stdout)
	local := fs.Bool("local", false, "keep .agents/ out of the repository")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents init: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents init: not inside a git repository; nothing to do")
		return exitcode.Skip
	}

	if err := scaffold.Create(rc.Root, *local); err != nil {
		fmt.Fprintf(stdout, "agents init: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintf(stdout, "initialized %s\n", repo.AgentsDir(rc.Root))

	if code := wireAll(rc.Root, stdout); code != exitcode.OK {
		return code
	}

	// Exit advisory, not OK. Wiring is written but not yet live, and reporting
	// success for a setup that is not recording anything would be the exact
	// silent failure this design exists to prevent.
	fmt.Fprintln(stdout, "\nRemaining trust steps (a hook cannot install itself):")
	for _, a := range harness.All() {
		for _, s := range a.TrustSteps(rc.Root) {
			fmt.Fprintf(stdout, "  - %s\n", s)
		}
	}
	fmt.Fprintln(stdout, "\nRun `agents doctor` here afterwards to confirm.")
	return exitcode.Advisory
}

// binaryPath is the absolute path to write into generated configs. A harness
// runs hooks with an environment that is not the user's shell, so a bare
// "agents" is not reliably resolvable.
func binaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

func wireAll(root string, stdout io.Writer) int {
	bin, err := binaryPath()
	if err != nil {
		fmt.Fprintf(stdout, "agents: cannot resolve own path: %v\n", err)
		return exitcode.NoRecord
	}
	for _, a := range harness.All() {
		if err := a.Wire(root, bin); err != nil {
			fmt.Fprintf(stdout, "agents: wiring %s: %v\n", a.Name(), err)
			return exitcode.NoRecord
		}
		fmt.Fprintf(stdout, "wired %s -> %s\n", a.Name(), a.WireConfigPath(root))
	}
	return exitcode.OK
}
```

`agents/cmd_wire.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// runWire regenerates harness configs without touching .agents/ content. It is
// the command to run after `make agents` moves the binary, or after a harness
// changes its schema.
func runWire(args []string, stdout io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents wire: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents wire: not inside a git repository")
		return exitcode.Skip
	}
	return wireAll(rc.Root, stdout)
}
```

Register both in `main.go`'s switch:

```go
	case "init":
		return runInit(args[1:], os.Stdout)
	case "wire":
		return runWire(args[1:], os.Stdout)
```

- [ ] **Step 9: Run the full suite**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS. `TestInitScaffoldsWiresAndReportsTrust` asserts `.codex/hooks.json` exists, so the Codex adapter must at least be registered with a working `Wire` — if Task 9 has not landed yet, that subtest will fail. Implement the Codex adapter's `Wire` now if so; its recording half stays in Task 9.

- [ ] **Step 10: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): init and wire, with merge-preserving harness config generation"
```

---

### Task 8: `make agents`, and prove the loop on a live Claude Code session

**Files:**
- Modify: `Makefile:1` (`.PHONY` line) and add an `agents` target
- Create: `docs/superpowers/specs/agents/fixtures/2026-08-07-claude-code-hook-payloads/` (captured in this task)
- Modify: `agents/internal/harness/claudecode_test.go` (reconcile against the captured payload)

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: `~/bin/agents` on `PATH`; a captured Claude Code payload fixture directory.

**Why the live capture is part of this task:** the Claude Code payload shape in `claudecode_test.go` was reconstructed from the spec's record of prior art, not measured in this session. The Codex fixtures were measured. Ending Phase 1 without closing that gap would leave the one untested assumption in the whole phase sitting under the harness the phase is named after.

- [ ] **Step 1: Add the Makefile target**

Change line 1 to include the new target:

```make
.PHONY: all dep links editors tmux extra omz bins dotfiles fish agents
```

and add, after the `bins` target:

```make
# The agents binary lives in dotfiles and is invoked by absolute path from
# generated harness configs. Nothing is vendored per-repo.
agents:
	mkdir -p $(HOME)/bin
	cd $(CURDIR)/agents && go build -trimpath -o $(HOME)/bin/agents .
	@echo "built $(HOME)/bin/agents"
```

- [ ] **Step 2: Build and confirm it is reachable**

```bash
cd ~/dotfiles && make agents && command -v agents && agents 2>&1 | head -5
```

Expected: `/Users/nilbot/bin/agents`, then the usage text. (`~/bin` is already on `PATH` via `fish_user_paths`.)

- [ ] **Step 3: Capture real Claude Code payloads**

Create a throwaway repo, wire a dump script by hand, and run one session with a subagent:

```bash
mkdir -p /tmp/ccprobe && cd /tmp/ccprobe && git init -b main && mkdir -p .claude out
printf '#!/bin/bash\ncat > "/tmp/ccprobe/out/$1-$(date +%%s%%N).json"\n' > dump.sh && chmod +x dump.sh
cat > .claude/settings.json <<'JSON'
{"hooks":{
  "SessionStart":[{"hooks":[{"type":"command","command":"/tmp/ccprobe/dump.sh cc-session-start"}]}],
  "SubagentStart":[{"hooks":[{"type":"command","command":"/tmp/ccprobe/dump.sh cc-subagent-start"}]}],
  "SubagentStop":[{"hooks":[{"type":"command","command":"/tmp/ccprobe/dump.sh cc-subagent-stop"}]}],
  "Stop":[{"hooks":[{"type":"command","command":"/tmp/ccprobe/dump.sh cc-stop"}]}]
}}
JSON
```

Then, in an interactive Claude Code session started in `/tmp/ccprobe`, ask it to dispatch one Explore subagent at anything trivial. Inspect what landed:

```bash
ls /tmp/ccprobe/out/ && jq -S 'keys' /tmp/ccprobe/out/*.json
```

Two things to read off this, both of which the plan assumes:

1. **Does `SubagentStart` exist?** If no `cc-subagent-start-*.json` appears, Claude Code does not implement that event. Remove it from `claudeCode.Events()` and note it in the spec's Measured facts section.
2. **What is the transcript field called, and is `description` in the payload or only in the sidecar?**

- [ ] **Step 4: Sanitize and commit the fixtures**

Replace `last_assistant_message`, `tool_input`, and `tool_response` values with `"<REDACTED — see spec 3.2>"` before committing anything — the Codex capture found encrypted task blobs in `tool_input.message`, and the same hazard applies here:

```bash
mkdir -p ~/dotfiles/docs/superpowers/specs/agents/fixtures/2026-08-07-claude-code-hook-payloads
for f in /tmp/ccprobe/out/*.json; do
  jq 'if has("last_assistant_message") then .last_assistant_message = "<REDACTED — see spec 3.2>" else . end
      | if has("tool_input") then .tool_input = "<REDACTED — see spec 3.2>" else . end
      | if has("tool_response") then .tool_response = "<REDACTED — see spec 3.2>" else . end' "$f" \
    > ~/dotfiles/docs/superpowers/specs/agents/fixtures/2026-08-07-claude-code-hook-payloads/"$(basename "$f")"
done
```

Read every produced file before committing. Rename them to stable names (`cc-subagent-stop.json`, etc.) and write a `README.md` in that directory in the same shape as the Codex one, stating the capture method and what was redacted.

- [ ] **Step 5: Reconcile the test against reality**

Update `ccSubagentStopPayload` in `claudecode_test.go` to the captured JSON (with the transcript path still parameterized via `PLACEHOLDER`). If field names differ from the reconstruction, fix `harness.Payload` and `claudeCode.Events()` to match what was measured, then re-run.

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS against measured input.

- [ ] **Step 6: Prove the end-to-end loop**

```bash
cd /tmp/ccprobe && rm -f .claude/settings.json && agents init; agents doctor 2>/dev/null || true
```

Then, in an interactive Claude Code session in `/tmp/ccprobe`, dispatch one subagent again and check:

```bash
cat /tmp/ccprobe/.agents/reports/traces/*.jsonl | jq -c '{event,harness,machine,lane,cwd,agent_id,pointer_verified}'
```

Expected: at least one `subagent_stop` line with `"pointer_verified": true`, `"machine"` set, and `"lane": "main"`. Confirm no line contains message text:

```bash
grep -c -iE 'last_assistant_message|tool_input|tool_response' /tmp/ccprobe/.agents/reports/traces/*.jsonl
```

Expected: `0`.

- [ ] **Step 7: Clean up and commit**

```bash
rm -rf /tmp/ccprobe
cd ~/dotfiles && git add Makefile agents/ docs/superpowers/specs/agents/fixtures/ && \
  git commit -m "feat(agents): make target, and Claude Code payloads captured from a live session"
```

---

## Phase 2 — Codex

Ends with: the same loop running under Codex, asserted against the payloads captured on 2026-08-07.

### Task 9: Codex adapter, golden-tested against the captured fixtures

**Files:**
- Create: `agents/internal/harness/codex.go`
- Test: `agents/internal/harness/codex_test.go`

**Interfaces:**
- Consumes: `harness.Adapter`, `harness.Build`, `writeHooksJSON`, `linkSkills`, `hookCommand`.
- Produces: the `codex` adapter registered under the name `"codex"`.

**Fixtures:** `docs/superpowers/specs/agents/fixtures/2026-08-07-codex-hook-payloads/`. The package sits at `agents/internal/harness/`, so the relative path from a test is `../../../docs/superpowers/specs/agents/fixtures/2026-08-07-codex-hook-payloads`.

- [ ] **Step 1: Write the failing golden tests**

`agents/internal/harness/codex_test.go`:

```go
package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtures = "../../../docs/superpowers/specs/agents/fixtures/2026-08-07-codex-hook-payloads"

func loadFixture(t *testing.T, name string) Payload {
	t.Helper()
	f, err := os.Open(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	defer f.Close()
	p, err := Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return p
}

// SubagentStop hands over both transcripts and it is the parent that sits in
// transcript_path. Picking the wrong one would point every subagent record at
// the session that spawned it, which is worse than not recording.
func TestCodexSubagentStopPicksTheChildTranscript(t *testing.T) {
	p := loadFixture(t, "codex-subagent-stop.json")
	a, _ := Get("codex")
	tr := Build(a, SubagentStop, p)

	const wantID = "019fdcab-ac94-7502-a322-d01f047c274a"
	if tr.AgentID != wantID {
		t.Fatalf("AgentID = %q, want %q", tr.AgentID, wantID)
	}
	if !strings.Contains(filepath.Base(tr.Transcript), wantID) {
		t.Fatalf("Transcript = %q, want the child's (basename contains %s)", tr.Transcript, wantID)
	}
	if !tr.PointerVerified {
		t.Fatal("PointerVerified = false, want true")
	}
	if tr.TurnID != "019fdcab-ad07-7f23-bba4-e3261c09eae7" {
		t.Errorf("TurnID = %q; each subagent gets its own turn", tr.TurnID)
	}
}

// SubagentStart supplies only transcript_path, and it is already the child's --
// the opposite of SubagentStop. This asymmetry is the reason pointer resolution
// derives instead of consulting a per-event field map.
func TestCodexSubagentStartAlsoPicksTheChild(t *testing.T) {
	p := loadFixture(t, "codex-subagent-start.json")
	if p.AgentTranscriptPath != "" {
		t.Fatal("fixture changed: SubagentStart used to omit agent_transcript_path")
	}
	a, _ := Get("codex")
	tr := Build(a, SubagentStart, p)
	if !strings.Contains(filepath.Base(tr.Transcript), tr.AgentID) || !tr.PointerVerified {
		t.Fatalf("Transcript = %q for agent %q, verified=%v", tr.Transcript, tr.AgentID, tr.PointerVerified)
	}
}

func TestCodexSessionEventsVerifyOnSessionID(t *testing.T) {
	for _, name := range []string{"codex-session-start.json", "codex-stop.json"} {
		p := loadFixture(t, name)
		a, _ := Get("codex")
		semantic := SessionStart
		if strings.Contains(name, "stop") {
			semantic = Stop
		}
		tr := Build(a, semantic, p)
		if !tr.PointerVerified {
			t.Errorf("%s: PointerVerified = false", name)
		}
		if tr.AgentID != "" {
			t.Errorf("%s: AgentID = %q, want empty", name, tr.AgentID)
		}
	}
}

// Codex has no description field anywhere in any payload. Declaring the gap is
// the point: retrieval for Codex records leans on lane, cwd and agent_type.
func TestCodexDeclaresNoDescriptionCapability(t *testing.T) {
	a, _ := Get("codex")
	if a.Capabilities().Description {
		t.Fatal("Codex supplies no description; the adapter must say so")
	}
	tr := Build(a, SubagentStop, loadFixture(t, "codex-subagent-stop.json"))
	if tr.Description != "" {
		t.Fatalf("Description = %q, want empty", tr.Description)
	}
}

// The redaction promise, checked against a payload that really carries the
// forbidden field.
func TestCodexFixtureCarriesForbiddenFieldAndItIsDropped(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtures, "codex-subagent-stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "last_assistant_message") {
		t.Fatal("fixture no longer exercises the case this test exists for")
	}
	tr := Build(mustGet(t, "codex"), SubagentStop, loadFixture(t, "codex-subagent-stop.json"))
	joined := tr.Description + tr.Transcript + tr.AgentType + tr.SessionID
	if strings.Contains(joined, "REDACTED") {
		t.Fatalf("payload content leaked into the trace: %q", joined)
	}
}

func mustGet(t *testing.T, name string) Adapter {
	t.Helper()
	a, ok := Get(name)
	if !ok {
		t.Fatalf("adapter %q not registered", name)
	}
	return a
}

func TestCodexWireMatchesTheVerifiedSchema(t *testing.T) {
	root := t.TempDir()
	a := mustGet(t, "codex")
	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}

	settings := readSettings(t, a.WireConfigPath(root))
	if a.WireConfigPath(root) != filepath.Join(root, ".codex", "hooks.json") {
		t.Fatalf("config path = %q", a.WireConfigPath(root))
	}
	for _, ev := range []string{"SessionStart", "SubagentStart", "SubagentStop", "Stop"} {
		cmds := commandsFor(t, settings, ev)
		if len(cmds) != 1 || !strings.Contains(cmds[0], "--harness codex") {
			t.Errorf("%s: commands = %v", ev, cmds)
		}
	}
	if _, err := os.Readlink(filepath.Join(root, ".codex", "skills")); err != nil {
		t.Errorf(".codex/skills must be a symlink: %v", err)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/harness/ -run Codex -v`
Expected: FAIL — adapter `"codex"` is not registered (or, if a `Wire`-only stub was added in Task 7 step 9, the recording tests fail).

- [ ] **Step 3: Implement**

`agents/internal/harness/codex.go`:

```go
package harness

import "path/filepath"

func init() { register(codex{}) }

type codex struct{}

func (codex) Name() string { return "codex" }

// Codex sends no description in any payload -- verified across every captured
// event on 0.147.0. Saying so here is what keeps the record format honest
// instead of implying the two harnesses are equal.
func (codex) Capabilities() Capabilities { return Capabilities{Description: false} }

func (codex) Events() []Event {
	return []Event{
		// The matcher is Codex's own vocabulary for SessionStart and was part
		// of the configuration verified to fire on 2026-08-07.
		{Semantic: SessionStart, Vendor: "SessionStart", Matcher: "startup|resume"},
		{Semantic: SubagentStart, Vendor: "SubagentStart"},
		{Semantic: SubagentStop, Vendor: "SubagentStop"},
		{Semantic: Stop, Vendor: "Stop"},
	}
}

func (codex) Describe(Payload, string) string { return "" }

// hooks.json, not an inline [hooks] block in config.toml: ~/.codex/config.toml
// is co-owned by the ChatGPT desktop app, which writes [desktop] and [projects]
// sections into it.
func (codex) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".codex", "hooks.json")
}

func (c codex) Wire(repoRoot, binary string) error {
	if err := writeHooksJSON(c.WireConfigPath(repoRoot), c.Name(), c.Events(), binary); err != nil {
		return err
	}
	return linkSkills(filepath.Join(repoRoot, ".codex", "skills"))
}

func (codex) TrustSteps(repoRoot string) []string {
	return []string{
		"Codex: run `codex` once in " + repoRoot + " and accept the project-trust prompt.",
		"Codex: run /hooks in that session and trust the agents hooks. Trust is hash-based, " +
			"so this recurs after every `agents wire` that changes the commands.",
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dotfiles && git add agents/internal/harness/ && git commit -m "feat(agents): Codex adapter, golden-tested against captured payloads"
```

---

### Task 10: Prove the loop on a live Codex session

**Files:**
- Modify: `docs/superpowers/specs/agents/2026-08-07-agents-repo-context-design.md` (Measured facts, only if reality differs)

**Interfaces:**
- Consumes: `~/bin/agents` from Task 8.
- Produces: nothing in code. This task's deliverable is evidence.

- [ ] **Step 1: Set up a throwaway repo and initialize it**

```bash
mkdir -p /tmp/codexprobe2 && cd /tmp/codexprobe2 && git init -b main && make -C ~/dotfiles agents && agents init
```

Expected: exit 1 with the trust steps printed, and `.codex/hooks.json` present.

- [ ] **Step 2: Trust the hooks and run a session with a subagent**

```bash
cd /tmp/codexprobe2 && codex exec --dangerously-bypass-hook-trust 'Use a subagent to count the files in this directory, then stop.'
```

Wiring `--dangerously-bypass-hook-trust` into generated configs is an explicit non-goal (§9); it is used here, by hand, once, only to avoid an interactive step inside a verification run.

- [ ] **Step 3: Inspect the records**

```bash
jq -c '{event,harness,machine,lane,cwd,agent_id,agent_type,description,pointer_verified}' \
  /tmp/codexprobe2/.agents/reports/traces/*.jsonl
```

Expected: `session_start`, at least one `subagent_start` / `subagent_stop` pair, and `stop`; every line with `machine` set and `pointer_verified: true`; `description` empty on every line, which is the declared Codex gap and not a bug.

- [ ] **Step 4: Confirm the transcripts the pointers name actually exist**

```bash
jq -r 'select(.pointer_verified) | .transcript' /tmp/codexprobe2/.agents/reports/traces/*.jsonl \
  | sort -u | while read -r p; do [ -f "$p" ] && echo "OK  $p" || echo "MISSING  $p"; done
```

Expected: every line `OK`. A `MISSING` here means pointer resolution picked a plausible-looking path that does not exist — investigate before proceeding, because every later phase trusts these pointers.

- [ ] **Step 5: Confirm no message content was recorded**

```bash
grep -c -iE 'last_assistant_message|tool_input|tool_response|gAAAAAB' /tmp/codexprobe2/.agents/reports/traces/*.jsonl
```

Expected: `0`.

- [ ] **Step 6: Record any divergence, then clean up**

If Codex 0.147.x behaved differently from the 2026-08-07 capture in any respect, add it to the spec's Measured facts section with the new date, re-capture the fixtures, and re-run the golden tests. Otherwise:

```bash
rm -rf /tmp/codexprobe2
```

- [ ] **Step 7: Commit (only if the spec changed)**

```bash
cd ~/dotfiles && git add docs/ && git commit -m "docs(agents): reconcile measured Codex facts after live verification"
```

---

## Phase 3 — Retrieval

Ends with: the records written in Phases 1–2 are answerable. Daily JSONL files are storage; the CLI is the index (§3.6). A derived index file would drift out of sync with storage; a query command cannot.

### Task 11: `agents trace ls`

**Files:**
- Create: `agents/internal/trace/trace.go`
- Create: `agents/internal/trace/trace_test.go`
- Create: `agents/cmd_trace.go`
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `record.Record`, `repo.Discover`, `repo.AgentsDir`.
- Produces: `trace.Filter{Lane, Module, Machine, Harness, Event, Grep string; Since time.Duration; Limit int}`; `trace.Result{Records []record.Record; Skipped int}`; `trace.Query(agentsDir string, f Filter, now time.Time) (Result, error)`; `trace.ParseSince(string) (time.Duration, error)`; `runTrace(args []string, stdout io.Writer) int`.

- [ ] **Step 1: Write the failing tests**

`agents/internal/trace/trace_test.go`:

```go
package trace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

func seed(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	w := record.NewWriter(dir)

	recs := []record.Record{
		{When: now.Add(-1 * time.Hour), Harness: "codex", Machine: "m1", Event: "subagent_stop",
			Lane: "sq-123-payments", Cwd: "payments/api", AgentID: "a1",
			Transcript: "/t/a1.jsonl", PointerVerified: true},
		{When: now.Add(-2 * time.Hour), Harness: "claude-code", Machine: "m1", Event: "subagent_stop",
			Lane: "sq-123-payments", Cwd: "payments/web", AgentID: "a2",
			Description: "trace the retry window", Transcript: "/t/a2.jsonl", PointerVerified: true},
		{When: now.Add(-72 * time.Hour), Harness: "codex", Machine: "m2", Event: "stop",
			Lane: "other-lane", Cwd: ".", Transcript: "/t/a3.jsonl"},
	}
	for _, r := range recs {
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return dir, now
}

func TestQueryNewestFirst(t *testing.T) {
	dir, now := seed(t)
	res, err := Query(dir, Filter{}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Records) != 3 {
		t.Fatalf("got %d records, want 3", len(res.Records))
	}
	if res.Records[0].AgentID != "a1" {
		t.Errorf("first = %q, want the newest (a1)", res.Records[0].AgentID)
	}
}

func TestQueryFilters(t *testing.T) {
	dir, now := seed(t)
	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"lane", Filter{Lane: "sq-123-payments"}, 2},
		{"module prefix", Filter{Module: "payments"}, 2},
		{"module exact", Filter{Module: "payments/api"}, 1},
		{"machine", Filter{Machine: "m2"}, 1},
		{"harness", Filter{Harness: "codex"}, 2},
		{"event", Filter{Event: "stop"}, 1},
		{"since 3h", Filter{Since: 3 * time.Hour}, 2},
		{"grep description", Filter{Grep: "retry window"}, 1},
		{"limit", Filter{Limit: 1}, 1},
		{"combined", Filter{Lane: "sq-123-payments", Harness: "codex"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Query(dir, tc.f, now)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(res.Records) != tc.want {
				t.Fatalf("got %d, want %d: %+v", len(res.Records), tc.want, res.Records)
			}
		})
	}
}

// A merge that went wrong leaves conflict markers, which are not valid JSON.
// Dropping them silently is how a reader lies about coverage; count them.
func TestQueryCountsUnreadableLines(t *testing.T) {
	dir, now := seed(t)
	path := filepath.Join(dir, "reports", "traces", "2026-08-10.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("<<<<<<< HEAD\n")
	f.Close()

	res, err := Query(dir, Filter{}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", res.Skipped)
	}
}

func TestQueryOnEmptyRepo(t *testing.T) {
	res, err := Query(t.TempDir(), Filter{}, time.Now())
	if err != nil {
		t.Fatalf("an uninitialized repo is not an error: %v", err)
	}
	if len(res.Records) != 0 {
		t.Fatalf("got %d records", len(res.Records))
	}
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"3d":  72 * time.Hour,
		"12h": 12 * time.Hour,
		"90m": 90 * time.Minute,
		"2w":  14 * 24 * time.Hour,
		"":    0,
	}
	for in, want := range cases {
		got, err := ParseSince(in)
		if err != nil {
			t.Fatalf("ParseSince(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseSince(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseSince("soon"); err == nil {
		t.Error("want an error for unparseable input")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/trace/ -v`
Expected: FAIL — `undefined: Query`.

- [ ] **Step 3: Implement the query engine**

`agents/internal/trace/trace.go`:

```go
// Package trace queries the tracked pointer index.
//
// The daily JSONL files are storage; this package is the index. A generated
// index file would drift out of sync with what is on disk the moment anything
// wrote a record without regenerating it. A query cannot drift.
package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

type Filter struct {
	Lane    string        // exact
	Module  string        // path prefix on the repo-relative cwd
	Machine string        // exact
	Harness string        // exact
	Event   string        // exact
	Grep    string        // case-insensitive substring of description or agent_type
	Since   time.Duration // zero means no time bound
	Limit   int           // zero means no limit
}

type Result struct {
	Records []record.Record
	// Skipped counts lines that were not valid JSON. Conflict markers from a
	// merge that lost its merge=union attribute look exactly like this, and
	// reporting the count is how that gets noticed instead of silently
	// shrinking the history.
	Skipped int
}

// ParseSince accepts d/w on top of Go's own duration units, because "3d" is
// what a person types and time.ParseDuration does not know about days.
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && strings.HasSuffix(s, "d") {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "w")); err == nil && strings.HasSuffix(s, "w") {
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func Query(agentsDir string, f Filter, now time.Time) (Result, error) {
	paths, err := filepath.Glob(filepath.Join(agentsDir, "reports", "traces", "*.jsonl"))
	if err != nil {
		return Result{}, err
	}
	sort.Strings(paths)

	var res Result
	var cutoff time.Time
	if f.Since > 0 {
		cutoff = now.Add(-f.Since)
	}

	for _, p := range paths {
		file, err := os.Open(p)
		if err != nil {
			return res, err
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var r record.Record
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				res.Skipped++
				continue
			}
			if matches(r, f, cutoff) {
				res.Records = append(res.Records, r)
			}
		}
		file.Close()
		if err := sc.Err(); err != nil {
			return res, err
		}
	}

	// Newest first: the question is almost always "what happened recently".
	sort.SliceStable(res.Records, func(i, j int) bool {
		return res.Records[i].When.After(res.Records[j].When)
	})
	if f.Limit > 0 && len(res.Records) > f.Limit {
		res.Records = res.Records[:f.Limit]
	}
	return res, nil
}

func matches(r record.Record, f Filter, cutoff time.Time) bool {
	if f.Lane != "" && r.Lane != f.Lane {
		return false
	}
	if f.Machine != "" && r.Machine != f.Machine {
		return false
	}
	if f.Harness != "" && r.Harness != f.Harness {
		return false
	}
	if f.Event != "" && r.Event != f.Event {
		return false
	}
	if !cutoff.IsZero() && r.When.Before(cutoff) {
		return false
	}
	if f.Module != "" && r.Cwd != f.Module && !strings.HasPrefix(r.Cwd, f.Module+"/") {
		return false
	}
	if f.Grep != "" {
		// Mechanical filters collapse the candidate set; choosing among the
		// survivors is semantic, and this is the honest limit of what the
		// record can offer. Codex does not populate description at all.
		hay := strings.ToLower(r.Description + " " + r.AgentType)
		if !strings.Contains(hay, strings.ToLower(f.Grep)) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Implement the command**

`agents/cmd_trace.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

func runTrace(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: agents trace ls|cache [flags]")
		return exitcode.Malformed
	}
	switch args[0] {
	case "ls":
		return runTraceLS(args[1:], stdout)
	case "cache":
		return runTraceCache(args[1:], stdout)
	default:
		fmt.Fprintf(stdout, "agents trace: unknown subcommand %q\n", args[0])
		return exitcode.Malformed
	}
}

func agentsDirHere(stdout io.Writer) (string, int) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents: %v\n", err)
		return "", exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents: not inside a git repository")
		return "", exitcode.Skip
	}
	return repo.AgentsDir(rc.Root), exitcode.OK
}

func runTraceLS(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace ls", flag.ContinueOnError)
	fs.SetOutput(stdout)
	var f trace.Filter
	fs.StringVar(&f.Lane, "lane", "", "exact lane")
	fs.StringVar(&f.Module, "module", "", "repo-relative path prefix")
	fs.StringVar(&f.Machine, "machine", "", "exact machine id")
	fs.StringVar(&f.Harness, "harness", "", "exact harness name")
	fs.StringVar(&f.Event, "event", "", "exact event name")
	fs.StringVar(&f.Grep, "grep", "", "substring of description or agent type")
	fs.IntVar(&f.Limit, "limit", 50, "maximum records (0 for all)")
	since := fs.String("since", "", "time window, e.g. 3d, 12h, 2w")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	d, err := trace.ParseSince(*since)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace ls: --since %q: %v\n", *since, err)
		return exitcode.Malformed
	}
	f.Since = d

	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}

	res, err := trace.Query(dir, f, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents trace ls: %v\n", err)
		return exitcode.NoRecord
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tHARNESS\tEVENT\tLANE\tCWD\tAGENT\tOK\tDESCRIPTION")
	for _, r := range res.Records {
		ok := "?"
		if r.PointerVerified {
			ok = "y"
		}
		agent := r.AgentType
		if agent == "" {
			agent = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.When.Format("2006-01-02 15:04"), r.Harness, r.Event, r.Lane, r.Cwd, agent, ok, r.Description)
	}
	tw.Flush()

	if res.Skipped > 0 {
		// Advisory rather than silent: unreadable lines mean the history is
		// smaller than it looks.
		fmt.Fprintf(stdout, "\n%d unreadable line(s) skipped — check for merge conflict markers in .agents/reports/traces/\n", res.Skipped)
		return exitcode.Advisory
	}
	return exitcode.OK
}
```

Register in `main.go`: `case "trace": return runTrace(args[1:], os.Stdout)`.

`runTraceCache` lands in Task 12; add a temporary stub so the package compiles:

```go
func runTraceCache(args []string, stdout io.Writer) int {
	fmt.Fprintln(stdout, "agents trace cache: not implemented")
	return exitcode.Skip
}
```

- [ ] **Step 5: Run tests and try it by hand**

Run: `cd ~/dotfiles/agents && go test ./... && make -C ~/dotfiles agents && cd ~/dotfiles && agents trace ls --limit 5`
Expected: tests PASS; the command prints a header and either rows or nothing (dotfiles is not initialized yet, so `not inside`/empty output is correct).

- [ ] **Step 6: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): trace query with mechanical filters"
```

---

### Task 12: `agents trace cache` — materialize reachable transcripts

**Files:**
- Create: `agents/internal/trace/cache.go`
- Create: `agents/internal/trace/cache_test.go`
- Modify: `agents/cmd_trace.go` (replace the `runTraceCache` stub)

**Interfaces:**
- Consumes: `trace.Query`, `machine.ID`.
- Produces: `trace.CacheReport{Copied, Skipped, Unreachable, Elsewhere int; Details []string}`; `trace.Cache(agentsDir, thisMachine string, recs []record.Record) (CacheReport, error)`.

**What this implements:** the Materialize operation from §1. Check (`doctor`) and Distil (spec 3) are elsewhere. The cache directory is git-ignored — `scaffold` already excludes `/.agents/.trace-cache/`.

- [ ] **Step 1: Write the failing test**

`agents/internal/trace/cache_test.go`:

```go
package trace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

func TestCacheCopiesOnlyReachableLocalTranscripts(t *testing.T) {
	agentsDir := t.TempDir()
	src := t.TempDir()

	here := filepath.Join(src, "rollout-here.jsonl")
	if err := os.WriteFile(here, []byte(`{"line":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	recs := []record.Record{
		{When: time.Now(), Machine: "m1", Harness: "codex", Transcript: here, PointerVerified: true},
		{When: time.Now(), Machine: "m1", Harness: "codex", Transcript: filepath.Join(src, "gone.jsonl"), PointerVerified: true},
		{When: time.Now(), Machine: "m2", Harness: "codex", Transcript: "/elsewhere/rollout.jsonl", PointerVerified: true},
	}

	rep, err := Cache(agentsDir, "m1", recs)
	if err != nil {
		t.Fatalf("Cache: %v", err)
	}
	if rep.Copied != 1 {
		t.Errorf("Copied = %d, want 1", rep.Copied)
	}
	if rep.Unreachable != 1 {
		t.Errorf("Unreachable = %d, want 1 (this machine, file gone)", rep.Unreachable)
	}
	if rep.Elsewhere != 1 {
		t.Errorf("Elsewhere = %d, want 1 (another machine holds it)", rep.Elsewhere)
	}

	dst := filepath.Join(agentsDir, ".trace-cache", "codex", "rollout-here.jsonl")
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected %s: %v", dst, err)
	}
	if string(b) != `{"line":1}`+"\n" {
		t.Errorf("cached content = %q", b)
	}
}

func TestCacheIsIdempotent(t *testing.T) {
	agentsDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "rollout-x.jsonl")
	if err := os.WriteFile(src, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs := []record.Record{{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}}

	if _, err := Cache(agentsDir, "m1", recs); err != nil {
		t.Fatal(err)
	}
	rep, err := Cache(agentsDir, "m1", recs)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 0 || rep.Skipped != 1 {
		t.Fatalf("second run: Copied=%d Skipped=%d, want 0 and 1", rep.Copied, rep.Skipped)
	}
}

// A record whose pointer never verified names a path that may belong to a
// different session. Copying it would put unrelated content under a name that
// implies it is related.
func TestCacheSkipsUnverifiedPointers(t *testing.T) {
	agentsDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "rollout-y.jsonl")
	if err := os.WriteFile(src, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 0 {
		t.Fatalf("Copied = %d, want 0 for an unverified pointer", rep.Copied)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/trace/ -run Cache -v`
Expected: FAIL — `undefined: Cache`.

- [ ] **Step 3: Implement**

`agents/internal/trace/cache.go`:

```go
package trace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

// CacheReport separates the two reasons a transcript is not in the cache,
// because they call for different actions: Unreachable means it was here and
// is gone, Elsewhere means go to that machine (or wait for `agents distill`).
type CacheReport struct {
	Copied      int
	Skipped     int // already cached
	Unreachable int // this machine, but the file is not there
	Elsewhere   int // another machine holds it
	Details     []string
}

// Cache copies transcripts that are reachable from here into a git-ignored
// directory inside .agents/. It never copies a transcript belonging to another
// machine, and never one whose pointer did not verify.
func Cache(agentsDir, thisMachine string, recs []record.Record) (CacheReport, error) {
	var rep CacheReport
	seen := map[string]bool{}

	for _, r := range recs {
		if r.Transcript == "" || !r.PointerVerified {
			continue
		}
		if seen[r.Transcript] {
			continue
		}
		seen[r.Transcript] = true

		if r.Machine != thisMachine {
			rep.Elsewhere++
			rep.Details = append(rep.Details,
				fmt.Sprintf("elsewhere (%s): %s", r.Machine, r.Transcript))
			continue
		}
		if _, err := os.Stat(r.Transcript); err != nil {
			rep.Unreachable++
			rep.Details = append(rep.Details, "unreachable: "+r.Transcript)
			continue
		}

		harnessName := r.Harness
		if harnessName == "" {
			harnessName = "unknown"
		}
		dst := filepath.Join(agentsDir, ".trace-cache", harnessName, filepath.Base(r.Transcript))
		if _, err := os.Stat(dst); err == nil {
			rep.Skipped++
			continue
		}
		if err := copyFile(r.Transcript, dst); err != nil {
			return rep, err
		}
		rep.Copied++
	}
	return rep, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Write to a temporary name and rename, so an interrupted copy never leaves
	// a truncated transcript behind under a name that says it is complete.
	tmp := dst + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
```

- [ ] **Step 4: Replace the command stub**

In `agents/cmd_trace.go`:

```go
func runTraceCache(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace cache", flag.ContinueOnError)
	fs.SetOutput(stdout)
	lane := fs.String("lane", "", "only this lane")
	since := fs.String("since", "30d", "time window, e.g. 3d, 12h, 2w")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	d, err := trace.ParseSince(*since)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: --since %q: %v\n", *since, err)
		return exitcode.Malformed
	}

	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}
	mid, err := machine.ID()
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}

	res, err := trace.Query(dir, trace.Filter{Lane: *lane, Since: d}, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}
	rep, err := trace.Cache(dir, mid, res.Records)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}

	fmt.Fprintf(stdout, "copied %d, already cached %d, unreachable here %d, on another machine %d\n",
		rep.Copied, rep.Skipped, rep.Unreachable, rep.Elsewhere)
	for _, d := range rep.Details {
		fmt.Fprintln(stdout, "  "+d)
	}
	if rep.Unreachable > 0 || rep.Elsewhere > 0 {
		return exitcode.Advisory
	}
	return exitcode.OK
}
```

Add `"github.com/nilbot/dotfiles/agents/internal/machine"` and `"github.com/nilbot/dotfiles/agents/internal/trace"` to the imports.

- [ ] **Step 5: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): materialize reachable transcripts into a git-ignored cache"
```

---

## Phase 4 — Memory, handoffs, and generated indexes

Ends with: the tracked content half of `.agents/` is writable and navigable, and every write that could produce drift regenerates its own index in the same operation (§ Drift).

### Task 13: Memory frontmatter and the generated memory index

**Files:**
- Create: `agents/internal/memory/memory.go`
- Create: `agents/internal/memory/memory_test.go`
- Create: `agents/cmd_index.go`
- Modify: `agents/go.mod`, `agents/go.sum` (adds `gopkg.in/yaml.v3`)
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `memory.Source{Kind, Machine, Harness, Ref, Note string}`; `memory.Frontmatter{Name, Description string; Type string; Sources []Source; Path string}`; `memory.Parse(path string) (Frontmatter, error)`; `memory.List(memoryDir string) ([]Frontmatter, error)`; `memory.RenderIndex([]Frontmatter) []byte`; `memory.WriteIndex(memoryDir string) error`; `runIndex(args []string, stdout io.Writer) int`.

- [ ] **Step 1: Add the dependency**

```bash
cd ~/dotfiles/agents && go get gopkg.in/yaml.v3@v3.0.1
```

This is the only external dependency the Global Constraints permit. Hand-rolling a YAML subset for frontmatter is the kind of shortcut that works until someone writes a multi-line `note:`.

- [ ] **Step 2: Write the failing tests**

`agents/internal/memory/memory_test.go`:

```go
package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const entry = `---
name: payments-retry-semantics
description: Why the retry window is 90s and not the documented 30s
metadata:
  type: reference
sources:
  - kind: transcript
    machine: m1-mbp-a7f3
    ref: 019fdcab-ac94-7502-a322-d01f047c274a
  - kind: harness-memory
    machine: m1-mbp-a7f3
    harness: codex
    note: "full derivation in Codex auto-memory; distil before relying on the numbers"
---

The window is 90s. See [[payments-timeouts]].
`

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseFrontmatterAndSources(t *testing.T) {
	fm, err := Parse(write(t, t.TempDir(), "retry.md", entry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fm.Name != "payments-retry-semantics" {
		t.Errorf("Name = %q", fm.Name)
	}
	if fm.Type != "reference" {
		t.Errorf("Type = %q, want reference", fm.Type)
	}
	if len(fm.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(fm.Sources))
	}
	if fm.Sources[0].Kind != "transcript" || fm.Sources[0].Ref == "" {
		t.Errorf("source 0 = %+v", fm.Sources[0])
	}
	if fm.Sources[1].Harness != "codex" || fm.Sources[1].Note == "" {
		t.Errorf("source 1 = %+v", fm.Sources[1])
	}
}

func TestParseRejectsMissingFrontmatter(t *testing.T) {
	if _, err := Parse(write(t, t.TempDir(), "bare.md", "just prose\n")); err == nil {
		t.Fatal("want an error: an entry with no frontmatter cannot be indexed")
	}
}

func TestParseRequiresNameAndDescription(t *testing.T) {
	body := "---\nname: only-a-name\n---\n\nbody\n"
	if _, err := Parse(write(t, t.TempDir(), "partial.md", body)); err == nil {
		t.Fatal("want an error when description is missing")
	}
}

func TestRenderIndexIsDeterministicAndGrouped(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "retry.md", entry)
	write(t, dir, "b.md", "---\nname: b-thing\ndescription: B\nmetadata:\n  type: project\n---\n\nx\n")
	write(t, dir, "a.md", "---\nname: a-thing\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	first := string(RenderIndex(entries))
	second := string(RenderIndex(entries))
	if first != second {
		t.Fatal("RenderIndex must be deterministic; the guard compares it byte for byte")
	}
	if !strings.Contains(first, "GENERATED") {
		t.Error("the index must say it is generated")
	}
	if strings.Index(first, "a-thing") > strings.Index(first, "b-thing") {
		t.Error("entries must be sorted by name within a type")
	}
	if !strings.Contains(first, "sources: 2") {
		t.Error("the index must surface that an entry depends on material outside the repo")
	}
}

// INDEX.md is generated from the entries; it must never be read as one.
func TestListIgnoresTheIndexItself(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")
	if err := WriteIndex(dir); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (INDEX.md must be excluded)", len(entries))
	}
}

func TestWriteIndexIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")
	if err := WriteIndex(dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if err := WriteIndex(dir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if string(first) != string(second) {
		t.Fatal("WriteIndex is not idempotent; the pre-commit guard would block on every commit")
	}
}

// A malformed entry must not silently vanish from the index -- that would make
// the index quietly wrong, which is worse than an error.
func TestListReportsUnparseableEntries(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")
	write(t, dir, "broken.md", "no frontmatter here\n")
	if _, err := List(dir); err == nil {
		t.Fatal("want an error naming the unparseable entry")
	} else if !strings.Contains(err.Error(), "broken.md") {
		t.Fatalf("error must name the file: %v", err)
	}
}
```

- [ ] **Step 3: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/memory/ -v`
Expected: FAIL — `undefined: Parse`.

- [ ] **Step 4: Implement**

`agents/internal/memory/memory.go`:

```go
// Package memory reads curated memory entries and generates their index.
//
// The index is generated rather than hand-maintained because a hand-maintained
// list of what is in a directory disagrees with the directory eventually, and a
// context document whose whole value is trustworthiness cannot afford that.
package memory

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source points at material that does not travel with the repository.
//
// The discipline that goes with it: a memory entry must never depend on its
// source being present in order to be correct. State the takeaway in the entry;
// the source is corroboration and a route to more detail.
type Source struct {
	Kind    string `yaml:"kind"`    // transcript | harness-memory | other
	Machine string `yaml:"machine"` // which computer holds it
	Harness string `yaml:"harness"` // for kind: harness-memory
	Ref     string `yaml:"ref"`     // agent_id, resolvable through the trace index
	Note    string `yaml:"note"`
}

type Frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Sources     []Source `yaml:"sources"`
	Metadata    struct {
		Type string `yaml:"type"`
	} `yaml:"metadata"`

	Type string `yaml:"-"` // flattened from Metadata.Type for convenience
	Path string `yaml:"-"` // basename, for linking from the index
}

const delim = "---"

func Parse(path string) (Frontmatter, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Frontmatter{}, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, delim+"\n") {
		return Frontmatter{}, fmt.Errorf("%s: no frontmatter", filepath.Base(path))
	}
	rest := text[len(delim)+1:]
	end := strings.Index(rest, "\n"+delim)
	if end < 0 {
		return Frontmatter{}, fmt.Errorf("%s: frontmatter is not closed", filepath.Base(path))
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return Frontmatter{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if fm.Name == "" || fm.Description == "" {
		return Frontmatter{}, fmt.Errorf("%s: name and description are both required", filepath.Base(path))
	}
	fm.Type = fm.Metadata.Type
	if fm.Type == "" {
		fm.Type = "uncategorized"
	}
	fm.Path = filepath.Base(path)
	return fm, nil
}

// List reads every entry in a memory directory.
//
// It fails on the first unparseable entry rather than skipping it: an entry
// that silently drops out of the index is invisible, and the index would then
// be confidently incomplete.
func List(dir string) ([]Frontmatter, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	var out []Frontmatter
	for _, p := range matches {
		if filepath.Base(p) == "INDEX.md" {
			continue
		}
		fm, err := Parse(p)
		if err != nil {
			return nil, err
		}
		out = append(out, fm)
	}
	return out, nil
}

// typeOrder fixes the section order so the output is stable regardless of what
// happens to be on disk.
var typeOrder = []string{"user", "project", "feedback", "reference", "uncategorized"}

func RenderIndex(entries []Frontmatter) []byte {
	byType := map[string][]Frontmatter{}
	for _, e := range entries {
		byType[e.Type] = append(byType[e.Type], e)
	}
	var extra []string
	for k := range byType {
		known := false
		for _, t := range typeOrder {
			if t == k {
				known = true
			}
		}
		if !known {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)

	var b bytes.Buffer
	b.WriteString("# Memory index\n\n")
	b.WriteString("GENERATED by `agents index`. Do not hand-edit — the pre-commit guard\n")
	b.WriteString("regenerates this file and blocks the commit if it differs.\n")

	for _, typ := range append(append([]string{}, typeOrder...), extra...) {
		group := byType[typ]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].Name < group[j].Name })

		fmt.Fprintf(&b, "\n## %s\n\n", typ)
		for _, e := range group {
			fmt.Fprintf(&b, "- [%s](%s) — %s", e.Name, e.Path, e.Description)
			if n := len(e.Sources); n > 0 {
				// Surfaced here because it is the one thing about an entry
				// that a reader on a different machine needs to know before
				// trusting the detail behind it.
				fmt.Fprintf(&b, " _(sources: %d)_", n)
			}
			b.WriteString("\n")
		}
	}
	return b.Bytes()
}

func WriteIndex(dir string) error {
	entries, err := List(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "INDEX.md"), RenderIndex(entries), 0o644)
}
```

- [ ] **Step 5: Implement `agents index`**

`agents/cmd_index.go` — `handoff.WriteIndex` arrives in Task 14; write this file then, or write it now and add the handoff line in Task 14. Writing it now with only the memory half:

```go
package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/memory"
)

// runIndex regenerates every generated file under .agents/.
//
// The normal path never needs it: every command that writes memory or a handoff
// regenerates the relevant index in the same operation. This exists for
// hand-edits and for out-of-band writes.
func runIndex(args []string, stdout io.Writer) int {
	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}
	if err := memory.WriteIndex(filepath.Join(dir, "memory")); err != nil {
		fmt.Fprintf(stdout, "agents index: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, "regenerated memory/INDEX.md")
	return exitcode.OK
}
```

Register in `main.go`: `case "index": return runIndex(args[1:], os.Stdout)`.

- [ ] **Step 6: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): memory frontmatter parsing and generated memory index"
```

---

### Task 14: Lane-scoped handoffs

**Files:**
- Create: `agents/internal/handoff/handoff.go`
- Create: `agents/internal/handoff/handoff_test.go`
- Create: `agents/cmd_handoff.go`
- Modify: `agents/cmd_index.go` (add the handoff index)
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `lane.Slugify`, `repo.Discover`.
- Produces: `handoff.Entry{Lane, Session, Status, Path string; When time.Time}`; `handoff.Write(agentsDir, laneName, session, status, body string, when time.Time) (string, error)`; `handoff.List(agentsDir string) ([]Entry, error)`; `handoff.Prune(agentsDir string, keep int) ([]string, error)`; `handoff.RenderIndex([]Entry) []byte`; `handoff.WriteIndex(agentsDir string) error`; `runHandoff(args []string, stdin io.Reader, stdout io.Writer) int`.

**Why one file per (lane, session)** (§4): two agents on the same branch never touch the same file, so they cannot clobber each other and git merges cleanly — a property no single-file scheme has, since markdown cannot `merge=union`.

**Provenance:** `reviewed` for a handoff written deliberately, `draft` for one auto-written at session end. The reader is told to weigh them differently. This reduces the "stale draft reads as authoritative" failure; it does not eliminate it.

- [ ] **Step 1: Write the failing tests**

`agents/internal/handoff/handoff_test.go`:

```go
package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritePlacesFilePerLaneAndSession(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	p, err := Write(dir, "sq-123-payments", "019fdcab", "reviewed", "Left off at the retry test.", when)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := filepath.Join(dir, "reports", "handoff", "sq-123-payments", "2026-08-10-019fdcab.md")
	if p != want {
		t.Fatalf("path = %q, want %q", p, want)
	}

	b, _ := os.ReadFile(p)
	for _, want := range []string{"lane: sq-123-payments", "status: reviewed", "session: 019fdcab", "Left off at the retry test."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file missing %q:\n%s", want, b)
		}
	}
}

// Two agents on one branch must not be able to clobber each other. Distinct
// sessions means distinct files, which is the entire reason for this scheme.
func TestConcurrentSessionsGetSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	a, _ := Write(dir, "same-lane", "sess-a", "draft", "A", when)
	b, _ := Write(dir, "same-lane", "sess-b", "draft", "B", when)
	if a == b {
		t.Fatal("two sessions collided on one path")
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s missing: %v", p, err)
		}
	}
}

func TestListAndRenderIndex(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	Write(dir, "lane-a", "s1", "reviewed", "x", base)
	Write(dir, "lane-a", "s2", "draft", "x", base.Add(24*time.Hour))
	Write(dir, "lane-b", "s3", "draft", "x", base.Add(48*time.Hour))

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	idx := string(RenderIndex(entries))
	if !strings.Contains(idx, "GENERATED") {
		t.Error("the index must say it is generated")
	}
	// Newest lane first: the reader wants what is live, not what is oldest.
	if strings.Index(idx, "lane-b") > strings.Index(idx, "lane-a") {
		t.Error("lanes must be ordered by most recent activity")
	}
	if !strings.Contains(idx, "draft") || !strings.Contains(idx, "reviewed") {
		t.Error("the index must carry provenance so a reader can weigh entries")
	}

	if string(RenderIndex(entries)) != idx {
		t.Error("RenderIndex must be deterministic")
	}
}

func TestPruneKeepsNewestPerLane(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		Write(dir, "busy", "s"+string(rune('a'+i)), "draft", "x", base.AddDate(0, 0, i))
	}
	Write(dir, "quiet", "s1", "draft", "x", base)

	removed, err := Prune(dir, 2)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 3 {
		t.Fatalf("removed %d, want 3", len(removed))
	}

	entries, _ := List(dir)
	byLane := map[string]int{}
	for _, e := range entries {
		byLane[e.Lane]++
	}
	if byLane["busy"] != 2 {
		t.Errorf("busy has %d, want 2", byLane["busy"])
	}
	if byLane["quiet"] != 1 {
		t.Errorf("quiet has %d, want 1 (prune is per lane)", byLane["quiet"])
	}
}

func TestWriteRegeneratesTheIndex(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, "lane-a", "s1", "draft", "x", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "reports", "handoff", "INDEX.md"))
	if err != nil {
		t.Fatalf("Write must regenerate the index in the same operation: %v", err)
	}
	if !strings.Contains(string(b), "lane-a") {
		t.Errorf("index does not list the entry just written:\n%s", b)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/handoff/ -v`
Expected: FAIL — `undefined: Write`.

- [ ] **Step 3: Implement**

`agents/internal/handoff/handoff.go`:

```go
// Package handoff manages lane-scoped notes about work in flight.
//
// One file per (lane, session), not one rolling file. A single rolling handoff
// assumes one person, one thread, one machine; it breaks with concurrent agents
// and with a repo where several tickets are in flight under no common story.
// Distinct files also merge cleanly, which markdown cannot do with merge=union.
package handoff

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	StatusReviewed = "reviewed" // written deliberately
	StatusDraft    = "draft"    // auto-written at session end
)

type Entry struct {
	Lane    string    `yaml:"lane"`
	Session string    `yaml:"session"`
	Status  string    `yaml:"status"`
	When    time.Time `yaml:"when"`
	Path    string    `yaml:"-"` // relative to reports/handoff
}

func root(agentsDir string) string { return filepath.Join(agentsDir, "reports", "handoff") }

// Write creates one handoff and regenerates the index in the same operation, so
// the normal path can never produce a stale index.
func Write(agentsDir, laneName, session, status, body string, when time.Time) (string, error) {
	if laneName == "" {
		laneName = "default"
	}
	if session == "" {
		session = "unknown"
	}
	if status != StatusReviewed {
		status = StatusDraft
	}

	dir := filepath.Join(root(agentsDir), laneName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.md", when.UTC().Format("2006-01-02"), session))

	var b bytes.Buffer
	b.WriteString("---\n")
	fmt.Fprintf(&b, "lane: %s\n", laneName)
	fmt.Fprintf(&b, "session: %s\n", session)
	fmt.Fprintf(&b, "status: %s\n", status)
	fmt.Fprintf(&b, "when: %s\n", when.UTC().Format(time.RFC3339))
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")

	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	return path, WriteIndex(agentsDir)
}

func List(agentsDir string) ([]Entry, error) {
	base := root(agentsDir)
	matches, err := filepath.Glob(filepath.Join(base, "*", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	var out []Entry
	for _, p := range matches {
		e, err := parse(p)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return nil, err
		}
		e.Path = filepath.ToSlash(rel)
		out = append(out, e)
	}
	return out, nil
}

func parse(path string) (Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Entry{}, fmt.Errorf("%s: no frontmatter", filepath.Base(path))
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Entry{}, fmt.Errorf("%s: frontmatter is not closed", filepath.Base(path))
	}
	var e Entry
	if err := yaml.Unmarshal([]byte(rest[:end]), &e); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if e.Lane == "" {
		e.Lane = filepath.Base(filepath.Dir(path))
	}
	return e, nil
}

// Prune bounds growth per lane, keeping the newest `keep` entries in each. It is
// per lane rather than global so a busy lane cannot evict a quiet one.
func Prune(agentsDir string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	entries, err := List(agentsDir)
	if err != nil {
		return nil, err
	}

	byLane := map[string][]Entry{}
	for _, e := range entries {
		byLane[e.Lane] = append(byLane[e.Lane], e)
	}

	var removed []string
	for _, group := range byLane {
		sort.Slice(group, func(i, j int) bool { return group[i].When.After(group[j].When) })
		for _, e := range group[min(keep, len(group)):] {
			p := filepath.Join(root(agentsDir), e.Path)
			if err := os.Remove(p); err != nil {
				return removed, err
			}
			removed = append(removed, e.Path)
		}
	}
	sort.Strings(removed)
	return removed, WriteIndex(agentsDir)
}

func RenderIndex(entries []Entry) []byte {
	byLane := map[string][]Entry{}
	for _, e := range entries {
		byLane[e.Lane] = append(byLane[e.Lane], e)
	}

	lanes := make([]string, 0, len(byLane))
	for l := range byLane {
		lanes = append(lanes, l)
		sort.Slice(byLane[l], func(i, j int) bool { return byLane[l][i].When.After(byLane[l][j].When) })
	}
	// Most recently active lane first: the reader wants what is live.
	sort.Slice(lanes, func(i, j int) bool {
		a, b := byLane[lanes[i]][0].When, byLane[lanes[j]][0].When
		if a.Equal(b) {
			return lanes[i] < lanes[j]
		}
		return a.After(b)
	})

	var b bytes.Buffer
	b.WriteString("# Handoff index\n\n")
	b.WriteString("GENERATED by `agents index`. Do not hand-edit — the pre-commit guard\n")
	b.WriteString("regenerates this file and blocks the commit if it differs.\n\n")
	b.WriteString("`reviewed` was written deliberately. `draft` was written automatically at\n")
	b.WriteString("session end and has not been checked by anyone. Weigh them differently.\n")

	for _, l := range lanes {
		fmt.Fprintf(&b, "\n## %s\n\n", l)
		fmt.Fprintln(&b, "| when | status | session | file |")
		fmt.Fprintln(&b, "|---|---|---|---|")
		for _, e := range byLane[l] {
			fmt.Fprintf(&b, "| %s | %s | %s | [%s](%s) |\n",
				e.When.UTC().Format("2006-01-02 15:04"), e.Status, e.Session,
				filepath.Base(e.Path), e.Path)
		}
	}
	return b.Bytes()
}

func WriteIndex(agentsDir string) error {
	entries, err := List(agentsDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root(agentsDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root(agentsDir), "INDEX.md"), RenderIndex(entries), 0o644)
}
```

- [ ] **Step 4: Implement the command**

`agents/cmd_handoff.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/lane"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func runHandoff(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: agents handoff write|prune [flags]")
		return exitcode.Malformed
	}
	switch args[0] {
	case "write":
		return runHandoffWrite(args[1:], stdin, stdout)
	case "prune":
		return runHandoffPrune(args[1:], stdout)
	default:
		fmt.Fprintf(stdout, "agents handoff: unknown subcommand %q\n", args[0])
		return exitcode.Malformed
	}
}

func runHandoffWrite(args []string, stdin io.Reader, stdout io.Writer) int {
	fs := flag.NewFlagSet("handoff write", flag.ContinueOnError)
	fs.SetOutput(stdout)
	laneFlag := fs.String("lane", "", "override lane resolution")
	session := fs.String("session", "", "session id (required)")
	draft := fs.Bool("draft", false, "mark as an unreviewed auto-draft")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if *session == "" {
		fmt.Fprintln(stdout, "agents handoff write: --session is required; it is what keeps concurrent agents from clobbering each other")
		return exitcode.Malformed
	}

	body, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.Malformed
	}
	if len(body) == 0 {
		fmt.Fprintln(stdout, "agents handoff write: refusing to write an empty handoff")
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents handoff write: not inside a git repository")
		return exitcode.Skip
	}

	status := handoff.StatusReviewed
	if *draft {
		status = handoff.StatusDraft
	}
	path, err := handoff.Write(repo.AgentsDir(rc.Root), lane.Resolve(*laneFlag, rc),
		*session, status, string(body), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, path)
	return exitcode.OK
}

func runHandoffPrune(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("handoff prune", flag.ContinueOnError)
	fs.SetOutput(stdout)
	keep := fs.Int("keep", 5, "handoffs to keep per lane")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}
	removed, err := handoff.Prune(dir, *keep)
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff prune: %v\n", err)
		return exitcode.NoRecord
	}
	for _, r := range removed {
		fmt.Fprintln(stdout, "removed "+r)
	}
	fmt.Fprintf(stdout, "kept the newest %d per lane\n", *keep)
	return exitcode.OK
}
```

Register in `main.go`: `case "handoff": return runHandoff(args[1:], os.Stdin, os.Stdout)`.

- [ ] **Step 5: Add the handoff index to `agents index`**

In `agents/cmd_index.go`, after the memory index:

```go
	if err := handoff.WriteIndex(dir); err != nil {
		fmt.Fprintf(stdout, "agents index: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, "regenerated reports/handoff/INDEX.md")
```

Add the `handoff` import.

- [ ] **Step 6: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): lane-scoped handoffs with generated index"
```

---

### Task 15: `agents save` — the scoped commit

**Files:**
- Create: `agents/cmd_save.go`
- Test: `agents/cmd_save_test.go`
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `repo.Discover`, `memory.WriteIndex`, `handoff.WriteIndex`.
- Produces: `runSave(args []string, stdout io.Writer) int`.

**Why it exists:** `git add -A` sweeping trace records into a code commit is a listed risk. `agents save` is the easy path that avoids it; the mixed-commit warning in Task 16 is what makes the habit unnecessary for correctness.

- [ ] **Step 1: Write the failing test**

`agents/cmd_save_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSaveCommitsOnlyAgentsPaths(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	// An unrelated change staged alongside: it must survive uncommitted.
	if err := os.WriteFile(filepath.Join(root, "code.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, root, "add", "code.go")

	memDir := filepath.Join(root, ".agents", "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n"
	if err := os.WriteFile(filepath.Join(memDir, "a.md"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != 0 {
		t.Fatalf("exit = %d; output:\n%s", code, out.String())
	}

	files := gitOut(t, root, "show", "--name-only", "--pretty=format:", "HEAD")
	if strings.Contains(files, "code.go") {
		t.Errorf("save swept in a code file:\n%s", files)
	}
	if !strings.Contains(files, ".agents/memory/a.md") {
		t.Errorf("save did not commit the memory entry:\n%s", files)
	}
	// The index is generated as part of the same operation, so it lands in the
	// same commit and the pre-commit guard has nothing to complain about.
	if !strings.Contains(files, ".agents/memory/INDEX.md") {
		t.Errorf("save did not regenerate and commit the index:\n%s", files)
	}
	if gitOut(t, root, "diff", "--cached", "--name-only") != "code.go" {
		t.Error("the unrelated staged change must still be staged afterwards")
	}
}

func TestSaveWithNothingToDo(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	if code := runSave(nil, &out); code != 4 {
		t.Fatalf("exit = %d, want 4 (skip) when there is nothing to save; output:\n%s", code, out.String())
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test . -run TestSave -v`
Expected: FAIL — `undefined: runSave`.

- [ ] **Step 3: Implement**

`agents/cmd_save.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/memory"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// runSave commits .agents/ and nothing else.
//
// The scoping is the point: `git add -A` in a repo that is accumulating trace
// records sweeps them into whatever commit happens to be next, and the record
// then arrives in someone's code review.
func runSave(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("save", flag.ContinueOnError)
	fs.SetOutput(stdout)
	msg := fs.String("m", "chore(agents): update agent context", "commit message")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents save: not inside a git repository")
		return exitcode.Skip
	}
	agentsDir := repo.AgentsDir(rc.Root)
	if fi, err := os.Stat(agentsDir); err != nil || !fi.IsDir() {
		fmt.Fprintln(stdout, "agents save: no .agents/ here; run `agents init` first")
		return exitcode.Skip
	}

	// Regenerate before staging so the generated files land in the same commit
	// as what they describe. Otherwise the pre-commit guard blocks on a
	// mismatch that this command itself created.
	if err := memory.WriteIndex(filepath.Join(agentsDir, "memory")); err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}
	if err := handoff.WriteIndex(agentsDir); err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}

	if out, err := git(rc.Root, "add", "--", ".agents"); err != nil {
		fmt.Fprintf(stdout, "agents save: git add: %v\n%s", err, out)
		return exitcode.NoRecord
	}

	staged, err := git(rc.Root, "diff", "--cached", "--name-only", "--", ".agents")
	if err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}
	if strings.TrimSpace(staged) == "" {
		fmt.Fprintln(stdout, "agents save: nothing to save")
		return exitcode.Skip
	}

	// Pathspec-scoped commit: anything else the user had staged stays staged.
	out, err := git(rc.Root, "commit", "-m", *msg, "--", ".agents")
	fmt.Fprint(stdout, out)
	if err != nil {
		return exitcode.Block
	}
	return exitcode.OK
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}
```

- [ ] **Step 4: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): scoped save that never sweeps code into an agents commit"
```

---

## Phase 5 — Guards and the Go-backed git hook subsystem

Ends with: every commit in every repo on this machine — new or pre-existing — passes through the binary, and the dotfiles hook subsystem's two divergent bash dispatchers are gone.

### Task 16: `agents guard --staged`

**Files:**
- Create: `agents/internal/guard/guard.go`
- Create: `agents/internal/guard/guard_test.go`
- Create: `agents/cmd_guard.go`
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `memory.RenderIndex/List`, `handoff.RenderIndex/List`, `git`.
- Produces: `guard.Finding{Path string; Line int; Rule string; Blocking bool; Detail string}`; `guard.Staged(repoRoot string) ([]Finding, error)`; `runGuard(args []string, stdout io.Writer) int`.

**Why the secret gate is here and not in a `PreToolUse` hook** (§7): commit is where "tracked" is actually decided. A `PreToolUse` guard sees only one harness's own tool calls — it misses subagents under another harness, the hooks' own writes, and anything done by hand. It is early warning and must never be described as the guarantee.

**This is the one command that fails closed.** Exit 2 stops the commit.

#### Secret detection is delegated to gitleaks, not hand-rolled

**Revised 2026-08-08.** An earlier draft of this task carried six hand-written regexes (AWS keys, PEM blocks, `Authorization:` headers, GitHub tokens, `sk-` API keys, Codex task blobs) and the spec's open questions conceded they would need "tuning against false positives." That concession is the tell: a bespoke pattern list is a maintenance treadmill against an adversary that changes quarterly, and every credential format this project does *not* know about is a silent miss. Detection is delegated.

**The tool: [gitleaks](https://github.com/gitleaks/gitleaks) 8.30.1**, available from Homebrew. Local, fast, no network, ~170 maintained rules, and `[extend] useDefault = true` lets us add domain rules as *configuration* rather than code.

**Integration: subprocess, not library.** `gitleaks` as a Go import declares **14 direct dependencies** — cobra, viper, lipgloss, zerolog, sprig, archives among them — into a module that currently has exactly one (`gopkg.in/yaml.v3`). That trades a small auditable binary for a large transitive tree and couples our build to their API. As a subprocess we keep the module clean, the user upgrades rules with `brew upgrade` without rebuilding `agents`, and gitleaks' own config mechanism carries our additions.

Use `gitleaks stdin`, one invocation per staged `.agents/` path, feeding it the **staged blob** (`git show :<path>`) rather than the working tree. That scopes detection precisely to what this guard is responsible for, needs no gitleaks understanding of the repository, and keeps file attribution for the finding report. Pass `--redact` so a matched secret never reaches a terminal or scrollback. Exit 1 from gitleaks means findings; treat anything else non-zero as a failure to scan.

**Rejected: trufflehog.** It *verifies* candidate secrets by calling the issuing service. In a pre-commit hook that means latency, a network dependency, and sending candidate credentials off-machine — precisely wrong for a gate whose whole purpose is that secrets do not leave.

**One domain rule we must add, as config:** the Fernet-style encrypted task blob (`gAAAAAB…`) observed in real Codex `PreToolUse` payloads on 2026-08-07. gitleaks has no rule for it because it is specific to agent tooling. It ships in our `.gitleaks.toml` under `[extend] useDefault = true`, not in Go.

**Missing-tool policy: fail closed, bounded.** If nothing under `.agents/` is staged, the scan has nothing to do and the guard returns without needing gitleaks at all. If `.agents/` content *is* staged and `gitleaks` is not on `PATH`, **block the commit** with an install instruction. A security gate that silently degrades to a weaker check is the exact failure this project keeps finding; bounding the block to "you are actually committing agent content" keeps it proportionate. `agents doctor` (Task 20) reports its absence before you hit it, and `gitleaks` becomes a declared dependency of the dotfiles bootstrap.

**The config ships with dotfiles, not per-repo.** Put it at `git/gitleaks.toml`, linked or passed by absolute path via `--config`. A per-repo `.gitleaks.toml` would be editable by the same agent whose output the guard exists to check, which defeats it. Contents: `[extend] useDefault = true`, plus the one `encrypted-task-blob` rule for `gAAAAAB[A-Za-z0-9_-]{24,}`.

**Verify the rule coverage; do not assume it.** Before writing test expectations, confirm empirically which of the sample secrets gitleaks' default rules actually catch — pipe each through `gitleaks stdin` and look. This project has repeatedly shipped conclusions drawn from an unverified search. An AWS key and a PEM block are near-certain; an `Authorization: Bearer` header is not, and if the default set misses something this guard must catch, that gap belongs in our `[extend]` block with the evidence recorded beside it.

- [ ] **Step 1: Write the failing tests**

`agents/internal/guard/guard_test.go`:

```go
package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoWith(t *testing.T, files map[string]string) string {
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
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}

func blocking(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Blocking {
			out = append(out, f)
		}
	}
	return out
}

func TestSecretScanBlocks(t *testing.T) {
	cases := map[string]string{
		"aws key":         "note\nAKIAIOSFODNN7EXAMPLE\n",
		"pem block":       "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n",
		"auth header":     "curl -H 'Authorization: Bearer abcdefghijklmnop'\n",
		"github token":    "ghp_0123456789012345678901234567890123456\n",
		"anthropic key":   "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAA\n",
		"codex task blob": "tool_input was gAAAAABmZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			root := repoWith(t, map[string]string{".agents/memory/x.md": content})
			found, err := Staged(root)
			if err != nil {
				t.Fatalf("Staged: %v", err)
			}
			b := blocking(found)
			if len(b) == 0 {
				t.Fatalf("no blocking finding for %s: %+v", name, found)
			}
			if b[0].Line == 0 {
				t.Error("a finding must name the line so it can be fixed")
			}
		})
	}
}

// The scan is scoped to .agents/. The rest of the repo is not this tool's
// business, and scanning it would make the guard a general secret scanner --
// a much bigger promise than this design makes.
func TestSecretScanIgnoresNonAgentsPaths(t *testing.T) {
	root := repoWith(t, map[string]string{"src/config.go": "const k = \"AKIAIOSFODNN7EXAMPLE\"\n"})
	found, err := Staged(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocking(found)) != 0 {
		t.Fatalf("guard reached outside .agents/: %+v", found)
	}
}

func TestGeneratedIndexMismatchBlocks(t *testing.T) {
	root := repoWith(t, map[string]string{
		".agents/memory/a.md":     "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n",
		".agents/memory/INDEX.md": "# Memory index\n\nhand-written nonsense\n",
	})
	found, err := Staged(root)
	if err != nil {
		t.Fatal(err)
	}
	var hit bool
	for _, f := range blocking(found) {
		if f.Rule == "generated-file" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("a hand-edited INDEX.md must block: %+v", found)
	}
}

func TestGeneratedIndexMatchPasses(t *testing.T) {
	root := repoWith(t, map[string]string{
		".agents/memory/a.md": "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n",
	})
	// Regenerate exactly as `agents index` would, then stage it.
	if err := regenerateAndStage(t, root); err != nil {
		t.Fatal(err)
	}
	found, err := Staged(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range blocking(found) {
		if f.Rule == "generated-file" {
			t.Fatalf("a correctly generated index must not block: %+v", f)
		}
	}
}

func TestMixedCommitIsAdvisoryNotBlocking(t *testing.T) {
	root := repoWith(t, map[string]string{
		".agents/memory/a.md": "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n",
		"src/main.go":         "package main\n",
	})
	if err := regenerateAndStage(t, root); err != nil {
		t.Fatal(err)
	}
	found, err := Staged(root)
	if err != nil {
		t.Fatal(err)
	}
	var mixed *Finding
	for i, f := range found {
		if f.Rule == "mixed-commit" {
			mixed = &found[i]
		}
	}
	if mixed == nil {
		t.Fatalf("want a mixed-commit finding: %+v", found)
	}
	if mixed.Blocking {
		t.Error("mixed-commit is advisory; `agents save` is a habit, not a requirement")
	}
}

func TestCleanCommitProducesNothing(t *testing.T) {
	root := repoWith(t, map[string]string{"src/main.go": "package main\n"})
	found, err := Staged(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("a plain code commit must be silent: %+v", found)
	}
}
```

Add this helper at the bottom of the test file:

```go
func regenerateAndStage(t *testing.T, root string) error {
	t.Helper()
	if err := memoryWriteIndex(filepath.Join(root, ".agents", "memory")); err != nil {
		return err
	}
	if err := handoffWriteIndex(filepath.Join(root, ".agents")); err != nil {
		return err
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}
	return nil
}
```

The test file needs `"fmt"` in its imports and does not need `"strings"` unless a test above uses it — run `gofmt -l` and `go vet` after writing it.

and in `guard.go` expose the two package-level indirections the test uses (`var memoryWriteIndex = memory.WriteIndex`, `var handoffWriteIndex = handoff.WriteIndex`) so the test regenerates through exactly the same code path the guard compares against.

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/guard/ -v`
Expected: FAIL — `undefined: Staged`.

- [ ] **Step 3: Implement**

`agents/internal/guard/guard.go`:

```go
// Package guard is the check that runs at the commit boundary.
//
// It is the authoritative layer of spec 7 because commit is where "tracked" is
// actually decided. A PreToolUse hook in one harness sees only that harness's
// own tool calls; it misses subagents under another harness, the hooks' own
// writes, and anything done by hand.
//
// This is the one command in the tool that fails closed.
package guard

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/memory"
)

// Indirected so tests regenerate through the same path the guard compares to.
var (
	memoryWriteIndex  = memory.WriteIndex
	handoffWriteIndex = handoff.WriteIndex
)

type Finding struct {
	Path     string
	Line     int
	Rule     string
	Blocking bool
	Detail   string
}

// Secret detection is delegated to gitleaks. See the task's "Secret detection is
// delegated to gitleaks, not hand-rolled" section for why, and for the rejected
// alternatives (importing it as a library; trufflehog).
//
// This file owns three things only: deciding WHICH staged blobs are in scope,
// feeding them to the scanner, and turning its output into Findings. It owns no
// patterns. The one domain rule this project needs -- the Fernet-style encrypted
// task blob observed in real Codex payloads on 2026-08-07 -- lives in the
// shipped .gitleaks.toml under [extend], as configuration.
const (
	scannerBin  = "gitleaks"
	agentsPrefix = ".agents/"
)

// scanStaged runs the scanner over one staged blob and returns its findings.
//
// It feeds the blob on stdin rather than pointing the scanner at the working
// tree: the guard is responsible for what the COMMIT will contain, and the
// working tree can differ from the index. --redact keeps a matched secret out of
// the terminal and out of scrollback.
//
// Exit 0 means clean, exit 1 means findings. Any other outcome -- including the
// binary being absent -- is a failure to scan, never a pass. Callers must
// surface that as blocking; a security gate that quietly degrades to a weaker
// check is the failure this guard exists to prevent.
func scanStaged(repoRoot, path string, blob []byte, configPath string) ([]Finding, error)

const agentsPrefix = ".agents/"

func Staged(repoRoot string) ([]Finding, error) {
	staged, err := stagedPaths(repoRoot)
	if err != nil {
		return nil, err
	}

	var agentsPaths, otherPaths []string
	for _, p := range staged {
		if strings.HasPrefix(p, agentsPrefix) {
			agentsPaths = append(agentsPaths, p)
		} else {
			otherPaths = append(otherPaths, p)
		}
	}

	var findings []Finding
	for _, p := range agentsPaths {
		content, err := stagedContent(repoRoot, p)
		if err != nil {
			// A staged path whose blob cannot be read is not something to wave
			// through: it is exactly the case where "we could not check" must
			// not be reported as "it is fine".
			findings = append(findings, Finding{
				Path: p, Rule: "unreadable", Blocking: true,
				Detail: fmt.Sprintf("cannot read staged content: %v", err),
			})
			continue
		}
		findings = append(findings, scanSecrets(p, content)...)
	}

	gen, err := checkGenerated(repoRoot, agentsPaths)
	if err != nil {
		return nil, err
	}
	findings = append(findings, gen...)

	if len(agentsPaths) > 0 && len(otherPaths) > 0 {
		findings = append(findings, Finding{
			Rule: "mixed-commit", Blocking: false,
			Detail: fmt.Sprintf("this commit touches %d agent path(s) and %d other path(s); "+
				"`agents save` commits .agents/ on its own", len(agentsPaths), len(otherPaths)),
		})
	}
	return findings, nil
}

// scanSecrets turns one blob into Findings by delegating to the scanner.
//
// Implement it over `gitleaks stdin --redact --report-format json`, mapping each
// reported finding to a blocking Finding carrying the scanner's RuleID and
// StartLine. The matched value is never echoed even though --redact already
// masks it -- defence in depth costs nothing here, and printing a secret to a
// terminal and its scrollback is not a remedy.
//
// The scanner not being installed is NOT a clean result. Return an error the
// caller renders as blocking, with the install instruction.
func scanSecrets(path string, content []byte, configPath string) ([]Finding, error)

// checkGenerated regenerates the indexes and compares byte for byte against
// what is staged.
//
// This is the backstop, not the mechanism: every command that writes memory or
// a handoff regenerates the relevant index in the same operation, so the normal
// path never reaches here with a mismatch. It exists for hand-edits and
// out-of-band writes.
//
// Known false positive: staging an index while leaving an entry's edit
// unstaged. The comparison is against the working tree, so it reports the
// inconsistency. The remedy is `agents save`, which stages both.
func checkGenerated(repoRoot string, agentsPaths []string) ([]Finding, error) {
	agentsDir := filepath.Join(repoRoot, ".agents")
	targets := []struct {
		trigger  string // any staged path under here makes the index relevant
		index    string // repo-relative path of the generated file
		generate func() error
	}{
		{
			trigger:  ".agents/memory/",
			index:    ".agents/memory/INDEX.md",
			generate: func() error { return memoryWriteIndex(filepath.Join(agentsDir, "memory")) },
		},
		{
			trigger:  ".agents/reports/handoff/",
			index:    ".agents/reports/handoff/INDEX.md",
			generate: func() error { return handoffWriteIndex(agentsDir) },
		},
	}

	var out []Finding
	for _, t := range targets {
		relevant := false
		for _, p := range agentsPaths {
			if strings.HasPrefix(p, t.trigger) {
				relevant = true
				break
			}
		}
		if !relevant {
			continue
		}

		want, err := regenerated(repoRoot, t.index, t.generate)
		if err != nil {
			return nil, err
		}
		got, err := stagedContent(repoRoot, t.index)
		if err != nil {
			out = append(out, Finding{
				Path: t.index, Rule: "generated-file", Blocking: true,
				Detail: "an entry changed but the generated index is not staged; run `agents index` and stage it, or use `agents save`",
			})
			continue
		}
		if !bytes.Equal(want, got) {
			out = append(out, Finding{
				Path: t.index, Rule: "generated-file", Blocking: true,
				Detail: "staged content differs from what `agents index` produces; do not hand-edit generated files",
			})
		}
	}
	return out, nil
}

// regenerated produces the current correct content of a generated file without
// leaving the regenerated version behind on disk.
func regenerated(repoRoot, indexRel string, generate func() error) ([]byte, error) {
	abs := filepath.Join(repoRoot, indexRel)
	before, readErr := os.ReadFile(abs)
	if err := generate(); err != nil {
		return nil, err
	}
	after, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if readErr == nil {
		if err := os.WriteFile(abs, before, 0o644); err != nil {
			return nil, err
		}
	} else {
		os.Remove(abs)
	}
	return after, nil
}

func stagedPaths(repoRoot string) ([]string, error) {
	out, err := git(repoRoot, "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	return paths, nil
}

// stagedContent reads what is actually in the index, not what is in the working
// tree. They differ, and the index is what the commit will contain.
func stagedContent(repoRoot, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", ":"+path)
	cmd.Dir = repoRoot
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}
```

- [ ] **Step 4: Implement the command**

`agents/cmd_guard.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/guard"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func runGuard(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	fs.SetOutput(stdout)
	staged := fs.Bool("staged", false, "check what is staged for commit")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if !*staged {
		fmt.Fprintln(stdout, "usage: agents guard --staged")
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents guard: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		return exitcode.Skip
	}

	findings, err := guard.Staged(rc.Root)
	if err != nil {
		// The guard failing is not the same as the guard passing. Block rather
		// than wave a commit through on the strength of a check that did not run.
		fmt.Fprintf(stdout, "agents guard: could not complete: %v\n", err)
		return exitcode.Block
	}

	blocked := false
	for _, f := range findings {
		label := "warning"
		if f.Blocking {
			label = "BLOCKED"
			blocked = true
		}
		if f.Path == "" {
			fmt.Fprintf(stdout, "%s [%s] %s\n", label, f.Rule, f.Detail)
			continue
		}
		fmt.Fprintf(stdout, "%s %s:%d [%s] %s\n", label, f.Path, f.Line, f.Rule, f.Detail)
	}

	switch {
	case blocked:
		return exitcode.Block
	case len(findings) > 0:
		return exitcode.Advisory
	default:
		return exitcode.OK
	}
}
```

Register in `main.go`: `case "guard": return runGuard(args[1:], os.Stdout)`.

- [ ] **Step 5: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): staged-content guard with secret, generated-file and mixed-commit checks"
```

---

### Task 17: The git hook multicall dispatcher and its chain

**Files:**
- Create: `agents/internal/githook/githook.go`
- Create: `agents/internal/githook/githook_test.go`
- Modify: `agents/main.go` (multicall check before the subcommand table)

**Interfaces:**
- Consumes: `guard.Staged`.
- Produces: `githook.IsHookName(string) bool`; `githook.Chain{RepoHooksDir, ExtrasDir string}`; `githook.Run(c Chain, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int`; `githook.StripFooters([]byte) []byte`.

**The measured facts this implements** (§8.2, §8.3): a dispatcher symlinked under three hook names fired correctly as each and received git's arguments (`commit-msg` got `.git/COMMIT_EDITMSG`). And `core.hooksPath` **shadows** `.git/hooks/` — a repo-local `pre-commit` ran on a normal commit and was silently skipped once `core.hooksPath` was set. Chaining is therefore mandatory, not a nicety.

**Environment note:** the spec's caution about not passing an inherited environment unfiltered does not apply to this chain. Git sets `GIT_*` variables that chained hooks legitimately need, and stripping them would break correct hooks. The contamination problem was about *harness identity inference*, which `--harness` already solved.

- [ ] **Step 1: Write the failing tests**

`agents/internal/githook/githook_test.go`:

```go
package githook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func script(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestIsHookName(t *testing.T) {
	for _, n := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		if !IsHookName(n) {
			t.Errorf("IsHookName(%q) = false", n)
		}
	}
	for _, n := range []string{"agents", "init", "wire", ""} {
		if IsHookName(n) {
			t.Errorf("IsHookName(%q) = true; a subcommand is not a hook", n)
		}
	}
}

// core.hooksPath shadows .git/hooks/. Running the repo's own hook first is what
// keeps every repo that already had one working.
func TestChainRunsRepoHookFirst(t *testing.T) {
	repoHooks := t.TempDir()
	extras := t.TempDir()
	out := filepath.Join(t.TempDir(), "order.txt")

	script(t, filepath.Join(repoHooks, "pre-commit"), "echo repo >> "+out)
	script(t, filepath.Join(extras, "z.pre-commit"), "echo extra-z >> "+out)
	script(t, filepath.Join(extras, "a.pre-commit"), "echo extra-a >> "+out)

	var stdout, stderr bytes.Buffer
	code := Run(Chain{RepoHooksDir: repoHooks, ExtrasDir: extras, SkipBuiltin: true},
		"pre-commit", nil, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, stderr.String())
	}

	b, _ := os.ReadFile(out)
	if got := strings.Fields(string(b)); len(got) != 3 ||
		got[0] != "repo" || got[1] != "extra-a" || got[2] != "extra-z" {
		t.Fatalf("order = %v, want [repo extra-a extra-z]", got)
	}
}

// The 2021 dispatcher passed no arguments, so recent.post-checkout lost git's
// three positional args. Forwarding verbatim is the fix.
func TestChainForwardsArgumentsVerbatim(t *testing.T) {
	extras := t.TempDir()
	out := filepath.Join(t.TempDir(), "args.txt")
	script(t, filepath.Join(extras, "a.commit-msg"), `printf '%s\n' "$@" > `+out)

	var stdout, stderr bytes.Buffer
	Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "commit-msg",
		[]string{".git/COMMIT_EDITMSG", "message", "HEAD"}, strings.NewReader(""), &stdout, &stderr)

	b, _ := os.ReadFile(out)
	if got := strings.Fields(string(b)); len(got) != 3 || got[0] != ".git/COMMIT_EDITMSG" {
		t.Fatalf("args = %v", got)
	}
}

func TestChainStopsAndPropagatesOnFailure(t *testing.T) {
	extras := t.TempDir()
	out := filepath.Join(t.TempDir(), "ran.txt")
	script(t, filepath.Join(extras, "a.pre-commit"), "echo a >> "+out+"; exit 3")
	script(t, filepath.Join(extras, "b.pre-commit"), "echo b >> "+out)

	var stdout, stderr bytes.Buffer
	code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "pre-commit", nil,
		strings.NewReader(""), &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code = %d, want the failing hook's own 3", code)
	}
	b, _ := os.ReadFile(out)
	if strings.Contains(string(b), "b") {
		t.Fatal("the chain continued past a failure")
	}
}

func TestChainWithNothingToRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(Chain{RepoHooksDir: "/nonexistent", ExtrasDir: "/also-nonexistent", SkipBuiltin: true},
		"post-merge", nil, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0 when there is nothing to run", code)
	}
}

// A non-executable file in the extras directory must be skipped, not exec'd --
// the 2021 dispatcher would hard-fail on it.
func TestChainSkipsNonExecutableExtras(t *testing.T) {
	extras := t.TempDir()
	if err := os.WriteFile(filepath.Join(extras, "notes.pre-commit"), []byte("just a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "pre-commit", nil,
		strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}

func TestStripFooters(t *testing.T) {
	in := []byte(`feat: add a thing

Body text stays.

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude <noreply@anthropic.com>
`)
	got := string(StripFooters(in))
	for _, bad := range []string{"Generated with", "Co-Authored-By: Claude"} {
		if strings.Contains(got, bad) {
			t.Errorf("footer survived: %q\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "Body text stays.") {
		t.Errorf("body was damaged:\n%s", got)
	}
	if strings.HasSuffix(got, "\n\n\n") {
		t.Errorf("stripping left a run of blank lines:\n%q", got)
	}
}

func TestStripFootersIsCaseInsensitive(t *testing.T) {
	in := []byte("fix: x\n\nco-authored-by: Claude <noreply@anthropic.com>\n")
	if strings.Contains(strings.ToLower(string(StripFooters(in))), "co-authored-by") {
		t.Error("the trailer must be stripped regardless of case")
	}
}

func TestStripFootersLeavesOtherCoauthorsAlone(t *testing.T) {
	in := []byte("fix: x\n\nCo-Authored-By: A Person <a@example.com>\n")
	if !strings.Contains(string(StripFooters(in)), "A Person") {
		t.Error("only the Claude trailer is stripped; a real co-author is a real co-author")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/githook/ -v`
Expected: FAIL — `undefined: Run`.

- [ ] **Step 3: Implement**

`agents/internal/githook/githook.go`:

```go
// Package githook is the git-hook side of the binary: one executable
// symlinked under several hook names, dispatching on basename(argv[0]).
//
// It exists because core.hooksPath -- the only mechanism that covers repos that
// already exist, with no per-repo install and no backfill script -- shadows
// .git/hooks/ entirely. A repo-local hook is silently skipped once it is set.
// So this dispatcher chains rather than replaces.
package githook

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// hookNames is the set of git hook names this binary answers to. Anything else
// as argv[0] is a normal CLI invocation.
var hookNames = map[string]bool{
	"applypatch-msg": true, "pre-applypatch": true, "post-applypatch": true,
	"pre-commit": true, "pre-merge-commit": true, "prepare-commit-msg": true,
	"commit-msg": true, "post-commit": true, "pre-rebase": true,
	"post-checkout": true, "post-merge": true, "pre-push": true,
	"post-rewrite": true, "pre-auto-gc": true,
}

func IsHookName(s string) bool { return hookNames[s] }

type Chain struct {
	RepoHooksDir string // <repo>/.git/hooks
	ExtrasDir    string // ~/dotfiles/git/hooks
	SkipBuiltin  bool   // tests only
}

// Run executes the chain for one hook, in order:
//
//	1. the repo's own .git/hooks/<name>
//	2. ~/dotfiles/git/hooks/*.<name>, sorted
//	3. this binary's built-in behaviour
//
// Any non-zero exit stops the chain and propagates. Arguments are forwarded
// verbatim to every stage -- the bug the 2021 bash dispatcher has today, where
// recent.post-checkout loses git's three positional arguments.
func Run(c Chain, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	for _, stage := range stages(c, name) {
		if code := exec1(stage, args, stdin, stdout, stderr); code != 0 {
			fmt.Fprintf(stderr, "%s: %s exited %d\n", name, stage, code)
			return code
		}
	}
	if c.SkipBuiltin {
		return 0
	}
	return builtin(name, args, stdout, stderr)
}

func stages(c Chain, name string) []string {
	var out []string
	if c.RepoHooksDir != "" {
		p := filepath.Join(c.RepoHooksDir, name)
		if executable(p) {
			out = append(out, p)
		}
	}
	if c.ExtrasDir != "" {
		// The *.<hook-type> glob is today's convention in ~/dotfiles/git/hooks;
		// it is preserved so adding a personal hook stays a one-file change.
		matches, _ := filepath.Glob(filepath.Join(c.ExtrasDir, "*."+name))
		sort.Strings(matches)
		for _, m := range matches {
			if executable(m) {
				out = append(out, m)
			}
		}
	}
	return out
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func exec1(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := exec.Command(path, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// The environment passes through unfiltered on purpose: git sets GIT_*
	// variables that a chained hook legitimately needs, and stripping them
	// would break correct hooks. The contamination hazard in the spec was about
	// inferring harness identity, which the --harness flag already settles.
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return 1
	}
	return 0
}

// builtin is this binary's own contribution to a hook, after everything else in
// the chain has had its turn.
func builtin(name string, args []string, stdout, stderr io.Writer) int {
	switch name {
	case "commit-msg":
		if len(args) == 0 {
			return 0
		}
		return stripFootersInFile(args[0], stderr)
	default:
		// pre-commit's built-in is agents guard, wired in main.go where the
		// command table lives.
		return 0
	}
}

var footerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*co-authored-by:\s*claude\b`),
	regexp.MustCompile(`(?i)^\s*🤖?\s*generated with \[claude code\]`),
}

// StripFooters removes AI attribution trailers from a commit message.
//
// The rule comes from ~/.claude/CLAUDE.md: commits use standard, clean messages
// without AI attribution. Making it mechanical here means it holds even when an
// agent forgets, which is the only way a rule like this actually holds.
func StripFooters(msg []byte) []byte {
	var kept []string
	for _, line := range strings.Split(string(msg), "\n") {
		drop := false
		for _, re := range footerPatterns {
			if re.MatchString(line) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}

	out := strings.Join(kept, "\n")
	// Collapse the blank-line runs the removal left behind, then restore the
	// single trailing newline git expects.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return []byte(strings.TrimRight(out, "\n") + "\n")
}

func stripFootersInFile(path string, stderr io.Writer) int {
	b, err := os.ReadFile(path)
	if err != nil {
		// A commit-msg hook that cannot read the message must not block the
		// commit over it.
		fmt.Fprintf(stderr, "commit-msg: %v\n", err)
		return 0
	}
	out := StripFooters(b)
	if bytes.Equal(b, out) {
		return 0
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintf(stderr, "commit-msg: %v\n", err)
		return 0
	}
	return 0
}
```

- [ ] **Step 4: Wire the multicall check into `main.go`**

`main` must check `argv[0]` **before** the subcommand table, because git invokes the binary with no arguments at all for `pre-commit`:

```go
func main() {
	// Multicall dispatch: git invokes us through a symlink named for the hook,
	// so the name we were called by is the command. This must come before the
	// subcommand table -- `pre-commit` arrives with no arguments.
	if name := filepath.Base(os.Args[0]); githook.IsHookName(name) {
		os.Exit(runGitHook(name, os.Args[1:]))
	}
	os.Exit(run(os.Args[1:]))
}

func runGitHook(name string, args []string) int {
	c := githook.Chain{ExtrasDir: filepath.Join(os.Getenv("HOME"), "dotfiles", "git", "hooks")}
	if cwd, err := os.Getwd(); err == nil {
		if rc, err := repo.Discover(cwd); err == nil {
			c.RepoHooksDir = filepath.Join(rc.Root, ".git", "hooks")
		}
	}

	if code := githook.Run(c, name, args, os.Stdin, os.Stdout, os.Stderr); code != 0 {
		return code
	}
	if name == "pre-commit" {
		// The guard is the built-in stage of pre-commit. It runs last so a
		// repo's own hook and any personal extras get their say first.
		return runGuard([]string{"--staged"}, os.Stdout)
	}
	return 0
}
```

`githook.Run` already ran the `commit-msg` built-in; `SkipBuiltin` stays false here.

Add `path/filepath` and the `githook` and `repo` imports to `main.go`.

- [ ] **Step 5: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 6: Prove multicall dispatch works for real**

```bash
make -C ~/dotfiles agents
mkdir -p /tmp/hookchain && cd /tmp/hookchain && git init -b main
mkdir -p hooks.d && ln -sfn ~/bin/agents hooks.d/commit-msg && ln -sfn ~/bin/agents hooks.d/pre-commit
git config core.hooksPath /tmp/hookchain/hooks.d
git config user.email t@example.com && git config user.name T && git config commit.gpgsign false
printf '#!/bin/bash\necho "repo pre-commit ran"\n' > .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
echo hi > f.txt && git add f.txt
git commit -m "$(printf 'test: multicall\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n')"
git log -1 --format=%B
```

Expected: `repo pre-commit ran` appears (the chain did not let `core.hooksPath` shadow it), the commit succeeds, and the logged message contains no `Co-Authored-By` line.

```bash
rm -rf /tmp/hookchain
```

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): multicall git hook dispatcher that chains instead of shadowing"
```

---

### Task 18: Install the hook subsystem and retire the bash one

**Files:**
- Create: `git/hooks.d/.gitignore`
- Create: `git/gitattributes`
- Modify: `Makefile` (add `githooks`, extend `.PHONY` and `dotfiles`)
- Modify: `git/gitconfig.symlink:256-257` (remove `[init] templatedir`)
- Delete: `git/templates/hooks/commit-msg`, `post-checkout`, `post-merge`, `pre-commit`, `run-hooks.sh`

**Interfaces:**
- Consumes: `~/bin/agents` from Task 8.
- Produces: `core.hooksPath` pointing at `~/dotfiles/git/hooks.d`; `~/.gitattributes`.

**Why `init.templatedir` must go, not merely may:** the chain runs `.git/hooks/<name>` as stage 1 and `~/dotfiles/git/hooks/*.<name>` as stage 2. A templated `run-hooks.sh` shim sitting in `.git/hooks/` also globs `~/dotfiles/git/hooks/*.<name>`. Leaving both in place would run every personal hook **twice** in any repo cloned after this lands.

**Why `git/hooks.d/` contents are ignored rather than tracked:** the symlinks point at `/Users/nilbot/bin/agents`, an absolute machine-specific path, and this repo is public. A self-ignoring directory keeps the location the spec specifies without publishing a path that is wrong everywhere else.

- [ ] **Step 1: Create the self-ignoring hooks directory**

`git/hooks.d/.gitignore`:

```gitignore
# The symlinks in this directory point at an absolute path on one machine
# (~/bin/agents). `make githooks` creates them. Only this file is tracked.
*
!.gitignore
```

- [ ] **Step 2: Add the global gitattributes file**

`core.attributesfile = ~/.gitattributes` is already set in `git/gitconfig.symlink:104` and currently points at a file that does not exist. Create `git/gitattributes`:

```gitattributes
# Global git attributes, linked to ~/.gitattributes by `make dotfiles`.
#
# core.attributesfile in git/gitconfig.symlink has pointed here all along; the
# file simply did not exist. Per-repo .gitattributes (written by `agents init`)
# takes precedence over anything here.

# Trace indexes are append-only. Without merge=union, two branches appending on
# the same day produce conflict markers that are not valid JSON, and a
# line-oriented reader silently drops those lines.
.agents/reports/traces/*.jsonl merge=union
```

- [ ] **Step 3: Add the Makefile targets**

Extend `.PHONY` on line 1 with `githooks`, and add:

```make
# Global git hooks, for every repo on this machine -- new or pre-existing.
# core.hooksPath needs no per-repo installation, which is what retires both
# init.templatedir and claude/update-repo-hooks.sh.
githooks: agents
	mkdir -p $(CURDIR)/git/hooks.d
	@for h in pre-commit commit-msg post-merge post-checkout; do \
		ln -sfn $(HOME)/bin/agents $(CURDIR)/git/hooks.d/$$h; \
		echo "  git/hooks.d/$$h -> $(HOME)/bin/agents"; \
	done
	git config --global core.hooksPath $(CURDIR)/git/hooks.d
	@echo "core.hooksPath = $$(git config --global core.hooksPath)"
```

and add the gitattributes link to the `dotfiles` target, next to the `.gitignore` line at `Makefile:24`:

```make
	ln -sf $(CURDIR)/git/gitattributes $(HOME)/.gitattributes;
```

Also add `githooks` to the `links` target so a fresh machine gets it:

```make
links: bins dotfiles githooks
```

- [ ] **Step 4: Remove `init.templatedir` and the bash dispatchers**

Delete these two lines from the end of `git/gitconfig.symlink`:

```
[init]
	templatedir = ~/dotfiles/git/templates
```

Then:

```bash
cd ~/dotfiles && git rm -r git/templates/
```

`git/hooks/*.{pre-commit,commit-msg,post-checkout,post-merge}` all stay — they are stage 2 of the chain and now finally receive their arguments.

- [ ] **Step 5: Check for the one thing this could break**

`git config --global core.hooksPath` is global; a repo that sets it **locally** (husky and similar) overrides it, and these hooks do not run there at all. That is correct git behaviour and is not fought. Find out now whether any repo on this machine does:

```bash
for d in ~/devel/*/*/.git ~/dotfiles/.git; do
  [ -d "$d" ] || continue
  v=$(git --git-dir="$d" config --local core.hooksPath 2>/dev/null) && echo "LOCAL OVERRIDE: $d -> $v"
done; echo done
```

Any repo listed will not get these hooks. Task 20 makes `agents doctor` report it per repo.

- [ ] **Step 6: Install and verify**

```bash
cd ~/dotfiles && make githooks && ls -l git/hooks.d/ && git config --global --get core.hooksPath
```

Expected: four symlinks to `/Users/nilbot/bin/agents`, and `core.hooksPath` set.

Then confirm it landed in the machine-local file and not in published content:

```bash
grep -c hooksPath ~/dotfiles/git/gitconfig.symlink; grep -A2 hooksPath ~/.gitconfig
```

Expected: `0` from the first command (nothing written into the public repo — this is the exact failure mode `37f00a0` fixed), and the setting present in `~/.gitconfig`.

- [ ] **Step 7: Verify against a pre-existing repo and against dotfiles itself**

```bash
cd ~/dotfiles && echo "# scratch" >> README.md && git add README.md && \
  git commit -m "$(printf 'test: hooks\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n')" && \
  git log -1 --format=%B && git reset --hard HEAD~1
```

Expected: the commit succeeds, `Co-Authored-By` is absent from the logged message, and the `recent.*` hooks still print what they printed before. If `README.md` does not exist, use any tracked file.

- [ ] **Step 8: Commit**

```bash
cd ~/dotfiles && git add -A Makefile git/ && \
  git commit -m "feat(git): global Go-backed hooks via core.hooksPath, retiring the bash dispatchers"
```

---

## Phase 6 — Fleet and doctor

Ends with: the setup can report on itself, across every repo it knows about.

### Task 19: The machine-local fleet registry

**Files:**
- Create: `agents/internal/registry/registry.go`
- Create: `agents/internal/registry/registry_test.go`
- Create: `agents/cmd_fleet.go`
- Modify: `agents/cmd_init.go` (register the repo)
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `machine.StateDir`, `harness.All`.
- Produces: `registry.Entry{Path string; Added time.Time; Local bool}`; `registry.Registry`; `registry.Load() (*Registry, error)`; `(*Registry).Save() error`; `(*Registry).Add(path string, local bool) bool`; `(*Registry).Remove(path string) bool`; `(*Registry).Reconcile() (present []Entry, missing []Entry)`; `runFleetLS(args []string, stdout io.Writer) int`; `runFleetUpdate(args []string, stdout io.Writer) int`.

**Hard requirement, not a preference (§10):** the registry lives at `~/.local/state/agents/registry.json` and is never tracked. **The dotfiles repo is public on GitHub**, and a registry enumerating every repo path on this machine would publish project names, client or employer names, and directory structure. It is also genuinely machine-specific — the same dotfiles clone on two machines should have two different registries.

**Presence of `.agents/` in a repo is the truth; the registry is a cache.** Drift is a normal state to report, not an error to block — a repo can be moved, archived, or deleted at any time, and none of those are mistakes. This is why the fleet cache gets no regenerate-and-compare guard, while the generated indexes do: an index has one correct value derivable from tracked source, so a mismatch is a defect; a cache describes a mutable world it does not control, so a mismatch is news.

- [ ] **Step 1: Write the failing tests**

`agents/internal/registry/registry_test.go`:

```go
package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndDedupe(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	r, err := Load()
	if err != nil {
		t.Fatalf("Load on a fresh machine must not fail: %v", err)
	}
	if !r.Add("/Users/n/work/a", false) {
		t.Fatal("Add should report that it added")
	}
	if r.Add("/Users/n/work/a", false) {
		t.Fatal("Add should report that it did nothing the second time")
	}
	r.Add("/Users/n/work/b", true)
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	again, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(again.Repos))
	}
	if again.Repos[1].Local != true {
		t.Error("the --local flag must round-trip")
	}
}

// The path is a hard requirement: the dotfiles repo is public, and a list of
// every repo on this machine is project names, employer names, and directory
// structure.
func TestRegistryLivesInMachineLocalState(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	r, _ := Load()
	r.Add("/Users/n/work/a", false)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(state, "agents", "registry.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("registry not at %s: %v", want, err)
	}
	fi, _ := os.Stat(want)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestReconcileReportsBothDirections(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	live := t.TempDir()
	if err := os.MkdirAll(filepath.Join(live, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	r, _ := Load()
	r.Add(live, false)
	r.Add("/gone/for/good", false)

	present, missing := r.Reconcile()
	if len(present) != 1 || present[0].Path != live {
		t.Errorf("present = %+v", present)
	}
	if len(missing) != 1 || missing[0].Path != "/gone/for/good" {
		t.Errorf("missing = %+v", missing)
	}
}

// A registry with a repo whose .agents/ was deleted is not corrupt.
func TestReconcileTreatsRemovedAgentsDirAsMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir() // a real directory with no .agents/
	r, _ := Load()
	r.Add(dir, false)
	_, missing := r.Reconcile()
	if len(missing) != 1 {
		t.Fatalf("missing = %+v, want the repo that lost its .agents/", missing)
	}
}

func TestLoadToleratesACorruptFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	if err := os.MkdirAll(filepath.Join(state, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agents", "registry.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("want an error naming the file, so it can be deleted")
	} else if !strings.Contains(err.Error(), "registry.json") {
		t.Fatalf("error must name the file: %v", err)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/registry/ -v`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Implement**

`agents/internal/registry/registry.go`:

```go
// Package registry caches which repositories on this machine have been
// initialized, so fleet-wide commands do not have to scan the disk.
//
// Presence of .agents/ in a repo is the truth. This is only a cache, and it
// lives in machine-local XDG state, never in the dotfiles repo -- that repo is
// public, and a list of every repo path on this machine is project names,
// employer names, and directory structure.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/machine"
)

type Entry struct {
	Path  string    `json:"path"`
	Added time.Time `json:"added"`
	Local bool      `json:"local"` // initialized with --local; .agents/ is not tracked
}

type Registry struct {
	Repos []Entry `json:"repos"`
}

func Path() string { return filepath.Join(machine.StateDir(), "registry.json") }

func Load() (*Registry, error) {
	b, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return &Registry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		// Naming the file matters: the remedy is to delete it, and a cache is
		// safe to delete.
		return nil, fmt.Errorf("%s is not valid JSON (safe to delete; it is a cache): %w", Path(), err)
	}
	return &r, nil
}

func (r *Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	sort.Slice(r.Repos, func(i, j int) bool { return r.Repos[i].Path < r.Repos[j].Path })
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	// 0600: it names every project on this machine.
	return os.WriteFile(Path(), append(b, '\n'), 0o600)
}

// Add reports whether it changed anything.
func (r *Registry) Add(path string, local bool) bool {
	for _, e := range r.Repos {
		if e.Path == path {
			return false
		}
	}
	r.Repos = append(r.Repos, Entry{Path: path, Added: time.Now().UTC(), Local: local})
	return true
}

func (r *Registry) Remove(path string) bool {
	for i, e := range r.Repos {
		if e.Path == path {
			r.Repos = append(r.Repos[:i], r.Repos[i+1:]...)
			return true
		}
	}
	return false
}

// Reconcile splits the cache against reality. Drift is news, not an error: a
// repo can be moved, archived, or deleted, and none of those are mistakes.
func (r *Registry) Reconcile() (present, missing []Entry) {
	for _, e := range r.Repos {
		if fi, err := os.Stat(filepath.Join(e.Path, ".agents")); err == nil && fi.IsDir() {
			present = append(present, e)
		} else {
			missing = append(missing, e)
		}
	}
	return present, missing
}
```

- [ ] **Step 4: Implement the fleet commands**

`agents/cmd_fleet.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/registry"
)

func runFleetLS(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(stdout)
	prune := fs.Bool("prune", false, "drop entries whose .agents/ is gone")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	r, err := registry.Load()
	if err != nil {
		fmt.Fprintf(stdout, "agents ls: %v\n", err)
		return exitcode.NoRecord
	}
	present, missing := r.Reconcile()

	for _, e := range present {
		local := ""
		if e.Local {
			local = "  (local)"
		}
		fmt.Fprintf(stdout, "%s%s\n", e.Path, local)
	}
	for _, e := range missing {
		fmt.Fprintf(stdout, "%s  — no .agents/ here any more\n", e.Path)
	}

	if *prune && len(missing) > 0 {
		for _, e := range missing {
			r.Remove(e.Path)
		}
		if err := r.Save(); err != nil {
			fmt.Fprintf(stdout, "agents ls: %v\n", err)
			return exitcode.NoRecord
		}
		fmt.Fprintf(stdout, "\npruned %d entries\n", len(missing))
		return exitcode.OK
	}
	if len(missing) > 0 {
		// Advisory, never blocking: a moved or archived repo is normal.
		fmt.Fprintf(stdout, "\n%d registered repo(s) no longer have .agents/; `agents ls --prune` forgets them\n", len(missing))
		return exitcode.Advisory
	}
	return exitcode.OK
}

func runFleetUpdate(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stdout)
	all := fs.Bool("all", false, "every registered repo")
	apply := fs.Bool("apply", false, "actually rewrite the wiring")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if !*all {
		fmt.Fprintln(stdout, "agents update: --all is the only mode; use `agents wire` for one repo")
		return exitcode.Malformed
	}

	r, err := registry.Load()
	if err != nil {
		fmt.Fprintf(stdout, "agents update: %v\n", err)
		return exitcode.NoRecord
	}
	present, missing := r.Reconcile()

	// Dry run by default: this command writes into every repo on the machine,
	// and a mistake would be tedious to undo across all of them.
	if !*apply {
		fmt.Fprintf(stdout, "would rewire %d repo(s); re-run with --apply\n", len(present))
		for _, e := range present {
			fmt.Fprintln(stdout, "  "+e.Path)
		}
		for _, e := range missing {
			fmt.Fprintln(stdout, "  skip (gone): "+e.Path)
		}
		return exitcode.Advisory
	}

	failed := 0
	for _, e := range present {
		if code := wireAll(e.Path, stdout); code != exitcode.OK {
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(stdout, "%d repo(s) failed to rewire\n", failed)
		return exitcode.Advisory
	}
	fmt.Fprintf(stdout, "rewired %d repo(s)\n", len(present))
	return exitcode.OK
}
```

Register in `main.go`: `case "ls": return runFleetLS(args[1:], os.Stdout)` and `case "update": return runFleetUpdate(args[1:], os.Stdout)`.

- [ ] **Step 5: Register the repo from `agents init`**

In `agents/cmd_init.go`, after `scaffold.Create` succeeds:

```go
	if r, err := registry.Load(); err != nil {
		// A broken cache must not stop initialization. The repo is initialized
		// either way; the cache only makes fleet commands cheap.
		fmt.Fprintf(stdout, "agents init: registry unavailable (%v); continuing\n", err)
	} else if r.Add(rc.Root, *local) {
		if err := r.Save(); err != nil {
			fmt.Fprintf(stdout, "agents init: could not update the registry: %v\n", err)
		}
	}
```

Add the `registry` import.

- [ ] **Step 6: Run tests**

Run: `cd ~/dotfiles/agents && go test ./... -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): machine-local fleet registry, never tracked"
```

---

### Task 20: `agents doctor`

**Files:**
- Create: `agents/internal/doctor/doctor.go`
- Create: `agents/internal/doctor/doctor_test.go`
- Create: `agents/cmd_doctor.go`
- Modify: `agents/main.go`

**Interfaces:**
- Consumes: `harness.All`, `trace.Query`, `memory.List`, `repo.Discover`, `machine.ID`.
- Produces: `doctor.Check{Name, Status, Detail, Remedy string}` with `Status` in `ok|warn|fail`; `doctor.Thresholds{Window time.Duration; Modules, Days, Sessions int}`; `doctor.DefaultThresholds() Thresholds`; `doctor.Run(repoRoot, agentsDir, thisMachine, binary string, th Thresholds, now time.Time) ([]Check, error)`; `doctor.LaneHealth(recs []record.Record, th Thresholds, now time.Time) []Check`; `runDoctor(args []string, stdout io.Writer) int`.

**Deviation from spec §9, stated deliberately.** The spec's third and fourth questions are "is the project trusted by this harness?" and "is the hook's current hash trusted?". Neither is reliably answerable from disk — the trust stores are private to each harness and undocumented, and reading them by guesswork would produce a check that is confidently wrong. The question the user actually has is *"is anything being recorded?"*, and that **is** answerable: query the trace index for a record from each wired harness. An untrusted project produces no records, and a Codex re-flag after `agents wire` produces no records since the wiring changed. The empirical check subsumes both and cannot be wrong about the thing that matters.

**Lane-health defaults** (settling the spec's open question, provisionally): a 30-day window, flagged at more than 3 distinct top-level `cwd` values, more than 14 days of span, or more than 20 sessions. These are guesses and are exposed as flags. Revisit once there is a month of real records. **Advisory only — it never creates or switches branches**: automating git surgery on a heuristic is a much worse failure than a stale lane.

- [ ] **Step 1: Write the failing tests**

`agents/internal/doctor/doctor_test.go`:

```go
package doctor

import (
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

func statuses(checks []Check) map[string]string {
	m := map[string]string{}
	for _, c := range checks {
		m[c.Name] = c.Status
	}
	return m
}

func TestLaneHealthFlagsASprawlingLane(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	var recs []record.Record
	for _, mod := range []string{"payments/api", "billing/web", "infra/tf", "docs"} {
		recs = append(recs, record.Record{
			When: now.Add(-time.Hour), Lane: "sprawl", Cwd: mod, SessionID: mod,
		})
	}
	recs = append(recs, record.Record{When: now.Add(-time.Hour), Lane: "tidy", Cwd: "payments/api", SessionID: "s"})

	checks := LaneHealth(recs, Thresholds{Window: 30 * 24 * time.Hour, Modules: 3, Days: 14, Sessions: 20}, now)
	st := statuses(checks)
	if st["lane:sprawl"] != "warn" {
		t.Errorf("sprawling lane not flagged: %+v", checks)
	}
	if _, flagged := st["lane:tidy"]; flagged {
		t.Errorf("a tidy lane must not be flagged: %+v", checks)
	}

	// The report has to name the modules, or splitting is guesswork.
	for _, c := range checks {
		if c.Name == "lane:sprawl" && c.Detail == "" {
			t.Error("a lane-health warning must name the distinct modules it saw")
		}
	}
}

func TestLaneHealthNeverBlocks(t *testing.T) {
	now := time.Now().UTC()
	var recs []record.Record
	for i := 0; i < 50; i++ {
		recs = append(recs, record.Record{
			When: now, Lane: "everything", Cwd: string(rune('a' + i%26)), SessionID: string(rune('a' + i)),
		})
	}
	for _, c := range LaneHealth(recs, Thresholds{Window: 30 * 24 * time.Hour, Modules: 3, Days: 14, Sessions: 20}, now) {
		if c.Status == "fail" {
			t.Fatal("lane health is advisory; it must never produce a failing check")
		}
	}
}

func TestLaneHealthIgnoresRecordsOutsideTheWindow(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	var recs []record.Record
	for _, mod := range []string{"a", "b", "c", "d"} {
		recs = append(recs, record.Record{When: old, Lane: "ancient", Cwd: mod, SessionID: mod})
	}
	checks := LaneHealth(recs, Thresholds{Window: 30 * 24 * time.Hour, Modules: 3, Days: 14, Sessions: 20}, now)
	if len(checks) != 0 {
		t.Fatalf("records outside the window must not be judged: %+v", checks)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd ~/dotfiles/agents && go test ./internal/doctor/ -v`
Expected: FAIL — `undefined: LaneHealth`.

- [ ] **Step 3: Implement**

`agents/internal/doctor/doctor.go`:

```go
// Package doctor answers "is any of this actually working?".
//
// It exists because a hook cannot install itself and a missing hook fails
// silently -- the worst failure mode in the whole design, since the setup looks
// fine and records nothing.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/memory"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

type Check struct {
	Name   string
	Status string // ok | warn | fail
	Detail string
	Remedy string
}

type Thresholds struct {
	Window   time.Duration
	Modules  int
	Days     int
	Sessions int
}

func DefaultThresholds() Thresholds {
	// Guesses. They are flags precisely because they need a month of real
	// records before they mean anything.
	return Thresholds{Window: 30 * 24 * time.Hour, Modules: 3, Days: 14, Sessions: 20}
}

func Run(repoRoot, agentsDir, thisMachine, binary string, th Thresholds, now time.Time) ([]Check, error) {
	var checks []Check

	checks = append(checks, checkBinary(binary))
	for _, a := range harness.All() {
		checks = append(checks, checkWiring(a, repoRoot, binary))
	}

	res, err := trace.Query(agentsDir, trace.Filter{}, now)
	if err != nil {
		return nil, err
	}

	// The empirical trust check. Whether a harness trusts this project and
	// whether it trusts the current hook hash are both private to that harness
	// and not reliably readable; whether it has actually recorded anything is
	// observable, and it is the question that matters.
	for _, a := range harness.All() {
		checks = append(checks, checkRecording(a, res.Records, now))
	}

	if res.Skipped > 0 {
		checks = append(checks, Check{
			Name: "trace-index", Status: "warn",
			Detail: fmt.Sprintf("%d unreadable line(s)", res.Skipped),
			Remedy: "look for merge conflict markers in .agents/reports/traces/ and confirm merge=union is set in .gitattributes",
		})
	}

	checks = append(checks, checkLocalHooksPath(repoRoot))
	checks = append(checks, checkPointers(res.Records, thisMachine)...)
	checks = append(checks, checkMemorySources(agentsDir, thisMachine)...)
	checks = append(checks, LaneHealth(res.Records, th, now)...)
	return checks, nil
}

func checkBinary(binary string) Check {
	if _, err := exec.LookPath("agents"); err != nil {
		return Check{
			Name: "binary-on-path", Status: "fail",
			Detail: "`agents` is not on PATH",
			Remedy: "run `make -C ~/dotfiles agents` and make sure ~/bin is on PATH",
		}
	}
	if _, err := os.Stat(binary); err != nil {
		return Check{
			Name: "binary-on-path", Status: "warn",
			Detail: fmt.Sprintf("running from %s, which does not stat", binary),
		}
	}
	return Check{Name: "binary-on-path", Status: "ok", Detail: binary}
}

func checkWiring(a harness.Adapter, repoRoot, binary string) Check {
	name := "wiring:" + a.Name()
	path := a.WireConfigPath(repoRoot)
	b, err := os.ReadFile(path)
	if err != nil {
		return Check{Name: name, Status: "fail", Detail: "no " + path, Remedy: "run `agents wire`"}
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Check{Name: name, Status: "fail", Detail: path + " is not valid JSON", Remedy: "delete it and run `agents wire`"}
	}
	if !strings.Contains(string(b), binary) {
		// A stale absolute path is a hook that silently does nothing.
		return Check{
			Name: name, Status: "warn",
			Detail: "wired to a different binary path than the one running",
			Remedy: "run `agents wire`",
		}
	}
	return Check{Name: name, Status: "ok", Detail: path}
}

func checkRecording(a harness.Adapter, recs []record.Record, now time.Time) Check {
	name := "recording:" + a.Name()
	var last time.Time
	for _, r := range recs {
		if r.Harness == a.Name() && r.When.After(last) {
			last = r.When
		}
	}
	if last.IsZero() {
		return Check{
			Name: name, Status: "warn",
			Detail: "this harness has never recorded anything here",
			Remedy: strings.Join(a.TrustSteps("this repo"), " / "),
		}
	}
	return Check{Name: name, Status: "ok", Detail: "last record " + last.Format(time.RFC3339)}
}

// checkLocalHooksPath finds the one configuration that silently disables the
// git hooks. Local config beating global is correct git behaviour and is not
// fought -- but it should be said out loud.
func checkLocalHooksPath(repoRoot string) Check {
	cmd := exec.Command("git", "config", "--local", "--get", "core.hooksPath")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return Check{Name: "git-hooks", Status: "ok", Detail: "no local core.hooksPath override"}
	}
	return Check{
		Name: "git-hooks", Status: "warn",
		Detail: "this repo sets core.hooksPath locally to " + strings.TrimSpace(string(out)),
		Remedy: "the global agents hooks do not run here; chain them from that directory if you want them",
	}
}

func checkPointers(recs []record.Record, thisMachine string) []Check {
	elsewhere := map[string]int{}
	unreachable := 0
	for _, r := range recs {
		if r.Transcript == "" || !r.PointerVerified {
			continue
		}
		if r.Machine != thisMachine {
			elsewhere[r.Machine]++
			continue
		}
		if _, err := os.Stat(r.Transcript); err != nil {
			unreachable++
		}
	}

	var out []Check
	if unreachable > 0 {
		out = append(out, Check{
			Name: "pointers", Status: "warn",
			Detail: fmt.Sprintf("%d transcript(s) recorded on this machine are gone", unreachable),
			Remedy: "harness transcripts are cleaned up periodically; run `agents trace cache` sooner next time",
		})
	}
	machines := make([]string, 0, len(elsewhere))
	for m := range elsewhere {
		machines = append(machines, m)
	}
	sort.Strings(machines)
	for _, m := range machines {
		out = append(out, Check{
			Name: "pointers:" + m, Status: "warn",
			Detail: fmt.Sprintf("%d transcript(s) live on %s", elsewhere[m], m),
			Remedy: "run `agents trace cache` there, or wait for `agents distill` (spec 3)",
		})
	}
	return out
}

func checkMemorySources(agentsDir, thisMachine string) []Check {
	entries, err := memory.List(filepath.Join(agentsDir, "memory"))
	if err != nil {
		return []Check{{
			Name: "memory", Status: "fail", Detail: err.Error(),
			Remedy: "fix the frontmatter, then run `agents index`",
		}}
	}
	var out []Check
	for _, e := range entries {
		for _, s := range e.Sources {
			if s.Machine != "" && s.Machine != thisMachine {
				out = append(out, Check{
					Name: "memory:" + e.Name, Status: "warn",
					Detail: fmt.Sprintf("depends on %s material held by %s", s.Kind, s.Machine),
					// The discipline: the entry must already state its takeaway.
					// This is a route to more detail, not a missing fact.
					Remedy: "go to that machine to distil the detail; the entry's own claim should stand without it",
				})
			}
		}
	}
	return out
}

// LaneHealth reports lanes that have absorbed unrelated work.
//
// Branch-as-lane degrades silently when one branch becomes a catch-all, at
// which point both handoff scoping and trace retrieval quietly lose their edge.
// The data needed to notice is already in the records.
//
// Advisory only, and never a failure: it does not create branches, switch
// branches, or move anything. Automating git surgery on a heuristic is a much
// worse failure than a stale lane.
func LaneHealth(recs []record.Record, th Thresholds, now time.Time) []Check {
	type stat struct {
		modules  map[string]bool
		sessions map[string]bool
		first    time.Time
		last     time.Time
	}
	byLane := map[string]*stat{}

	cutoff := now.Add(-th.Window)
	for _, r := range recs {
		if r.Lane == "" || r.When.Before(cutoff) {
			continue
		}
		s := byLane[r.Lane]
		if s == nil {
			s = &stat{modules: map[string]bool{}, sessions: map[string]bool{}, first: r.When, last: r.When}
			byLane[r.Lane] = s
		}
		top := r.Cwd
		if i := strings.Index(top, "/"); i > 0 {
			top = top[:i]
		}
		s.modules[top] = true
		s.sessions[r.SessionID] = true
		if r.When.Before(s.first) {
			s.first = r.When
		}
		if r.When.After(s.last) {
			s.last = r.When
		}
	}

	lanes := make([]string, 0, len(byLane))
	for l := range byLane {
		lanes = append(lanes, l)
	}
	sort.Strings(lanes)

	var out []Check
	for _, l := range lanes {
		s := byLane[l]
		var reasons []string
		if len(s.modules) > th.Modules {
			mods := make([]string, 0, len(s.modules))
			for m := range s.modules {
				mods = append(mods, m)
			}
			sort.Strings(mods)
			reasons = append(reasons, fmt.Sprintf("%d modules (%s)", len(mods), strings.Join(mods, ", ")))
		}
		if days := int(s.last.Sub(s.first).Hours() / 24); days > th.Days {
			reasons = append(reasons, fmt.Sprintf("%d days", days))
		}
		if len(s.sessions) > th.Sessions {
			reasons = append(reasons, fmt.Sprintf("%d sessions", len(s.sessions)))
		}
		if len(reasons) == 0 {
			continue
		}
		out = append(out, Check{
			Name: "lane:" + l, Status: "warn",
			Detail: strings.Join(reasons, "; "),
			Remedy: "splitting this into separate branches would sharpen handoffs and trace retrieval — nothing here does it for you",
		})
	}
	return out
}
```

- [ ] **Step 4: Implement the command**

`agents/cmd_doctor.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/doctor"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func runDoctor(args []string, stdout io.Writer) int {
	th := doctor.DefaultThresholds()
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.IntVar(&th.Modules, "lane-modules", th.Modules, "distinct top-level modules before a lane is flagged")
	fs.IntVar(&th.Days, "lane-days", th.Days, "days of span before a lane is flagged")
	fs.IntVar(&th.Sessions, "lane-sessions", th.Sessions, "sessions before a lane is flagged")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents doctor: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents doctor: not inside a git repository")
		return exitcode.Skip
	}
	mid, err := machine.ID()
	if err != nil {
		fmt.Fprintf(stdout, "agents doctor: %v\n", err)
		return exitcode.NoRecord
	}
	bin, err := binaryPath()
	if err != nil {
		fmt.Fprintf(stdout, "agents doctor: %v\n", err)
		return exitcode.NoRecord
	}

	checks, err := doctor.Run(rc.Root, repo.AgentsDir(rc.Root), mid, bin, th, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents doctor: %v\n", err)
		return exitcode.NoRecord
	}

	worst := exitcode.OK
	for _, c := range checks {
		mark := map[string]string{"ok": "ok  ", "warn": "warn", "fail": "FAIL"}[c.Status]
		fmt.Fprintf(stdout, "%s  %-28s %s\n", mark, c.Name, c.Detail)
		if c.Remedy != "" && c.Status != "ok" {
			fmt.Fprintf(stdout, "      -> %s\n", c.Remedy)
		}
		switch c.Status {
		case "fail":
			worst = exitcode.Block
		case "warn":
			if worst == exitcode.OK {
				worst = exitcode.Advisory
			}
		}
	}
	return worst
}
```

Register in `main.go`: `case "doctor": return runDoctor(args[1:], os.Stdout)`.

- [ ] **Step 5: Update the usage text**

Replace `usage()` in `main.go` with the full surface, so `agents` with no arguments is the documentation:

```go
func usage() {
	fmt.Fprint(os.Stderr, `usage: agents <command> [flags]

  init [--local]              create .agents/, triggers, wiring, fleet entry
  wire                        regenerate harness configs (merges, never overwrites)
  doctor                      is any of this working? what is unreachable? lane health?
  index                       regenerate memory/INDEX.md and handoff/INDEX.md
  save [-m msg]               commit .agents/ paths and nothing else
  handoff write|prune         lane-scoped handoff management
  trace ls|cache              query records; copy reachable transcripts locally
  ls [--prune]                the fleet on this machine
  update --all [--apply]      rewire every registered repo (dry run by default)
  guard --staged              pre-commit checks (the only command that blocks)
  hook <event> --harness <n>  harness hook entrypoint

exit codes: 0 ok, 1 advisory, 2 block, 3 malformed, 4 skip, 5 could not record
`)
}
```

- [ ] **Step 6: Run the whole suite and use the tool on itself**

```bash
cd ~/dotfiles/agents && go test ./... && go vet ./... && gofmt -l .
```

Expected: tests pass, vet is silent, `gofmt -l` prints nothing.

```bash
cd ~/dotfiles && make agents && agents init && agents doctor
```

Expected: `init` exits 1 with trust steps; `doctor` reports `binary-on-path ok`, wiring `ok` for both harnesses, and `recording:*` warnings (nothing has recorded here yet). Confirm `.agents/` appeared and that `git status` shows it as untracked content, not as a modification to something else.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles && git add agents/ && git commit -m "feat(agents): doctor, with empirical recording checks and lane health"
```

- [ ] **Step 8: Commit the dotfiles repo's own `.agents/`**

The tool's first real user is the repo it lives in:

```bash
cd ~/dotfiles && agents save -m "chore(agents): initialize agent context for dotfiles"
```

Expected: the commit contains only `.agents/` paths, `CLAUDE.md`, and `AGENTS.md` are untracked or already committed separately, and the pre-commit guard runs clean.

---

## Self-Review

Run against the spec after writing, before executing.

**Spec coverage.** §1 placement rule → Tasks 2, 7, 19 (three tiers, each in the right place); Check → Task 20, Materialize → Task 12, Distil → deferred to spec 3 by design. §2 layout → Task 7. §3.1–3.3 → Task 2. §3.4 memory provenance → Tasks 13, 20. §3.5 → Task 4. §3.6 → Task 11. §3.7 → Tasks 7, 18. §4 handoffs → Task 14. §4.1 lane health → Task 20. §5 adapters → Tasks 5, 9. §6 binary surface → all twelve subcommands are implemented and listed in `usage()`. §7 guards → Task 16, with the `PreToolUse` advisory layer **deliberately not built** (see below). §8 git hooks → Tasks 17, 18. §9 bootstrap and trust → Tasks 7, 20. §10 fleet → Task 19. §11 testing → every task is test-first, and the captured fixtures are load-bearing in Task 9.

**One spec item is deliberately not implemented.** §7's middle layer, a narrow `PreToolUse` guard, is advisory-only by the spec's own account and duplicates a check that Task 16 performs authoritatively at the commit boundary. Building it would add a per-tool-call subprocess to every session for a check that cannot be relied upon. If early warning turns out to be wanted in practice, it is a small addition on top of `guard.Staged`.

**Two open questions are settled here rather than left open**, and both should be reviewed as decisions rather than accepted as detail: machine identity (Task 1 — a generated stable id, not the live hostname) and secret detection (Task 16 — delegated to gitleaks as a subprocess, with one domain rule shipped as config; revised 2026-08-08 from an earlier hand-rolled pattern list). Lane-health thresholds (Task 20) are set to provisional values and exposed as flags, exactly as the spec asked.

**One documented deviation from the spec's letter:** §9 asks `doctor` to check harness trust state directly. Task 20 checks whether each harness has actually recorded anything instead, because the trust stores are private and undocumented, and a check built on guesswork would be confidently wrong about the one thing the user needs to be right. The empirical check subsumes both trust questions.

**Type consistency.** `harness.Payload` is decoded in exactly one place and consumed by `harness.Build`, which returns `harness.Trace`; `cmd_hook.go` is the only place `Trace` becomes `record.Record`. `agentsDirHere` is defined once (Task 11, `cmd_trace.go`) and reused by Tasks 13, 14, 16, 19. `git()` exists in two packages with the same signature — `main` (Task 15) and `guard` (Task 16) — which is intentional; they are separate packages and the helper is four lines.

**Ordering constraint to watch:** Task 7's `TestInitScaffoldsWiresAndReportsTrust` asserts that `.codex/hooks.json` exists, which needs the Codex adapter's `Wire` method from Task 9. Task 7 step 9 says to implement that method early if the test fails; the rest of the Codex adapter stays in Task 9.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-07-agents-repo-context.md`. Two execution options:

**1. Subagent-Driven (recommended)** — a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
