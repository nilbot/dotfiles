# Spec 7 Phases A and B′ Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the trace store out of the tracked tree into one bounded
machine-local store, and close the capture loop with an instruction, an untracked
draft queue, and a review command that promotes and commits in one act.

**Architecture:** `<git-common-dir>/agents/` becomes the single store, holding
`traces/` (the index, previously tracked under `.agents/reports/traces/`),
`trace-cache/` (unchanged location, now with retention), and `queue/` (new,
untracked drafts). Capture is an instruction in the scaffolded `CLAUDE.md`;
`agents handoff draft` writes to the queue; `agents review --keep` validates a
draft, writes it into `.agents/`, regenerates the index, and commits — scoped to
`.agents/` — as one operation.

**Tech Stack:** Go 1.26, stdlib plus `gopkg.in/yaml.v3`. No new dependencies.

## Global Constraints

- **Do not build spec 7 §3c.** No `Stop` gate, no budget, no watermarks, no
  ceilings. The gate is a contingency the spec orders behind a measurement.
- Recording hooks exit 0 on every path (spec 1 §6). `agents guard` remains the
  sole deliberate exception.
- Exit codes come from `internal/exitcode`: `OK=0`, `Advisory=1`, `Block=2`,
  `Malformed=3`, `Skip=4`, `NoRecord=5`.
- Never serialize `last_assistant_message`, `tool_input`, `tool_response`. The
  record type has no field capable of carrying them; keep it that way.
- Path handling inside repository-controlled directories goes through
  `internal/safeio`, never bare `filepath.Join` + `os.Open`.
- Tests use real payload fixtures where payloads are involved, and assert
  structurally rather than by grepping output (spec 1 §11).
- Commit messages carry no AI attribution.
- Do not `git push`.

---

### Task 1: One store root, and the trace index inside it

**Files:**
- Modify: `agents/internal/repo/repo.go` (add `StoreDir`, restate `TraceCacheDir` in terms of it)
- Modify: `agents/internal/record/record.go:77-95` (`Writer` takes the store dir)
- Modify: `agents/internal/trace/trace.go:90-115` (`Query` reads `<store>/traces`)
- Modify: `agents/cmd_hook.go`, `agents/cmd_trace.go`, `agents/internal/doctor/doctor.go` (call sites)
- Test: `agents/internal/repo/repo_test.go`, `agents/internal/record/record_test.go`, `agents/internal/trace/trace_test.go`

**Interfaces:**
- Consumes: `repo.gitPath(dir, "--git-common-dir")`, already present.
- Produces: `repo.StoreDir(dir string) (string, error)` returning
  `<git-common-dir>/agents`. `record.NewWriter(storeDir string) *Writer` whose
  `Append` writes `<storeDir>/traces/YYYY-MM-DD.jsonl`.
  `trace.Query(storeDir string, f Filter, now time.Time) (Result, error)`.

- [ ] **Step 1: Write the failing test for `StoreDir`**

```go
func TestStoreDirIsTheCommonDirectory(t *testing.T) {
	dir := t.TempDir()
	mustGit(t, dir, "init")
	got, err := repo.StoreDir(dir)
	if err != nil {
		t.Fatalf("StoreDir: %v", err)
	}
	want := filepath.Join(dir, ".git", "agents")
	if got != want {
		t.Errorf("StoreDir = %q, want %q", got, want)
	}
	cache, err := repo.TraceCacheDir(dir)
	if err != nil {
		t.Fatalf("TraceCacheDir: %v", err)
	}
	// The cache must stay inside the store, or "one store" is a claim the
	// layout does not make.
	if cache != filepath.Join(want, "trace-cache") {
		t.Errorf("TraceCacheDir = %q, want it under %q", cache, want)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd agents && go test ./internal/repo/ -run TestStoreDir -v`
Expected: FAIL, `undefined: repo.StoreDir`

- [ ] **Step 3: Add `StoreDir` and restate `TraceCacheDir`**

```go
// StoreDir resolves the machine-local store for a repository.
//
// One store, not two halves under different rationales. It holds the trace
// index, the transcript cache, and the untracked draft queue -- everything
// upstream of a conclusion. The tracked tier holds conclusions only.
//
// The common directory for the reasons TraceCacheDir already documents: it
// survives `git worktree remove`, git does not track it structurally rather
// than by an ignore rule someone remembered, and it is per-repository for free.
// Not XDG state, whose contract would require a stable repo identity and would
// strand orphaned stores for repositories deleted months ago.
func StoreDir(dir string) (string, error) {
	common, err := gitPath(dir, "--git-common-dir")
	if err != nil {
		return "", ErrNotARepo
	}
	return filepath.Join(common, "agents"), nil
}

func TraceCacheDir(dir string) (string, error) {
	store, err := StoreDir(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(store, "trace-cache"), nil
}
```

- [ ] **Step 4: Run it and watch it pass**

Run: `cd agents && go test ./internal/repo/ -run TestStoreDir -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for the relocated index**

```go
func TestWriterAppendsUnderTheStoreNotTheTrackedTree(t *testing.T) {
	store := t.TempDir()
	rec := record.Record{
		When: time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC),
		Harness: "claude-code", Machine: "m", Event: "stop", Lane: "master",
	}
	if err := record.NewWriter(store).Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "traces", "2026-08-12.jsonl")); err != nil {
		t.Fatalf("record not written under the store: %v", err)
	}
	// The old tracked location must not be recreated by a write.
	if _, err := os.Stat(filepath.Join(store, "reports")); !os.IsNotExist(err) {
		t.Errorf("writer recreated a reports/ tree under the store")
	}
}
```

- [ ] **Step 6: Run it and watch it fail**

Run: `cd agents && go test ./internal/record/ -run TestWriterAppends -v`
Expected: FAIL — the file lands under `reports/traces/`

- [ ] **Step 7: Point the writer and the query at `<store>/traces`**

In `record.go`, rename the field and drop the `reports` segment:

```go
type Writer struct{ storeDir string }

func NewWriter(storeDir string) *Writer { return &Writer{storeDir: storeDir} }

func (w *Writer) Append(r Record) error {
	dir := filepath.Join(w.storeDir, "traces")
	// ... rest unchanged
```

In `trace.go`, `Query` opens the store root and then `traces` directly, dropping
the `reports` hop:

```go
func Query(storeDir string, f Filter, now time.Time) (Result, error) {
	storeRoot, err := os.OpenRoot(storeDir)
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	defer storeRoot.Close()
	tracesRoot, err := safeio.OpenDirAt(storeRoot, "traces")
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	defer tracesRoot.Close()
	// ... body unchanged, but the path built for error messages becomes
	// filepath.Join(storeDir, "traces", name)
```

- [ ] **Step 8: Update every call site**

`cmd_hook.go:106`, `cmd_trace.go` (all `trace.Query` calls), and
`doctor.go`'s trace-index check take `repo.StoreDir(root)` instead of the agents
directory. `doctor.Dependencies` gains `StoreDir func(string) (string, error)`
defaulting to `repo.StoreDir`.

- [ ] **Step 9: Run the whole suite**

Run: `cd agents && go build ./... && go test ./...`
Expected: PASS. Fix any call site the compiler names.

- [ ] **Step 10: Commit**

```bash
git add agents/
git commit -m "refactor(agents): put the trace index in the machine-local store"
```

---

### Task 2: Retention, so the store stops growing without bound

**Files:**
- Modify: `agents/internal/trace/cache.go`
- Modify: `agents/cmd_trace.go` (a `--age`/`--size` prune path)
- Test: `agents/internal/trace/cache_test.go`

**Interfaces:**
- Consumes: `trace.PruneReport` (exists: `Files int`, `Bytes int64`, plus detail fields).
- Produces: `trace.PruneRetention(root string, maxAge time.Duration, maxBytes int64, now time.Time, apply bool) (PruneReport, error)`.
  Defaults live in `cmd_trace.go` as `defaultMaxAge = 14 * 24 * time.Hour` and
  `defaultMaxBytes int64 = 1 << 30`.

- [ ] **Step 1: Write the failing tests**

```go
func TestPruneRetentionEvictsByAge(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "claude-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	old := filepath.Join(dir, "old.jsonl")
	fresh := filepath.Join(dir, "fresh.jsonl")
	for path, age := range map[string]time.Duration{old: 20 * 24 * time.Hour, fresh: time.Hour} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := trace.PruneRetention(root, 14*24*time.Hour, 1<<30, now, true)
	if err != nil {
		t.Fatalf("PruneRetention: %v", err)
	}
	if rep.Files != 1 {
		t.Errorf("Files = %d, want 1", rep.Files)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the aged-out copy survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh copy was evicted")
	}
}

func TestPruneRetentionEvictsOldestFirstUnderTheSizeCap(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "claude-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	// Three 100-byte copies, one hour apart, against a 250-byte cap: the
	// oldest must go and the two newest must stay.
	names := []string{"a", "b", "c"}
	for i, n := range names {
		p := filepath.Join(dir, n+".jsonl")
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), 100), 0o644); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-time.Duration(len(names)-i) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := trace.PruneRetention(root, 0, 250, now, true)
	if err != nil {
		t.Fatalf("PruneRetention: %v", err)
	}
	if rep.Files != 1 {
		t.Fatalf("Files = %d, want 1", rep.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jsonl")); !os.IsNotExist(err) {
		t.Error("the oldest copy survived the size cap")
	}
	for _, n := range []string{"b", "c"} {
		if _, err := os.Stat(filepath.Join(dir, n+".jsonl")); err != nil {
			t.Errorf("%s.jsonl was evicted below the cap", n)
		}
	}
}

func TestPruneRetentionDryRunRemovesNothing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "claude-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	p := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rep, err := trace.PruneRetention(root, 14*24*time.Hour, 1<<30, now, false)
	if err != nil {
		t.Fatalf("PruneRetention: %v", err)
	}
	if rep.Files != 1 {
		t.Errorf("Files = %d, want 1 reported", rep.Files)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("a dry run deleted a copy")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `cd agents && go test ./internal/trace/ -run TestPruneRetention -v`
Expected: FAIL, `undefined: trace.PruneRetention`

- [ ] **Step 3: Implement `PruneRetention`**

```go
// PruneRetention bounds the cache by age and total size.
//
// Both caps or neither is a false choice: they must be sized against each
// other or one is decoration. At the growth this repository measured -- 51 MB
// in a day -- a 500 MB cap would evict at ten days and a 14-day age cap could
// never bind. The defaults in cmd_trace.go are 14 days and 1 GB for that
// reason.
//
// Age first, then size over what survives, oldest evicted first. Modification
// time is the ordering key rather than the record timestamp because a cached
// copy is a file, and the file is what has to fit.
//
// A zero cap disables that dimension. Nothing here reads a record: this bounds
// copies, and the index it is a copy of is never touched.
func PruneRetention(root string, maxAge time.Duration, maxBytes int64, now time.Time, apply bool) (PruneReport, error) {
	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var kept []entry
	var rep PruneReport

	remove := func(e entry) error {
		rep.Files++
		rep.Bytes += e.size
		rep.Details = append(rep.Details, e.path)
		if !apply {
			return nil
		}
		return os.Remove(e.path)
	}

	dirs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, d.Name()))
		if err != nil {
			return rep, err
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				return rep, err
			}
			e := entry{path: filepath.Join(root, d.Name(), f.Name()), size: info.Size(), mod: info.ModTime()}
			if maxAge > 0 && now.Sub(e.mod) > maxAge {
				if err := remove(e); err != nil {
					return rep, err
				}
				continue
			}
			kept = append(kept, e)
		}
	}

	if maxBytes <= 0 {
		return rep, nil
	}
	var total int64
	for _, e := range kept {
		total += e.size
	}
	if total <= maxBytes {
		return rep, nil
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].mod.Before(kept[j].mod) })
	for _, e := range kept {
		if total <= maxBytes {
			break
		}
		if err := remove(e); err != nil {
			return rep, err
		}
		total -= e.size
	}
	return rep, nil
}
```

Add `Details []string` to `PruneReport` if it is not already there.

- [ ] **Step 4: Run and watch them pass**

Run: `cd agents && go test ./internal/trace/ -run TestPruneRetention -v`
Expected: PASS

- [ ] **Step 5: Wire it to the CLI and to `post-merge`**

`agents trace cache prune` gains `--age` and `--size` (defaults above), keeping
its dry-run-unless-`--yes` convention. The `post-merge` git hook calls
`PruneRetention` with the defaults and `apply=true`, ignoring errors — a failed
prune must never fail a merge.

- [ ] **Step 6: Run the suite and commit**

```bash
cd agents && go test ./...
cd .. && git add agents/ && git commit -m "feat(agents): bound the transcript cache by age and size"
```

---

### Task 3: Migrate the tracked index out, and stop scaffolding it

**Files:**
- Create: `agents/cmd_trace_migrate.go`
- Modify: `agents/internal/scaffold/scaffold.go:59-62` (drop the `merge=union` line), `:76-84` (drop `reports/traces`)
- Modify: `agents/cmd_trace.go` (dispatch `migrate`)
- Test: `agents/cmd_trace_test.go`, `agents/internal/scaffold/scaffold_test.go`

**Interfaces:**
- Consumes: `repo.StoreDir`, `repo.Git`.
- Produces: `runTraceMigrate(args []string, stdout io.Writer) int`, and
  `trace.MigrateTrackedIndex(agentsDir, storeDir string) (moved int, err error)`.

- [ ] **Step 1: Write the failing test for the copy step**

```go
func TestMigrateTrackedIndexIsIdempotentAndLossless(t *testing.T) {
	agentsDir, store := t.TempDir(), t.TempDir()
	src := filepath.Join(agentsDir, "reports", "traces")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := "{\"when\":\"2026-08-10T00:00:00Z\",\"event\":\"stop\"}\n"
	if err := os.WriteFile(filepath.Join(src, "2026-08-10.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := trace.MigrateTrackedIndex(agentsDir, store); err != nil {
			t.Fatalf("MigrateTrackedIndex: %v", err)
		}
	}
	got, err := os.ReadFile(filepath.Join(store, "traces", "2026-08-10.jsonl"))
	if err != nil {
		t.Fatalf("migrated file missing: %v", err)
	}
	// Running twice must not double the history.
	if string(got) != lines {
		t.Errorf("content = %q, want %q", got, lines)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd agents && go test ./internal/trace/ -run TestMigrateTrackedIndex -v`
Expected: FAIL, `undefined: trace.MigrateTrackedIndex`

- [ ] **Step 3: Implement it**

```go
// MigrateTrackedIndex copies the previously tracked daily files into the store.
//
// Line-wise and set-based rather than a file copy, because the two sides can
// both be non-empty: the store may already hold today's records written by a
// migrated binary while the tracked file still holds this morning's. Appending
// only lines the store does not have makes a second run a no-op, which is what
// lets the migration be re-run after a merge brings more tracked records in.
func MigrateTrackedIndex(agentsDir, storeDir string) (int, error) {
	src := filepath.Join(agentsDir, "reports", "traces")
	entries, err := os.ReadDir(src)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	dst := filepath.Join(storeDir, "traces")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		in, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return moved, err
		}
		target := filepath.Join(dst, e.Name())
		have := map[string]bool{}
		if existing, err := os.ReadFile(target); err == nil {
			for _, l := range strings.Split(string(existing), "\n") {
				if l != "" {
					have[l] = true
				}
			}
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		var add []string
		for _, l := range strings.Split(string(in), "\n") {
			if l == "" || have[l] {
				continue
			}
			have[l] = true
			add = append(add, l)
		}
		if len(add) == 0 {
			continue
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return moved, err
		}
		_, werr := f.WriteString(strings.Join(add, "\n") + "\n")
		cerr := f.Close()
		if werr != nil {
			return moved, werr
		}
		if cerr != nil {
			return moved, cerr
		}
		moved++
	}
	return moved, nil
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `cd agents && go test ./internal/trace/ -run TestMigrateTrackedIndex -v`
Expected: PASS

- [ ] **Step 5: Add `agents trace migrate`**

Dry run by default, `--yes` to act, matching `trace cache prune`. On `--yes`:
copy via `MigrateTrackedIndex`, then `git rm -r --cached -- .agents/reports/traces`,
then remove the `merge=union` line from `.gitattributes` if present. It prints
what it did and reminds that the removal still needs committing — it must not
commit on the user's behalf, because that decision belongs with whatever else is
in flight.

- [ ] **Step 6: Stop scaffolding the tracked directory**

Delete `"reports/traces"` from `scaffold.dirs` and
`".agents/reports/traces/*.jsonl merge=union"` from `gitattributesLines`. Leave
`".agents/** linguist-generated=true"`. Update the layout comment in the package
doc.

- [ ] **Step 7: Assert the scaffold no longer creates it**

```go
func TestCreateDoesNotScaffoldATrackedTraceDirectory(t *testing.T) {
	root := newRepo(t)
	if err := scaffold.Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "reports", "traces")); !os.IsNotExist(err) {
		t.Error("scaffold still creates .agents/reports/traces")
	}
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(attrs), "merge=union") {
		t.Error("merge=union survives, but nothing tracked appends concurrently now")
	}
}
```

- [ ] **Step 8: Run the suite and commit**

```bash
cd agents && go test ./...
cd .. && git add agents/ && git commit -m "feat(agents): migrate the trace index out of the tracked tree"
```

---

### Task 4: The draft queue, and `agents handoff draft`

**Files:**
- Create: `agents/internal/queue/queue.go`, `agents/internal/queue/queue_test.go`
- Modify: `agents/cmd_handoff.go` (add the `draft` verb)
- Test: `agents/cmd_handoff_test.go`

**Interfaces:**
- Consumes: `repo.StoreDir`, `handoff.CheckSession`, `safetext.CheckSingleLine`.
- Produces:

```go
package queue

type Draft struct {
	ID          string    // "<lane>/<session>-<n>", assigned on Write
	Kind        string    // "handoff" | "memory"
	Lane        string
	Session     string
	When        time.Time
	Subject     string
	Name        string   // memory only: the slug
	Description string   // memory only
	Type        string   // memory only: metadata.type
	Body        string
}

func Write(storeDir string, d Draft) (Draft, error)
func List(storeDir string) ([]Draft, error)
func Get(storeDir, id string) (Draft, error)
func Remove(storeDir, id string) error
func Validate(d Draft) error
```

`Validate` returns nil for a `handoff` draft with a lane, session and non-empty
body. For `memory` it additionally requires `Name`, `Description` and `Type` —
this is the promotion contract, enforced here so an invalid draft can never
reach the tracked tree.

- [ ] **Step 1: Write the failing tests**

```go
func TestWriteThenGetRoundTrips(t *testing.T) {
	store := t.TempDir()
	in := queue.Draft{
		Kind: "handoff", Lane: "master", Session: "s1",
		When:    time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC),
		Subject: "why the retry window is 90s", Body: "- measured\n",
	}
	out, err := queue.Write(store, in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out.ID == "" {
		t.Fatal("Write returned no ID")
	}
	got, err := queue.Get(store, out.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != in.Body || got.Subject != in.Subject || got.Kind != in.Kind {
		t.Errorf("round trip lost content: %+v", got)
	}
}

func TestValidateRefusesAMemoryDraftMissingItsFrontmatter(t *testing.T) {
	base := queue.Draft{Kind: "memory", Lane: "master", Session: "s1", Body: "- x\n"}
	if err := queue.Validate(base); err == nil {
		t.Error("a memory draft with no name, description or type was accepted")
	}
	full := base
	full.Name, full.Description, full.Type = "retry-window", "why it is 90s", "reference"
	if err := queue.Validate(full); err != nil {
		t.Errorf("a complete memory draft was refused: %v", err)
	}
}

func TestWriteRefusesAnInvalidDraft(t *testing.T) {
	store := t.TempDir()
	if _, err := queue.Write(store, queue.Draft{Kind: "memory", Lane: "l", Session: "s", Body: "x"}); err == nil {
		t.Error("Write accepted a memory draft that Validate rejects")
	}
}

func TestQueueLivesOutsideTheWorkTree(t *testing.T) {
	// Structural: the queue path must be under the store, which is inside the
	// git common directory and therefore untrackable, rather than under a
	// directory kept out of commits by an ignore rule.
	root := newRepo(t)
	store, err := repo.StoreDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.Write(store, queue.Draft{
		Kind: "handoff", Lane: "master", Session: "s1", Body: "- x\n",
		When: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := mustGit(t, root, "status", "--porcelain", "--untracked-files=all")
	if strings.Contains(out, "queue") {
		t.Errorf("the queue is visible to git:\n%s", out)
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `cd agents && go test ./internal/queue/ -v`
Expected: FAIL, package does not exist

- [ ] **Step 3: Implement the package**

Store one markdown file per draft at `<store>/queue/<lane>/<session>-<n>.md`,
YAML frontmatter plus body, mirroring how `internal/handoff` and
`internal/memory` already serialize. Reuse `handoff.CheckSession` and the same
`checkComponent` rules for lane and session so a draft cannot name a path
component the handoff writer would later refuse. `Write` calls `Validate` first.

- [ ] **Step 4: Run and watch them pass**

Run: `cd agents && go test ./internal/queue/ -v`
Expected: PASS

- [ ] **Step 5: Add the `draft` verb**

```
agents handoff draft --lane <l> --session <id> [--kind memory --name <slug>
    --description <text> --type <t>] [--subject <text>]        # body on stdin
```

Same stdin contract as `handoff write`, same `--session` requirement, same
`exitcode.Malformed` on a bad flag and `NoRecord` on a failed write. It prints
the assigned ID so an agent can name it back at review time.

- [ ] **Step 6: Run the suite and commit**

```bash
cd agents && go test ./...
cd .. && git add agents/ && git commit -m "feat(agents): add an untracked draft queue and \`handoff draft\`"
```

---

### Task 5: `agents review` — promotion is one act

**Files:**
- Create: `agents/cmd_review.go`, `agents/cmd_review_test.go`
- Modify: `agents/main.go` (dispatch `review`), `agents/root.go` if the usage literal lives there
- Test: `agents/cmd_review_test.go`

**Interfaces:**
- Consumes: `queue.List/Get/Remove`, `handoff.Write`, `handoff.WriteIndex`,
  `memory.WriteIndex`, `repo.Git`, `repo.Discover`.
- Produces: `runReview(args []string, stdout io.Writer) int`.

- [ ] **Step 1: Write the failing tests**

```go
func TestKeepPromotesAHandoffAndCommitsScoped(t *testing.T) {
	root := newInitializedRepo(t)
	store, _ := repo.StoreDir(root)
	d, err := queue.Write(store, queue.Draft{
		Kind: "handoff", Lane: "master", Session: "s1",
		When: time.Now().UTC(), Body: "- the retry window is 90s\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A dirty code path must survive: promotion is scoped to .agents/.
	if err := os.WriteFile(filepath.Join(root, "code.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runReviewIn(t, root, []string{"--keep", d.ID}); code != exitcode.OK {
		t.Fatalf("review --keep exited %d", code)
	}
	if _, err := queue.Get(store, d.ID); err == nil {
		t.Error("the promoted draft is still in the queue")
	}
	files := mustGit(t, root, "show", "--stat", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, ".agents/reports/handoff/") {
		t.Errorf("the handoff was not committed:\n%s", files)
	}
	if strings.Contains(files, "code.go") {
		t.Errorf("promotion swept an unrelated code path into the commit:\n%s", files)
	}
}

func TestKeepRefusesAnInvalidMemoryDraft(t *testing.T) {
	root := newInitializedRepo(t)
	store, _ := repo.StoreDir(root)
	// Written through the raw file API so an invalid draft can exist at all.
	id := writeRawDraft(t, store, "master", "s1", "kind: memory\nlane: master\nsession: s1\n")
	if code := runReviewIn(t, root, []string{"--keep", id}); code != exitcode.Malformed {
		t.Fatalf("exit = %d, want Malformed", code)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "memory")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(root, ".agents", "memory"))
		for _, e := range entries {
			if !strings.EqualFold(e.Name(), "INDEX.md") && e.Name() != ".gitkeep" {
				t.Fatalf("an invalid draft reached the tracked tree as %s", e.Name())
			}
		}
	}
}

func TestKeepWarnsWhenPromotingMemoryOffTheDefaultBranch(t *testing.T) {
	root := newInitializedRepo(t)
	mustGit(t, root, "checkout", "-b", "feature")
	store, _ := repo.StoreDir(root)
	d, _ := queue.Write(store, queue.Draft{
		Kind: "memory", Lane: "feature", Session: "s1", When: time.Now().UTC(),
		Name: "retry-window", Description: "why it is 90s", Type: "reference",
		Body: "- measured\n",
	})
	var out bytes.Buffer
	code := runReviewInto(t, root, &out, []string{"--keep", d.ID})
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want OK: promotion must warn and proceed", code)
	}
	if !strings.Contains(out.String(), "feature") {
		t.Errorf("the warning did not name the branch:\n%s", out.String())
	}
}

func TestThereIsNoBulkPromotion(t *testing.T) {
	root := newInitializedRepo(t)
	if code := runReviewIn(t, root, []string{"--keep", "--all"}); code != exitcode.Malformed {
		t.Fatal("--keep --all was accepted; bulk promotion closes the review loop with no human in it")
	}
}

func TestBinRemovesWithoutTouchingTheTree(t *testing.T) {
	root := newInitializedRepo(t)
	store, _ := repo.StoreDir(root)
	d, _ := queue.Write(store, queue.Draft{
		Kind: "handoff", Lane: "master", Session: "s1",
		When: time.Now().UTC(), Body: "- x\n",
	})
	before := mustGit(t, root, "rev-parse", "HEAD")
	if code := runReviewIn(t, root, []string{"--bin", d.ID}); code != exitcode.OK {
		t.Fatalf("review --bin exited %d", code)
	}
	if _, err := queue.Get(store, d.ID); err == nil {
		t.Error("the binned draft survived")
	}
	if after := mustGit(t, root, "rev-parse", "HEAD"); after != before {
		t.Error("binning made a commit")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `cd agents && go test ./ -run TestKeep -v`
Expected: FAIL, `undefined: runReview`

- [ ] **Step 3: Implement `runReview`**

Order inside `--keep`, and the order matters — nothing reaches the tree until it
has been validated, and nothing is removed from the queue until it is committed:

1. `queue.Get`, then `queue.Validate`. Invalid → `exitcode.Malformed`, queue
   untouched.
2. `kind: memory` and the current branch is not the default → print a warning
   naming the branch, and continue.
3. Write: `handoff.Write(agentsDir, lane, session, handoff.StatusReviewed, body, when)`
   or the memory file plus `memory.WriteIndex`. A promoted draft is `reviewed`
   because a human selected it, which is what the index legend tells the reader
   the word means.
4. `git add -- .agents` then `git commit -m <subject> -- .agents`, the same
   sequence `agents save` uses, including its mid-merge refusal.
5. Only now `queue.Remove`. A failure at any earlier step leaves the draft
   recoverable.

`--keep` takes exactly one id. `--all` is not a flag; the flag set rejects it as
an unknown flag, which is what the test above pins.

- [ ] **Step 4: Run and watch them pass**

Run: `cd agents && go test ./ -run 'TestKeep|TestBin|TestThereIsNoBulk' -v`
Expected: PASS

- [ ] **Step 5: Add `--show`, `--edit`, and the bare listing; dispatch `review`**

Bare `agents review` lists pending drafts grouped by lane. `--show <id>` prints
one. `--edit <id>` opens `$EDITOR` on the queue file and returns without
promoting.

- [ ] **Step 6: Run the suite and commit**

```bash
cd agents && go test ./...
cd .. && git add agents/ && git commit -m "feat(agents): add \`agents review\` with scoped-commit promotion"
```

---

### Task 6: The capture instruction, and the checks that keep it honest

**Files:**
- Modify: `agents/internal/scaffold/scaffold.go:27-51`
- Modify: `agents/internal/doctor/doctor.go` (two checks added, one deleted)
- Modify: `CLAUDE.md` at the repository root
- Test: `agents/internal/scaffold/scaffold_test.go`, `agents/internal/doctor/doctor_test.go`

**Interfaces:**
- Produces: `scaffold.CaptureInstruction` (a `const string`), and doctor checks
  named `scaffold:capture-instruction`, `queue:pending`, `store:size`.

- [ ] **Step 1: Write the failing tests**

```go
func TestClaudeMDCarriesTheCaptureInstruction(t *testing.T) {
	if !strings.Contains(scaffold.ClaudeMD, scaffold.CaptureInstruction) {
		t.Error("the scaffolded CLAUDE.md does not carry the capture instruction")
	}
	// It must say when, not only how: an instruction that names the tool and
	// not the moment is what produced zero handoffs in twenty sessions.
	if !strings.Contains(scaffold.CaptureInstruction, "concludes") {
		t.Error("the capture instruction does not name the moment it applies")
	}
	if !strings.Contains(scaffold.CaptureInstruction, "agents handoff draft") {
		t.Error("the capture instruction does not name the command")
	}
}

func TestDoctorReportsAMissingCaptureInstruction(t *testing.T) {
	root := newInitializedRepo(t)
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# nothing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := runDoctor(t, root)
	c := findCheck(t, checks, "scaffold:capture-instruction")
	if c.Status != doctor.Warn {
		t.Errorf("Status = %q, want warn: under phase B' this instruction is the capture mechanism", c.Status)
	}
}

func TestDoctorNoLongerReportsUnreachablePointers(t *testing.T) {
	root := newInitializedRepo(t)
	checks := runDoctor(t, root)
	for _, c := range checks {
		if c.Name == "pointers:local-unreachable" {
			t.Error("the unreachable-pointer check survives; it reported a race the design cannot win")
		}
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `cd agents && go test ./internal/scaffold/ ./internal/doctor/ -run 'Capture|Unreachable' -v`
Expected: FAIL, `undefined: scaffold.CaptureInstruction`

- [ ] **Step 3: Add the instruction to the scaffold**

```go
// CaptureInstruction is the capture mechanism under spec 7 phase B'.
//
// The sentence it replaces -- "Write handoffs with `agents handoff write`, not
// by hand" -- instructs HOW and never WHETHER or WHEN. An agent following it
// perfectly writes zero handoffs, which is what twenty sessions produced. This
// one names the moment, bounds the output, and says the cost is nothing,
// because an unbounded ask reads as expensive and gets deferred.
const CaptureInstruction = "When a stretch of work concludes — a bug understood, a decision made, an approach abandoned — record it before moving on: at most three bullets, covering what a future agent could not get from the code or the git log. Write it with `agents handoff draft --lane <lane> --session <id>`. Drafts are untracked until you review them, so drafting costs nothing and commits you to nothing."
```

Replace the trailing paragraph of `ClaudeMD` with `CaptureInstruction` plus a
line pointing at `agents review`.

- [ ] **Step 4: Add and remove the doctor checks**

Add `scaffold:capture-instruction`, modelled exactly on the existing
`scaffold:doctor-instruction` check. Add `queue:pending` (count per lane, `OK`
when empty, `Warn` past a threshold) and `store:size` (against the retention
caps). Delete the `pointers:local-unreachable` check and its remedy string.

- [ ] **Step 5: Run and watch them pass**

Run: `cd agents && go test ./internal/scaffold/ ./internal/doctor/ -v`
Expected: PASS

- [ ] **Step 6: Update this repository's own `CLAUDE.md`**

`scaffold.Create` never rewrites an existing `CLAUDE.md`, so this repository's
copy is edited by hand to carry the same paragraph — otherwise the tool would
report a warning against itself.

- [ ] **Step 7: Run everything and commit**

```bash
cd agents && gofmt -l . && go vet ./... && go test ./...
cd bootstrap.d && go vet ./... && go test ./...
cd .. && git add agents/ CLAUDE.md && git commit -m "feat(agents): give CLAUDE.md a capture instruction that names the moment"
```

---

## Self-Review

**Spec coverage.** §1 corrected rule → Tasks 1 and 3. §2 one store → Task 1;
retention → Task 2. §3a instruction → Task 6. §3b and §3c → deliberately not
built, per the Global Constraints. §4 review, promotion, no bulk promote,
bounded drafts → Tasks 4 and 5. §5 retires → Tasks 3 and 6. §6 migration →
Task 3. Boundary notifications (`post-checkout` printing pending counts) are
**not** in this plan: they are a convenience whose absence does not block the
loop, and Task 2 already touches `post-merge`. Recorded here so the omission is
deliberate rather than missed.

**Placeholders.** None. Every code step carries the code.

**Type consistency.** `queue.Draft` is the only new type crossing tasks; Task 5
uses exactly the fields Task 4 defines. `repo.StoreDir` has one signature
throughout. `PruneReport` gains `Details` in Task 2 and is not otherwise
reshaped.
