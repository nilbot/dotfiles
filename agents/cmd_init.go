package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/registry"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func runInit(args []string, stdout io.Writer) int {
	return runInitWithVersion(args, stdout, version)
}

// runInitWithVersion is runInit with the running version injected, so a test
// can ask what init does in a repository whose manifest this binary may not
// write. The version decides only whether a declared v2 layout is mutable
// (design §5.3); a repository with no manifest is v1 and unaffected by it.
func runInitWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stdout)
	local := fs.Bool("local", false, "keep .agents/ out of the repository")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	// Task 9 defines --template, --stores, and --archive and sets this from
	// them. Until those flags exist no `init` invocation writes a v2 layout, so
	// the trackedness pre-flight stays off -- which is what keeps `init --local`
	// working, the command whose whole purpose is to create an ignored .agents/.
	layoutFlagsPresent := false

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents init: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents init: not inside a git repository; nothing to do")
		return exitcode.Skip
	}

	// Before anything is written: an existing manifest wins (design §7.5), and
	// a layout this binary may not mutate is refused rather than half-scaffolded.
	// Advisory, not OK: the repository is untouched and the operator has a
	// remedy to read.
	if _, refusal := layoutRefusal(rc.Root, running, layoutFlagsPresent); refusal != "" {
		fmt.Fprintf(stdout, "agents init: refusing to write: layout %s\n", refusal)
		return exitcode.Advisory
	}

	if err := scaffold.Create(rc.Root, *local); err != nil {
		fmt.Fprintf(stdout, "agents init: %v\n", err)
		return exitcode.NoRecord
	}
	// The generated indexes are part of the scaffolded tree, not an optional
	// extra. Without them the pre-commit guard regenerates, finds them missing
	// from the index, and blocks -- so a freshly initialized repository could
	// not make its first commit without someone knowing to regenerate it
	// first. `init` owns the tree it creates, including the derived files in it.
	agentsDir := repo.AgentsDir(rc.Root)
	fmt.Fprintf(stdout, "initialized %s\n", agentsDir)
	if _, err := registry.Register(rc.Root, *local); err != nil {
		// The repository has already been initialized. The registry is a
		// disposable fleet cache, so its failure is a warning, never a rollback
		// or a reason to skip wiring this repository.
		fmt.Fprintf(stdout, "agents init: registry unavailable (%v); continuing\n", err)
	}

	if code := wireAll(rc.Root, stdout); code != exitcode.OK {
		return code
	}

	// Exit advisory, not OK. Wiring is written but not yet live, and reporting
	// success for a setup that is not recording anything would be the exact
	// silent failure this design exists to prevent.
	fmt.Fprintln(stdout, "\nRemaining trust steps (a hook cannot install itself):")
	for _, a := range harness.All() {
		for _, s := range a.TrustSteps(rc.Root) {
			fmt.Fprintf(stdout, "  - %s\n", s)
		}
	}
	fmt.Fprintln(stdout, "\nTo confirm the setup is recording, check Codex `/hooks` for Active hooks, or run `agents trace ls`.")
	return exitcode.Advisory
}

// binaryPath is the absolute path to write into generated configs. A harness
// runs hooks with an environment that is not the user's shell, so a bare
// "agents" is not reliably resolvable.
func binaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

// layoutRefusal returns a non-empty reason when a mutating command must not
// touch this repository, and the resolved layout alongside it. v2Mutation
// selects the trackedness pre-flight from design §0.7: it applies to writes
// that would create or refresh a v2 layout, never to plain v1 or `init --local`,
// whose whole point is the ignored .agents/.
//
// The reason is the bare token from layout.Support -- `below_floor`,
// `unknown_schema`, `unreleased`, `invalid`, `migrating` -- because fleet update
// prints it as `skip (layout <reason>)` and a sentence there would make one
// reason print two different lines. The two cases that are not tokens carry
// their own prose: a V-rule problem list, and an ignored .agents/, whose remedy
// is not an upgrade.
//
// Shared by `init` and fleet `update`; it is the single place a mutating
// command asks whether the manifest permits it (design §5.3). Reads are not
// gated by it: `layout show`, `layout validate`, `drift`, and `doctor` display
// a layout this refuses to write.
func layoutRefusal(root, running string, v2Mutation bool) (layout.Layout, string) {
	l := layout.Resolve(root)
	if len(l.Problems) > 0 {
		return l, "manifest invalid: " + problemsText(l.Problems)
	}
	if ok, reason := layout.Support(running, l); !ok {
		return l, reason
	}
	if v2Mutation {
		ignored, err := layout.AgentsIgnored(root)
		if err != nil {
			return l, "cannot determine whether .agents/ is ignored: " + safetext.Flatten(err.Error())
		}
		if ignored {
			return l, ".agents/ is ignored, so a manifest here would be machine-local (drop the ignore rule, or run without --local)"
		}
	}
	return l, ""
}

// problemsText renders a layout's problems as one line, the way drift and
// doctor render the same list. It is flattened because the reason lands on a
// single output line and a detail carries foreign text -- the operating
// system's or git's own message -- which can hold newlines.
func problemsText(problems []layout.Problem) string {
	return safetext.Flatten(drift.ProblemsText(problems))
}

func wireAll(root string, stdout io.Writer) int {
	bin, err := binaryPath()
	if err != nil {
		fmt.Fprintf(stdout, "agents: cannot resolve own path: %v\n", err)
		return exitcode.NoRecord
	}
	for _, a := range harness.All() {
		if err := a.Wire(root, bin); err != nil {
			fmt.Fprintf(stdout, "agents: wiring %s: %v\n", a.Name(), err)
			return exitcode.NoRecord
		}
		fmt.Fprintf(stdout, "wired %s -> %s\n", a.Name(), a.WireConfigPath(root))
	}
	return exitcode.OK
}
