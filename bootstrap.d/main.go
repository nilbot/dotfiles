// Command bootstrap provisions this workstation.
// See docs/design/2026-08-07-spec-2-dotfiles-hygiene.md
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/check"
	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
	"github.com/nilbot/dotfiles/bootstrap/internal/migrate"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

// Exit codes are spec 1 §6's shared table, so one vocabulary covers both tools
// in this repository.
const (
	exitOK            = 0
	exitAdvisory      = 1
	exitBlock         = 2
	exitMalformed     = 3
	exitNotApplicable = 4
)

const usage = `usage: bootstrap <verb> [argument]

verbs:
  plan  [profile]   show what would change; writes nothing
  apply [profile]   converge this machine
  check [profile]   report whether this machine is healthy
  migrate [name]    run reconciling migrations; list reclaiming ones
                    with a name, run that one migration
  --help            this text

profiles:
  workstation       everything (default): packages, config, shell, devtools
  dotfiles          preflight + config + verify only.
                    No sudo, no network, no package manager, no shell change.

exit codes: 0 ok  1 advisory  2 block  3 malformed input  4 not applicable
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	verb := "--help"
	if len(args) > 0 {
		verb = args[0]
	}
	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}

	switch verb {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "plan", "apply":
		return runProfile(verb, orDefault(arg, "workstation"), stdout, stderr)
	case "check":
		return runCheck(orDefault(arg, "workstation"), stdout, stderr)
	case "migrate":
		return runMigrate(arg, stdout, stderr)
	}
	fmt.Fprintf(stderr, "bootstrap: unknown verb %q; try --help\n", verb)
	return exitMalformed
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func platform() (string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		return runtime.GOOS, nil
	}
	return "", fmt.Errorf("unsupported operating system %q", runtime.GOOS)
}

// root is the repository root. BOOTSTRAP_ROOT is set by the shim; the fallback
// walks up from the executable. Never pwd.
func root() (string, error) {
	if r := os.Getenv("BOOTSTRAP_ROOT"); r != "" {
		return r, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// currentUser prefers the passwd database over $USER. $USER is inherited and
// can name someone else entirely -- after `sudo -E`, or a container started
// with `docker exec -u` -- and the login shell being changed belongs to the
// account this process actually runs as, not to whoever exported the variable.
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// commandOutput runs a command and returns what it wrote to standard output.
// loginShell takes one of these rather than calling exec itself so its parsing
// and its fallbacks can be exercised without running dscl or getent against the
// accounts of whatever machine the suite is on.
type commandOutput func(name string, args ...string) ([]byte, error)

// databaseTimeout bounds the user-database read.
//
// Reading $SHELL could not hang; running a command can. `getent passwd` goes
// through NSS, so on a machine bound to a directory service it can block on the
// network, and a wedged DirectoryService does the same to dscl -- which would
// stop plan, apply and check dead, on the machines least likely to be able to
// afford it. The resolver's whole posture is that it never fails, it falls back;
// an unbounded wait is the one failure mode that contradicts that, because it
// does not degrade, it stops.
//
// Five seconds is far longer than a local record read (milliseconds) and far
// shorter than a stuck directory lookup, which has no bound at all. The
// trade-off it accepts: a genuine lookup slower than this falls back to $SHELL
// and may report the stale answer -- the original defect, for that one run, on
// that one class of machine. Hanging forever is worse.
const databaseTimeout = 5 * time.Second

// runCommand is the real commandOutput, bounded by databaseTimeout.
func runCommand(name string, args ...string) ([]byte, error) {
	return runCommandWithin(databaseTimeout, name, args...)
}

// runCommandWithin takes the deadline as an argument so a test can pin the
// giving-up behaviour without waiting the real one out.
//
// Standard error is discarded rather than combined, because nothing a tool
// writes there is part of the answer and a diagnostic must never be offered to
// the parser as a record. Neither choice can reach this process's own stderr --
// Output captures it into ExitError.Stderr, CombinedOutput returns it in the
// slice -- so nothing here can leak into a plan either way.
func runCommandWithin(limit time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// The context kills the process, but Output() waits for every writer of the
	// stdout pipe to close -- and a shell's grandchild inherits that pipe and
	// outlives the kill, so the deadline bounded nothing. Measured on
	// ubuntu-latest: a 50ms deadline waited the full 10s. WaitDelay bounds the
	// post-cancel I/O wait to one second, then closes the pipes and returns.
	cmd.WaitDelay = 1 * time.Second
	return cmd.Output()
}

// loginShell reports the shell this account logs in with, which is a property
// of the passwd database and not of this process.
//
// hint is $SHELL, and it is ONLY a hint: it is inherited from whatever process
// started this one, so a bootstrap run launched from a zsh terminal reports
// /bin/zsh on a machine whose passwd entry has said /opt/homebrew/bin/fish for
// months. That is what happened -- `check` reported a false login-shell failure
// ("the login shell is /bin/zsh, not fish; run './bootstrap apply workstation'")
// on a machine already provisioned exactly as this repository intends, purely
// because of which shell the run was started from. $SHELL describes the
// session; the passwd database describes the account, and the account is what
// the login-shell check and the fish phase are both about.
//
// Go's os/user deliberately exposes no shell field, so the database is read
// through the platform's own tool. Every way that can fail -- the tool absent,
// a non-zero exit, output that does not parse, an empty shell field -- falls
// back to the hint, which is a worse answer than the database and a far better
// one than none: check reads an empty shell as "the login shell cannot be
// determined at all".
func loginShell(read commandOutput, plat, name, hint string) string {
	var argv []string
	switch {
	// No name, no record: `dscl . -read /Users/ UserShell` and `getent passwd ""`
	// ask about an account that does not exist, and running a command to be told
	// so is still running a command during a plan.
	case name == "":
		return hint
	case plat == "darwin":
		argv = []string{"dscl", ".", "-read", "/Users/" + name, "UserShell"}
	case plat == "linux":
		argv = []string{"getent", "passwd", name}
	default:
		return hint
	}
	out, err := read(argv[0], argv[1:]...)
	if err != nil {
		return hint
	}
	if shell := parseLoginShell(plat, string(out)); shell != "" {
		return shell
	}
	return hint
}

// parseLoginShell reads the shell out of one tool's output, answering "" for
// anything it does not recognise -- which the caller reads as "use the hint".
//
// Neither format is matched loosely. dscl can print its DS Error for a record
// it cannot find on standard output, and a passwd line can arrive truncated; a
// parser that returned whatever it found would hand the login-shell check a
// path that is not a shell, and the check would then fail while naming it.
func parseLoginShell(plat, out string) string {
	for _, line := range strings.Split(out, "\n") {
		switch plat {
		case "darwin":
			// `UserShell: /opt/homebrew/bin/fish`. dscl prints the key alone with
			// the value indented beneath it when the value contains a space; no
			// shell path here does, and answering "" sends such a machine to the
			// hint rather than to a wrong path.
			if rest, found := strings.CutPrefix(line, "UserShell:"); found {
				return strings.TrimSpace(rest)
			}
		case "linux":
			// name:x:uid:gid:gecos:home:shell -- seven fields, the shell last. A
			// line with fewer is truncated rather than short, and reaching for its
			// final field would return the home directory.
			if fields := strings.Split(line, ":"); len(fields) == 7 {
				return strings.TrimSpace(fields[6])
			}
		}
	}
	return ""
}

func runProfile(verb, profile string, stdout, stderr io.Writer) int {
	phases, err := phase.For(profile)
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitMalformed
	}
	plat, err := platform()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitNotApplicable
	}
	repoRoot, err := root()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitBlock
	}

	applier := change.NewApplier(stdout, repoRoot)
	var machine change.Interface = applier
	if verb == "plan" {
		machine = change.NewPlanner(applier, stdout, repoRoot)
	}

	name := currentUser()
	ctx := phase.Context{
		Change: machine, Root: repoRoot, Home: os.Getenv("HOME"),
		Platform: plat, Profile: profile,
		Shell: loginShell(runCommand, plat, name, os.Getenv("SHELL")),
		User:  name, Out: stdout,
	}
	for _, p := range phases {
		if err := p.Run(ctx); err != nil {
			// A Refusal carries a remediation; surfacing it on its own line is
			// the entire reason the type has that field. Everything else prints
			// plainly.
			var refusal *change.Refusal
			if errors.As(err, &refusal) {
				fmt.Fprintf(stderr, "bootstrap: %s: refusing: %s\n  problem: %s\n  remedy:  %s\n",
					p.Name, refusal.Path, refusal.Problem, refusal.Remediation)
				return exitBlock
			}
			fmt.Fprintf(stderr, "bootstrap: %s: %v\n", p.Name, err)
			// A malformed manifest is bad INPUT, not a refused machine. A
			// wrapping script must be able to tell "fix your typo" from
			// "bootstrap declined to touch this box".
			var syntax *manifest.SyntaxError
			if errors.As(err, &syntax) {
				return exitMalformed
			}
			return exitBlock
		}
	}
	return exitOK
}

// runCheck reports whether this machine is healthy, and is the one caller that
// exits on the answer. phase.Verify runs the same checks and returns nil: an
// advisory finding at the end of an apply must not look like a failed apply.
func runCheck(profile string, stdout, stderr io.Writer) int {
	// Validated by the one place that knows the profile names, so `check` and
	// `apply` can never disagree about which profiles exist. The phase list
	// itself is not wanted here -- check runs no phases.
	if _, err := phase.For(profile); err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitMalformed
	}
	plat, err := platform()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitNotApplicable
	}
	repoRoot, err := root()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitBlock
	}
	home := os.Getenv("HOME")
	// The same guard preflight applies, for the same reason: without HOME every
	// managed path resolves somewhere else entirely, and a report about the
	// wrong paths is worse than no report.
	if home == "" {
		fmt.Fprintln(stderr, "bootstrap: check: HOME is empty; "+
			"every managed path is resolved against it")
		return exitBlock
	}

	// An Applier, never a Planner: a check that asks its question by running a
	// command would read a Planner's recorded nil as success. See
	// internal/check's package comment.
	results, err := check.All(check.Context{
		Change: change.NewApplier(stdout, repoRoot), Root: repoRoot, Home: home,
		Platform: plat, Profile: profile,
		Shell: loginShell(runCommand, plat, currentUser(), os.Getenv("SHELL")),
	})
	fmt.Fprintln(stdout, "== check")
	check.Write(stdout, results)

	// A manifest that does not parse is bad INPUT, and the same typo must answer
	// 3 to every verb. Reporting it as 2 would say this machine is in a state
	// bootstrap will not touch, which is not what happened -- and telling those
	// apart is the whole reason both codes exist.
	var syntax *manifest.SyntaxError
	if errors.As(err, &syntax) {
		fmt.Fprintf(stderr, "bootstrap: check: %v\n", err)
		return exitMalformed
	}

	// The shared vocabulary crosses a package boundary here as a bare int, and
	// that is exactly how `exitAdvisory` came to look like a constant nobody
	// returns: the 1 is produced in internal/check (its own test calls that
	// case "a warning is advisory"), the name is declared in this file, and
	// nothing connected the two. A grep of main.go therefore reported the
	// behaviour as absent when `bootstrap check` has always had it.
	//
	// Mapping explicitly ties each number to the name for it, and the default
	// fails closed if internal/check ever returns a code this table has no
	// name for -- rather than passing an unrecognised number to the caller.
	switch code := check.ExitCode(results); code {
	case exitOK:
		return exitOK
	case exitAdvisory:
		return exitAdvisory
	case exitBlock:
		return exitBlock
	default:
		fmt.Fprintf(stderr, "bootstrap: check: internal/check returned %d, "+
			"which is not in the shared exit-code table\n", code)
		return exitBlock
	}
}

// runMigrate reconciles a machine provisioned by an older layout. With no name
// it runs every pending reconciling migration and lists the reclaiming ones it
// is eligible to run; with one, it runs that migration alone.
//
// An Applier, never a Planner, for the same reason internal/check gives: there
// is no `plan migrate`, and a Planner here would report a machine reconciled
// without having moved a byte.
//
// Nothing pending answers 0, not 4. A migration that has already run is not
// "not applicable" -- the machine is in the state that was wanted -- and
// `./bootstrap migrate && ./bootstrap apply` must not fail on a healthy box.
func runMigrate(name string, stdout, stderr io.Writer) int {
	repoRoot, err := root()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitBlock
	}
	home := os.Getenv("HOME")
	// The same guard preflight and check apply. A migration resolved against ""
	// would move data out of a checkout and into "/", which is the one mistake
	// here that cannot be undone.
	if home == "" {
		fmt.Fprintln(stderr, "bootstrap: migrate: HOME is empty; "+
			"every managed path is resolved against it")
		return exitBlock
	}

	err = migrate.Run(migrate.Context{
		Change: change.NewApplier(stdout, repoRoot),
		Root:   repoRoot, Home: home, Out: stdout,
	}, name)
	if err == nil {
		return exitOK
	}

	// A name that matches no migration is bad INPUT, exactly like an unknown
	// verb or an unknown profile, and answers 3 as they do.
	var unknown *migrate.UnknownError
	if errors.As(err, &unknown) {
		fmt.Fprintf(stderr, "bootstrap: migrate: %v\n", err)
		return exitMalformed
	}
	var refusal *change.Refusal
	if errors.As(err, &refusal) {
		fmt.Fprintf(stderr, "bootstrap: migrate: refusing: %s\n  problem: %s\n  remedy:  %s\n",
			refusal.Path, refusal.Problem, refusal.Remediation)
		return exitBlock
	}
	fmt.Fprintf(stderr, "bootstrap: migrate: %v\n", err)
	return exitBlock
}
