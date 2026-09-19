package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

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
	if ok, reason := layout.Support(running, l); ok {
		fmt.Fprintf(w, "Mutating:          yes (running %s)\n", safetext.Flatten(running))
	} else {
		fmt.Fprintf(w, "Mutating:          no (%s; running %s)\n",
			safetext.Flatten(reason), safetext.Flatten(running))
	}
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
	// file it restores. It is selected by schema, not by validity: a repository
	// mid-migration still has to be able to restore its own router.
	if *router {
		if l.Schema == layout.SchemaV2 {
			fmt.Fprint(stdout, layout.V2AgentsMD)
		} else {
			fmt.Fprint(stdout, scaffold.DefaultAgentsMD)
		}
		return exitcode.OK
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
	// Support answers "may this binary mutate it", which is a separate question
	// from "is it valid". The report keeps them in separate fields, but the
	// summary boolean folds them together the way drift's `unsupported` field
	// does: a V-rule makes the whole layout unsupported for mutation (design
	// §5.2), so reporting `supported: true` beside a problem list would invite
	// exactly the mutation the rule just refused.
	supported, reason := layout.Support(running, l)
	if len(l.Problems) > 0 {
		supported = false
		switch {
		case layout.HasProblem(l.Problems, layout.ProblemSchemaUnknown):
			reason = "unknown_schema"
		default:
			reason = "invalid"
		}
	}

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
	// The layout's own state is weighed before the role is looked up, because
	// design §7.1 gives an invalid or unsupported layout its own disposition
	// (1 or 4, nothing printed) in every case -- including the case where the
	// manifest is too broken to resolve the role at all, which would otherwise
	// be reported as a caller's typo.
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
