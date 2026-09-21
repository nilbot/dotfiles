# Bootstrap Self-Sufficiency Implementation Plan

> **PARTLY SUPERSEDED 2026-09-21.**
> - **Task 1 (`linkdir`) is superseded** by
>   [`2026-09-21-simplification-plan.md`](2026-09-21-simplification-plan.md),
>   Task 4: the blocker is removed by deleting the manifest rows, not by adding a
>   manifest kind. Its `change.Interface` widening and `linkdir` kind are not
>   built.
> - **Task 2 (the Go path) survives** as Task 6 of that plan, with two measured
>   corrections: `TestMissingGoRefusesWithTheInstallCommand` is a **warm-cache**
>   case and would exit 0 after the reorder, and the proposed
>   `TestGoOutsidePathIsFoundAndUsed` stub is shadowed by `/opt/homebrew/bin/go`
>   on this machine.
>
> Kept as the record of the Go analysis, which is still the source of that work.
> **Do not execute Task 1.**

**Status: Task 1 superseded, Task 2 carried forward.**

**Goal:** Make `bootstrap` work on a machine it does not fully control, in the two
places where it currently cannot: a `$HOME` directory that another program also
writes into, and a machine whose Go is either absent, off `PATH`, or installed
by something other than Homebrew.

**Architecture:** Two independent changes with one property in common — each
removes a hard dependency rather than adding a capability.

- **Task 1** adds a fourth manifest kind, `linkdir`, whose target is a real
  directory shared with another program. It links one entry per source child and
  never reads, writes or reports anything else in that directory. It introduces
  no new per-entry verdict logic: each entry goes through the existing
  `linkVerdict`, so `planner` and `applier` cannot drift apart. It does widen
  `change.Interface` by two methods, and that widening has a measured blast
  radius — three sub-interfaces, two fakes, and one hardcoded method count.
- **Task 2** moves the shim's Go check inside the branch that actually needs Go,
  searches a fixed list of install locations before giving up, names an
  installer *this machine has* rather than one its kernel usually implies, and
  ends with a platform-specific place to get Go when no installer exists.

**Why these two, now.** They are the first cut of the simplification recorded in
[the boundary review](../design/2026-09-20-agents-and-bootstrap-boundary.md),
and they are the two steps that block or precede every other one:

1. Task 1 resolves the review's §7 item 1, which currently stops `bootstrap plan
   workstation` at `config` — so nothing after that phase runs on this machine.
2. Task 2 is the precondition for deleting the release pipeline. `spec 2 §2.2`
   names release distribution as *"the seam where spec 6 removes Go as a
   dependency."* If that seam is not filled, Go becomes a permanent dependency
   guarded by exactly one manual step. Everything Task 2 adds lands inside the
   shim's existing and only job — its own comment already says *"its whole job is
   to reach Go and hand over."*

**Deliberately out of scope:** deleting `trace`, `layout`, `drift`, the fleet
registry, or the release pipeline; retiring `agents`; rewriting `agents guard`
as a repository script. This plan touches `bootstrap/` and one manifest row.

**Spec:**
[spec 2 — dotfiles hygiene](../design/2026-08-07-spec-2-dotfiles-hygiene.md) §2.1
(the shim, and the one manual step), §5 (refuse, never clobber), §6 (the
manifest) · [the boundary review](../design/2026-09-20-agents-and-bootstrap-boundary.md)
§4b (the incident), §6 rule 2 (a declaration must not claim a path another
system writes into), §7 item 1 · [the incident
journal](../journal/2026-09-19-harness-skills-wrote-into-the-checkout.md) ·
[why is `~/.claude/skills` a real directory](../qna/why-is-claude-skills-a-real-directory.md)

---

## Global Constraints

Properties the existing code and suite enforce. A change that breaks one is
wrong even if its own tests pass.

1. **The shim has exactly two ways out.** One `exit`, inside `die()`; one
   `exec`, the handover. `TestShimHasExactlyTwoWaysOut` reads `bootstrap` as
   text and counts them, skipping whole-line comments only. A helper added to
   the shim must use `return`, never `exit`. It finds `die()`'s closing brace as
   the first column-0 `}`, so keep `die` and any new function defined at column 0.
2. **No `${VAR:?}` in the shim.** It exits 1 — "advisory" — where a hard stop
   needs 2. The same test rejects it.
3. **Refuse, never clobber.** An occupied target is refused, never overwritten,
   and the refusal carries a remediation naming the manual step. `Refusal` reads
   `refusing: <path>: <problem>; <remediation>`.
4. **One verdict per rule, consulted by both paths.** `Planner` and `Applier`
   must reach the same answer about the same machine state, so every new
   operation goes through a shared `*Verdict` function rather than duplicating
   its `switch` in both methods. Divergence between the two is the defect class
   this package's comments name five separate times.
5. **`IsDir` means a real directory.** In `internal/change` a symlink to a
   directory is `IsLink`, never `IsDir`. Deliberate: writing into a symlinked
   tree is the "wrong tree" failure the shim's root checks exist to prevent.
6. **A kind added to `manifest` must be handled in both `phase.Config` and
   `check.rowProblem`.** Both switch on `manifest`'s own constants and both have
   a `default` arm that fails loudly, so an unhandled kind is a reported fault
   rather than a silently skipped row.
7. **Plan output matches apply output.** For any machine state, the lines `plan`
   prints are the lines `apply` prints, in the same order. A shared verdict is
   necessary but not sufficient; the operation's own ordering must be
   deterministic too (`os.ReadDir` sorts by name).

Verification commands, from `bootstrap.d/`:

```sh
gofmt -d . && go vet ./... && go test -count=1 ./...
```

---

### Task 1: `linkdir` — a manifest kind that shares a directory

**The problem this solves.** `links.manifest:20` declares
`link claude/skills .claude/skills`. `link`'s contract is *"nothing else writes
to this path"*, and a harness does: on 2026-09-19 it wrote its own skill content
through that symlink and into the tracked working tree, 200+ files deep. The
path was replaced by a real directory holding per-skill links, so the
declaration now names a path the machine deliberately does not satisfy, and
`bootstrap plan workstation` exits 2 at `config`.

**Why a new kind rather than dropping the row.** The review's rule 2 says a
declaration must not claim a path another system writes into; it does not say
the path must go unmanaged. Manifest kinds are single-owner by construction
(`link` owns a path, `seed` owns its first write, `dir` owns a directory), and
what this path needs is *"we own three named entries inside a directory we do
not own."* `linkdir` states exactly that, so the fact that a harness writes here
becomes a row in the manifest instead of a paragraph of prose. It also covers
`~/.gemini/skills`, which is a whole-directory symlink today and is the same
defect one harness away.

**Files:**
- Modify: `bootstrap.d/internal/manifest/manifest.go` (kind constant, accepted set)
- Modify: `bootstrap.d/internal/manifest/manifest_test.go`
- Modify: `bootstrap.d/internal/change/change.go` (interface methods, two verdicts)
- Modify: `bootstrap.d/internal/change/applier.go`
- Modify: `bootstrap.d/internal/change/planner.go`
- Create: `bootstrap.d/internal/change/linkdir_test.go`
- Modify: `bootstrap.d/internal/phase/config.go`, `bootstrap.d/internal/phase/phase.go`
- Modify: `bootstrap.d/internal/phase/preflight_test.go` (`fakeChange`)
- Modify: `bootstrap.d/internal/check/checks.go`, `bootstrap.d/internal/check/check.go`
- Modify: `bootstrap.d/internal/check/check_test.go` (`fakeChange`)
- Modify: `bootstrap.d/internal/migrate/migrate.go`, `bootstrap.d/internal/migrate/migrate_test.go`
- Modify: `bootstrap.d/links.manifest`
- Modify: `bootstrap.d/main_test.go` (end-to-end regression case)
- Modify: `docs/design/2026-08-07-spec-2-dotfiles-hygiene.md` (kind table)
- Modify: `docs/design/2026-09-20-agents-and-bootstrap-boundary.md` (§7 item 1 → resolved)

**The contract, stated once:**

```
linkdir  <repo-relative source dir>  <$HOME-relative target dir>  <platform>
```

- **Source** must be a real directory in the checkout. A missing source, or one
  that is not a real directory, refuses.
- **Target:**
  - *missing* → created, then every source child is linked into it;
  - *a real directory* → every source child is linked into it; correct links are
    no-ops;
  - *a symlink* → **refused**, with a remediation that names this as the
    whole-directory handover a harness's writes land in. Following it re-arms
    the incident; deleting it silently destroys a decision the operator may have
    made deliberately.
  - *a regular file* → refused.
- **Each child `X`** obeys `linkVerdict` unchanged: absent → link it; a symlink
  to the right place → no-op; a symlink elsewhere, or a non-symlink → refused.
- **Anything in the target the source does not name is never read, written,
  moved or reported.** That sentence is the whole difference from `link`:
  `synced/` survives, and so does whatever the next harness invents.

---

- [ ] **Step 1: Write the failing manifest test**

Add to `bootstrap.d/internal/manifest/manifest_test.go`:

`TestParseAcceptsLinkDir` — parse `linkdir  claude/skills  .claude/skills  *`
and assert one row with `Kind == KindLinkDir` and the source and target fields in
the right slots. A kind parsed into the wrong field is the failure the keyed
literal comment in `Parse` exists to prevent, so the test must be able to see it.

No test for a `-` source. A `linkdir` row with `-` produces a source path that
does not exist, and `linkSourceVerdict`'s precedent is that a missing source is a
*refusal* (exit 2), not malformed input (exit 3) — the same treatment `link` gets.
Keeping that consistent is worth more than an extra syntax rule.

Run: `go test ./internal/manifest/` — fails with `unknown kind "linkdir"`.

- [ ] **Step 2: Implement `manifest.KindLinkDir`**

In `manifest.go`: add `KindLinkDir Kind = "linkdir"` to the constant block with a
comment in its neighbours' voice — what it owns, and when to use it — and add it
to `Parse`'s accepted set. Nothing else in this package changes; `For` and
`DuplicateTargets` are kind-agnostic.

Run: `go test ./internal/manifest/` — green. `TestRealManifestIsWellFormed`
still passes here; it starts exercising the new kind in Step 7.

- [ ] **Step 3: Write the failing change tests**

Create `bootstrap.d/internal/change/linkdir_test.go`. Every case runs against
**both** paths — the `Planner` over a real reader, and the `Applier` — and
asserts they agree, following the `...OnBothPaths` shape and the table helper
already in `planner_test.go` (see `TestSeedRefusesUnusableSourceOnBothPaths` for
the idiom).

- `TestLinkDirLinksEachEntryOnBothPaths` — a source with two child directories; a
  target that does not exist. Assert one `create directory` line and exactly two
  `link` lines in name order; after apply, both links resolve to `source/X`.
- `TestLinkDirPreservesForeignEntriesOnBothPaths` — the target exists as a real
  directory containing `synced/` (itself a real directory holding a file) and one
  unrelated regular file. Assert both survive byte-for-byte with unchanged
  modification times, and that **neither name appears in the output of either
  path**. This is the test the kind exists for.
- `TestLinkDirRefusesASymlinkedTargetOnBothPaths` — the target is a symlink to a
  real directory. Assert both paths refuse, the message contains `is a symlink`,
  and nothing was created at the destination.
- `TestLinkDirRefusesAnOccupiedEntryOnBothPaths` — the target exists and already
  contains a real directory named like one of the source's children. Assert both
  paths refuse and the occupied entry is untouched. The refusal lands mid-loop, so
  also assert the two paths produced the **identical prefix** of work before it —
  that prefix assertion is what catches an ordering divergence.
- `TestLinkDirRefusesAMissingSourceOnBothPaths` — nothing at the source path.
- `TestLinkDirIsIdempotent` — apply twice against the same tree; the second run
  prints nothing and changes nothing. This also pins the ordering constraint 7
  depends on.

Run: `go test ./internal/change/` — fails to compile. That is the expected first
failure for a method that does not exist.

- [ ] **Step 4: Implement `change.LinkDir` and its verdicts**

In `change.go`, add to `Interface`:

```go
ReadDir(path string) ([]string, error)
LinkDir(source, target string) error
```

Two verdicts, named for the side they judge. **Both return only an error**: there
is no `verdictSatisfied` for a target directory, because an existing directory
still has entries to check.

- `linkDirTargetVerdict(info FileInfo, target string) error` — missing and real
  directory both proceed; a symlink refuses with the incident's lesson in the
  remediation (*"replace it with a real directory deliberately, then retry; a
  symlink here hands the whole directory to this checkout"*); anything else
  refuses with `moveAside`.
- `linkDirSourceVerdict(info FileInfo, source string) error` — exists, and
  `IsDir` (a real directory, per constraint 5; a symlinked source refuses and
  suggests naming the real directory).

In `applier.go`:

```go
func (a *Applier) LinkDir(source, target string) error {
    // a.Lstat(target) -> linkDirTargetVerdict
    // a.Lstat(source) -> linkDirSourceVerdict
    // a.Dir(target)     // creates it when absent, reports once
    // a.ReadDir(source) -> names
    // for each name: a.Link(filepath.Join(source, name), filepath.Join(target, name))
}
```

No summary line of its own: each child's `Link` reports its own line and `Dir`
reports the directory once, which is what makes plan and apply produce identical
output for free (constraint 7). And `Applier.ReadDir` is `os.ReadDir` mapped to
names — document there that the sorted order is load-bearing, not incidental.

In `planner.go`, the same method, reading the target kind through `p.Lstat` (so a
directory an earlier row planned into existence is honoured) and the source's
names through `p.reader.ReadDir`, **forwarded directly**. Add a comment in the
voice of `Planner.Seed`'s explaining why forwarding is correct rather than an
overlay gap: a `linkdir` source is always a tracked directory in the checkout, no
row creates one, so there is nothing for the overlay to satisfy.

Run: `go test ./internal/change/` — green.

- [ ] **Step 5: Wire it through, and absorb the widening**

Two new `Interface` methods do not stay local. This step is the whole blast
radius, measured on this tree:

1. **`phase/config.go`** — add `case manifest.KindLinkDir: err = c.Change.LinkDir(source, target)`
   beside the existing three. The `default` arm already names an unhandled kind.
2. **`phase/phase.go`** — add `LinkDir(source, target string) error` to
   `phase.Machine`. It is a converging operation like `Dir`/`Link`/`Seed`, which
   is exactly what that interface exists to admit, so extend its doc comment's
   list rather than leaving the interface silently one short.
3. **`check/checks.go`** — add a `KindLinkDir` case to `rowProblem`: the target
   must be a real directory, and each source child must be a symlink at
   `target/<name>` pointing at `source/<name>`. Report the first problem found, in
   the existing `describe(info) + ", want ..."` idiom; name the child in the
   message when the fault is a child's, since the caller only prefixes `~/<target>`.
4. **`check/check.go`** — add only `ReadDir(path string) ([]string, error)` to
   `check.Machine`. It is a read, so it does not weaken that interface's purpose —
   say so in the comment, next to the sentence explaining why `Run` was allowed to
   stay. `LinkDir` must **not** be added here: a check that could link would be a
   check that mutates.
5. **`migrate/migrate.go`** — add both methods to `migrate.Machine`, which is
   `change.Interface` entire by definition.
6. **`migrate/migrate_test.go`** — `TestTheTwoMigrateInterfacesAreThirteenAndThree`
   asserts `machine.NumMethod() != 13` twice and is named for the number. Rename it
   to `...AreFifteenAndThree` and update both counts to 15. In the same test, the
   loop listing methods `migrate.Reader` must not expose gains **`LinkDir`** —
   it writes, and `Reader` is what preflight uses to decide whether a migration
   applies.
7. **`phase/preflight_test.go`'s `fakeChange`** — must gain `LinkDir`, or it stops
   satisfying `phase.Machine`. It should gain `ReadDir` as well, because its doc
   comment claims it satisfies `change.Interface` entire, and a comment that stops
   being true is worse than the four lines it costs to keep it.
8. **`check/check_test.go`'s `fakeChange`** — must gain `ReadDir`, or it stops
   satisfying `check.Machine`. It does **not** gain `LinkDir`: that interface
   deliberately excludes it.
9. **Both `ReadDir` fakes return names sorted**, as the real one does. A fake that
   returns map-iteration order would make a plan/apply divergence untestable in
   exactly the case constraint 7 is about.

Run: `gofmt -d . && go vet ./... && go test -count=1 ./...` — green, and the
migrate count test is the one to watch: it is the only place the widening is
asserted rather than assumed.

- [ ] **Step 6: Change the manifest row, then run the real thing on this machine**

In `bootstrap.d/links.manifest`, change the `claude/skills` row's kind from `link`
to `linkdir`, and add a row to that file's header kind table — same three columns
as its neighbours, padded to the same width:

| kind | what it means | use when |
|---|---|---|
| `linkdir` | per-entry symlinks inside a real directory | a program writes its own files there too |

Then, from the repository root:

```sh
./bootstrap plan dotfiles      # must exit 0 — this is the blocker cleared
./bootstrap plan workstation   # must get past the config phase
./bootstrap apply workstation
./bootstrap check              # must report manifest-kinds as satisfied
ls -la ~/.claude/skills
```

Expected on this machine: the plan reports linking the three entries
`~/.claude/skills` does not yet have (`agents-tool`, `codebase-memory`,
`recording-what-you-learn`); `antigravity-handoff` already points at
`<root>/claude/skills/antigravity-handoff` and is not reported; `synced/` is not
mentioned at all.

If `plan workstation` stops somewhere other than `config`, that is a different
defect — record it and do not fix it inside this task.

- [ ] **Step 7: Add the end-to-end regression case**

In `bootstrap.d/main_test.go`, following `TestCheckOnABareHomeNamesTheMissingRows`
for the shape (`tempHome(t)` plus `runShim(t, home, "plan", "dotfiles")` — the
shim runs the real checkout, and `plan` is read-only, so no copy is needed):

`TestPlanSucceedsWhenAManagedSkillsDirectoryIsShared` — a temporary home whose
`.claude/skills` is a real directory containing a `synced/` directory and one
regular file whose name no source child uses. Assert exit 0; assert the output
names the source's children; assert it does **not** contain `synced`.

Before Step 6 this case exits 2 with `exists and is not a symlink`. It is the
regression that keeps the blocker from coming back.

- [ ] **Step 8 (optional, separate commit): convert `~/.gemini/skills`**

`~/.gemini/skills` is still a symlink to `gemini/skills` — the same shape the
incident destroyed a checkout through. Converting it needs one deliberate manual
step, because `linkdir` refuses a symlinked target rather than deleting it:

```sh
ls -la ~/.gemini/skills     # confirm it holds nothing but this checkout
rm ~/.gemini/skills         # deliberate: the directory is recreated below
# then change the row to linkdir and:
./bootstrap apply workstation
```

Skip this if "does anything but this repository write here" is not yet answered
for that path. It is a separate commit precisely so it can be reverted without
touching Task 1's fix.

- [ ] **Step 9: Record the decision in the design docs**

- `docs/design/2026-08-07-spec-2-dotfiles-hygiene.md` §6: add `linkdir` to the
  kind table with its "use when" clause.
- `docs/design/2026-09-20-agents-and-bootstrap-boundary.md`: mark §7 item 1
  resolved, state what was decided — the declaration changed rather than the
  filesystem, and it changed by gaining a kind that can express a shared
  directory — and link this plan. §4b's measurements stay as written: they are
  evidence, and evidence does not expire when its conclusion is acted on.

- [ ] **Step 10: Commit**

```sh
git add bootstrap.d/ docs/design/
git add docs/plans/2026-09-21-bootstrap-self-sufficiency-plan.md
git commit -m "feat(bootstrap): add a manifest kind for a directory this repository shares"
```

---

### Task 2: Stage zero can help a machine get Go

**The problem this solves.** `spec 2 §2.1` chose "refuse, naming the exact
command" over installing Go, because the earlier `brew install go` branch
swallowed its own failure and reached the build as an empty command. That
reasoning is about *control flow* — never take a command's output on trust — and
it survives intact. But the refusal itself has four gaps, and they matter more
once release distribution is deleted, because then this is the only path onto a
machine.

**Files:**
- Modify: `bootstrap` (the shim; nothing else in this task)
- Modify: `bootstrap.d/main_test.go`
- Modify: `docs/design/2026-08-07-spec-2-dotfiles-hygiene.md` (§2.1)

---

- [ ] **Step 1: Move the Go check inside the build branch**

Today the shim asks for Go before it knows whether it needs one:

```
1. command -v go  → die if absent
2. compare sources against the cached binary → needs_build
3. build if needs_build
4. exec the binary
```

Go is required only at step 3. `bootstrap.d` needs Go to be *built*, not to run,
so a machine whose Go was removed — or is off `PATH` in a non-interactive shell —
currently refuses a `bootstrap` that has a perfectly good cached binary beside it.

Reorder to: compute `needs_build`, then find Go **inside** the
`if [ "$needs_build" -eq 1 ]` branch, then build, then exec. Leave the
`XDG_CACHE_HOME`/`HOME` check where it is: the cache path is needed to locate the
binary even when nothing is built.

Test `TestFreshCacheDoesNotNeedGo` — warm the cache the way a non-cold
`runShimIn` already does (`sharedTestBinary` written into the keyed cache
directory), then run with `PATH=/usr/bin:/bin` and assert exit 0, with neither
`building` nor `Go is required` in stderr. Skip where `go` resolves at
`/usr/bin/go`, exactly as `TestMissingGoRefusesWithTheInstallCommand` does and
for the same reason. Note that the case passes whether or not a candidate
directory holds a Go: with a fresh cache the shim never asks.

`TestMissingGoRefusesWithTheInstallCommand` must keep passing — with a cold cache
there is still nothing to exec, so it must still refuse.

- [ ] **Step 2: Look where Go is, not only on `PATH`**

Measured on this machine: `env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin bash -c
'command -v go'` prints nothing, while an interactive shell resolves
`/opt/homebrew/bin/go`. `brew shellenv` is an interactive-shell facility, so every
restricted environment — an agent's tool call, a CI step, `sudo -E` — is told to
install a Go that is already installed.

Add a helper to the shim, at column 0, using `return` and never `exit`
(constraints 1 and 2):

```sh
# find_go prints the path of a usable Go, or nothing.
find_go() {
    if go_bin=$(command -v go); then printf '%s\n' "$go_bin"; return 0; fi
    for candidate in \
        /opt/homebrew/bin/go \
        /usr/local/go/bin/go \
        /home/linuxbrew/.linuxbrew/bin/go \
        "${HOME:-}/.local/share/go/bin/go" \
        "${HOME:-}/go/bin/go" \
        /nix/var/nix/profiles/default/bin/go
    do
        [ -x "$candidate" ] && { printf '%s\n' "$candidate"; return 0; }
    done
    # A keg that is installed but not linked. The glob is unquoted on purpose:
    # an unmatched glob must expand to itself and fail -x, not to an empty word.
    for candidate in /opt/homebrew/Cellar/go/*/bin/go /usr/local/Cellar/go/*/bin/go; do
        [ -x "$candidate" ] && { printf '%s\n' "$candidate"; return 0; }
    done
    return 1
}
```

`${HOME:-}`, not `$HOME`: under `set -u` a container with `XDG_CACHE_HOME` set
and `HOME` unset would abort here with an unbound-variable error, and that
container shape is the one the `dotfiles` profile exists for. The shim only
requires that *one* of the two is set.

When the search finds Go off `PATH`, prepend its directory so the build's own
subprocesses see it.

Test `TestGoOutsidePathIsFoundAndUsed` — create
`<tempHome>/.local/share/go/bin/go` as a stub that records that it ran (write a
marker file) and exits 1, then run cold (`runShimColdEnv`) with
`PATH=/usr/bin:/bin`. Assert exit 2 (a build failure blocks), stderr contains the
build-failure message and **not** `Go is required`, and the marker file exists.
The two messages being distinct is the assertion: it is the difference between
"we never looked" and "we looked, and it did not work".

- [ ] **Step 3: Name an installer this machine has**

The current hint branches on `uname -s`, which answers the wrong question. A
Linux box can have Homebrew — that is what Linuxbrew is — and this machine's
`PATH` begins with a Nix profile while the Darwin arm names `brew`, whose own
install is then a second missing step. That is two manual steps, not one.

Branch on `command -v` instead, testing in order `brew`, `apt-get`, `pacman`,
`nix`; the fall-through arm keeps the platform guidance from Step 4.

Tests build **their own stub directory** in `t.TempDir()` and put it first on
`PATH` via `extraEnv`; do not add stubs to `stubToolDir`, which every other case
in the file funnels through. With `brew` the only stub present, the hint must name
`brew install go`. A second case stubs two installers and pins the priority order.
Do not assert on the absence of the other installers' names unless the stub
environment makes that absence true.

- [ ] **Step 4: When no installer exists, name the right tarball**

`uname -s` and `uname -m` earn their place here, and only here:

| uname -s | uname -m | asset suffix |
|---|---|---|
| Darwin | arm64 | `darwin-arm64` |
| Darwin | x86_64 | `darwin-amd64` |
| Linux | x86_64 | `linux-amd64` |
| Linux | aarch64 / arm64 | `linux-arm64` |

The hint names `https://go.dev/dl/` and the suffix, and stops. It does not print a
`curl` line carrying a version: a hardcoded version goes stale, and one cannot be
derived from `go.mod`'s `go 1.26`, which carries no patch level. Naming the asset
is the useful half; inventing a version is not.

Test `TestGoHintNamesTheTarballForThisPlatform` — with a `PATH` where no installer
resolves, assert the hint contains `go.dev/dl` and the suffix that
`runtime.GOOS`/`runtime.GOARCH` imply. Skip when an installer is reachable on the
test's `PATH` (`/usr/bin/apt-get` on a Debian runner), and say why in a comment:
the case is about the fall-through arm, and a machine with `apt-get` never reaches
it.

- [ ] **Step 5: Update spec 2 §2.1, then commit**

§2.1's justification for refusing stays: it records a real defect. What changes
is the sentence *"The honest cost: a machine without Go needs one manual command
before `./bootstrap` runs."* Record what the manual step now is, that Go is
sought in six fixed locations before the machine is told it is missing, that the
hint names an installer the machine actually has, and that Go is required to
*build* rather than to run — so a fresh cache survives its absence.

```sh
git add bootstrap bootstrap.d/main_test.go docs/design/2026-08-07-spec-2-dotfiles-hygiene.md
git commit -m "feat(bootstrap): help a machine reach Go instead of only naming the command"
```

---

### Task 3: Whole-branch verification

- [ ] **Step 1:** `cd bootstrap.d && gofmt -d . && go vet ./... && go test -count=1 ./...`
- [ ] **Step 2:** `cd agents && gofmt -d . && go vet ./... && go test -count=1 ./...` — nothing there should have changed, which is the point of running it.
- [ ] **Step 3:** From the repository root: `./bootstrap plan dotfiles` (exit 0), `./bootstrap apply workstation`, `./bootstrap check` (no `manifest-kinds` fault).
- [ ] **Step 4:** `./bootstrap plan workstation` — record where it stops, if it does. Stopping anywhere other than `config` is out of scope and belongs in a new Q&A entry, not in this branch.
- [ ] **Step 5:** Confirm the shim guard actually guards, by breaking it on purpose in a scratch copy and reading the failure: `go test ./ -run TestShimHasExactlyTwoWaysOut -count=1` after adding a stray `exit` to a helper. Revert the scratch change.
- [ ] **Step 6:** Doc invariants: `agents/README.md` and the root `README.md` name no changed command (none is changed here); the kind table in `links.manifest` and spec 2 §6 agree with the constants in `manifest.go`.

---

## What this plan does not decide

- **Whether `bootstrap` may install Go for you.** Steps 1–4 keep the refusal and
  make it useful. An auto-installing mode is a separate decision, because it gives
  the shim a machine-mutating capability, and the shim's design premise is that it
  has none and therefore needs no dry-run to stay honest. If that decision is ever
  taken, the branch must not be written as `go_bin=$(install_go)` — that is the
  exact shape §2.1 removed.
- **Which of `claude/skills/`'s four entries should still exist.** This plan makes
  the directory installable, not its contents current.
  `recording-what-you-learn` exists in `claude/skills/`, `.agents/skills/` and
  twice under `agents/internal/scaffold/assets/skills/` with three different texts,
  and `agents-tool` describes a command set the simplification is likely to shrink.
  Both are follow-ups.
- **The fate of `.codex/skills`.** It is a real directory on this machine and is not
  in the manifest at all. If it should carry this repository's skills, that is a new
  `linkdir` row — and a decision about what belongs in it.
