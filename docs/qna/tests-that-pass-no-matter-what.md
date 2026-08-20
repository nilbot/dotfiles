# Why do this repository's tests keep passing when the code is broken?

## Context

Six instances surfaced across four consecutive tasks on one branch. Every one was
found by mutating the production code and watching the test stay green. **Not one
was found by reading the test.**

| where | what the double failed to discriminate |
|---|---|
| `cmd_doctor_test.go` | asserted only an exit code, and doctor returns Advisory whenever *any* check is non-ok — so it passed with an environment leak present |
| `preflight_test.go` | recorded argv as `strings.Join(a, " ")`, so `["-ldflags", "-X k=v"]` and `["-ldflags", "-X", "k=v"]` rendered identically; the second builds nothing |
| `main_test.go` | `dscl`/`getent` stubs answered any argv, so dropping every argument from the exec call left the suite green |
| `main_test.go` | the same stubs matched `/Users/?*`, so asking the passwd database about the *wrong account* read as correct |
| `makefile_test.go` | searched the whole `make -n` output rather than the `go build` line, so moving a flag into an `@echo` greened it with an unstamped binary |
| `fish_test.go` | matched a substring loose enough that a trailing comment satisfied it while narrowing the `rm` it existed to pin |

## Answer

**A test double that answers the same way regardless of what it is asked is not a
double, it is a constant.** It makes the test unfalsifiable while leaving it
looking thorough — worse than no test, because the suite reports success and the
reviewer stops looking.

The gap opens later than the writing. A double is built against the case its
author had in mind; it answers that case correctly and every other case *also*
correctly, because answering is all it does. Three of the six above were latent
for months and became live the moment the surrounding code asked something new —
the first multi-word argv element, the first non-default account.

**How to find them:** treat a test as unfalsifiable until you have watched it
fail. Break the production line it names, run it, see red, restore. If it stays
green it was never coverage. A mutation that only breaks the *build* proves
nothing — an unused variable is not a failing assertion — so redo it in a form
that still compiles.

**How to stop writing them:**

- A stub standing in for something that takes input must **reject input it was
  not built to answer** — `exit 64`, a `t.Fatal`, an error. An unconditional
  `printf` cannot tell a correct command from a broken one.
- Assert the *decision*, not the *text*. `strings.Contains` over a whole file or
  a joined string is the recurring shape; anchor to the line, the statement or
  the operand set, and strip comments before matching.
- Watch for assertions that cannot fail for a structural reason — a status
  already non-ok for unrelated reasons proves nothing about the thing under test.
- When production starts passing a new *kind* of value through an existing seam,
  re-examine the double in the same change. That is the moment the gap opens.

Related: [can-this-check-actually-fail](can-this-check-actually-fail.md).
