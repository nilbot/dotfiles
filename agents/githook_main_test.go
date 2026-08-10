package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func buildTemporaryAgentsBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents")
	cmd := exec.Command("go", "build", "-o", path, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build temporary agents binary: %v\n%s", err, out)
	}
	return path
}

type liveHookRepo struct {
	root   string
	home   string
	extras string
}

func newLiveHookRepo(t *testing.T, binary string) liveHookRepo {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	emptyTemplate := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main", "--template=" + emptyTemplate},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := gitAttempt(root, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	hooksPath := filepath.Join(t.TempDir(), "configured-hooks")
	if err := os.MkdirAll(hooksPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		if err := os.Symlink(binary, filepath.Join(hooksPath, name)); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := gitAttempt(root, "config", "core.hooksPath", hooksPath); err != nil {
		t.Fatalf("configure local core.hooksPath: %v\n%s", err, out)
	}
	extras := filepath.Join(home, "dotfiles", "git", "hooks")
	if err := os.MkdirAll(extras, 0o755); err != nil {
		t.Fatal(err)
	}
	return liveHookRepo{root: root, home: home, extras: extras}
}

func gitAttempt(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	return cmd.CombinedOutput()
}

func writeLiveFile(t *testing.T, root, rel, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func installHookTestPath(t *testing.T, gitBinary, scannerBody string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.Symlink(gitBinary, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	if scannerBody != "" {
		writeLiveFile(t, bin, "gitleaks", "#!/bin/sh\n"+scannerBody+"\n", 0o755)
	}
	t.Setenv("PATH", bin)
}

func stageLive(t *testing.T, root string) {
	t.Helper()
	if out, err := gitAttempt(root, "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}

func TestTemporaryMulticallBinaryWithLiveGitCommits(t *testing.T) {
	binary := buildTemporaryAgentsBinary(t)
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(t.TempDir(), "global.gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	t.Run("zero-arg pre-commit chains then maps advisory to success and commit-msg receives its exact argument", func(t *testing.T) {
		repo := newLiveHookRepo(t, binary)
		t.Setenv("HOME", repo.home)
		installHookTestPath(t, gitBinary, "exit 0")
		repoCount := filepath.Join(t.TempDir(), "repo-count")
		extraCount := filepath.Join(t.TempDir(), "extra-count")
		commitArgs := filepath.Join(t.TempDir(), "commit-args")
		t.Setenv("REPO_COUNT", repoCount)
		t.Setenv("EXTRA_COUNT", extraCount)
		t.Setenv("COMMIT_ARGS", commitArgs)
		writeLiveFile(t, repo.root, ".git/hooks/pre-commit", "#!/bin/sh\nprintf 'repo:%s\\n' \"$#\" >> \"$REPO_COUNT\"\n", 0o755)
		writeLiveFile(t, repo.extras, "a.pre-commit", "#!/bin/sh\nprintf 'extra:%s\\n' \"$#\" >> \"$EXTRA_COUNT\"\n", 0o755)
		writeLiveFile(t, repo.extras, "a.commit-msg", "#!/bin/sh\nfor arg do printf '<%s>\\n' \"$arg\"; done > \"$COMMIT_ARGS\"\n", 0o755)
		writeLiveFile(t, repo.root, ".agents/note.md", "ordinary agent note\n", 0o644)
		writeLiveFile(t, repo.root, "main.go", "package main\n", 0o644)
		stageLive(t, repo.root)
		message := "test: multicall\n\nCo-Authored-By: Claude <noreply@anthropic.com>"
		out, err := gitAttempt(repo.root, "commit", "-m", message)
		if err != nil {
			t.Fatalf("mixed commit should succeed on advisory: %v\n%s", err, out)
		}
		if !bytes.Contains(out, []byte("warning [mixed-commit]")) {
			t.Fatalf("mixed advisory was not visible: %s", out)
		}
		for path, want := range map[string]string{repoCount: "repo:0\n", extraCount: "extra:0\n", commitArgs: "<.git/COMMIT_EDITMSG>\n"} {
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != want {
				t.Fatalf("%s = %q, want %q (err=%v)", filepath.Base(path), got, want, readErr)
			}
		}
		logged, err := gitAttempt(repo.root, "log", "-1", "--format=%B")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(bytes.ToLower(logged), []byte("co-authored-by: claude")) {
			t.Fatalf("commit-msg attribution survived: %q", logged)
		}
	})

	t.Run("plain non-agent commit succeeds without scanner", func(t *testing.T) {
		repo := newLiveHookRepo(t, binary)
		t.Setenv("HOME", repo.home)
		installHookTestPath(t, gitBinary, "")
		writeLiveFile(t, repo.root, "plain.txt", "plain\n", 0o644)
		stageLive(t, repo.root)
		if out, err := gitAttempt(repo.root, "commit", "-m", "plain"); err != nil {
			t.Fatalf("plain commit consulted missing scanner: %v\n%s", err, out)
		}
	})

	t.Run("scanner finding blocks without rendering staged content", func(t *testing.T) {
		repo := newLiveHookRepo(t, binary)
		t.Setenv("HOME", repo.home)
		installHookTestPath(t, gitBinary, `printf '[{"RuleID":"fixture-rule","StartLine":1}]\n'; exit 1`)
		privateMarker := "private staged marker"
		writeLiveFile(t, repo.root, ".agents/note.md", privateMarker+"\n", 0o644)
		stageLive(t, repo.root)
		out, err := gitAttempt(repo.root, "commit", "-m", "blocked")
		if err == nil {
			t.Fatalf("scanner finding did not block: %s", out)
		}
		if !bytes.Contains(out, []byte("[fixture-rule]")) || bytes.Contains(out, []byte(privateMarker)) {
			t.Fatalf("scanner diagnostic missing attribution or leaked content: %q", out)
		}
	})

	t.Run("scanner failure blocks agent commit", func(t *testing.T) {
		repo := newLiveHookRepo(t, binary)
		t.Setenv("HOME", repo.home)
		installHookTestPath(t, gitBinary, "exit 9")
		writeLiveFile(t, repo.root, ".agents/note.md", "ordinary\n", 0o644)
		stageLive(t, repo.root)
		out, err := gitAttempt(repo.root, "commit", "-m", "blocked")
		if err == nil || !bytes.Contains(out, []byte("could not complete operation")) {
			t.Fatalf("scanner failure did not block safely: err=%v out=%q", err, out)
		}
	})

	t.Run("foreign repo hook mutation reaches guard once", func(t *testing.T) {
		repo := newLiveHookRepo(t, binary)
		t.Setenv("HOME", repo.home)
		installHookTestPath(t, gitBinary, "exit 0")
		count := filepath.Join(t.TempDir(), "repo-count")
		t.Setenv("REPO_COUNT", count)
		writeLiveFile(t, repo.root, ".git/hooks/pre-commit", "#!/bin/sh\nprintf 'repo\\n' >> \"$REPO_COUNT\"\n/bin/mkdir -p .agents/memory\nprintf 'hand edited\\n' > .agents/memory/INDEX.md\ngit add .agents/memory/INDEX.md\n", 0o755)
		writeLiveFile(t, repo.root, "plain.txt", "plain\n", 0o644)
		stageLive(t, repo.root)
		out, err := gitAttempt(repo.root, "commit", "-m", "blocked")
		if err == nil {
			t.Fatalf("guard missed final index mutation: %s", out)
		}
		if strings.Count(string(out), "[generated-file]") != 1 {
			t.Fatalf("guard did not report final index exactly once: %q", out)
		}
		got, readErr := os.ReadFile(count)
		if readErr != nil || string(got) != "repo\n" {
			t.Fatalf("foreign hook count=%q err=%v", got, readErr)
		}
	})

	t.Run("foreign failure stops extras and guard", func(t *testing.T) {
		repo := newLiveHookRepo(t, binary)
		t.Setenv("HOME", repo.home)
		scannerMarker := filepath.Join(t.TempDir(), "scanner-ran")
		t.Setenv("SCANNER_MARKER", scannerMarker)
		installHookTestPath(t, gitBinary, `printf 'ran\n' > "$SCANNER_MARKER"; exit 0`)
		extraMarker := filepath.Join(t.TempDir(), "extra-ran")
		t.Setenv("EXTRA_MARKER", extraMarker)
		writeLiveFile(t, repo.root, ".git/hooks/pre-commit", "#!/bin/sh\nexit 7\n", 0o755)
		writeLiveFile(t, repo.extras, "a.pre-commit", "#!/bin/sh\nprintf 'ran\\n' > \"$EXTRA_MARKER\"\n", 0o755)
		writeLiveFile(t, repo.root, ".agents/note.md", "ordinary\n", 0o644)
		stageLive(t, repo.root)
		if out, err := gitAttempt(repo.root, "commit", "-m", "blocked"); err == nil {
			t.Fatalf("foreign failure did not block: %s", out)
		}
		for _, path := range []string{extraMarker, scannerMarker} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("later stage ran after foreign failure (%s): %v", filepath.Base(path), err)
			}
		}
	})
}

func TestRunGitHookMapsGuardExitClassesExactly(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		scannerBody string
		seed        func(*testing.T, string)
		want        int
		wantOutput  string
	}{
		{
			name: "ok",
			seed: func(t *testing.T, root string) {
				writeLiveFile(t, root, "plain.txt", "plain\n", 0o644)
			},
			want: exitcode.OK,
		},
		{
			name:        "advisory becomes hook success",
			scannerBody: "exit 0",
			seed: func(t *testing.T, root string) {
				writeLiveFile(t, root, ".agents/note.md", "ordinary\n", 0o644)
				writeLiveFile(t, root, "plain.txt", "plain\n", 0o644)
			},
			want:       exitcode.OK,
			wantOutput: "warning [mixed-commit]",
		},
		{
			name:        "finding blocks",
			scannerBody: `printf '[{"RuleID":"fixture-rule","StartLine":1}]\n'; exit 1`,
			seed: func(t *testing.T, root string) {
				writeLiveFile(t, root, ".agents/note.md", "ordinary\n", 0o644)
			},
			want:       exitcode.Block,
			wantOutput: "BLOCKED",
		},
		{
			name:        "incomplete scanner blocks",
			scannerBody: "exit 9",
			seed: func(t *testing.T, root string) {
				writeLiveFile(t, root, ".agents/note.md", "ordinary\n", 0o644)
			},
			want:       exitcode.Block,
			wantOutput: "could not complete operation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			t.Setenv("HOME", t.TempDir())
			installHookTestPath(t, gitBinary, tc.scannerBody)
			tc.seed(t, root)
			stageLive(t, root)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			got := runGitHook("pre-commit", nil, strings.NewReader(""), &stdout, &stderr)
			if got != tc.want {
				t.Fatalf("runGitHook exit=%d want=%d stdout=%q stderr=%q", got, tc.want, stdout.String(), stderr.String())
			}
			if tc.wantOutput != "" && !strings.Contains(stdout.String(), tc.wantOutput) {
				t.Fatalf("stdout missing %q: %q", tc.wantOutput, stdout.String())
			}
		})
	}
}
