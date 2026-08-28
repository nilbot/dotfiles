package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func TestMulticallDispatcherRejectsIndirectSameHookRecursion(t *testing.T) {
	binary := buildTemporaryAgentsBinary(t)
	repo := newLiveHookRepo(t, binary)
	t.Setenv("HOME", repo.home)
	dispatcherHook := filepath.Join(t.TempDir(), "post-merge")
	if err := os.Symlink(binary, dispatcherHook); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPATCHER_HOOK", dispatcherHook)
	writeLiveFile(t, repo.root, ".git/hooks/post-merge",
		"#!/bin/sh\nexec \"$DISPATCHER_HOOK\"\n", 0o755)

	cmd := exec.Command(dispatcherHook)
	cmd.Dir = repo.root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		t.Fatal("indirect wrapper recursion did not terminate")
	}
	if err == nil {
		t.Fatal("indirect wrapper recursion was accepted")
	}
	if !strings.Contains(stderr.String(), "recursive") || !strings.Contains(stderr.String(), "post-merge") {
		t.Fatalf("recursion diagnostic is not actionable: %q", stderr.String())
	}
}

func TestMulticallDispatcherRunsOrdinaryWrapperWithInheritedEnvironment(t *testing.T) {
	binary := buildTemporaryAgentsBinary(t)
	repo := newLiveHookRepo(t, binary)
	t.Setenv("HOME", repo.home)
	dispatcherHook := filepath.Join(t.TempDir(), "post-merge")
	if err := os.Symlink(binary, dispatcherHook); err != nil {
		t.Fatal(err)
	}
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOOK_OBSERVED", observed)
	t.Setenv("GIT_INDEX_FILE", "/tmp/index with spaces")
	t.Setenv("ORDINARY_ENV", "ordinary value")
	// A different active hook is not same-hook recursion. The wrapper sees all
	// inherited values unchanged plus the dispatcher's documented private stack.
	t.Setenv("AGENTS_ACTIVE_GIT_HOOKS", "pre-commit")
	writeLiveFile(t, repo.root, ".git/hooks/post-merge", "#!/bin/sh\n"+
		"printf '%s\\n%s\\n%s\\n%s\\n' \"$1\" \"$GIT_INDEX_FILE\" \"$ORDINARY_ENV\" \"$AGENTS_ACTIVE_GIT_HOOKS\" > \"$HOOK_OBSERVED\"\n", 0o755)

	cmd := exec.Command(dispatcherHook, "1")
	cmd.Dir = repo.root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ordinary wrapper failed: %v\n%s", err, out)
	}
	want := "1\n/tmp/index with spaces\nordinary value\npre-commit,post-merge\n"
	if got, err := os.ReadFile(observed); err != nil || string(got) != want {
		t.Fatalf("wrapper environment/argv = %q, want %q (err=%v)", got, want, err)
	}
}

func buildTemporaryAgentsBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents")
	cmd := exec.Command("go", "build", "-o", path, ".")
	// TestMain moves the working directory out of the checkout, so `.` and the
	// module it belongs to have to be named explicitly.
	cmd.Dir = packageDir
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
	// The personal hooks this fixture lays out are found through DotfilesRoot.
	// Since unstamped test binaries operate in Standalone Mode by default,
	// AGENTS_DOTFILES_ROOT is explicitly configured to point to the fixture's
	// dotfiles root so personal hooks under git/hooks are executed.
	t.Setenv("AGENTS_DOTFILES_ROOT", filepath.Join(home, "dotfiles"))
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

// TestRunGitHookRunsPersonalHooksFromThisBinarysCheckout pins the silent half of
// the relocated-checkout defect. githook treats a missing extras directory as
// "no personal hooks" and carries on at exit 0, so a dispatcher looking under
// $HOME/dotfiles on a machine checked out anywhere else runs none of them and
// reports nothing -- no failure, no warning, no output at all.
//
// The decoy under HOME is what makes this falsifiable in the useful direction.
// Asserting only that the checkout's hook ran would also pass for a dispatcher
// that ran both; asserting that nothing ran under HOME would pass for one that
// ran neither.
//
// post-merge rather than pre-commit: the chain is identical, and pre-commit
// additionally runs the guard, whose scanner and staging machinery has nothing
// to do with which directory the personal hooks were found in.
//
// Both ways of naming the checkout are exercised. The build stamp case is not
// redundant with root_test.go: it is the only place a consumer proves it honours
// the stamp, and the two builders that set it landed with this change.
func TestRunGitHookRunsPersonalHooksFromThisBinarysCheckout(t *testing.T) {
	for _, tc := range []struct {
		name string
		// nameCheckout points the binary at checkout the way one builder or one
		// operator would, and leaves the other way empty so each case pins one
		// branch of DotfilesRoot on its own.
		nameCheckout func(t *testing.T, checkout string)
	}{
		{"build stamp", func(t *testing.T, checkout string) {
			stampRoot(t, checkout)
			t.Setenv("AGENTS_DOTFILES_ROOT", "")
		}},
		{"environment", func(t *testing.T, checkout string) {
			stampRoot(t, "")
			t.Setenv("AGENTS_DOTFILES_ROOT", checkout)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			checkout := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)
			tc.nameCheckout(t, checkout)

			ran := filepath.Join(t.TempDir(), "ran")
			t.Setenv("PERSONAL_HOOK_RAN", ran)
			writeLiveFile(t, checkout, "git/hooks/a.post-merge",
				"#!/bin/sh\nprintf 'checkout\\n' >> \"$PERSONAL_HOOK_RAN\"\n", 0o755)
			writeLiveFile(t, home, "dotfiles/git/hooks/a.post-merge",
				"#!/bin/sh\nprintf 'home\\n' >> \"$PERSONAL_HOOK_RAN\"\n", 0o755)

			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			if code := runGitHook("post-merge", nil, strings.NewReader(""), &stdout, &stderr); code != exitcode.OK {
				t.Fatalf("runGitHook exit=%d want=%d stdout=%q stderr=%q",
					code, exitcode.OK, stdout.String(), stderr.String())
			}

			got, err := os.ReadFile(ran)
			if err != nil {
				t.Fatalf("no personal hook ran at all (%v); a missing extras directory "+
					"is not an error to githook, so this is exactly how a relocated "+
					"checkout loses its hooks without a word", err)
			}
			if string(got) != "checkout\n" {
				t.Errorf("personal hooks that ran = %q, want %q; the dispatcher looked "+
					"under HOME instead of the checkout this binary belongs to",
					got, "checkout\n")
			}
		})
	}
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
			// Empty HOME is how this case says "no personal hooks". Since the
			// dispatcher asks DotfilesRoot, an exported AGENTS_DOTFILES_ROOT
			// would answer first and run the real checkout's personal hooks
			// against this fixture's staged content.
			t.Setenv("AGENTS_DOTFILES_ROOT", "")
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
