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

// githookCase is one invocation of the subcommand with somewhere to record
// what the hook chain actually did, in order. A log rather than a set, because
// half of what this command owes is the ORDER of the three parts.
type githookCase struct {
	repo     string
	checkout string
	log      string
}

func newGithookCase(t *testing.T) githookCase {
	t.Helper()
	return githookCase{
		repo:     newRepo(t),
		checkout: t.TempDir(),
		log:      filepath.Join(t.TempDir(), "chain ran"),
	}
}

// stage writes an executable stage that appends its own label to the case's
// log, so a case can assert both that a stage ran and where in the chain it
// sat. dir is absolute and name is relative to it.
func (c githookCase) stage(t *testing.T, dir, name, label string) string {
	t.Helper()
	return writeLiveFile(t, dir, name,
		"#!/bin/sh\nprintf '%s\\n' "+shellQuote(label)+" >> "+shellQuote(c.log)+"\n", 0o755)
}

// ran is the log as a list, or nil when nothing ran at all. A missing file and
// an empty one mean different things here and the cases say which they want.
func (c githookCase) ran(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(c.log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// run invokes the subcommand from inside the repository, the way git runs a
// hook: the working directory is the repository, and stdin is whatever the
// hook would have been handed.
func (c githookCase) run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Chdir(c.repo)
	var out, errOut bytes.Buffer
	code = runGithook(args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), code
}

// The three parts, in the order the design states them: the repository's own
// hook first, then the personal stages, then the built-in stage.
//
// The two personal stages are written out of alphabetical order because the
// chain runs `<anything>.<hook>` in sorted order, and a case that wrote them
// in order would pass for a dispatcher that ran them in directory order.
//
// The decoy under $HOME is what makes the checkout half falsifiable. This
// command must take the checkout from `--checkout` and from nowhere else: a
// dispatcher that fell back to `$HOME/dotfiles`, or to the working directory,
// would run the decoy, and a case that only asserted the real stage ran would
// pass for one that ran both.
func TestGithookRunsTheRepositoryHookThenPersonalStagesThenBuiltinStages(t *testing.T) {
	c := newGithookCase(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	c.stage(t, filepath.Join(c.repo, ".git", "hooks"), "post-merge", "repository")
	c.stage(t, filepath.Join(c.checkout, "git", "hooks"), "b.post-merge", "personal b")
	c.stage(t, filepath.Join(c.checkout, "git", "hooks"), "a.post-merge", "personal a")
	decoy := c.stage(t, filepath.Join(home, "dotfiles", "git", "hooks"), "a.post-merge", "decoy under HOME")

	stdout, stderr, code := c.run(t, "post-merge", "--checkout", c.checkout)
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", code, exitcode.OK, stdout, stderr)
	}
	want := []string{"repository", "personal a", "personal b"}
	if got := c.ran(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the chain ran %q, want %q", got, want)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("the decoy stage was never written, so this case proves nothing: %v", err)
	}
	if strings.Contains(strings.Join(c.ran(t), ","), "decoy") {
		t.Errorf("a stage under $HOME ran; the checkout must come from --checkout alone")
	}
}

// `-` is the chain record saying this machine has no checkout, and it must
// stay distinguishable from "no personal stages were found". The decoy is the
// relative git/hooks/ inside the repository, which is exactly what a
// dispatcher guessing from its own working directory would find -- the probe
// that used to make a relocated checkout lose its hooks in silence.
func TestGithookTreatsDashAsNoPersonalStages(t *testing.T) {
	c := newGithookCase(t)
	c.stage(t, filepath.Join(c.repo, ".git", "hooks"), "post-merge", "repository")
	c.stage(t, filepath.Join(c.repo, "git", "hooks"), "a.post-merge", "probed relative")

	stdout, stderr, code := c.run(t, "post-merge", "--checkout", "-")
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", code, exitcode.OK, stdout, stderr)
	}
	want := []string{"repository"}
	if got := c.ran(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the chain ran %q, want %q; \"-\" means no personal stages, not a directory to guess at", got, want)
	}
}

// Git's own arguments follow the flag and are forwarded verbatim, and the
// built-in stage still runs after them. commit-msg is where both halves are
// load-bearing: Git hands it the message file's path, and the built-in stage
// is the one that reads that file and strips the attribution footer.
func TestGithookForwardsGitArgumentsVerbatimAndRunsTheBuiltinStage(t *testing.T) {
	c := newGithookCase(t)
	seen := filepath.Join(t.TempDir(), "repository hook args")
	writeLiveFile(t, filepath.Join(c.repo, ".git", "hooks"), "commit-msg",
		"#!/bin/sh\nprintf '<%s>\\n' \"$@\" > "+shellQuote(seen)+"\n", 0o755)

	message := writeLiveFile(t, c.repo, ".git/COMMIT_EDITMSG",
		"subject\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n", 0o644)

	stdout, stderr, code := c.run(t, "commit-msg", "--checkout", "-", message)
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", code, exitcode.OK, stdout, stderr)
	}
	data, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the repository hook did not run: %v", err)
	}
	if want := "<" + message + ">\n"; string(data) != want {
		t.Errorf("Git's argument reached the repository hook as %q, want %q", data, want)
	}
	stripped, err := os.ReadFile(message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stripped), "Co-Authored-By: Claude") {
		t.Errorf("the built-in commit-msg stage did not run; the message still carries the trailer:\n%s", stripped)
	}
}

// The invocation is generated by the installer, so anything that is not that
// shape is a caller this binary did not design for. Guessing would run a hook
// against a checkout nobody named, which is worse than refusing because it
// succeeds.
func TestGithookRefusesAnInvocationThatIsNotTheGeneratedShape(t *testing.T) {
	for _, tc := range []struct {
		what string
		args []string
	}{
		{"no arguments at all", nil},
		{"the hook name and nothing else", []string{"post-merge"}},
		{"the flag with no value", []string{"post-merge", "--checkout"}},
		{"the flag before the name", []string{"--checkout", "-", "post-merge"}},
		{"a hook name this tool does not install", []string{"post-rewrite", "--checkout", "-"}},
		{"a relative checkout path", []string{"post-merge", "--checkout", "dotfiles"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			c := newGithookCase(t)
			c.stage(t, filepath.Join(c.repo, ".git", "hooks"), "post-merge", "repository")

			stdout, stderr, code := c.run(t, tc.args...)
			if code != exitcode.Malformed {
				t.Errorf("exit = %d, want %d\nstdout: %s\nstderr: %s",
					code, exitcode.Malformed, stdout, stderr)
			}
			if !strings.Contains(stderr, "agents githook") {
				t.Errorf("the refusal does not name the command it refused:\n%s", stderr)
			}
			if got := c.ran(t); len(got) != 0 {
				t.Errorf("a refused invocation still ran the chain: %q", got)
			}
		})
	}
}

// The seam no other case crosses, and the reason this task exists: the entry
// the installer actually writes, executed by the shell, reaching the
// subcommand this file is about. Every piece is real -- the installer, the
// generated entry, the built binary, a real repository.
//
// The installer's own suite stops at a recording stub: it proves the entry
// execs `$binary githook <hook> --checkout <checkout>` and nothing more. What
// it cannot prove is that the real binary answers that call, which is the
// half that was missing and the half a commit would have failed on.
func TestTheInstalledEntryDrivesGithookEndToEnd(t *testing.T) {
	fixture := newHookInstallFixture(t)
	buildAgentsBinaryAt(t, fixture.binary)

	ran := filepath.Join(t.TempDir(), "chain ran")
	writeLiveFile(t, fixture.repoRoot, "git/hooks/a.post-merge",
		"#!/bin/sh\nprintf 'personal\\n' >> "+shellQuote(ran)+"\n", 0o755)

	if output, err := runHookInstaller(t, fixture, "install"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	repo := newRepo(t)
	writeLiveFile(t, repo, ".git/hooks/post-merge",
		"#!/bin/sh\nprintf 'repository\\n' >> "+shellQuote(ran)+"\n", 0o755)

	command := exec.Command(hookEntryPath(fixture, "post-merge"))
	command.Dir = repo
	command.Env = isolatedGitEnvironment(t, fixture.home, fixture.globalConfig)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("the installed entry failed: %v\n%s", err, output)
	}
	got, readErr := os.ReadFile(ran)
	if readErr != nil {
		t.Fatalf("neither the repository hook nor the personal stage ran: %v\n%s", readErr, output)
	}
	if want := "repository\npersonal\n"; string(got) != want {
		t.Errorf("the installed entry ran %q, want %q\nentry output:\n%s", got, want, output)
	}
}

// buildAgentsBinaryAt builds the real binary where the installer expects to
// find one, replacing the stub the fixture lays down.
//
// It builds beside the target and renames, because `go build -o` refuses a
// path that already holds a non-object file -- and the fixture's stub is a
// shell script. Renaming over it is also what makes the replacement atomic,
// which matters here: the installer is handed this path and validates it.
func buildAgentsBinaryAt(t *testing.T, path string) {
	t.Helper()
	built := path + ".built"
	command := exec.Command("go", "build", "-o", built, ".")
	// TestMain moves the working directory out of the checkout, so `.` and the
	// module it belongs to have to be named explicitly.
	command.Dir = packageDir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build agents binary beside %s: %v\n%s", path, err, out)
	}
	if err := os.Rename(built, path); err != nil {
		t.Fatalf("replace the fixture's stub at %s: %v", path, err)
	}
}
