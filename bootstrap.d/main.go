// Command bootstrap provisions this workstation.
// See docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/check"
	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
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

	applier := change.NewApplier(stdout)
	var machine change.Interface = applier
	if verb == "plan" {
		machine = change.NewPlanner(applier, stdout)
	}

	ctx := phase.Context{
		Change: machine, Root: repoRoot, Home: os.Getenv("HOME"),
		Platform: plat, Profile: profile, Shell: os.Getenv("SHELL"), Out: stdout,
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

	results := check.All(check.Context{
		Change: change.NewApplier(stdout), Root: repoRoot, Home: home,
		Platform: plat, Profile: profile, Shell: os.Getenv("SHELL"),
	})
	fmt.Fprintln(stdout, "== check")
	check.Write(stdout, results)
	return check.ExitCode(results)
}

// runMigrate arrives in a later task; until then every invocation is honestly
// "not applicable" rather than a false success.
func runMigrate(string, io.Writer, io.Writer) int { return exitNotApplicable }
