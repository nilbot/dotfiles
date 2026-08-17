package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// A tree with a subcommand no person invokes, nested under a parent every
// person invokes. The real tree has no such node yet, which is precisely why
// the filter needs its own fixture: the behaviour cannot be observed from the
// shipped command set until the day someone adds one, and by then the leak
// would already have shipped.
func fixtureWithAHiddenSubcommand() *Command {
	return &Command{Name: "agents", Usage: "agents <command> [flags]", Sub: []*Command{{
		Name: "trace", Summary: "query the store", Usage: "agents trace <sub>",
		Detail:   "Reads the machine-local trace store.",
		Audience: []Audience{Human},
		Sub: []*Command{
			{
				Name: "ls", Summary: "query the index", Usage: "agents trace ls",
				Detail: "Filters the index.", Audience: []Audience{Human},
			},
			{
				Name: "harnessonly", Summary: "entrypoint for a harness", Usage: "agents trace harnessonly",
				Detail: "Invoked by a harness. Not for people.", Audience: []Audience{Harness},
			},
		},
	}}}
}

// RenderUsage filters the top level by audience; RenderHelp must apply the same
// rule one level down. Without this, a harness-only subcommand under a
// human-visible parent lands in that parent's page unfiltered -- the filter
// applied at the root and nowhere else.
func TestRenderHelpFiltersSubcommandsByAudience(t *testing.T) {
	tree := fixtureWithAHiddenSubcommand()
	cmd, _ := tree.Find([]string{"trace"})

	var narrow, wide bytes.Buffer
	RenderHelp(cmd, []string{"trace"}, &narrow, false)
	RenderHelp(cmd, []string{"trace"}, &wide, true)

	if strings.Contains(narrow.String(), "harnessonly") {
		t.Errorf("a harness-only subcommand leaked into the page a person reads:\n%s", narrow.String())
	}
	if !strings.Contains(narrow.String(), "ls") {
		t.Errorf("the filter also dropped a human subcommand:\n%s", narrow.String())
	}
	if !strings.Contains(wide.String(), "harnessonly") {
		t.Errorf("--all must show what audience filtering hid:\n%s", wide.String())
	}
	// Hidden is not the same as unmentioned: whatever the filter removed must
	// still be reachable by a reader who can tell something is missing.
	if !strings.Contains(narrow.String(), "agents help trace --all") {
		t.Errorf("nothing points at the flag that reveals the hidden subcommand:\n%s", narrow.String())
	}
}

// parsedSources parses every non-test source file of this package. TestMain has
// already moved the working directory out of the checkout, so the path comes
// from packageDir rather than from ".".
func parsedSources(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, packageDir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", packageDir, err)
	}
	files := map[string]*ast.File{}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			files[name] = f
		}
	}
	// A scan that matched nothing would pass silently and prove nothing, so
	// pin that the sources this is meant to police were actually read.
	for _, want := range []string{"cmd_trace.go", "cmd_review.go", "cmd_guard.go", "cmd_hook.go", "main.go"} {
		found := false
		for name := range files {
			if strings.HasSuffix(name, "/"+want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s was not parsed; the scan below would prove nothing", want)
		}
	}
	return fset, files
}

// No handler may carry its own copy of its usage line.
//
// This is the claim the whole registry rests on: help cannot diverge from
// dispatch. Comparing today's handful of strings against today's tree nodes
// would not hold it -- the failure mode is a future handler that writes a fresh
// literal, which no equality check between existing pairs can see. So the test
// scans the source instead: any string literal that both says "usage:" and
// names the binary is a restatement of something the tree already declares,
// wherever it appears and whether or not it happens to agree today.
func TestNoSourceFileRestatesAUsageLine(t *testing.T) {
	fset, files := parsedSources(t)
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if strings.Contains(v, "usage:") && strings.Contains(v, "agents") {
				t.Errorf("%s: %q restates a usage line; render it from the tree with usageFor(...)",
					fset.Position(lit.Pos()), v)
			}
			return true
		})
	}
}

// usageFor is only as good as the path it is handed: a call site naming a
// command the tree does not have would print a plausible line assembled from
// its own arguments, which is the same divergence in a new costume.
func TestEveryUsageForCallSiteNamesARealCommand(t *testing.T) {
	fset, files := parsedSources(t)
	root := rootCommand()
	calls := 0
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "usageFor" {
				return true
			}
			calls++
			var path []string
			for _, a := range call.Args {
				lit, ok := a.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s: usageFor called with a non-literal argument; this test cannot check it",
						fset.Position(call.Pos()))
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", fset.Position(lit.Pos()), err)
				}
				path = append(path, v)
			}
			if cmd, rest := root.Find(path); cmd == nil || len(rest) > 0 {
				t.Errorf("%s: usageFor(%q) names no command in the tree",
					fset.Position(call.Pos()), strings.Join(path, " "))
			}
			return true
		})
	}
	if calls == 0 {
		t.Fatal("no usageFor call sites found; the handlers are restating their usage again, or this scan is broken")
	}
}

// The rendered line is the tree's line, prefixed and nothing else.
func TestUsageForRendersTheTreesLine(t *testing.T) {
	rootCommand().Walk(func(path []string, c *Command) {
		if got, want := usageFor(path...), "usage: "+c.Usage; got != want {
			t.Errorf("usageFor(%q) = %q, want %q", strings.Join(path, " "), got, want)
		}
	})
}

// `agents help <command> [subcommands]` must reach every command at any depth.
// `trace cache prune` is the destructive verb that motivated the tree, and it
// is three levels down -- the depth at which a listing of root.Sub, which is
// all the previous help surface had, stops being able to answer.
func TestDispatchHelpReachesAThreeLevelLeaf(t *testing.T) {
	var code int
	stdout, stderr := captureStdoutAndStderr(t, func() {
		code = run([]string{"help", "trace", "cache", "prune"})
	})
	if code != exitcode.OK {
		t.Errorf("exit = %d, want OK (%d)", code, exitcode.OK)
	}
	if stderr != "" {
		t.Errorf("help wrote to stderr:\n%s", stderr)
	}
	for _, want := range []string{"agents trace cache prune", "--retention", "never the records"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the leaf's page omitted %q:\n%s", want, stdout)
		}
	}
	// The heading must carry the whole path, not just the leaf's own name.
	// "prune" alone names three different commands in this tree, and the
	// usage line below it satisfies a substring check either way -- so
	// without this, dropping the path from the heading goes unnoticed.
	cmd, _ := rootCommand().Find([]string{"trace", "cache", "prune"})
	want := "agents trace cache prune -- " + cmd.Summary
	if first, _, _ := strings.Cut(stdout, "\n"); first != want {
		t.Errorf("first line = %q, want %q", first, want)
	}
}

// --help used to be a top-level idiom: `agents trace --help` answered `unknown
// subcommand "--help"` and exited 3.
func TestHelpFlagWorksAtDepth(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"trace", "--help"}, "agents trace --"},
		{[]string{"trace", "cache", "prune", "-h"}, "agents trace cache prune --"},
		{[]string{"doctor", "--help"}, "agents doctor --"},
		// A flag before --help must not become part of the path.
		{[]string{"trace", "cache", "prune", "--lane", "x", "--help"}, "agents trace cache prune --"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var code int
			stdout, _ := captureStdoutAndStderr(t, func() { code = run(tc.args) })
			if code != exitcode.OK {
				t.Errorf("exit = %d, want OK (%d)", code, exitcode.OK)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tc.want)
			}
		})
	}
}

// A branch invoked with no subcommand ran nothing, which is the same event as a
// bare `agents` and an unknown top-level command. All three now report on
// stderr; this one used to report on stdout, into whatever the caller piped.
func TestBranchWithNoSubcommandReportsOnStderr(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"trace"}, "usage: agents trace"},
		{[]string{"trace", "nosuch"}, `unknown subcommand "nosuch"`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var code int
			stdout, stderr := captureStdoutAndStderr(t, func() { code = run(tc.args) })
			if code != exitcode.Malformed {
				t.Errorf("exit = %d, want Malformed (%d)", code, exitcode.Malformed)
			}
			if stdout != "" {
				t.Errorf("a usage error went to stdout:\n%s", stdout)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.want)
			}
		})
	}
}

// The generated README block Task 9 consumes. Every command, at every depth --
// a table built from root.Sub alone would silently stop at the second level.
func TestRenderMarkdownListsEveryCommandAtEveryDepth(t *testing.T) {
	var out bytes.Buffer
	if code := runHelp([]string{"--render=markdown"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d)", code, exitcode.OK)
	}
	rootCommand().Walk(func(path []string, c *Command) {
		row := "| `agents " + strings.Join(path, " ") + "` |"
		if !strings.Contains(out.String(), row) {
			t.Errorf("the table omits %q:\n%s", row, out.String())
		}
	})
	if !strings.Contains(out.String(), "|---|---|") {
		t.Errorf("no table header, so this is not markdown a README can hold:\n%s", out.String())
	}
}
