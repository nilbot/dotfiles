# Does Go 1.27's JSON rewrite make this repository faster?

## Context

Go 1.27 (released 2026-08-19) added `encoding/json/v2` and
`encoding/json/jsontext`, and — the part that matters for a version bump — made
the existing `encoding/json` API **run on the v2 implementation**. No code change
and no `GOEXPERIMENT` is needed; `GOEXPERIMENT=nojsonv2` at build time opts back
out. The release notes summarise the effect as *"Marshal performance is broadly
at parity with the previous implementation, while unmarshal performance is
significantly faster"* ([go1.27 release
notes](https://go.dev/doc/go1.27)).

The question was whether to move this repository's CI pin off `1.26.6` to get
that. It has five JSON call sites, and they are not one shape:

| call site | shape |
|---|---|
| `agents/internal/harness/harness.go:415` | `Unmarshal` into `map[string]any` |
| `agents/internal/harness/harness.go:468` | `MarshalIndent` of `map[string]any` |
| `agents/internal/doctor/doctor.go:207` | `Unmarshal` into `map[string]any` |
| `agents/internal/doctor/doctor.go:474` | `Unmarshal` into a small struct |
| `agents/internal/guard/guard.go:247` | `Unmarshal` into a slice of findings |

## Answer

**No, not for the JSON.** The general claim does not hold for the shape this
repository leans on, and the end-to-end effect is not measurable.

Measured on go1.27.1, toggling only the implementation with
`GOEXPERIMENT=nojsonv2`, six to nine runs per configuration:

| shape | old v1 | v2-backed | change |
|---|---|---|---|
| `map[string]any`, 1.2 KB config | 10,622 ns | 15,486 ns | **1.46x slower** |
| `map[string]any`, 10 KB config | 232 us | 363 us | **1.56x slower** |
| slice of findings, 5 entries | 5,661 ns | 2,872 ns | **1.97x faster** |
| `MarshalIndent` of `map[string]any` | 13,540 ns | 7,700 ns | **1.76x faster** (129 -> 11 allocs) |

Unmarshalling into an untyped `map[string]any` — what both harness-config
readers do — is about **1.5x slower**, not faster. The "significantly faster"
half lands on the typed-struct path, which here is only the gitleaks report in
`guard.go`. Marshal is better than the notes' "parity" suggests, because the
untyped path's allocations collapse; that helps `harness.go:468`, which writes a
config file once per `wire`.

The end-to-end effect is inside the noise. `agents doctor` in a repository with
a 2 KB `.claude/settings.json` it really parses (it reported
`wiring:claude-code  no stale entries`) took a median of 85.9 ms with the old
implementation and 86.0 ms with the new one, over 60 interleaved runs. Four
parses at roughly 5 us each is a budget of about 0.02 ms against a command that
costs 86 ms.

Two things that decide whether a bump is *safe* were checked rather than
assumed:

- **The `go` directive does not gate it.** The same benchmarks run identically
  with `go 1.26` and `go 1.27` in `go.mod`; the implementation follows the
  toolchain that builds the binary. Bumping `go.mod` is not what turns it on.
- **v1 leniency is preserved.** Through the v1 API, duplicate object names and
  invalid UTF-8 are still accepted under the v2 backing, exactly as before. That
  matters because the config files this tool reads are written by other harnesses
  and by hand; the stricter v2 defaults apply only to code that imports
  `encoding/json/v2` directly.

A toolchain bump may still be worth making for unrelated reasons, and then the
JSON change rides along as a wash. `1.26.7` and `1.26.8` carry non-security bug
fixes; Go 1.27 reduces the cost of small allocations by up to 30 percent (about
1 percent overall) at roughly 60 KB of binary size; and `1.27.1` fixes
`encoding/json` bugs that `1.27.0` shipped. Its costs are that two pins must move
together — `.github/workflows/verify.yml`'s `GO_VERSION` and the hardcoded
`go-version` in `.github/workflows/release.yml` — the Go cache keys rotate once,
and Go 1.27 requires **macOS 13 or later**, which sets the floor for the released
`darwin/amd64` and `darwin/arm64` binaries.

Re-run the comparison at the next Go major:

```bash
GOEXPERIMENT=nojsonv2 go test -run '^$' -bench . -benchmem ./...
go test -run '^$' -bench . -benchmem ./...
```

with a benchmark over the two shapes above: an untyped object of about 1 KB with
nested arrays, and a slice of typed structs.
