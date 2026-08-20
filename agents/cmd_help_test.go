package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
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
	for _, want := range []string{"cmd_trace.go", "cmd_doctor.go", "cmd_guard.go", "cmd_hook.go", "main.go"} {
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

// staticText folds a string literal, or a concatenation of them, into the text
// it produces. Non-literal operands contribute nothing, which is the point:
// `"usage: " + cmd.Usage` folds to "usage: " and is rendering from the tree,
// while `"usage: " + "agents trace show <id>"` folds whole and is not. That
// second shape is exactly what the deleted cmd_trace.go literal looked like.
func staticText(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLiteral(v)
	case *ast.ParenExpr:
		return staticText(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lok := staticText(v.X)
		r, rok := staticText(v.Y)
		if !lok && !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// No handler may carry its own copy of its usage line.
//
// This is the claim the whole registry rests on: help cannot diverge from
// dispatch. Comparing today's handful of strings against today's tree nodes
// would not hold it -- the failure mode is a future handler that writes a fresh
// literal, which no equality check between existing pairs can see. So the test
// scans the source instead: any static string that both says "usage:" and names
// the binary is a restatement of something the tree already declares, wherever
// it appears and whether or not it happens to agree today. Matching is
// case-insensitive and folds concatenation, so neither "Usage: agents …" nor
// "usage: " + "agents …" slips through.
//
// What it still cannot see: text assembled at run time, of which
// fmt.Sprintf("usage: %s …", "agents") is the plausible case. Reaching that
// means interpreting format strings, and a scan that guessed at Sprintf
// arguments would have to be taught that fmt.Fprintf(w, "usage: %s", cmd.Usage)
// -- the correct rendering, three lines away in command.go -- is fine. The
// residual gap is narrower than that false positive would be.
func TestNoSourceFileRestatesAUsageLine(t *testing.T) {
	fset, files := parsedSources(t)
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.BasicLit, *ast.BinaryExpr:
			default:
				return true
			}
			v, ok := staticText(n.(ast.Expr))
			if !ok {
				return true
			}
			lower := strings.ToLower(v)
			if strings.Contains(lower, "usage:") && strings.Contains(lower, "agents") {
				t.Errorf("%s: %q restates a usage line; render it from the tree with usageFor(...)",
					fset.Position(n.Pos()), v)
			}
			// A concatenation is checked whole; its operands are parts of that
			// one string, so descending would report the same text twice.
			_, isBinary := n.(*ast.BinaryExpr)
			return !isBinary
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
		if got, want := usageFor(path...), usageBlock(c.Usage); got != want {
			t.Errorf("usageFor(%q) = %q, want %q", strings.Join(path, " "), got, want)
		}
		// Whatever the wrapping does, the declared text must survive it.
		for _, form := range strings.Split(c.Usage, "\n") {
			if !strings.Contains(usageFor(path...), strings.TrimSpace(form)) {
				t.Errorf("usageFor(%q) dropped the form %q", strings.Join(path, " "), form)
			}
		}
	})
}

// A command whose flags do not all combine needs more than one usage form --
// `trace cache prune --lane` and `--retention` are separate operations, and
// documenting only one of them is how --yes, --age and --size came to be
// accepted by the handler and named nowhere. Multiple forms must therefore
// render readably in both places usage appears, and the one-row-per-command
// listing must not inherit a command's second and third form.
func TestMultiFormUsageIndentsAndTheListingShowsTheFirstFormOnly(t *testing.T) {
	if got, want := usageBlock("first form\nsecond form"), "usage: first form\n       second form"; got != want {
		t.Errorf("usageBlock = %q, want %q; the second form must align under the first", got, want)
	}
	if got := usageSynopsis("first form\nsecond form"); got != "first form" {
		t.Errorf("usageSynopsis = %q, want %q", got, "first form")
	}

	// The real tree, through the real renderers.
	prune, _ := rootCommand().Find([]string{"trace", "cache", "prune"})
	var page bytes.Buffer
	RenderHelp(prune, []string{"trace", "cache", "prune"}, &page, false)
	for _, want := range []string{
		"usage: agents trace cache prune --lane <name> [--yes]",
		"       agents trace cache prune --retention [--age <d>] [--size <bytes>] [--yes]",
	} {
		if !strings.Contains(page.String(), want) {
			t.Errorf("the leaf's page omitted the line %q:\n%s", want, page.String())
		}
	}

	var listing bytes.Buffer
	RenderUsage(rootCommand(), &listing, true)
	for _, line := range strings.Split(listing.String(), "\n") {
		if strings.Count(line, "agents ") > 1 {
			t.Errorf("a listing row carries more than one usage form: %q", line)
		}
	}
	if strings.Contains(listing.String(), "agents doctor [--lane-window") {
		t.Errorf("the listing inherited doctor's second form:\n%s", listing.String())
	}
	if !strings.Contains(listing.String(), "agents doctor  ") {
		t.Errorf("the listing lost doctor's first form:\n%s", listing.String())
	}
}

// --render=markdown is a real affordance of `agents help`, so it belongs in the
// declaration like every other one. It ignored any path given beside it, which
// is the same defect as a flag a handler accepts and documents nowhere: the
// caller asked for something and was silently given something else.
func TestRenderMarkdownFlagIsDeclaredAndRefusesWhatItCannotAnswer(t *testing.T) {
	help, _ := rootCommand().Find([]string{"help"})
	if !strings.Contains(help.Usage+help.Detail, "--render=markdown") {
		t.Errorf("`agents help` accepts --render=markdown and declares it nowhere:\n%s\n%s",
			help.Usage, help.Detail)
	}

	for _, args := range [][]string{
		{"--render=markdown", "trace"},
		{"--render=markdown", "--all"},
	} {
		var out bytes.Buffer
		if code := runHelp(args, &out); code != exitcode.Malformed {
			t.Errorf("runHelp(%q) exit = %d, want Malformed (%d); it must not silently ignore the argument",
				args, code, exitcode.Malformed)
		}
		if strings.Contains(out.String(), "|---|---|") {
			t.Errorf("runHelp(%q) rendered the table anyway:\n%s", args, out.String())
		}
	}
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

// handlerFlagSets maps each command path to the flag names its handler
// registers, read out of the source. Every flag.NewFlagSet in this package is
// named with the command's own path -- "guard", "review", "trace cache prune"
// -- so the name resolves against the tree directly.
func handlerFlagSets(t *testing.T) map[string][]string {
	t.Helper()
	_, files := parsedSources(t)
	sets := map[string][]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name, variable := "", ""
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 1 {
					return true
				}
				call, ok := as.Rhs[0].(*ast.CallExpr)
				if !ok || !isSelector(call.Fun, "flag", "NewFlagSet") || len(call.Args) == 0 {
					return true
				}
				if s, ok := stringLiteral(call.Args[0]); ok {
					name = s
					if id, ok := as.Lhs[0].(*ast.Ident); ok {
						variable = id.Name
					}
				}
				return true
			})
			if name == "" || variable == "" {
				continue
			}
			var flags []string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				recv, ok := sel.X.(*ast.Ident)
				if !ok || recv.Name != variable || !strings.Contains(
					"Bool String Int Int64 Uint Uint64 Float64 Duration Var",
					strings.TrimSuffix(sel.Sel.Name, "Var")) {
					return true
				}
				// One rule covers both shapes: fs.String("lane", …) and
				// fs.StringVar(&f.Lane, "lane", …). The flag's name is the
				// first string literal in either.
				for _, a := range call.Args {
					if s, ok := stringLiteral(a); ok {
						flags = append(flags, s)
						break
					}
				}
				return true
			})
			if len(flags) > 0 {
				sets[name] = flags
			}
		}
	}
	if len(sets) < 10 {
		t.Fatalf("found only %d flag sets; the scan is broken and would prove nothing", len(sets))
	}
	return sets
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// A flag a handler accepts but its usage line does not name is documented
// nowhere at all.
//
// This is the other half of the divergence the registry was built to end.
// Rendering usage from the tree settled which string prints; it could not
// settle whether that string is complete, and on `trace cache prune` and
// `review` it was not -- collapsing onto the tree deleted the last mention of
// --yes, --age, --size, --lane and --since. Fixing those five would fix an
// instance; the class is "a handler accepts a flag its usage line does not
// name", and it recurs the next time anyone adds a flag.
func TestEveryRegisteredFlagIsDocumented(t *testing.T) {
	root := rootCommand()
	for name, flags := range handlerFlagSets(t) {
		path := strings.Fields(name)
		cmd, rest := root.Find(path)
		if cmd == nil || len(rest) > 0 {
			t.Errorf("flag set %q names no command in the tree; name it for the command's own path", name)
			continue
		}
		for _, f := range flags {
			// -m and --m are the same flag to the flag package, so either
			// spelling in the usage line documents it.
			re := regexp.MustCompile(`(^|[^-\w])--?` + regexp.QuoteMeta(f) + `($|[^\w-])`)
			if !re.MatchString(cmd.Usage) {
				t.Errorf("`agents %s` accepts --%s, and its usage line does not name it:\n  %s",
					name, f, cmd.Usage)
			}
		}
	}
}

// usageFlagPattern finds the flag tokens in a usage line: --lane, -m,
// --recording-freshness. It deliberately does not match <id>, msg or the |
// separating two forms, none of which are flags.
var usageFlagPattern = regexp.MustCompile(`(^|[^-\w])(--?[a-z][a-z0-9-]*)`)

// The mirror of TestEveryRegisteredFlagIsDocumented, and it is not redundant
// with it: that test walks registered -> documented, so it cannot see a usage
// line naming a flag no handler accepts. Measured before this existed --
// advertising `agents guard --staged [--nonexistent <x>]` left the whole suite
// green.
//
// The direction matters here more than most places. Closing the first gap meant
// hand-writing 21 flag names into usage strings across seven commands, and a
// typo in any of them tells a reader to pass something the handler rejects.
// Half a check, applied to freshly hand-copied data, is how you document a flag
// that does not exist.
func TestNoUsageLineNamesAFlagThatDoesNotExist(t *testing.T) {
	// `help` parses its own arguments in runHelp rather than through a
	// flag.FlagSet, because a FlagSet stops at the first non-flag argument and
	// `agents help trace --all` leads with one. Its flags are therefore real
	// but unregistered, and the scan below cannot see them. They are covered
	// behaviourally instead, by TestHelpAllIncludesTheAutomatedCommands and
	// TestRenderMarkdownListsEveryCommandAtEveryDepth, which invoke them.
	//
	// The exemption is a fixed list, not a predicate: a second hand-parser
	// added later fails here rather than quietly inheriting the hole.
	handParsed := map[string]bool{"help": true}

	sets := handlerFlagSets(t)
	checked := 0
	rootCommand().Walk(func(path []string, c *Command) {
		name := strings.Join(path, " ")
		flags, hasSet := sets[name]
		if handParsed[name] {
			return
		}
		if !hasSet {
			if usageFlagPattern.MatchString(c.Usage) {
				t.Errorf("`agents %s` advertises flags but registers no flag set:\n  %s", name, c.Usage)
			}
			return
		}
		registered := map[string]bool{}
		for _, f := range flags {
			registered[f] = true
		}
		for _, m := range usageFlagPattern.FindAllStringSubmatch(c.Usage, -1) {
			flag := strings.TrimLeft(m[2], "-")
			checked++
			if !registered[flag] {
				t.Errorf("`agents %s` advertises --%s, which its handler does not register:\n  %s",
					name, flag, c.Usage)
			}
		}
	})
	// A pattern that matched nothing would pass every command silently.
	if checked < 20 {
		t.Fatalf("only %d flag tokens found across every usage line; the pattern is broken", checked)
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
