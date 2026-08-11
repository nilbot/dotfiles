---
name: undiscriminating-test-doubles
description: The dominant defect in this repo's tests is a double that answers correctly no matter what it is asked; find them by mutation, not by reading
metadata:
  type: feedback
sources:
  - kind: transcript
    machine: manboobs-26a6
    ref: 4d29608c-d467-497c-8286-40aa992e6d23   # session; resolve via the trace index
    note: "six instances found across four tasks on branch claude/agents-checkout-path"
---

A test double that answers the same way regardless of what it is asked is not a
double, it is a constant. It makes the test unfalsifiable while leaving it
looking thorough, which is worse than having no test: the suite reports success
and the reviewer stops looking.

Six instances surfaced in four consecutive tasks on one branch. Every one was
found by mutating the production code and watching the test stay green. **Not
one was found by reading the test.** That ratio is the point.

| Where | What the double failed to discriminate |
|---|---|
| `cmd_doctor_test.go` | asserted only an exit code, and doctor returns Advisory whenever *any* check is non-ok — so it passed with an environment leak present |
| `preflight_test.go` | recorded argv as `strings.Join(a, " ")`, so `["-ldflags", "-X k=v"]` and `["-ldflags", "-X", "k=v"]` rendered identically; the second builds nothing |
| `main_test.go` | `dscl`/`getent` stubs answered any argv, so dropping every argument from the exec call left the suite green |
| `main_test.go` | the same stubs matched `/Users/?*`, so asking the passwd database about the *wrong account* read as correct |
| `makefile_test.go` | searched the whole `make -n` output rather than the `go build` line, so moving a flag into an `@echo` greened it with an unstamped binary |
| `fish_test.go` | matched a substring loose enough that a trailing `# themes is left alone` comment satisfied it while narrowing the `rm` it existed to pin |

**Why:** a double is written against the case the author has in mind. It answers
that case correctly and every other case *also* correctly, because answering is
all it does. The gap only opens when production later asks it something new —
the first multi-word argv element, the first non-default account — and by then
nobody re-reads the double. Three of the six above were latent for months and
became live the moment the code around them changed.

**How to apply:**

- Treat a test as unfalsifiable until you have watched it fail. Break the
  production line it names, run it, see red, restore. If it stays green, it was
  never coverage.
- When a double stands in for something that takes input, it must **reject input
  it was not built to answer** — `exit 64`, a `t.Fatal`, an error. A stub with an
  unconditional `printf` cannot tell a correct command from a broken one.
- Prefer asserting the *decision* over the *text*. `strings.Contains` on a whole
  file or a joined string is the recurring shape; anchor to the line, the
  statement, or the operand set instead, and strip comments before matching.
- When production starts passing a new *kind* of value through an existing seam
  (first argument with a space, first non-constant account), re-examine the
  double in the same change. That is the moment the gap opens.
- Watch for an assertion that cannot fail for a structural reason — e.g. a
  status that is already non-ok for unrelated reasons, so asserting non-ok
  proves nothing about the thing under test.

Related: [[mutation-testing-is-the-standard]], [[refuse-never-clobber]]
