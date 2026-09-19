package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/registry"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// storeFlags collects the repeatable --stores flag: each occurrence is one
// role=path override, applied on top of the template defaults in the order it
// was given. It is a flag.Value because the flag package has no repeatable
// string flag, and a comma-separated list would make a path's own separators
// ambiguous -- "--stores design=a,b" cannot say which role b belongs to.
type storeFlags []string

func (s *storeFlags) String() string { return strings.Join(*s, ",") }

func (s *storeFlags) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// localV2Reason is Decision 6's refusal (design §0.7), shared by the two paths
// that reach it: a repository whose manifest already resolves v2, and an
// invocation whose layout flags are about to create one. It is one string
// because it is one reason -- the manifest would be machine-local either way,
// and a clone would then resolve v1 while the stores sat at the v2 paths.
const localV2Reason = "--local is not supported with an agents.layout/v2 layout (design §0.7, Decision 6): the /.agents/ exclude rule would make .agents/layout.json machine-local, so a clone would resolve v1 while the stores sat at the v2 paths; run `agents init` without --local"

func runInit(args []string, stdout io.Writer) int {
	return runInitWithVersion(args, stdout, version)
}

// runInitWithVersion is runInit with the running version injected, so a test
// can ask what init does in a repository whose manifest this binary may not
// write. The version decides only whether a declared or created v2 layout is
// mutable (design §5.3): a repository with no manifest, and no layout flag, is
// v1 and unaffected by it.
//
// The resolution order is design §7.5's: an existing manifest wins; layout
// flags with no manifest construct and write a v2 layout; neither is the v1
// behavior this command has always had.
func runInitWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stdout)
	local := fs.Bool("local", false, "keep .agents/ out of the repository")
	template := fs.String("template", "", "store defaults for a v2 layout: code-repo, content-vault, or custom")
	var stores storeFlags
	fs.Var(&stores, "stores", "override one role's store path: role=path (repeatable)")
	archive := fs.String("archive", "", "repository-relative archive path to record (v2 only)")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	// Which layout flags this invocation carried, asked by presence rather than
	// by value: `--template=` and `--archive=` are inputs the operator supplied,
	// and both are refused below rather than read as "no flag at all". This is
	// also the v2Mutation answer for the guard -- creating a v2 layout is
	// exactly the mutation design §0.7's trackedness pre-flight exists for, and
	// it is turned on here rather than after the manifest exists, because the
	// manifest is what the pre-flight protects.
	present := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { present[f.Name] = true })
	layoutFlagsPresent := present["template"] || present["stores"] || present["archive"]

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
	l, refusal := layoutRefusal(rc.Root, running, layoutFlagsPresent)
	if refusal != "" {
		fmt.Fprintf(stdout, "agents init: refusing to write: layout %s\n", refusal)
		return exitcode.Advisory
	}

	// target is the layout this invocation scaffolds from. A manifest already
	// in the repository is its own declaration (resolution order 1), so it is
	// the target and nothing about its layout is written.
	target := l
	if layoutFlagsPresent && l.ManifestPath == "" {
		// Resolution order 2: layout flags and no manifest create a v2 layout.
		//
		// Adopting an existing repository is `agents layout migrate`'s job, not
		// init's (design §7.5): the migration moves the stores and proves the
		// router is boilerplate. Either marker -- the v1 router or the docs/
		// shell -- means the layout question has already been answered by the
		// tree, and answering it again here would write a v2 manifest over
		// stores nobody has inspected.
		if marker := v1LayoutMarker(rc.Root); marker != "" {
			fmt.Fprintf(stdout, "agents init: refusing %s: this repository already has a v1 layout (%s is present), and adopting it is `agents layout migrate`'s job -- the migration moves the stores and proves the router is boilerplate; run `agents layout migrate` instead\n",
				flagsText(present), marker)
			return exitcode.Advisory
		}
		// Decision 6's flag-path half: the same refusal the resolved-layout
		// path below carries, because the manifest about to be written is the
		// one --local's exclude rule would hide. The check runs before the
		// manifest exists, which is the only moment it can.
		if *local {
			fmt.Fprintf(stdout, "agents init: %s\n", localV2Reason)
			return exitcode.Advisory
		}

		overrides, err := parseStores(stores)
		if err != nil {
			fmt.Fprintf(stdout, "agents init: %v\n", err)
			return exitcode.Malformed
		}
		arch, err := archiveForInit(rc.Root, present["archive"], *archive)
		if err != nil {
			fmt.Fprintf(stdout, "agents init: %v\n", err)
			return exitcode.Malformed
		}
		built, err := constructV2Layout(*template, overrides, arch)
		if err != nil {
			fmt.Fprintf(stdout, "agents init: %v\n", err)
			return exitcode.Malformed
		}
		// The same V1-V16 rules every manifest is held to, run before the write:
		// an absolute or escaping store path, two roles on one path, or an
		// archive overlapping a store is a typo in this invocation, and a
		// manifest recorded from it would be one every later command refuses.
		if ps := layout.Validate(rc.Root, built); len(ps) > 0 {
			fmt.Fprintf(stdout, "agents init: refusing layout: %s\n", problemsText(ps))
			return exitcode.Malformed
		}
		if err := layout.WriteManifest(rc.Root, built.Manifest); err != nil {
			fmt.Fprintf(stdout, "agents init: %v\n", err)
			return exitcode.NoRecord
		}
		// The constructed layout, never a re-resolve (design §7.5): the no-op
		// keys on a manifest that is present, active, and problem-free, and the
		// document just written is exactly that -- so resolving again here would
		// return before creating a single store and leave the repository with a
		// manifest and an empty tree.
		target = built
	} else if *local && l.Schema == layout.SchemaV2 {
		// Decision 6, design §0.7: --local's one mechanism is an ignore rule for
		// the whole .agents/ directory, and on a v2 repository that rule hides
		// the manifest -- the file that says which stores are the repository's.
		// A clone would then resolve v1 while the tracked stores sat at the v2
		// paths, so the flag is refused rather than writing machine state that
		// breaks the repository everywhere else. The wiring paths --local exists
		// to keep out of the tree are already excluded for every layout.
		fmt.Fprintf(stdout, "agents init: %s\n", localV2Reason)
		return exitcode.Advisory
	}

	// Scaffold from the layout just resolved or constructed, never from the
	// implicit v1: that is what stops `init` recreating docs/ in the v2
	// repository the guard has approved, which is the anti-shell guarantee of
	// design §1.2. A manifest already in the repository is its own declaration,
	// so CreateWithLayout writes no layout for it (design §7.5); it still writes
	// the machine exclude file, which is not layout data.
	if err := scaffold.CreateWithLayout(rc.Root, *local, target); err != nil {
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

// v1LayoutMarker names the first thing that makes this repository already a v1
// layout, or "" when it is not one. AGENTS.md is the router v1 init writes and
// docs/ is its store shell; either one means the layout question has already
// been answered by the tree, and design §7.5 answers it with migrate rather
// than init. The name is returned rather than a bool so the refusal can say
// what it found.
func v1LayoutMarker(root string) string {
	for _, rel := range []string{"AGENTS.md", "docs"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			return rel
		}
	}
	return ""
}

// flagsText names the layout flags an invocation carried, in the order the
// usage line lists them, so a refusal says which input it is refusing instead
// of "a layout flag".
func flagsText(present map[string]bool) string {
	var names []string
	for _, name := range []string{"template", "stores", "archive"} {
		if present[name] {
			names = append(names, "--"+name)
		}
	}
	return strings.Join(names, ", ")
}

// parseStores parses the repeatable --stores flag. Each value must be exactly
// role=path: a missing "=", an empty role, or an empty path is a malformed
// invocation, named in the error, rather than a manifest to write. The role
// itself is checked against the four by layout.TemplateStores, which owns the
// template table and the required-role rule.
func parseStores(values []string) (map[string]string, error) {
	overrides := make(map[string]string, len(values))
	for _, v := range values {
		role, path, ok := strings.Cut(v, "=")
		switch {
		case !ok:
			return nil, fmt.Errorf("--stores %q is not role=path", v)
		case role == "":
			return nil, fmt.Errorf("--stores %q names no role", v)
		case path == "":
			return nil, fmt.Errorf("--stores %q names no path", v)
		}
		// A repeated role is last-wins: the flag is applied in the order given,
		// and the later value is the one the operator last typed.
		overrides[role] = path
	}
	return overrides, nil
}

// constructV2Layout builds the layout an `init` flag invocation creates:
// schema v2, the floor v0.6.0 writes, an active status, the expanded store map,
// and the resolved archive. It is deliberately not a re-read of the manifest
// after the write -- the §7.5 no-op keys on a document that is present and
// active, so a re-resolved layout would return before creating a single store.
func constructV2Layout(template string, overrides map[string]string, archive string) (layout.Layout, error) {
	stores, err := layout.TemplateStores(template, overrides)
	if err != nil {
		return layout.Layout{}, err
	}
	return layout.Layout{Manifest: layout.Manifest{
		Schema:         layout.SchemaV2,
		MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus:   layout.StatusActive,
		Archive:        archive,
		Stores:         stores,
	}}, nil
}

// archiveForInit is the archive rule (design §7.5): --archive when it was given,
// and otherwise the v1 archive the tree already records -- docs/archive when
// that directory exists, empty otherwise, taken from the resolved v1 layout so
// the value never invents a path. An explicitly empty --archive is an input the
// operator supplied, not the absence of one, so it is refused.
func archiveForInit(root string, supplied bool, value string) (string, error) {
	if !supplied {
		return layout.V1ForRoot(root).Archive, nil
	}
	if value == "" {
		return "", fmt.Errorf("--archive names no path: it is the repository-relative directory whose contents are immutable")
	}
	return value, nil
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
