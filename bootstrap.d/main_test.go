package main_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// runShim invokes the real ./bootstrap from an unrelated working directory, so
// a regression to pwd-based root resolution fails immediately.
func runShim(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	return runShimEnv(t, filepath.Join(repoRoot(t), "bootstrap"), home, nil, args...)
}

// runShimEnv is runShim over an arbitrary checkout, with extra environment
// appended last so it wins. Cases that need a second checkout or a doctored
// PATH go through this; everything else uses runShim.
func runShimEnv(t *testing.T, shim, home string, extraEnv []string, args ...string) (string, string, int) {
	t.Helper()
	return runShimIn(t, t.TempDir(), shim, home, extraEnv, args...)
}

// runShimIn is runShimEnv with an explicit working directory, so shim may be
// RELATIVE to it. Only the CDPATH case needs that: `cd` consults CDPATH for a
// relative operand and never for one starting with / or . -- an absolute $0
// cannot reproduce the failure at all.
func runShimIn(t *testing.T, dir, shim, home string, extraEnv []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(shim, args...)
	cmd.Dir = dir
	// XDG_CACHE_HOME must be redirected too, or an inherited value sends every
	// case into the developer's real ~/.cache and the suite stops being
	// hermetic -- the cache tests would then pass without exercising anything.
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME="+home+"/cache")
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// altCheckout copies the shim and its sources to a second directory, so a test
// can prove two checkouts do not share one cached binary -- the shape a git
// worktree alongside a main clone actually takes.
func altCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	copyInto(t, dir, "bootstrap", "bootstrap.d")
	return dir
}

// copyInto copies the named top-level entries of the repository into dst, which
// need not exist. Split out of altCheckout for the cases that choose the
// checkout's own name, its parent directory, or how much of the tree they need.
func copyInto(t *testing.T, dst string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		cp := exec.Command("cp", "-R", filepath.Join(repoRoot(t), name), filepath.Join(dst, name))
		if out, err := cp.CombinedOutput(); err != nil {
			t.Fatalf("cp %s: %v: %s", name, err, out)
		}
	}
}

// trackedEntries lists the repository's non-hidden top-level entries -- enough
// of the tree for a real `plan` to resolve every manifest source, which the two
// names altCheckout copies are not. Hidden entries are skipped: none of them is
// a manifest source, and .claude/worktrees contains this very checkout.
func trackedEntries(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("the repository appears empty; a copy of it would prove nothing")
	}
	return names
}

// backdate ages a tree so nothing in it is newer than an already-built binary,
// which is what forces a cache-key test to depend on the key and not on the
// staleness check. Directories are aged too, since the shim compares those.
func backdate(t *testing.T, dir string) {
	t.Helper()
	old := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, old, old)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func tempHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home dir")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestHelpExitsZero(t *testing.T) {
	stdout, _, code := runShim(t, tempHome(t), "--help")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	for _, want := range []string{"plan", "apply", "check", "migrate", "workstation", "dotfiles"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help omits %q:\n%s", want, stdout)
		}
	}
}

func TestUnknownVerbIsMalformedInput(t *testing.T) {
	_, stderr, code := runShim(t, tempHome(t), "frobnicate")
	if code != 3 {
		t.Fatalf("exit %d, want 3 (malformed input)", code)
	}
	if !strings.Contains(stderr, "frobnicate") {
		t.Errorf("error should name the verb: %s", stderr)
	}
}

func TestUnknownProfileIsMalformedInput(t *testing.T) {
	_, stderr, code := runShim(t, tempHome(t), "plan", "laptop")
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "laptop") {
		t.Errorf("error should name the profile: %s", stderr)
	}
}

func TestPlanRunsFromAnyDirectoryAndNamesItsPhases(t *testing.T) {
	stdout, stderr, code := runShim(t, tempHome(t), "plan", "workstation")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"preflight", "packages", "config", "fish", "devtools", "verify"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("workstation plan omits %q:\n%s", want, stdout)
		}
	}
}

func TestDotfilesProfileSkipsPrivilegedPhases(t *testing.T) {
	stdout, stderr, code := runShim(t, tempHome(t), "plan", "dotfiles")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, forbidden := range []string{"packages", "devtools"} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("dotfiles must not run the %s phase:\n%s", forbidden, stdout)
		}
	}
}

func TestPreflightDeclaresPrivilegeAndNetwork(t *testing.T) {
	stdout, _, code := runShim(t, tempHome(t), "plan", "workstation")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "sudo") || !strings.Contains(stdout, "network") {
		t.Errorf("preflight must declare what needs sudo and network:\n%s", stdout)
	}
}

// The shim must not rebuild when nothing changed. Both halves are asserted:
// without the positive one the case passes when the cache is never exercised.
func TestShimCachesTheBuild(t *testing.T) {
	home := tempHome(t)
	_, first, code := runShim(t, home, "--help")
	if code != 0 {
		t.Fatalf("first run exit %d: %s", code, first)
	}
	if !strings.Contains(first, "building") {
		t.Fatalf("first run into a fresh cache did not build; the case is not exercising the cache: %s", first)
	}
	_, stderr, code := runShim(t, home, "--help")
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "building") {
		t.Errorf("second run rebuilt; the cache is not working: %s", stderr)
	}
}

// Two checkouts must not share one cached binary. Unkeyed, whichever built
// first wins and the other silently runs old code against a new tree.
//
// The second checkout is backdated so its sources are OLDER than the first
// one's binary. That is deliberate: with fresh mtimes the staleness check
// rebuilds anyway and the case would pass without the cache key existing. Only
// the key can save this, and the assertion is on output from the second tree's
// own code rather than on the word "building".
func TestCacheIsKeyedOnTheCheckout(t *testing.T) {
	home := tempHome(t)
	if _, stderr, code := runShim(t, home, "--help"); code != 0 {
		t.Fatalf("first checkout exit %d: %s", code, stderr)
	}

	const marker = "ALTERNATE-CHECKOUT-MARKER"
	alt := altCheckout(t)
	main := filepath.Join(alt, "bootstrap.d", "main.go")
	source, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(source), "usage: bootstrap <verb>", marker+" <verb>", 1)
	if patched == string(source) {
		t.Fatal("could not patch the second checkout's usage text; the marker would prove nothing")
	}
	if err := os.WriteFile(main, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, filepath.Join(alt, "bootstrap.d"))

	stdout, stderr, code := runShimEnv(t, filepath.Join(alt, "bootstrap"), home, nil, "--help")
	if code != 0 {
		t.Fatalf("second checkout exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, marker) {
		t.Errorf("the second checkout ran the first one's binary -- old code against a new tree:\n%s", stdout)
	}
}

// A typo in the manifest is bad INPUT (3), not a refused machine (2). Both
// codes exist precisely so a wrapping script can tell "fix your typo" from
// "bootstrap declined to touch this box"; collapsing them to 2 tells it the
// machine is in a state it is not in. Exit 3 was unreachable from a phase
// until config became real, so nothing covered this path before.
//
// Driven end to end rather than through phase.Config, because the mapping
// under test lives in main's phase loop, not in the phase.
func TestMalformedManifestIsMalformedInput(t *testing.T) {
	alt := altCheckout(t)
	manifest := filepath.Join(alt, "bootstrap.d", "links.manifest")
	if err := os.WriteFile(manifest, []byte("hardlink  a  b  *\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runShimEnv(t, filepath.Join(alt, "bootstrap"), tempHome(t), nil,
		"plan", "dotfiles")
	if code != 3 {
		t.Fatalf("exit %d, want 3 (malformed input): %s", code, stderr)
	}
	if !strings.Contains(stderr, "hardlink") {
		t.Errorf("the error should name the offending kind: %s", stderr)
	}
	if !strings.Contains(stderr, "config") {
		t.Errorf("the error should name the phase that found it: %s", stderr)
	}
}

// The other half of the exit-3 mapping, and the half that regresses silently:
// a Refusal must still block (2), and must still print its remediation. Adding
// the syntax arm split one branch into two returns, so nothing but a test keeps
// them from drifting onto the same code.
func TestRefusalFromAPhaseBlocks(t *testing.T) {
	alt := altCheckout(t)
	manifest := filepath.Join(alt, "bootstrap.d", "links.manifest")
	// One space-free column per field: strings.Fields splits on whitespace, so
	// a source with spaces would be a column-count SyntaxError (exit 3) and
	// this case would silently stop testing refusals at all.
	if err := os.WriteFile(manifest, []byte("link  nosuchsource  dst  *\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runShimEnv(t, filepath.Join(alt, "bootstrap"), tempHome(t), nil,
		"plan", "dotfiles")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (block): %s", code, stderr)
	}
	// The remediation line is the entire reason Refusal carries that field;
	// %v would bury it inside Error().
	if !strings.Contains(stderr, "remedy:") {
		t.Errorf("a refusal must surface its remediation: %s", stderr)
	}
}

// A failure inside the shim must block (2). Never 1, which a CI job reads as
// advisory, and never 127, which is whatever the shell happened to return.
func TestShimBuildFailureIsBlock(t *testing.T) {
	alt := altCheckout(t)
	broken := filepath.Join(alt, "bootstrap.d", "main.go")
	if err := os.WriteFile(broken, []byte("package main\n\nthis is not Go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runShimEnv(t, filepath.Join(alt, "bootstrap"), tempHome(t), nil, "--help")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (block): %s", code, stderr)
	}
	if !strings.Contains(stderr, "build failed") {
		t.Errorf("the refusal should say the build failed: %s", stderr)
	}
}

// The shim installs nothing; without Go it must refuse with the platform's
// exact one-liner. /usr/bin:/bin carries every tool the shim needs and no Go.
func TestMissingGoRefusesWithTheInstallCommand(t *testing.T) {
	if _, err := exec.LookPath("go"); err == nil {
		if _, err := os.Stat("/usr/bin/go"); err == nil {
			t.Skip("go is installed in /usr/bin, so a restricted PATH cannot hide it")
		}
	}
	_, stderr, code := runShimEnv(t, filepath.Join(repoRoot(t), "bootstrap"),
		tempHome(t), []string{"PATH=/usr/bin:/bin"}, "--help")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (block): %s", code, stderr)
	}
	if !strings.Contains(stderr, "Go is required") {
		t.Errorf("the refusal should say Go is required: %s", stderr)
	}
	if !strings.Contains(stderr, "install") {
		t.Errorf("the refusal must name the command to run: %s", stderr)
	}
	if strings.Contains(stderr, "installing Go") {
		t.Errorf("the shim must not install anything: %s", stderr)
	}
}

// With neither variable set there is nowhere to cache the build, and that must
// block (2). Writing this as ${HOME:?...} was the obvious way and exits 1 --
// "advisory" -- so anything keying off the code reads a hard stop as a soft
// warning. PATH is kept so the shim reaches the check instead of dying at
// dirname; it is the two variables that must be absent, not the whole
// environment.
func TestNoCacheLocationIsBlock(t *testing.T) {
	_, stderr, code := runShimEnv(t, filepath.Join(repoRoot(t), "bootstrap"),
		tempHome(t), []string{"HOME=", "XDG_CACHE_HOME="}, "--help")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (block): %s", code, stderr)
	}
	if !strings.Contains(stderr, "nowhere to cache") {
		t.Errorf("the refusal should say there is nowhere to cache the build: %s", stderr)
	}
}

// A poisoned CDPATH must not move the repository root.
//
// `cd` searches CDPATH for a relative operand and, when it finds one there,
// echoes the directory it resolved to. So without the `CDPATH=` prefix in the
// shim, BOOTSTRAP_ROOT becomes a same-named decoy AND arrives as two lines --
// the "wrong tree" failure this whole redesign exists to prevent. It was found
// by hand and fixed with nothing in the suite holding it there.
//
// Three things make the reproduction work, and it reproduces nothing without
// any of them: the invocation is RELATIVE (cd never consults CDPATH for a path
// starting with / or .), the working directory is the checkout's PARENT (so
// dirname "$0" is a bare name rather than "." ), and the decoy carries the
// checkout's own basename.
func TestShimIgnoresAPoisonedCDPATH(t *testing.T) {
	name := filepath.Base(repoRoot(t))

	work := t.TempDir()
	checkout := filepath.Join(work, name)
	copyInto(t, checkout, trackedEntries(t)...)

	decoyParent := t.TempDir()
	decoy := filepath.Join(decoyParent, name)
	if err := os.MkdirAll(filepath.Join(decoy, "bootstrap.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	wantRoot, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatal(err)
	}
	decoyRoot, err := filepath.EvalSymlinks(decoy)
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runShimIn(t, work, filepath.Join(name, "bootstrap"), tempHome(t),
		[]string{"CDPATH=" + decoyParent}, "plan", "dotfiles")
	if code != 0 {
		t.Fatalf("exit %d under a poisoned CDPATH:\n%s%s", code, stdout, stderr)
	}

	// Compared as directories rather than as strings. bash spells the same
	// directory two ways -- an absolute operand keeps /var/folders, a relative
	// one is appended to a getcwd() that has already become /private/var/folders
	// -- and a case that failed on the spelling would look like a CDPATH failure
	// without being one.
	reported := reportedRoot(t, stdout)
	gotRoot, err := filepath.EvalSymlinks(reported)
	if err != nil {
		t.Fatalf("preflight reported a root that does not resolve: %q: %v", reported, err)
	}
	if gotRoot != wantRoot {
		t.Errorf("preflight reports %s, want the real checkout %s", gotRoot, wantRoot)
	}
	for _, path := range []string{decoy, decoyRoot} {
		if strings.Contains(stdout+stderr, path) {
			t.Errorf("the decoy %s reached the output:\n%s%s", path, stdout, stderr)
		}
	}
}

// reportedRoot returns the repository path preflight printed. Two spaces after
// the label, matching phase.Preflight's column layout.
func reportedRoot(t *testing.T, stdout string) string {
	t.Helper()
	const label = "repository  "
	for _, line := range strings.Split(stdout, "\n") {
		if i := strings.Index(line, label); i >= 0 {
			return strings.TrimSpace(line[i+len(label):])
		}
	}
	t.Fatalf("preflight printed no repository line:\n%s", stdout)
	return ""
}

// die and exec are the only ways out of the shim, checked lexically.
//
// TestPhasePackageCannotPerformIO rejects exactly this method for the Go
// packages, and the two do not contradict each other. There the subject is an
// open-ended set of statements across a package that will keep growing, so a
// scan can only approximate it -- the shell version's scan for mutating command
// names was written wrong twice -- and the invariant is stated over the import
// graph instead, where it is exact. Here the subject is one file of ninety
// lines, fixed in shape, with exactly two intended ways out. Enumerating them
// reads every line of the thing it constrains, so it is exhaustive rather than
// approximate.
//
// ${var:?} is checked because it is the obvious way to write the cache-location
// check and it exits 1 -- "advisory" in the shared table -- so a container with
// neither variable set would report a hard stop as a soft warning.
func TestShimHasExactlyTwoWaysOut(t *testing.T) {
	path := filepath.Join(repoRoot(t), "bootstrap")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	// die's body, as line numbers. Its opening and closing lines are matched
	// exactly, so a reformatted function fails here rather than silently
	// widening the region in which an exit is tolerated.
	dieStart, dieEnd := 0, 0
	for i, line := range lines {
		switch {
		case line == "die() {":
			dieStart = i + 1
		case dieStart != 0 && dieEnd == 0 && line == "}":
			dieEnd = i + 1
		}
	}
	if dieStart == 0 || dieEnd == 0 {
		t.Fatalf("%s: no die() { ... } found; this guard would check nothing", path)
	}

	exits, execs := 0, 0
	for i, line := range lines {
		n := i + 1
		// Whole-line comments only. The shim has none of any other kind, and a
		// guard with no parsing of its own has no parsing of its own to get
		// wrong; an inline comment added later fails loudly here instead of
		// quietly weakening the check.
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if errIfUnset.MatchString(line) {
			t.Errorf("%s:%d: ${var:?} exits 1, which is advisory, where a hard stop needs die: %s",
				path, n, strings.TrimSpace(line))
		}
		if word("exit").MatchString(line) {
			exits++
			if n < dieStart || n > dieEnd {
				t.Errorf("%s:%d: exit outside die(): %s", path, n, strings.TrimSpace(line))
			}
		}
		if word("exec").MatchString(line) {
			execs++
		}
	}
	if exits != 1 {
		t.Errorf("want exactly one exit, the one inside die(); found %d", exits)
	}
	if execs != 1 {
		t.Errorf("want exactly one exec, the handover to the built binary; found %d", execs)
	}
}

// errIfUnset matches a ${VAR:?...} expansion, and deliberately not ${VAR:-...}.
var errIfUnset = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z_0-9]*:\?`)

func word(s string) *regexp.Regexp { return regexp.MustCompile(`\b` + s + `\b`) }

// HOME unset with XDG_CACHE_HOME set is a normal container shape, and
// containers are why the dotfiles profile exists. Preflight must block rather
// than resolve every managed path against "/".
//
// The cache is warmed first, with HOME set, so the second run reaches the
// binary at all: `go build` itself refuses without HOME or GOCACHE, and its
// error text mentions HOME, so a cold-cache version of this case passes on a
// coincidence without ever entering preflight.
func TestEmptyHomeIsBlockedByPreflight(t *testing.T) {
	home := tempHome(t)
	shim := filepath.Join(repoRoot(t), "bootstrap")
	if _, stderr, code := runShimEnv(t, shim, home, nil, "--help"); code != 0 {
		t.Fatalf("warming the cache: exit %d: %s", code, stderr)
	}

	_, stderr, code := runShimEnv(t, shim, home, []string{"HOME="}, "plan", "dotfiles")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (block): %s", code, stderr)
	}
	if !strings.Contains(stderr, "preflight: HOME is empty") {
		t.Errorf("preflight itself must be what blocks, naming HOME: %s", stderr)
	}
}
