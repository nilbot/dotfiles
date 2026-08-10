package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	cmd := exec.Command(filepath.Join(repoRoot(t), "bootstrap"), args...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+home)
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

// The shim must not rebuild when nothing changed.
func TestShimCachesTheBuild(t *testing.T) {
	home := tempHome(t)
	if _, stderr, code := runShim(t, home, "--help"); code != 0 {
		t.Fatalf("first run exit %d: %s", code, stderr)
	}
	_, stderr, code := runShim(t, home, "--help")
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "building") {
		t.Errorf("second run rebuilt; the cache is not working: %s", stderr)
	}
}
