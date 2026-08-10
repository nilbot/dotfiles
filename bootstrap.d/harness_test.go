package bootstrap_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	root string // repository root
	home string // redirected HOME
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	// A space in the path catches unquoted expansions.
	home := filepath.Join(t.TempDir(), "home dir")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture{root: root, home: home}
}

// runLib sources lib.sh and runs the supplied script body under the fixture HOME.
//
// This harness runs shell by design -- that is the thing under test. Every
// script body is a literal in a _test.go file and every interpolated path comes
// from t.TempDir(), so there is no untrusted input to inject. Do not copy this
// pattern into non-test code.
func (f fixture) runLib(t *testing.T, body string) (string, string, int) {
	t.Helper()
	script := ". \"$BOOTSTRAP_ROOT/bootstrap.d/lib.sh\"\n" + body
	return f.runScript(t, script)
}

func (f fixture) runScript(t *testing.T, script string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(),
		"HOME="+f.home,
		"BOOTSTRAP_ROOT="+f.root,
	)
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

// tree returns a sorted listing of paths under dir with their kinds, for
// asserting that a dry run changed nothing.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		kind := "file"
		if info.IsDir() {
			kind = "dir"
		} else if info.Mode()&os.ModeSymlink != 0 {
			kind = "link"
		}
		rel, _ := filepath.Rel(dir, path)
		lines = append(lines, kind+" "+rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
