# Make the trace cache actually preserve transcripts

> **Executed** on branch `claude/trace-cache`. All nine tasks landed. Two
> decisions changed during execution, both on review and both recorded in place:
> pruning is spelled `agents trace cache prune` rather than `trace prune`,
> because naming the object is what distinguishes removing copies from removing
> the index; and the "most recent N" retention claim in the Context below was
> disproved by a wider census, corrected here and in
> `.agents/memory/harness-transcript-retention.md`.
>
> One acceptance criterion is not yet demonstrable and is not a gap in the work:
> reading back a transcript the harness has deleted requires one to have been
> cached *and then* dropped. At the time of writing no transcript is in that
> state — the 25 already lost were never cached, and the 224 cached still have
> live sources. Auto-caching at `subagent-stop` is what closes that window, so
> the case arrives with time rather than with more code.

## Context

`agents` records a pointer to every harness transcript, and `agents trace cache`
copies reachable ones into `.agents/.trace-cache/`. A census on this machine
showed the mechanism is not doing its job:

- Main checkout: 27 pointers → 15 unique transcripts → **2 still on disk**.
- A live session, still running: 243 pointers → 83 transcripts → **25 already gone**.

The 25 were recorded at `subagent-stop`, when the file demonstrably existed, and
vanished before the same session ended. Claude Code prunes subagent transcripts
**during** a session, and the rule is not one we can predict. Plotting the 111
subagent-stop records of one session oldest-first (`o` present, `X` gone):

```
XXXooXooXooXoXooooooooooooooooooooXooXoooXoooooXooXooXooooooooooooXXXooXoooooooXoooooooXXoooXXXXoXooooooooooooo
```

Casualties are scattered, some of the very oldest survive, and the newest ~15 are
all present. So it is neither "keep the most recent N" nor age-ordered — which
rules out any policy of the form "cache it once it is old enough". `-since 30d`
spans a window in which most content is already gone.

The one thing the shape does establish: at and shortly after the stop event, the
file reliably exists. That makes `subagent-stop` the earliest moment a complete
child transcript is on disk, and the unpredictability means every one must be
taken rather than chosen between.

Four gaps, each independently fatal to the feature:

1. **The cache is write-only.** Nothing reads it. `.trace-cache` appears in
   exactly two non-test places: `internal/trace/cache.go` (writes) and
   `internal/scaffold/scaffold.go:74` (the `.git/info/exclude` entry).
2. **`doctor` ignores it.** `internal/doctor/doctor.go:566` decides reachability
   with `os.Stat(rec.Transcript)` on the original harness path only. A
   successfully cached transcript still counts as unreachable forever — the
   remedy the warning prints ("cache reachable transcripts before harness
   cleanup") can never clear the warning.
3. **It is per-worktree.** `.trace-cache` sits under `repo.AgentsDir(rc.Root)`,
   and every linked worktree has its own root. Today: main holds 3, one worktree
   holds **58 (36 MB)**, another holds none. The 58 die with that worktree.
4. **It is manual and late.** By the time anyone runs it, ~30% is unrecoverable.

Intended outcome: transcripts are captured before the harness can delete them,
one cache serves every worktree, `doctor` tells the truth about what is lost
versus what was saved, and — the point of the whole mechanism — **a cached
transcript can be read back and turned into a memory entry**.

That last one is the acceptance criterion, not a nicety. A memory entry written
with `agents save` is meant to be derived from what actually happened, and its
`sources:` frontmatter points at a transcript by `agent_id` precisely so the
derivation can be checked or extended later. If the only surviving copy is in
the cache and nothing can read it, the pointer is a dead reference and every
insight has to be reconstructed from memory instead of evidence. This is also
the foundation the eventual distillation step would stand on, so getting the
read path right now costs nothing extra and getting it wrong blocks that later.

## Decisions

**Cache moves to the git common directory**, `<common-dir>/agents/trace-cache/`.
Precedent is `repo.InfoExcludePath` (`internal/repo/repo.go:34-50`), which
already resolves a machine-local file via `--git-common-dir` with the reasoning
that a worktree shares the common dir with its main checkout. Consequences: one
cache for all worktrees, it outlives any single worktree, and it is never inside
a working tree — git structurally never tracks its own directory, so the
protection `writeCacheIgnore` was built to provide is inherent rather than
maintained. Both it and the `scaffold.go:74` exclude entry get deleted, not kept
as belt-and-braces.

*Not `~/.cache`*: XDG defines `$XDG_CACHE_HOME` for **non-essential,
regenerable** data, and cleaners treat it accordingly. Once the harness deletes
a transcript the cached copy is the only one that will ever exist. The cache is
also per-repository; the common dir provides that namespacing for free.

**Auto-cache fires only at `subagent-stop`.** Measured: subagent transcripts
mean 424 KB, max 1.1 MB, and — per the timeline above — are present at the stop
event and deleted unpredictably later. That is the earliest moment a complete
child transcript exists. The session transcript is 12.9 MB and is referenced by
~30 `stop` events per day; copying it on a blocking hook is quadratic in session
length, and `Cache`'s skip-if-`dst`-exists rule would freeze the first partial
copy forever. Subagent transcripts have neither problem and are exactly the
class being lost.

**Pruning is by lane, explicit, and never inferred from git.** A cache that only
grows is its own problem, but "the branch is gone" does not mean "the content is
irrelevant": throwaway worktrees are often the informational ones, and a merged
branch *should* be deleted. So the tool never derives prunability from branch or
worktree existence. `agents trace cache prune --lane <name>` names the lane
explicitly, reports what it would remove, and requires confirmation — the same
refuse-never-clobber posture the rest of this repo takes, applied to the one
copy of something that cannot be regenerated.

**Spelled `trace cache prune`, because the object it names is the thing that
makes it safe.** There are two prunable things here and only one of them may
ever be pruned: the *cache* holds copies, while the *index*
(`.agents/reports/traces/*.jsonl`) is the durable record of what existed at all
— tracked, merged across machines, and the only thing that can still say a
transcript was ever taken. A bare `agents trace prune` does not say which it
touches, and the wrong reading destroys the history rather than the copies.
Naming the noun removes the ambiguity from the command itself rather than from
its documentation.

`prune` rather than `rm` because this repo already uses that word for exactly
this meaning, in `agents ls --prune` and `agents handoff prune`. It is a
subcommand rather than a flag on `cache` because hanging a destructive
operation off the flag surface of a copying one hides it.

## Tasks

Order matters twice: **task 6 must land with or immediately after task 1**,
because task 1 changes where the cache lives and would otherwise strand 36 MB of
already-salvaged transcripts; and **tasks 3 and 4 both consume task 2's
`Resolve`**. The rest are independent.

### 1. Resolve the cache directory from the git common dir

- Add `repo.TraceCacheDir(dir string) (string, error)` in
  `internal/repo/repo.go`, next to `InfoExcludePath` and built the same way
  (`rev-parse --git-common-dir`, absolutise against `dir`, join
  `agents/trace-cache`).
- Change `trace.Cache` (`internal/trace/cache.go:39`) to take the resolved cache
  root instead of deriving it from `agentsDir`. Update the caller
  `runTraceCache` (`cmd_trace.go:185`).
- Delete `writeCacheIgnore` (`internal/trace/cache.go:104-124`), its
  `cacheIgnoreBody` constant, and the `/.agents/.trace-cache/` entry at
  `internal/scaffold/scaffold.go:74`. Both exist solely because the cache used
  to sit in a working tree; git never tracks its own directory, so keeping them
  would assert a protection that no longer has a threat.
- Re-point `TestTraceCacheContentIsNotStageable` at the new location. The
  property — transcript text can never be staged — is the reason this move is
  safe, so it must still be asserted, now against the common dir.

Test that two worktrees of one repo resolve to the same cache root, and that a
non-repo directory returns `repo.ErrNotARepo`.

### 2. Resolve a record to readable content

The primitive everything else needs. Two exported functions in
`internal/trace`:

- `CachedPath(cacheRoot string, rec record.Record) string` — extract the
  destination rule already inside `Cache` (`cache.go:85`), reusing `harnessDir`
  and `cacheName` unchanged. It is already deterministic from
  `(harness, transcript path)`, which is what makes this possible.
- `Resolve(cacheRoot string, rec record.Record) (path, origin string, err error)`
  — return the original transcript if it is still there, otherwise the cached
  copy, otherwise an error naming both paths tried. `origin` is `"source"` or
  `"cache"` so a caller can say where the bytes came from.

`Resolve` is the seam a later distillation step consumes, so keep it a pure
lookup: no reading, no parsing, no harness-specific knowledge.

### 3. A command that reads a transcript back

Without this the cache stays write-only and task 2 is unused.

Add `agents trace show <id>` where `<id>` prefix-matches `agent_id` or
`session_id` from the trace index:

- default: write the resolved transcript to stdout, so it pipes.
- `--path`: print the resolved path only, for a caller that wants to open it
  itself.
- report on stderr which origin answered, and exit `5`
  ("could not complete the operation", per the existing table) when neither
  the source nor the cache holds it.
- an ambiguous prefix lists the candidates and exits `3` (malformed input)
  rather than guessing.

Reuse `trace.Query` for record lookup rather than re-reading the index.

### 4. Make `doctor` consult the cache

- Rewrite the reachability loop at `internal/doctor/doctor.go:552-572` to split
  verified local pointers three ways, using `Resolve`: reachable at source;
  **gone but cached**; gone entirely. Only the last is a `Warn`. Report the
  saved count as `ok` so the remedy visibly works.
- Update the remedy text: it currently says "cache reachable transcripts before
  harness cleanup", which is right but unachievable while caching never clears
  it.

This is the task that turns the warning into a true statement. Pin it with a
test where the source is deleted and the cached copy exists — that case must not
be a warning.

### 5. Auto-cache at `subagent-stop`

In `recordHook` (`cmd_hook.go:39-105`), after the `Append` succeeds and only when
`event == harness.SubagentStop`, cache that one record.

Hard constraints, from the existing contract at `cmd_hook.go:19-30`:

- `runHook` must keep returning 0 on every path. A failed copy is reported on
  stderr, never propagated.
- Cache **only the record just written**, never a query over the index.
- Skip silently if the pointer did not verify or the machine does not match —
  reuse `Cache`'s existing rules rather than restating them.

The cheapest correct shape is to call `trace.Cache(cacheRoot, mid,
[]record.Record{rec})` with the single record, so all the safety already in
`Cache` (Lstat not Stat, regular-file check, `harnessDir` sanitising, atomic
rename) applies unchanged.

Add an opt-out — `AGENTS_NO_AUTO_CACHE` or equivalent — because this writes to
disk on every subagent completion.

### 6. Migrate the existing caches

Two populated caches exist and must not be orphaned:
`~/dotfiles/.agents/.trace-cache` (3 files) and
`.claude/worktrees/spec-2-dotfiles-hygiene-5f92d4/.agents/.trace-cache`
(58 files, 36 MB — the salvage from this session).

`cacheName` is a hash of the **source path**, not of the cache location, so
existing filenames stay valid: moving is a plain file move, no renaming. Do it
as an explicit step in `agents trace cache` that reports what it moved, not
silently at startup, and leave the old directory in place if anything fails.

### 7. Re-copy a source that has grown

`Cache` skips when the destination exists (`cache.go:86-89`), so a transcript
cached while still being written is frozen at that size forever. This is latent
today and becomes live the moment task 3 caches at `subagent-stop`: if the
harness flushes the tail after the hook fires, the cached copy is short and
nothing will ever correct it.

Change the skip to compare sizes: re-copy when the source is larger than the
cached copy, skip when it is the same size or smaller. `copyFile` already writes
to `.partial` and renames, so a re-copy is atomic and an interrupted one cannot
truncate what is already held. This also repairs any partial copy taken before
the change, without a migration.

Do not compare mtime — a restored or touched source would trigger pointless
copies of a 12.9 MB file.

### 8. Lane-scoped pruning

Add `agents trace cache prune --lane <name>`: report the transcripts it would
remove and their total size, and remove only on confirmation. Never infer
prunability from whether a branch or worktree still exists — a deleted branch is
usually a *merged* one, and throwaway worktrees are frequently the informational
ones. The lane must be named by a human.

It removes cached **copies** only. The index is never touched: it is what
records that a transcript existed, and losing it loses the knowledge that
anything was there to look for.

Records carry `Lane` already (`record.Record`), and `trace.Query` filters on it,
so this reuses the existing filter rather than adding a second notion of scope.

### 9. Correct the memory entry

`.agents/memory/harness-transcript-retention.md` (committed as `91de8e7`) says
Claude Code retains "roughly the most recent N" subagent transcripts. The
111-record timeline above disproves that — losses are scattered, and some of the
oldest survive. Replace that sentence with the timeline and the conclusion it
actually supports: the rule is unpredictable, so capture must be immediate and
unconditional rather than scheduled.

The entry's practical advice is unaffected and stays. Update it with `agents
save` and regenerate the index with `agents index`, per `CLAUDE.md`.

## Verification

- `cd agents && go test -count=1 ./...`. `-count=1` is mandatory: several tests
  read tracked non-Go files and the build cache does not notice.
- `gofmt -l` empty, `go vet ./...` clean.
- Two-worktree check: run `agents trace cache` from a linked worktree, then
  confirm from the main checkout that the transcripts are present and that
  `agents doctor` counts them as saved rather than unreachable.
- End-to-end on the real gap: note the current unreachable count from `agents
  doctor`, delete a source transcript whose copy is cached, re-run `doctor`, and
  confirm it moves to the saved bucket instead of the warning.
- **The acceptance criterion — read back and derive.** Take a transcript whose
  source the harness has already deleted and whose only copy is cached; read it
  with `agents trace show <agent-id>`; confirm the content is intact JSONL and
  the run actually happened as recorded. Then write a memory entry from it with
  `agents save`, citing that `agent_id` in `sources:`, and confirm `agents
  doctor` reports the source as locally classified rather than unreachable. If
  this cannot be done, the rest of the plan has preserved bytes nobody can use.
- Auto-cache: after a session that spawns subagents, confirm each
  `subagent-stop` record has a cached copy without anyone running `trace cache`.
  This is the whole point — the 25 lost in one live session are the baseline to
  beat.
- Confirm the cache is invisible to git from every worktree: `git status
  --porcelain` clean, and `git add -A` stages nothing from it — with
  `writeCacheIgnore` deleted, so the guarantee comes from the location alone.
- Re-copy: cache a file, append to the source, re-run, confirm the cached copy
  grew. Then shrink the source and confirm it does **not**.
- Prune: `agents trace cache prune --lane <name>` on a lane with content reports
  and stops; only a confirmed run removes anything, and `agents trace ls` still
  lists every record for that lane afterwards — copies go, the index stays.

## Out of scope

Session-transcript auto-caching (size and staleness, per Decisions), cross-machine
transport (`agents distill`, spec 3), and any change to what a pointer records.
