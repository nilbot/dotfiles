package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func writeGuardFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGuardCommandExitClasses(t *testing.T) {
	cases := []struct {
		name string
		seed func(*testing.T, string)
		want int
		text string
	}{
		{"clean", func(t *testing.T, root string) {
			writeGuardFile(t, filepath.Join(root, "main.go"), "package main\n")
			git(t, root, "add", "-A")
		}, exitcode.OK, ""},
		{"mixed advisory", func(t *testing.T, root string) {
			writeGuardFile(t, filepath.Join(root, ".agents", "note.md"), "note\n")
			writeGuardFile(t, filepath.Join(root, "main.go"), "package main\n")
			git(t, root, "add", "-A")
		}, exitcode.Advisory, "warning [mixed-commit]"},
		{"generated block", func(t *testing.T, root string) {
			writeGuardFile(t, filepath.Join(root, ".agents", "memory", "a.md"), memoryEntryForCommand)
			git(t, root, "add", "-A")
		}, exitcode.Block, `BLOCKED ".agents/memory/INDEX.md":0 [generated-file]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			tc.seed(t, root)
			t.Chdir(root)
			var out bytes.Buffer
			if code := runGuard([]string{"--staged"}, &out); code != tc.want {
				t.Fatalf("exit = %d, want %d; output:\n%s", code, tc.want, out.String())
			}
			if tc.text != "" && !strings.Contains(out.String(), tc.text) {
				t.Fatalf("output does not contain %q:\n%s", tc.text, out.String())
			}
		})
	}
}

const memoryEntryForCommand = "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nbody\n"

func TestGuardCommandWorksFromARepositorySubdirectory(t *testing.T) {
	root := newRepo(t)
	writeGuardFile(t, filepath.Join(root, "main.go"), "package main\n")
	git(t, root, "add", "-A")
	subdir := filepath.Join(root, "nested", "deeper")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(subdir)
	var out bytes.Buffer
	if code := runGuard([]string{"--staged"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK; output:\n%s", code, out.String())
	}
}

func TestGuardCommandRejectsMalformedInvocation(t *testing.T) {
	for _, args := range [][]string{nil, {"--no-such-flag"}, {"--staged", "extra"}} {
		var out bytes.Buffer
		if code := runGuard(args, &out); code != exitcode.Malformed {
			t.Fatalf("runGuard(%v) = %d, want Malformed; output:\n%s", args, code, out.String())
		}
	}
}

func TestGuardCommandOutsideARepositorySkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	t.Chdir(dir)
	var out bytes.Buffer
	if code := runGuard([]string{"--staged"}, &out); code != exitcode.Skip {
		t.Fatalf("exit = %d, want Skip; output:\n%s", code, out.String())
	}
}

func TestGuardCommandQuotesAControlCharacterPath(t *testing.T) {
	root := newRepo(t)
	raw := ".agents/FORGED\nBLOCK.md"
	writeGuardFile(t, filepath.Join(root, raw), "note\n")
	git(t, root, "add", "-A")
	t.Chdir(root)
	var out bytes.Buffer
	if code := runGuard([]string{"--staged"}, &out); code != exitcode.Block {
		t.Fatalf("exit = %d, want Block; output=%q", code, out.String())
	}
	if strings.Contains(out.String(), raw) {
		t.Fatalf("raw path forged a command-output line: %q", out.String())
	}
	if !strings.Contains(out.String(), `FORGED\nBLOCK.md`) {
		t.Fatalf("quoted path is not actionable: %q", out.String())
	}
}

func TestGuardCommandASCIIQuotesPrintableHostilePath(t *testing.T) {
	root := newRepo(t)
	raw := ".agents/close] \u202e tail.md"
	privateFixtureValue := "ghp_" + strings.Repeat("0123456789", 3) + "012345"
	writeGuardFile(t, filepath.Join(root, raw), privateFixtureValue+"\n")
	git(t, root, "add", "-A")
	t.Chdir(root)
	var out bytes.Buffer
	if code := runGuard([]string{"--staged"}, &out); code != exitcode.Block {
		t.Fatalf("exit = %d, want Block", code)
	}
	if strings.Contains(out.String(), raw) {
		t.Fatal("repository path crossed the CLI boundary without ASCII quoting")
	}
	if !strings.Contains(out.String(), `\u202e`) || !strings.Contains(out.String(), `".agents/close]`) {
		t.Fatalf("quoted path is not actionable: %q", out.String())
	}
}

func TestGuardCommandDoesNotRenderMatchedMalformedContent(t *testing.T) {
	root := newRepo(t)
	privateFixtureValue := "ghp_" + strings.Repeat("0123456789", 3) + "012345"
	writeGuardFile(t, filepath.Join(root, ".agents", "memory", "a.md"), "---\nname: ["+privateFixtureValue+"\n---\n")
	writeGuardFile(t, filepath.Join(root, ".agents", "memory", "INDEX.md"), "hand edited\n")
	git(t, root, "add", "-A")
	t.Chdir(root)
	var out bytes.Buffer
	if code := runGuard([]string{"--staged"}, &out); code != exitcode.Block {
		t.Fatalf("exit = %d, want Block", code)
	}
	if strings.Contains(out.String(), privateFixtureValue) {
		t.Fatal("matched malformed staged content leaked into command output")
	}
	if !strings.Contains(out.String(), "[github-pat]") {
		t.Fatalf("safe scanner attribution missing: %q", out.String())
	}
}

// A well-implemented but unregistered command is unreachable from the binary.
func TestMainRegistersGuard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	t.Chdir(dir)
	if code := run([]string{"guard", "--staged"}); code != exitcode.Skip {
		t.Fatalf("run(guard --staged) = %d, want Skip", code)
	}
}
