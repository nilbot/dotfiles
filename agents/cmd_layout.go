package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// layoutRepo resolves the working directory to the repository root that owns
// .agents/. It returns the root and an exit code; a non-zero code means the
// caller must return it unchanged.
//
// The root comes from the working directory rather than a --repo flag because
// every caller of this family is a skill already running inside the repository
// it asks about; a path argument would be one more thing for a skill to get
// wrong. The refusal is exit 4, the code design §7.1 gives `validate` and
// `path` outside a repository with .agents/; `show` shares it so that all
// three answer the same way about the same state.
func layoutRepo(stdout io.Writer) (string, int) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents layout: %v\n", err)
		return "", exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents layout: not inside a git repository with .agents/; nothing to report")
		return "", exitcode.Skip
	}
	info, err := os.Stat(repo.AgentsDir(rc.Root))
	if err != nil || !info.IsDir() {
		fmt.Fprintln(stdout, "agents layout: this repository has no .agents/; run `agents init` first")
		return "", exitcode.Skip
	}
	return rc.Root, exitcode.OK
}

// printProblems writes one line per problem, in the shape drift and doctor
// render (`code path (detail)`) so the three surfaces cannot describe the same
// broken manifest differently.
//
// Every line is flattened: Problem.Detail is foreign text -- a failed read
// carries the operating system's or git's own message, which can hold newlines
// -- and design §7.1 promises one line per problem. There is deliberately no
// code-to-explanation table: a code this binary does not recognize is printed
// as the code it is, never dropped because no prose was written for it.
func printProblems(w io.Writer, problems []layout.Problem) {
	for _, p := range problems {
		fmt.Fprintln(w, safetext.Flatten(drift.ProblemsText([]layout.Problem{p})))
	}
}

// printLayout renders the resolved layout as design §7.1 describes it: schema,
// status, min_mut_ver_floor, stores, archive, and one line per role. The
// Mutating line answers the question a reader of `show` usually has -- whether
// this binary may act on what it just printed -- and is why the running
// version is threaded here.
func printLayout(w io.Writer, l layout.Layout, running string) {
	fmt.Fprintf(w, "Schema:            %s\n", safetext.Flatten(l.Schema))
	fmt.Fprintf(w, "Status:            %s\n", safetext.Flatten(l.LayoutStatus))
	fmt.Fprintf(w, "min_mut_ver_floor: %s\n", orNone(l.MinMutVerFloor))
	fmt.Fprintln(w, "Stores:")
	for _, role := range layout.Roles() {
		printStoreLine(w, l, role)
	}
	// A manifest may invent a role; V1 rejects it, and the operator who wrote
	// it needs to see it here as well as in the problem list.
	for _, role := range unknownRoles(l.Stores) {
		printStoreLine(w, l, role)
	}
	fmt.Fprintf(w, "Archive:           %s\n", orNone(l.Archive))
	if ok, reason := mutationVerdict(running, l); ok {
		fmt.Fprintf(w, "Mutating:          yes (running %s)\n", safetext.Flatten(running))
	} else {
		fmt.Fprintf(w, "Mutating:          no (%s; running %s)\n",
			safetext.Flatten(reason), safetext.Flatten(running))
	}
}

// mutationVerdict is the one answer `show`'s human report and `validate` both
// give to "may this binary mutate this repository". layout.Support is only the
// version and status gate; a V-rule makes the whole layout unsupported for
// mutation too (design §5.2), so an invalid manifest must not read as "yes" on
// one surface while the other says `invalid`. The reason strings are the ones
// drift's `unsupported` field uses.
func mutationVerdict(running string, l layout.Layout) (bool, string) {
	supported, reason := layout.Support(running, l)
	if len(l.Problems) > 0 {
		supported = false
		if layout.HasProblem(l.Problems, layout.ProblemSchemaUnknown) {
			reason = "unknown_schema"
		} else {
			reason = "invalid"
		}
	}
	return supported, reason
}

func printStoreLine(w io.Writer, l layout.Layout, role string) {
	path, ok := layout.Path(l, role)
	if !ok || path == "" {
		fmt.Fprintf(w, "  %-9s (unresolved)\n", safetext.Flatten(role)+":")
		return
	}
	fmt.Fprintf(w, "  %-9s %s\n", safetext.Flatten(role)+":", safetext.Flatten(path))
}

// unknownRoles lists the roles a manifest invented, sorted, so the report is
// the same on every run rather than in Go's map order.
func unknownRoles(stores map[string]string) []string {
	known := map[string]bool{}
	for _, role := range layout.Roles() {
		known[role] = true
	}
	var extra []string
	for role := range stores {
		if !known[role] {
			extra = append(extra, role)
		}
	}
	sort.Strings(extra)
	return extra
}

// orNone renders an absent optional field as "(none)" and flattens the value:
// it is manifest-authored text landing on a line the reader reads as
// structure.
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return safetext.Flatten(s)
}

func runLayoutShow(args []string, stdout io.Writer) int {
	return runLayoutShowWithVersion(args, stdout, version)
}

// runLayoutShowWithVersion is runLayoutShow with the running version injected,
// so a test can ask what a repository looks like to a binary that may not
// mutate it.
func runLayoutShowWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("layout show", flag.ContinueOnError)
	fs.SetOutput(stdout)
	asJSON := fs.Bool("json", false, "emit the resolved layout as JSON")
	router := fs.Bool("router", false, "print the canonical router and nothing else")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents layout show: unexpected operand")
		}
		return exitcode.Malformed
	}
	root, code := layoutRepo(stdout)
	if code != exitcode.OK {
		return code
	}
	l := layout.Resolve(root)

	// --router is the migration skill's byte-restore path: it captures this
	// output and writes it to AGENTS.md. It prints the router and nothing else
	// -- no problem lines, no report -- because any extra byte corrupts the
	// file it restores. It is selected by schema, not by validity: a v2
	// repository with a bad store path still has a v2 router to restore.
	//
	// The v1 bytes are only for the implicit layout, where no manifest exists
	// at all (ManifestPath == ""). A manifest that could not be read or parsed,
	// or that declares a schema this binary does not know, resolves to a
	// non-v2 schema with a manifest on disk -- and answering it with the v1
	// router would write bytes naming docs/ stores that repository may not
	// have, with exit 0. That is the corruption this path exists to prevent,
	// so it refuses instead: nothing printed, exit 1.
	if *router {
		switch {
		case l.Schema == layout.SchemaV2:
			fmt.Fprint(stdout, layout.V2AgentsMD)
			return exitcode.OK
		case l.ManifestPath == "":
			fmt.Fprint(stdout, scaffold.DefaultAgentsMD)
			return exitcode.OK
		default:
			return exitcode.Advisory
		}
	}

	// The machine path prints the object and nothing else. The Layout carries
	// its own `problems` array, so the human problem lines would be redundant
	// here -- and a line of prose in front of the object is not JSON, which
	// made every consumer of `show --json` fail on exactly the repositories
	// they most needed to hear about.
	if *asJSON {
		b, err := json.MarshalIndent(l, "", "  ")
		if err != nil {
			fmt.Fprintf(stdout, "agents layout show: %v\n", err)
			return exitcode.NoRecord
		}
		stdout.Write(append(b, '\n'))
	} else {
		// The human path prints the problems first and the report after, so a
		// reader asking what this repository resolves to is answered even when
		// the manifest is wrong. The exit code is what reports the problem.
		if len(l.Problems) > 0 {
			printProblems(stdout, l.Problems)
		}
		printLayout(stdout, l, running)
	}
	if len(l.Problems) > 0 {
		return exitcode.Advisory
	}
	// An unsupported layout reads fine and must still be shown; it is only
	// mutation that this binary cannot promise.
	if ok, _ := layout.Support(running, l); !ok {
		return exitcode.Advisory
	}
	return exitcode.OK
}

// layoutValidateReport is the `--json` shape of `layout validate`: one object,
// documented in the command's help and in agents/README.md. Problems is always
// an array, never null, so a consumer can count it without a nil check.
type layoutValidateReport struct {
	ManifestPath string           `json:"manifest_path,omitempty"`
	Problems     []layout.Problem `json:"problems"`
	Supported    bool             `json:"supported"`
	Reason       string           `json:"reason,omitempty"`
	Schema       string           `json:"schema"`
	LayoutStatus string           `json:"layout_status"`
}

func runLayoutValidate(args []string, stdout io.Writer) int {
	return runLayoutValidateWithVersion(args, stdout, version)
}

// runLayoutValidateWithVersion runs V1-V16 and reports each problem on its own
// line. Exit 0 for a valid layout this binary may mutate, 1 for problems or an
// unsupported layout, 4 outside a repository with .agents/.
func runLayoutValidateWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("layout validate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	asJSON := fs.Bool("json", false, "emit the validation result as one JSON object")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents layout validate: unexpected operand")
		}
		return exitcode.Malformed
	}
	root, code := layoutRepo(stdout)
	if code != exitcode.OK {
		return code
	}
	l := layout.Resolve(root)
	// The same verdict `show`'s human report prints, from the same helper: two
	// surfaces of one command must not disagree about one repository.
	supported, reason := mutationVerdict(running, l)

	if *asJSON {
		problems := l.Problems
		if problems == nil {
			problems = []layout.Problem{}
		}
		b, err := json.MarshalIndent(layoutValidateReport{
			ManifestPath: l.ManifestPath,
			Problems:     problems,
			Supported:    supported,
			Reason:       reason,
			Schema:       l.Schema,
			LayoutStatus: l.LayoutStatus,
		}, "", "  ")
		if err != nil {
			fmt.Fprintf(stdout, "agents layout validate: %v\n", err)
			return exitcode.NoRecord
		}
		stdout.Write(append(b, '\n'))
	} else {
		printProblems(stdout, l.Problems)
		if !supported {
			// The version is worth naming only when the version is the reason;
			// for `invalid` the problem lines above already say why.
			switch reason {
			case "below_floor", "unreleased":
				fmt.Fprintf(stdout, "unsupported: %s (running %s, min_mut_ver_floor %s)\n",
					safetext.Flatten(reason), safetext.Flatten(running), orNone(l.MinMutVerFloor))
			default:
				fmt.Fprintf(stdout, "unsupported: %s\n", safetext.Flatten(reason))
			}
		}
	}
	if !supported {
		return exitcode.Advisory
	}
	return exitcode.OK
}

func runLayoutPath(args []string, stdout io.Writer) int {
	return runLayoutPathWithVersion(args, stdout, version)
}

// runLayoutPathWithVersion prints one repository-relative store path and
// nothing else, so a skill can use it in a command substitution.
//
// A layout that is invalid, unsupported, or migrating refuses before any role
// is looked up: it prints nothing, because a caller that captured a refusal
// message would use it as a path, and the exit code names the state (1 for
// invalid, 4 for unsupported or migrating).
func runLayoutPathWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("layout path", flag.ContinueOnError)
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdout, "agents layout path: exactly one role is required")
		return exitcode.Malformed
	}
	role := fs.Arg(0)

	root, code := layoutRepo(stdout)
	if code != exitcode.OK {
		return code
	}
	l := layout.Resolve(root)
	// Precedence is deliberate, not an accident of ordering: the layout's own
	// state is weighed before the role is looked up, so a manifest too broken
	// to resolve a role is reported as an invalid layout (1) rather than as the
	// caller's typo (3), and an unsupported one refuses with 4 either way.
	// Design §7.1 gives an invalid or unsupported layout those dispositions in
	// every case, and a caller is never told its role name was wrong when the
	// real problem is the manifest.
	if len(l.Problems) > 0 {
		return exitcode.Advisory
	}
	if ok, _ := layout.Support(running, l); !ok {
		return exitcode.Skip
	}
	path, ok := layout.Path(l, role)
	if !ok {
		fmt.Fprintf(stdout, "agents layout path: unknown role %q\n", safetext.Flatten(role))
		return exitcode.Malformed
	}
	// The path is printed raw rather than flattened: this is a value the caller
	// uses as a path, and a flattened path is a different, wrong path. The
	// one-line promise belongs to the problem printers, where the text is
	// prose about a manifest rather than a path to open.
	fmt.Fprintln(stdout, path)
	return exitcode.OK
}

// ---------------------------------------------------------------------------
// agents layout migrate: the one mutating member of the family (design §7.2)
// ---------------------------------------------------------------------------

func runLayoutMigrate(args []string, stdout io.Writer) int {
	return runLayoutMigrateWithVersion(args, stdout, version)
}

// runLayoutMigrateWithVersion plans, applies, resumes, or aborts a v1 to v2
// migration. It is the only member of this family that writes, and it writes
// nothing itself: every mutation goes through layout.ApplyMigration,
// layout.ResumeMigration, or layout.AbortMigration.
//
// The order of the checks is the design's, and each one is early for a reason:
//
//   - flag combinations first, because a malformed invocation is malformed
//     wherever it runs;
//   - a `migrating` manifest second, before any planning (ruling R1): it
//     resolves as schema v2, so the planner would refuse the repository as a
//     v1 source and name neither the half-finished migration nor its remedy;
//   - the three git preconditions of design §9.1 third, in their own order
//     (ruling R2), and only on the planning path: a resume runs with the moved
//     stores staged by design, so a clean-tree rule there would make the
//     recovery it exists for impossible.
func runLayoutMigrateWithVersion(args []string, stdout io.Writer, running string) int {
	fs := flag.NewFlagSet("layout migrate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	template := fs.String("template", "", "store defaults for the target layout: code-repo, content-vault, or custom")
	var stores storeFlags
	fs.Var(&stores, "stores", "override one role's store path: role=path (repeatable)")
	archive := fs.String("archive", "", "repository-relative archive to record (default: the source's)")
	dryRun := fs.Bool("dry-run", false, "print the plan and write nothing (the default)")
	apply := fs.Bool("apply", false, "perform the migration")
	resume := fs.Bool("resume", false, "continue a `migrating` journal (requires --apply)")
	abort := fs.Bool("abort", false, "delete a planned manifest (requires --apply)")
	backupTag := fs.String("backup-tag", "", "annotated tag to create at HEAD before the first write")
	asJSON := fs.Bool("json", false, "emit the plan as one JSON object")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdout, "agents layout migrate: unexpected operand")
		return exitcode.Malformed
	}
	present := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { present[f.Name] = true })
	layoutFlags := present["template"] || present["stores"] || present["archive"]

	// Design §7.2 and ruling R3. `--backup-tag` is required with every --apply
	// that can move something: a resume continues a journal that already
	// records its own tag and an abort moves nothing at all, so both are
	// exempt, and every other --apply is refused without one. The tag is what
	// makes rollback possible even when the operator forgot a branch.
	switch {
	case *resume && *abort:
		fmt.Fprintln(stdout, "agents layout migrate: --resume and --abort are mutually exclusive")
		return exitcode.Malformed
	case *resume && !*apply:
		fmt.Fprintln(stdout, "agents layout migrate: --resume requires --apply; continuing a journal is a mutation")
		return exitcode.Malformed
	case *abort && !*apply:
		fmt.Fprintln(stdout, "agents layout migrate: --abort requires --apply; deleting the manifest is a mutation")
		return exitcode.Malformed
	case (*resume || *abort) && layoutFlags:
		fmt.Fprintln(stdout, "agents layout migrate: --resume and --abort take the target from the manifest, so --template, --stores, and --archive cannot be combined with them")
		return exitcode.Malformed
	case *dryRun && (*apply || *resume || *abort):
		fmt.Fprintln(stdout, "agents layout migrate: --dry-run and --apply are mutually exclusive")
		return exitcode.Malformed
	case *apply && !*resume && !*abort && *backupTag == "":
		fmt.Fprintln(stdout, "agents layout migrate: --backup-tag <name> is required with --apply: the tool creates the annotated tag at HEAD before the first write, so the rollback point exists even without a branch")
		return exitcode.Malformed
	}
	// The CLI rejects an unknown template itself: a name this binary does not
	// recognise is a typo, and letting it fall through to targetLayout's
	// fallback would report it as four missing stores.
	switch *template {
	case "", layout.TemplateCodeRepo, layout.TemplateContentVault, layout.TemplateCustom:
	default:
		fmt.Fprintf(stdout, "agents layout migrate: unknown template %q: the templates are %s, %s, and %s, or no --template at all with all four --stores\n",
			safetext.Flatten(*template), layout.TemplateCodeRepo, layout.TemplateContentVault, layout.TemplateCustom)
		return exitcode.Malformed
	}
	overrides, err := parseStores(stores)
	if err != nil {
		fmt.Fprintf(stdout, "agents layout migrate: %v\n", err)
		return exitcode.Malformed
	}

	root, code := layoutRepo(stdout)
	if code != exitcode.OK {
		return code
	}
	l := layout.Resolve(root)

	// Ruling R1: a `migrating` manifest is detected before planning and routed
	// to the journal verbs. PlanMigration would refuse it -- it resolves as
	// schema v2, not as a v1 source -- with a message that names neither the
	// half-finished migration nor the remedy, and ApplyMigration would fail on
	// the tag its first act creates. Nothing here plans, and nothing here
	// writes.
	if l.LayoutStatus == layout.StatusMigrating {
		switch {
		case *resume:
			return runMigrateResume(stdout, root, l, running, *asJSON)
		case *abort:
			return runMigrateAbort(stdout, root, l, running, *asJSON)
		default:
			fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: %s is layout_status migrating at phase %s; the journal is the plan now -- run `agents layout migrate --resume --apply` to continue it, or `agents layout migrate --abort --apply` while nothing has moved\n",
				layout.ManifestRel, journalPhase(l))
			return exitcode.Advisory
		}
	}
	// Neither journal verb has anything to act on. The refusal names the reason
	// rather than whatever the tree looks like, because a repository with no
	// journal is not a repository with a dirty tree.
	if *resume {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to resume: no migrating manifest; --resume continues the journal a migration froze and never re-plans, and this repository resolves %s -- run `agents layout migrate --dry-run` to plan a migration\n",
			safetext.Flatten(l.Schema))
		return exitcode.Advisory
	}
	if *abort {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to abort: no migrating manifest; --abort deletes the journal this migration created while nothing has moved, and this repository resolves %s\n",
			safetext.Flatten(l.Schema))
		return exitcode.Advisory
	}
	// An active v2 layout, an unreadable manifest, or a schema from the future
	// has nothing to plan: the source of a migration is a valid v1 layout.
	if l.Schema != layout.SchemaV1 || len(l.Problems) > 0 {
		printProblems(stdout, l.Problems)
		fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: the migration source must be a valid %s layout, and this repository resolves %s\n",
			layout.SchemaV1, orNone(l.Schema))
		return exitcode.Advisory
	}

	// Design §9.1's three git preconditions, in ruling R2's order. They run for
	// the dry run as well as for --apply, because a dry run that would fail on
	// apply is not a useful plan.
	if code := migratePreconditions(root, stdout); code != exitcode.OK {
		return code
	}
	// The planner does not read the router; the CLI asks drift for the state it
	// must judge. An unreadable router answers `unknown`, which the planner
	// reports as router_not_clean -- a refusal, never a pass.
	routerState := "unknown"
	if report, err := drift.InspectRepo(root, running); err == nil {
		routerState = string(report.RouterState)
	}
	p, err := layout.PlanMigration(root, layout.MigrateOptions{
		Template: *template, Stores: overrides, Archive: *archive,
		Running: running, RouterState: routerState,
	})
	if err != nil {
		fmt.Fprintf(stdout, "agents layout migrate: %v\n", err)
		return exitcode.NoRecord
	}

	if !*apply {
		if *asJSON {
			emitPlanJSON(stdout, p)
			// The plan is ready and nothing has been written: advisory by
			// design §7.2, whichever surface printed it.
			return exitcode.Advisory
		}
		printPlanReport(stdout, p, "dry run", true)
		fmt.Fprintln(stdout)
		printApplyHint(stdout, *template, stores, *archive)
		return exitcode.Advisory
	}
	// The report describes the invocation, not the planner's default: this run
	// applies.
	p.DryRun = false
	if len(p.Blockers) > 0 {
		if *asJSON {
			return emitPlanJSON(stdout, p)
		}
		printPlanReport(stdout, p, "apply", true)
		return exitcode.Advisory
	}
	if !*asJSON {
		printPlanReport(stdout, p, "apply", true)
	}
	if err := layout.ApplyMigration(root, p, *backupTag); err != nil {
		return reportMigrationFailure(stdout, p, err, *asJSON)
	}
	if p.Counts.Links > 0 {
		if *asJSON {
			emitPlanJSON(stdout, p)
		} else {
			planLine(stdout, "next", "%d link candidate(s) remain; run the migrating-fleet-context skill to rewrite them, then commit", p.Counts.Links)
		}
		return exitcode.Advisory
	}
	if *asJSON {
		return emitPlanJSON(stdout, p)
	}
	return exitcode.OK
}

// migratePreconditions enforces the three design §9.1 checks the planner cannot
// see, in ruling R2's order.
//
// The in-progress check is first because a merge, rebase, cherry-pick, revert,
// `am`, or bisect moves or discards HEAD on its own, and the migration's
// rollback point is a tag at HEAD -- so the operation is refused before the
// dirtiness it also causes is reported, which is the message that tells the
// operator what to finish. Every refusal is Advisory: each is a state to
// resolve, not malformed input and not a crash.
func migratePreconditions(root string, stdout io.Writer) int {
	op, err := repo.InProgress(root)
	if err != nil {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: cannot tell whether a git operation is in progress: %v\n", err)
		return exitcode.Advisory
	}
	if op != "" {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: a `git %s` is in progress, and it moves or discards HEAD on its own; the migration's rollback point is a tag at HEAD, which that would strand -- finish or abort it first\n",
			safetext.Flatten(op))
		return exitcode.Advisory
	}
	status, err := repo.Git(root, "status", "--porcelain")
	if err != nil {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: cannot read the working tree state: %v\n", err)
		return exitcode.Advisory
	}
	if strings.TrimSpace(status) != "" {
		fmt.Fprintln(stdout, "agents layout migrate: refusing to plan: working tree is not clean; commit or stash the changes first, because the migration moves tracked stores and restores them from the backup tag")
		return exitcode.Advisory
	}
	branch, err := repo.Git(root, "branch", "--show-current")
	if err != nil {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: cannot read the current branch: %v\n", err)
		return exitcode.Advisory
	}
	branch = strings.TrimSpace(branch)
	if branch == "master" || branch == "main" {
		fmt.Fprintf(stdout, "agents layout migrate: refusing to plan: the current branch is %s, a protected trunk; create a migration branch first (`git switch -c layout-v2`)\n",
			safetext.Flatten(branch))
		return exitcode.Advisory
	}
	return exitcode.OK
}

// runMigrateResume continues a `migrating` journal. It never re-plans (design
// §9.4): the report is the journal's own plan, the run is ResumeMigration, and
// the target comes from the manifest.
func runMigrateResume(w io.Writer, root string, l layout.Layout, running string, asJSON bool) int {
	if l.Migration == nil {
		fmt.Fprintf(w, "agents layout migrate: refusing to resume: %s is layout_status migrating with no journal to continue; run `agents layout validate`\n", layout.ManifestRel)
		return exitcode.Advisory
	}
	if ok, reason := versionGate(running, l); !ok {
		fmt.Fprintf(w, "agents layout migrate: refusing to resume: this binary (running %s) may not write this layout (%s, min_mut_ver_floor %s); resume with a binary at or above the floor\n",
			safetext.Flatten(running), safetext.Flatten(reason), orNone(l.MinMutVerFloor))
		return exitcode.Advisory
	}
	p := journalPlan(root, l, running)
	mode := "resume from " + safetext.Flatten(l.Migration.Phase)
	if err := layout.ResumeMigration(root); err != nil {
		return reportJournalFailure(w, p, mode, err, asJSON)
	}
	if asJSON {
		return emitPlanJSON(w, p)
	}
	printPlanReport(w, p, mode, false)
	return exitcode.OK
}

// runMigrateAbort deletes a manifest this migration created while nothing can
// have moved. It is a mutation, so it passes the same version guard as every
// other write (design §0.5): a binary below the floor refuses, and the human
// then removes the manifest by hand -- safe precisely because the permitted
// state proves nothing moved.
func runMigrateAbort(w io.Writer, root string, l layout.Layout, running string, asJSON bool) int {
	if l.Migration == nil {
		fmt.Fprintf(w, "agents layout migrate: refusing to abort: %s is layout_status migrating with no journal; run `agents layout validate`\n", layout.ManifestRel)
		return exitcode.Advisory
	}
	// Abort's whole permission is that the permitted state proves nothing has
	// moved (design §0.5). A manifest that does not validate cannot prove it, so
	// it is not read for a phase at all.
	if len(l.Problems) > 0 {
		printProblems(w, l.Problems)
		fmt.Fprintf(w, "agents layout migrate: refusing to abort: the manifest is invalid, so the state that makes a delete safe cannot be proven; run `agents layout validate`, then `agents layout migrate --resume --apply`\n")
		return exitcode.Advisory
	}
	if ok, reason := versionGate(running, l); !ok {
		fmt.Fprintf(w, "agents layout migrate: refusing to abort: this binary (running %s) may not write this layout (%s, min_mut_ver_floor %s); a binary at or above the floor may abort, or delete %s by hand -- the permitted state proves nothing moved\n",
			safetext.Flatten(running), safetext.Flatten(reason), orNone(l.MinMutVerFloor), layout.ManifestRel)
		return exitcode.Advisory
	}
	p := journalPlan(root, l, running)
	// The refusal and the success line are one abort's report; a --json consumer
	// gets the journal plan instead, and a header printed before it would make
	// the stream two documents.
	err := layout.AbortMigration(root)
	if !asJSON {
		fmt.Fprintf(w, "layout migrate (abort) — %s\n", safetext.Flatten(root))
	}
	if err != nil {
		if asJSON {
			emitWithBlocker(w, p, "abort_refused", err.Error())
		} else {
			planLine(w, "abort", "refused: %s", safetext.Flatten(err.Error()))
		}
		return exitcode.Advisory
	}
	if asJSON {
		return emitPlanJSON(w, p)
	}
	planLine(w, "abort", "removed %s; the backup tag %s remains the record", layout.ManifestRel, safetext.Flatten(l.Migration.BackupTag))
	return exitcode.OK
}

// versionGate asks layout.Support the question the two journal verbs need --
// may this binary write this repository at all -- with the `migrating` status
// set aside. Support reads `migrating` as unsupported-for-mutation by itself
// (it is: only resume and abort may write), so asking it about the resolved
// layout verbatim would refuse every resume and every abort on the status
// alone. The floor question is asked of a copy whose status is `active`, which
// is the layout the journal becomes.
func versionGate(running string, l layout.Layout) (bool, string) {
	probe := l
	probe.LayoutStatus = layout.StatusActive
	return layout.Support(running, probe)
}

// journalPlan renders the plan a `migrating` manifest records, so --resume and
// --abort report the journal's own plan rather than a re-planned one (design
// §7.2): the phase the run continues from, the frozen `from`, the manifest's
// `to`, and the journal's moves with their files, bytes, and digest unchanged.
//
// It writes nothing and asks nothing of the filesystem beyond the router state
// the header prints, which is why a refused abort can still describe what it
// refused to delete.
func journalPlan(root string, l layout.Layout, running string) layout.Plan {
	m := l.Migration
	from := layout.Layout{Manifest: layout.Manifest{
		Schema:       m.From.Schema,
		LayoutStatus: layout.StatusActive,
		Archive:      m.From.Archive,
		Stores:       m.From.Stores,
	}}
	to := layout.Layout{Manifest: layout.Manifest{
		Schema:         l.Schema,
		MinMutVerFloor: l.MinMutVerFloor,
		LayoutStatus:   layout.StatusActive,
		Archive:        l.Archive,
		Stores:         l.Stores,
	}}
	router := "unknown"
	if report, err := drift.InspectRepo(root, running); err == nil {
		router = string(report.RouterState)
	}
	p := layout.Plan{
		Repo: root, DryRun: false, Phase: m.Phase,
		From: from, To: to,
		RouterAction:   router + " -> canonical v2",
		Archive:        l.Archive,
		Moves:          append([]layout.Move(nil), m.Moves...),
		LinkCandidates: []layout.LinkCandidate{},
	}
	p.Counts = layout.Counts{Move: len(p.Moves), Blocked: len(p.Blockers), Links: len(p.LinkCandidates)}
	return p
}

// reportMigrationFailure renders a fresh apply's mid-flight failure.
func reportMigrationFailure(w io.Writer, p layout.Plan, err error, asJSON bool) int {
	var residue *layout.ResidueError
	var move *layout.MoveError
	switch {
	case errors.As(err, &residue):
		// The moves did complete and only the shell promise failed (design §9.5),
		// so the layout is active and the exit is advisory.
		if asJSON {
			emitWithBlocker(w, p, "docs_residue", err.Error())
		} else {
			reportResidue(w, residue)
		}
		return exitcode.Advisory
	case errors.As(err, &move):
		// A per-move refusal with its remedy; the journal stays in place for the
		// next --resume --apply.
		if asJSON {
			emitWithBlocker(w, p, "move_error", move.Error())
		} else {
			planLine(w, "failed", "%s", safetext.Flatten(move.Error()))
		}
		return exitcode.NoRecord
	default:
		if asJSON {
			emitWithBlocker(w, p, "apply_failed", err.Error())
		} else {
			planLine(w, "failed", "%s", safetext.Flatten(err.Error()))
		}
		return exitcode.NoRecord
	}
}

// reportJournalFailure renders a resume's failure: the same dispositions as a
// fresh apply, with the journal's own plan as the report.
func reportJournalFailure(w io.Writer, p layout.Plan, mode string, err error, asJSON bool) int {
	var residue *layout.ResidueError
	var move *layout.MoveError
	switch {
	case errors.As(err, &residue):
		if asJSON {
			emitWithBlocker(w, p, "docs_residue", err.Error())
		} else {
			printPlanReport(w, p, mode, false)
			reportResidue(w, residue)
		}
		return exitcode.Advisory
	case errors.As(err, &move):
		if asJSON {
			emitWithBlocker(w, p, "move_error", move.Error())
		} else {
			printPlanReport(w, p, mode, false)
			planLine(w, "failed", "%s", safetext.Flatten(move.Error()))
		}
		return exitcode.NoRecord
	default:
		if asJSON {
			emitWithBlocker(w, p, "resume_failed", err.Error())
		} else {
			printPlanReport(w, p, mode, false)
			planLine(w, "failed", "%s", safetext.Flatten(err.Error()))
		}
		return exitcode.NoRecord
	}
}

// reportResidue names every entry the anti-shell promise could not remove, one
// line each (design §9.5).
func reportResidue(w io.Writer, residue *layout.ResidueError) {
	for _, entry := range residue.Entries {
		planLine(w, "residue", "%s", safetext.Flatten(entry))
	}
	planLine(w, "next", "the layout is active; move the named entries out of docs/ or declare them, then re-run")
}

// printPlanReport renders design §9.2's human shape: a header naming the mode
// and the repository, the from/to layouts, the router action, the archive, and
// the git branch and tree state; then one line per move and per blocker; then
// the link count and the result counts.
//
// Keep rows are not invented. Nothing in the planner produces one, so
// Counts.Keep is 0 and the result line prints it as measured; a keep line for a
// list that does not exist would be a report of work the tool is not doing.
//
// removeLine is the caller's to pass because the two plan sources mean
// different things by it: a fresh plan will remove the docs/ shell, and a
// journal may already be past that step.
func printPlanReport(w io.Writer, p layout.Plan, mode string, removeLine bool) {
	fmt.Fprintf(w, "layout migrate (%s) — %s\n", mode, safetext.Flatten(p.Repo))
	planLine(w, "from", "%s  %s", safetext.Flatten(p.From.Schema), storesSummary(p.From))
	planLine(w, "to", "%s  stores=%s", safetext.Flatten(p.To.Schema), storesSummary(p.To))
	planLine(w, "router", "%s (deterministic swap)", safetext.Flatten(p.RouterAction))
	archive := p.Archive
	if archive == "" {
		// Design §9.2's line reads `archive none`, not `archive (none)`: this is
		// a field of the plan, not an absent optional value elsewhere.
		archive = "none"
	}
	planLine(w, "archive", "%s", safetext.Flatten(archive))
	planLine(w, "git", "%s", gitSummary(p.Repo))
	fmt.Fprintln(w)
	for _, m := range p.Moves {
		planLine(w, "move", "%s -> %s  (%s)", safetext.Flatten(m.From), safetext.Flatten(m.To), filesWord(m.Files))
	}
	for _, b := range p.Blockers {
		planLine(w, "blocker", "%s  %s", safetext.Flatten(b.Code), safetext.Flatten(b.Detail))
	}
	if removeLine {
		if p.To.Archive == "" {
			planLine(w, "remove", "docs/ after the moves (no archive, no other tracked content)")
		} else {
			planLine(w, "remove", "docs/ after the moves (keeps %s)", safetext.Flatten(p.To.Archive))
		}
	}
	planLine(w, "links", "%d markdown links point into the moved stores", p.Counts.Links)
	planLine(w, "result", "%d move, %d keep, %d blocked, %d link(s)",
		p.Counts.Move, p.Counts.Keep, p.Counts.Blocked, p.Counts.Links)
}

// planLine is the one column layout every report line uses, so the report is
// scannable and the verbs line up whatever their length.
func planLine(w io.Writer, verb, format string, args ...any) {
	fmt.Fprintf(w, "  %-7s ", verb)
	fmt.Fprintf(w, format, args...)
	fmt.Fprintln(w)
}

// printApplyHint names the invocation that would apply the plan just printed,
// reconstructed from the flags the operator supplied. The backup tag stays a
// placeholder: it is the operator's name for the rollback point, not this
// tool's to invent.
func printApplyHint(w io.Writer, template string, stores []string, archive string) {
	cmd := "agents layout migrate"
	if template != "" {
		cmd += " --template " + safetext.Flatten(template)
	}
	for _, s := range stores {
		cmd += " --stores " + safetext.Flatten(s)
	}
	if archive != "" {
		cmd += " --archive " + safetext.Flatten(archive)
	}
	cmd += " --apply --backup-tag <name>"
	planLine(w, "apply", "%s", cmd)
}

// filesWord counts a move's files the way a reader reads them.
func filesWord(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// storesSummary renders a store map on one line: the shared-parent shorthand
// when every role sits directly under one directory (`docs/{design,plans,
// journal,qna}`, `stores=.context/{...}`), and an explicit role=path list
// otherwise. It reads the layout's own map, so a target the planner had to
// report as incomplete still prints what resolved.
func storesSummary(l layout.Layout) string {
	var pairs []string
	parent := ""
	shorthand := true
	for _, role := range layout.Roles() {
		p, ok := layout.Path(l, role)
		if !ok || p == "" {
			shorthand = false
			pairs = append(pairs, role+"=(unresolved)")
			continue
		}
		pairs = append(pairs, role+"="+p)
		dir, base := path.Split(strings.TrimSuffix(p, "/"))
		dir = strings.TrimSuffix(dir, "/")
		if base != role || dir == "" {
			shorthand = false
			continue
		}
		if parent == "" {
			parent = dir
		} else if parent != dir {
			shorthand = false
		}
	}
	if shorthand && parent != "" {
		return parent + "/{" + strings.Join(layout.Roles(), ",") + "}"
	}
	return strings.Join(pairs, " ")
}

// gitSummary is the header's git line: the branch and whether the tree is
// clean, read at report time rather than assumed from the preconditions,
// because a journal verb runs with the moved stores staged by design.
func gitSummary(root string) string {
	branch, err := repo.Git(root, "branch", "--show-current")
	if err != nil {
		return "state unavailable"
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "(detached HEAD)"
	}
	status, err := repo.Git(root, "status", "--porcelain")
	if err != nil {
		return "branch " + safetext.Flatten(branch) + ", tree state unknown"
	}
	state := "clean"
	if strings.TrimSpace(status) != "" {
		state = "dirty"
	}
	return "branch " + safetext.Flatten(branch) + ", tree " + state
}

// journalPhase names the phase a migrating manifest records, for a refusal that
// must say which state it is refusing.
func journalPhase(l layout.Layout) string {
	if l.Migration == nil || l.Migration.Phase == "" {
		return "(unknown)"
	}
	return safetext.Flatten(l.Migration.Phase)
}

// emitPlanJSON writes the plan as one object and nothing else, the way every
// other --json surface in this family does: a consumer parses the bytes before
// it reads the exit code. The three list fields are normalized to arrays, so a
// plan without blockers or links is `[]` rather than `null` and a consumer can
// count them without a nil check.
func emitPlanJSON(w io.Writer, p layout.Plan) int {
	if p.Moves == nil {
		p.Moves = []layout.Move{}
	}
	if p.LinkCandidates == nil {
		p.LinkCandidates = []layout.LinkCandidate{}
	}
	if p.Blockers == nil {
		p.Blockers = []layout.Blocker{}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "agents layout migrate: %v\n", err)
		return exitcode.NoRecord
	}
	w.Write(append(b, '\n'))
	return exitcode.OK
}

// emitWithBlocker adds one blocker to a journal plan and emits it, so a failed
// or refused journal verb still answers a --json consumer with one parseable
// object carrying the reason, rather than with prose the consumer cannot read.
func emitWithBlocker(w io.Writer, p layout.Plan, code, detail string) {
	p.Blockers = append(append([]layout.Blocker{}, p.Blockers...), layout.Blocker{
		Code: code, Detail: safetext.Flatten(detail),
	})
	p.Counts.Blocked = len(p.Blockers)
	emitPlanJSON(w, p)
}
