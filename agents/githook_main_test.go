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

func TestSymlinkShimRejectsIndirectSameHookRecursion(t *testing.T) {
	t.Parallel()
	binary := buildTemporaryAgentsBinary(t)
	repo := newLiveHookRepo(t, t.TempDir(), "")
	dispatcherHook := filepath.Join(t.TempDir(), "post-merge")
	if err := os.Symlink(binary, dispatcherHook); err != nil {
		t.Fatal(err)
	}
	writeLiveFile(t, repo.root, ".git/hooks/post-merge",
		"#!/bin/sh\nexec \"$DISPATCHER_HOOK\"\n", 0o755)

	cmd := exec.Command(dispatcherHook)
	cmd.Dir = repo.root
	cmd.Env = repo.withEnv("DISPATCHER_HOOK=" + dispatcherHook).env
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
	case <-time.After(10 * time.Second):
		// A watchdog, not a budget: this case runs in parallel with the rest of
		// the package, and the property is "does not recurse forever".
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

// The property is the INHERITANCE: the dispatcher hands the ordinary wrapper the
// environment it was started with, plus its own active-hook stack, and nothing
// else. Which is why the values below are the spawned process's, not this
// process's: t.Setenv would have made the test's own environment the carrier,
// and the assertion would hold even for a dispatcher that invented the values.
func TestSymlinkShimRunsOrdinaryWrapperWithInheritedEnvironment(t *testing.T) {
	t.Parallel()
	binary := buildTemporaryAgentsBinary(t)
	repo := newLiveHookRepo(t, t.TempDir(), "")
	dispatcherHook := filepath.Join(t.TempDir(), "post-merge")
	if err := os.Symlink(binary, dispatcherHook); err != nil {
		t.Fatal(err)
	}
	observed := filepath.Join(t.TempDir(), "observed")
	// A different active hook is not same-hook recursion. The wrapper sees all
	// inherited values unchanged plus the dispatcher's documented private stack.
	env := repo.withEnv(
		"HOOK_OBSERVED="+observed,
		"GIT_INDEX_FILE=/tmp/index with spaces",
		"ORDINARY_ENV=ordinary value",
		"AGENTS_ACTIVE_GIT_HOOKS=pre-commit",
	).env
	writeLiveFile(t, repo.root, ".git/hooks/post-merge", "#!/bin/sh\n"+
		"printf '%s\\n%s\\n%s\\n%s\\n' \"$1\" \"$GIT_INDEX_FILE\" \"$ORDINARY_ENV\" \"$AGENTS_ACTIVE_GIT_HOOKS\" > \"$HOOK_OBSERVED\"\n", 0o755)

	cmd := exec.Command(dispatcherHook, "1")
	cmd.Dir = repo.root
	cmd.Env = env
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

// replaceEnv returns base with every variable named by an override replaced
// rather than added. A duplicate key in an environment is resolved differently
// by different libc implementations, so the ambient value has to be dropped
// rather than left underneath the fixture's.
func replaceEnv(base, overrides []string) []string {
	replaced := make(map[string]bool, len(overrides))
	for _, override := range overrides {
		if i := strings.IndexByte(override, '='); i > 0 {
			replaced[override[:i]] = true
		}
	}
	env := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		if i := strings.IndexByte(item, '='); i > 0 && replaced[item[:i]] {
			continue
		}
		env = append(env, item)
	}
	return append(env, overrides...)
}

// childEnv is the environment a fixture's git and its hooks run with: this
// process's, with the named variables replaced.
//
// These tests used to call t.Setenv for each value, which pins the whole PROCESS
// environment -- and t.Setenv and t.Parallel are mutually exclusive, so the
// process being the carrier is exactly what kept them serial (measured
// 2026-09-24: 5.3s of the package's 17.6s). A hook is a grandchild of the
// command that starts it, and git passes its own environment down, so handing
// the values to git is equivalent and more honest about where they travel.
func childEnv(overrides ...string) []string {
	return replaceEnv(os.Environ(), overrides)
}

type liveHookRepo struct {
	root   string
	home   string
	extras string
	// env is what this fixture's git commands, and their hooks, run with.
	env []string
}

// withEnv returns the fixture with more variables set for the commands it
// spawns. The values travel with the fixture instead of through the process, so
// two fixtures can hold different answers at the same time.
func (r liveHookRepo) withEnv(overrides ...string) liveHookRepo {
	r.env = replaceEnv(r.env, overrides)
	return r
}

// newLiveHookRepo is a git repository whose hooks git will actually run, with
// a home of its own so nothing here reads the developer's.
//
// chainRoot is the dotfiles root whose generated entries and chain.env govern
// this repository, or "" for a fixture that runs no chain of its own. When it
// is set, the repository carries NO local core.hooksPath: the one the installer
// wrote into the fixture's global config is the one git finds, which is the
// whole point of the route this fixture exists to exercise. When it is empty
// the repository is inert -- nothing git does here runs this tool.
func newLiveHookRepo(t *testing.T, home, chainRoot string) liveHookRepo {
	t.Helper()
	root := t.TempDir()
	// HOME is the fixture's, because nothing in this fixture may read the
	// developer's, and so is the global config: with no GIT_CONFIG_GLOBAL a
	// commit here would read whatever ~/.gitconfig the machine running it
	// happens to have, which is the ambient input the design removes. There is
	// deliberately no AGENTS_DOTFILES_ROOT -- that variable is gone, and a
	// fixture that still set it would be describing a binary nobody ships.
	env := childEnv(
		"HOME="+home,
		"GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
	)
	emptyTemplate := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main", "--template=" + emptyTemplate},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := gitAttempt(root, env, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var extras string
	if chainRoot != "" {
		extras = filepath.Join(chainRoot, "git", "hooks")
	}
	return liveHookRepo{root: root, home: home, extras: extras, env: env}
}

// installHookChain lays out a dotfiles root and runs the real installer against
// it, so a fixture's hooks arrive the way a machine's do: four generated
// entries named by core.hooksPath, each reading chain.env and exec'ing the
// binary with an explicit --checkout.
//
// This is the only route from git to the dispatcher that survives the design.
// The env var and the link-time stamp are both gone, so a fixture that still
// reached the dispatcher by either of them would be testing a mechanism the
// binary no longer has.
func installHookChain(t *testing.T, binary, home string) string {
	t.Helper()
	// The chain directory is deliberately NOT created: stage 2's installer
	// creates the machine-owned one under the home it is handed, and a fixture
	// that had it already would never exercise that.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "git", "gitattributes"),
		[]byte(".agents/** linguist-generated=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(task18RepoRoot(t), "git", "install-hooks.sh")
	command := exec.Command("bash", script, "install", root, home, binary)
	command.Env = isolatedGitEnvironment(t, home, filepath.Join(home, ".gitconfig"))
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install hook chain: %v\n%s", err, out)
	}
	return root
}

func gitAttempt(root string, env []string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = env
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

// hookTestPath lays out the PATH a fixture's commits run with: a git symlink, so
// the hooks resolve the real git, and a gitleaks stub when scannerBody is not
// empty. Returned rather than installed into this process, because the commands
// under test are the ones that need it.
//
// The system directories are appended, and they are not decoration. A chain
// entry is a POSIX sh script that runs `dirname`, so a PATH holding only the
// fixture's two names makes every entry fail before it reaches this binary --
// measured: `dirname: command not found`, after which the entry's `cd` fell
// back to the caller's working directory and reported `chain.env` missing.
// They go AFTER the fixture's directory, so git and gitleaks still resolve to
// the ones this case chose.
func hookTestPath(t *testing.T, gitBinary, scannerBody string) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.Symlink(gitBinary, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	if scannerBody != "" {
		writeLiveFile(t, bin, "gitleaks", "#!/bin/sh\n"+scannerBody+"\n", 0o755)
	}
	return bin + ":/usr/bin:/bin"
}

func stageLive(t *testing.T, root string, env []string) {
	t.Helper()
	if out, err := gitAttempt(root, env, "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}

func TestLiveGitCommitsThroughTheInstalledChain(t *testing.T) {
	binary := buildTemporaryAgentsBinary(t)
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The machine this fixture models: a home carrying the global config and an
	// installed hook chain. The installer writes core.hooksPath into that
	// config, and the four entries it generates are what git runs. There is no
	// environment variable and no link-time stamp left to route around them, so
	// this is the only path from a commit to the dispatcher that exists.
	//
	// Each case installs its OWN chain, and that is not tidiness. The personal
	// stages live in <chain>/git/hooks, so two cases sharing one chain would
	// run each other's stages -- measured, as a hook failing on an environment
	// variable only its owner sets.
	withScanner := func(t *testing.T, scannerBody string, extra ...string) liveHookRepo {
		t.Helper()
		home := t.TempDir()
		chainRoot := installHookChain(t, binary, home)
		repo := newLiveHookRepo(t, home, chainRoot)
		// PATH is whose git is a symlink to the real one and whose gitleaks is
		// the stub this case wants.
		return repo.withEnv(append([]string{
			"PATH=" + hookTestPath(t, gitBinary, scannerBody),
		}, extra...)...)
	}

	t.Run("zero-arg pre-commit chains then maps advisory to success and commit-msg receives its exact argument", func(t *testing.T) {
		t.Parallel()
		repoCount := filepath.Join(t.TempDir(), "repo-count")
		extraCount := filepath.Join(t.TempDir(), "extra-count")
		commitArgs := filepath.Join(t.TempDir(), "commit-args")
		repo := withScanner(t, "exit 0",
			"REPO_COUNT="+repoCount, "EXTRA_COUNT="+extraCount, "COMMIT_ARGS="+commitArgs)
		writeLiveFile(t, repo.root, ".git/hooks/pre-commit", "#!/bin/sh\nprintf 'repo:%s\\n' \"$#\" >> \"$REPO_COUNT\"\n", 0o755)
		writeLiveFile(t, repo.extras, "a.pre-commit", "#!/bin/sh\nprintf 'extra:%s\\n' \"$#\" >> \"$EXTRA_COUNT\"\n", 0o755)
		writeLiveFile(t, repo.extras, "a.commit-msg", "#!/bin/sh\nfor arg do printf '<%s>\\n' \"$arg\"; done > \"$COMMIT_ARGS\"\n", 0o755)
		writeLiveFile(t, repo.root, ".agents/note.md", "ordinary agent note\n", 0o644)
		writeLiveFile(t, repo.root, "main.go", "package main\n", 0o644)
		stageLive(t, repo.root, repo.env)
		message := "test: multicall\n\nCo-Authored-By: Claude <noreply@anthropic.com>"
		out, err := gitAttempt(repo.root, repo.env, "commit", "-m", message)
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
		logged, err := gitAttempt(repo.root, repo.env, "log", "-1", "--format=%B")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(bytes.ToLower(logged), []byte("co-authored-by: claude")) {
			t.Fatalf("commit-msg attribution survived: %q", logged)
		}
	})

	t.Run("plain non-agent commit succeeds without scanner", func(t *testing.T) {
		t.Parallel()
		repo := withScanner(t, "")
		writeLiveFile(t, repo.root, "plain.txt", "plain\n", 0o644)
		stageLive(t, repo.root, repo.env)
		if out, err := gitAttempt(repo.root, repo.env, "commit", "-m", "plain"); err != nil {
			t.Fatalf("plain commit consulted missing scanner: %v\n%s", err, out)
		}
	})

	t.Run("scanner finding blocks without rendering staged content", func(t *testing.T) {
		t.Parallel()
		repo := withScanner(t, `printf '[{"RuleID":"fixture-rule","StartLine":1}]\n'; exit 1`)
		privateMarker := "private staged marker"
		writeLiveFile(t, repo.root, ".agents/note.md", privateMarker+"\n", 0o644)
		stageLive(t, repo.root, repo.env)
		out, err := gitAttempt(repo.root, repo.env, "commit", "-m", "blocked")
		if err == nil {
			t.Fatalf("scanner finding did not block: %s", out)
		}
		if !bytes.Contains(out, []byte("[fixture-rule]")) || bytes.Contains(out, []byte(privateMarker)) {
			t.Fatalf("scanner diagnostic missing attribution or leaked content: %q", out)
		}
	})

	t.Run("scanner failure blocks agent commit", func(t *testing.T) {
		t.Parallel()
		repo := withScanner(t, "exit 9")
		writeLiveFile(t, repo.root, ".agents/note.md", "ordinary\n", 0o644)
		stageLive(t, repo.root, repo.env)
		out, err := gitAttempt(repo.root, repo.env, "commit", "-m", "blocked")
		if err == nil || !bytes.Contains(out, []byte("could not complete operation")) {
			t.Fatalf("scanner failure did not block safely: err=%v out=%q", err, out)
		}
	})

	t.Run("foreign failure stops extras and guard", func(t *testing.T) {
		t.Parallel()
		scannerMarker := filepath.Join(t.TempDir(), "scanner-ran")
		extraMarker := filepath.Join(t.TempDir(), "extra-ran")
		repo := withScanner(t,
			`printf 'ran\n' > "$SCANNER_MARKER"; exit 0`,
			"SCANNER_MARKER="+scannerMarker, "EXTRA_MARKER="+extraMarker)
		writeLiveFile(t, repo.root, ".git/hooks/pre-commit", "#!/bin/sh\nexit 7\n", 0o755)
		writeLiveFile(t, repo.extras, "a.pre-commit", "#!/bin/sh\nprintf 'ran\\n' > \"$EXTRA_MARKER\"\n", 0o755)
		writeLiveFile(t, repo.root, ".agents/note.md", "ordinary\n", 0o644)
		stageLive(t, repo.root, repo.env)
		if out, err := gitAttempt(repo.root, repo.env, "commit", "-m", "blocked"); err == nil {
			t.Fatalf("foreign failure did not block: %s", out)
		}
		for _, path := range []string{extraMarker, scannerMarker} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("later stage ran after foreign failure (%s): %v", filepath.Base(path), err)
			}
		}
	})
}

// The cases below are deliberately NOT parallel with each other. Each runs the
// binary as a subprocess, which is parallelisable, but each also takes the
// process's HOME (for the guard's staging machinery) through t.Setenv, which is
// not. Running the built binary rather than calling into the process is what
// the shim's cases need: the branch they test is chosen from argv[0], and only
// a real exec can name the program something else.
//
// TestRunGitHookRunsPersonalHooksFromThisBinarysCheckout is GONE, and its
// subject is not. What it pinned was that the dispatcher ran the personal
// stages under the checkout it was TOLD about and not the ones under $HOME --
// a real property that has simply moved: it now belongs to
// `agents githook --checkout`, and TestGithookRunsTheRepositoryHookThenPersonalStagesThenBuiltinStages
// holds it there, decoy under $HOME included. What cannot move is the
// mechanism it used to name that checkout, because DotfilesRoot and
// AGENTS_DOTFILES_ROOT are both deleted. The shim's own route has no checkout
// to name at all, which is the subject of the case below.

// The shim answers EVERY hook name, with Git's arguments, and says what it
// cannot carry.
//
// Every name, not just pre-commit: a commit runs pre-commit AND commit-msg, so
// a shim that only recognised the bare `pre-commit` form leaves the second one
// failing every commit with an unknown-command error. Git's arguments are
// asserted through the repository's own hook, which sees exactly what git
// passed.
//
// The notice is the other half, and it is the reason this case exists rather
// than being folded into the chain cases. With the stamp and
// AGENTS_DOTFILES_ROOT gone the shim cannot find the personal stages, and
// githook.Chain reads a missing extras directory as "no personal hooks" and
// returns 0 -- so without a line on stderr the machine is told nothing while
// its `<anything>.<hook>` stages stop running. That is the failure class this
// redesign exists to remove, and a shim that reintroduced it silently would be
// the redesign's own defect wearing the migration's clothes.
func TestSymlinkShimAnswersEveryHookNameAndReportsTheMissingCheckout(t *testing.T) {
	binary := buildTemporaryAgentsBinary(t)
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		// args is what Git hands the hook, filled in below for commit-msg
		// because that one names a file that has to exist.
		args []string
	}{
		{"pre-commit", nil},
		{"commit-msg", []string{"<message file>"}},
		{"post-merge", []string{"1"}},
		{"post-checkout", []string{"old-sha", "new-sha", "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			repo := newLiveHookRepo(t, home, "")
			args := tc.args
			if tc.name == "commit-msg" {
				args = []string{writeLiveFile(t, repo.root, ".git/COMMIT_EDITMSG", "subject\n", 0o644)}
			}
			seen := filepath.Join(t.TempDir(), "repository hook args")
			// A `for` loop rather than `printf ... "$@"`: with no arguments
			// printf still runs its format once and writes a bare `<>`, so the
			// zero-argument pre-commit case could not tell "no arguments" from
			// "one empty argument". A loop writes nothing when there is
			// nothing, which is the property this case is asserting.
			writeLiveFile(t, repo.root, ".git/hooks/"+tc.name,
				"#!/bin/sh\nfor arg do printf '<%s>\\n' \"$arg\"; done > "+shellQuote(seen)+"\n", 0o755)
			writeLiveFile(t, repo.root, "plain.txt", "plain\n", 0o644)

			// The old wiring, exactly: a symlink named for the hook, pointing
			// at this binary. Nothing else names the checkout.
			shim := filepath.Join(t.TempDir(), tc.name)
			if err := os.Symlink(binary, shim); err != nil {
				t.Fatal(err)
			}
			env := repo.withEnv("PATH=" + hookTestPath(t, gitBinary, "exit 0")).env
			stageLive(t, repo.root, env)

			command := exec.Command(shim, args...)
			command.Dir = repo.root
			command.Env = env
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("%s through the symlink failed: %v\nstdout: %s\nstderr: %s",
					tc.name, err, stdout.String(), stderr.String())
			}

			data, readErr := os.ReadFile(seen)
			if readErr != nil {
				t.Fatalf("the repository's own %s hook did not run: %v\nstderr: %s",
					tc.name, readErr, stderr.String())
			}
			var want strings.Builder
			for _, a := range args {
				want.WriteString("<" + a + ">\n")
			}
			if string(data) != want.String() {
				t.Errorf("Git's arguments reached the repository hook as %q, want %q",
					data, want.String())
			}
			for _, phrase := range []string{tc.name, "symlink", "personal stages", "install-hooks.sh"} {
				if !strings.Contains(stderr.String(), phrase) {
					t.Errorf("the shim's notice does not name %q:\n%s", phrase, stderr.String())
				}
			}
		})
	}
}

func TestSymlinkShimMapsGuardExitClassesExactly(t *testing.T) {
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
			// Empty HOME is how this case says "no personal stages". There is
			// no environment variable left that could answer instead: the shim
			// has no checkout at all, which is what
			// TestSymlinkShimAnswersEveryHookNameAndReportsTheMissingCheckout
			// is about.
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", hookTestPath(t, gitBinary, tc.scannerBody))
			tc.seed(t, root)
			stageLive(t, root, childEnv())
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			got := runGitHookShim("pre-commit", nil, strings.NewReader(""), &stdout, &stderr)
			if got != tc.want {
				t.Fatalf("runGitHookShim exit=%d want=%d stdout=%q stderr=%q", got, tc.want, stdout.String(), stderr.String())
			}
			if tc.wantOutput != "" && !strings.Contains(stdout.String(), tc.wantOutput) {
				t.Fatalf("stdout missing %q: %q", tc.wantOutput, stdout.String())
			}
		})
	}
}

// The shim has no checkout, so it must not go looking for one. The decoy is the
// relative git/hooks/ inside the repository, which is what a dispatcher
// resolving personal stages against its own working directory would find --
// the guess that used to make a relocated checkout lose its hooks in silence.
func TestSymlinkShimDoesNotProbeARelativePersonalStagesDir(t *testing.T) {
	binary := buildTemporaryAgentsBinary(t)
	repo := newLiveHookRepo(t, t.TempDir(), "")
	ran := filepath.Join(t.TempDir(), "relative-hook-ran")
	writeLiveFile(t, repo.root, "git/hooks/a.post-merge",
		"#!/bin/sh\nprintf 'relative\\n' >> "+shellQuote(ran)+"\n", 0o755)

	shim := filepath.Join(t.TempDir(), "post-merge")
	if err := os.Symlink(binary, shim); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(shim)
	command.Dir = repo.root
	command.Env = repo.env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("the shim failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatalf("the shim probed and executed a relative personal stage; it is told no "+
			"checkout and must not look for one\n%s", output)
	}
}

// §8 test 3. Stage 1 could not pass this: the chain lived inside the checkout,
// so deleting the checkout deleted the guard. Stage 2 moved the chain under the
// home directory, and the two halves of the row are what this asserts --
//
//	the built-in guard still runs, and the personal stages are reported missing
//
// The second half is the one worth a line of output. githook treats an absent
// extras directory as "no personal stages" and carries on at exit 0, so without
// the report a machine would have lost its personal stages with Git, the guard
// and the commit all saying nothing.
func TestTheGuardSurvivesADeletedCheckout(t *testing.T) {
	t.Parallel()
	binary := buildTemporaryAgentsBinary(t)
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	checkout := installHookChain(t, binary, home)
	writeLiveFile(t, checkout, "git/hooks/a.pre-commit", "#!/bin/sh\nexit 0\n", 0o755)

	repo := newLiveHookRepo(t, home, checkout)
	// A scanner that always reports a finding, so "the guard ran" is visible as
	// a refused commit rather than as prose.
	repo = repo.withEnv("PATH=" + hookTestPath(t, gitBinary, `printf '[{"RuleID":"fixture-rule","StartLine":1}]\n'; exit 1`))
	writeLiveFile(t, repo.root, ".agents/note.md", "agent note\n", 0o644)
	stageLive(t, repo.root, repo.env)

	if err := os.RemoveAll(checkout); err != nil {
		t.Fatal(err)
	}

	out, err := gitAttempt(repo.root, repo.env, "commit", "-m", "the checkout is gone")
	if err == nil {
		t.Fatalf("the guard did not run after the checkout was deleted:\n%s", out)
	}
	if !bytes.Contains(out, []byte("[fixture-rule]")) {
		t.Errorf("the built-in guard's finding is missing from the refusal:\n%s", out)
	}
	for _, want := range []string{"personal stages", "are missing", checkout} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the report must say %q:\n%s", want, out)
		}
	}
}
