package main_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command(shim, args...)
	cmd.Dir = t.TempDir()
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
	for _, name := range []string{"bootstrap", "bootstrap.d"} {
		cp := exec.Command("cp", "-R", filepath.Join(repoRoot(t), name), filepath.Join(dir, name))
		if out, err := cp.CombinedOutput(); err != nil {
			t.Fatalf("cp %s: %v: %s", name, err, out)
		}
	}
	return dir
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
