package phase_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

func fishContext(fake *fakeChange, out *bytes.Buffer, shell string) phase.Context {
	return phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Shell: shell, User: "someone", Out: out,
	}
}

func TestFishRegistersTheShellThenChangesItThenInstallsPlugins(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The quotes are the fake saying where each argv element ends. They matter
	// most on the two -c arguments: the script and the fish snippet are each one
	// element, and /repo is the element after the snippet rather than part of it.
	want := []string{
		`sudo /bin/sh -c "printf '%s\\n' \"$1\" >> /etc/shells" sh /usr/bin/fish`,
		"sudo chsh -s /usr/bin/fish someone",
		`run fish --no-config -c "source $argv[1]/fish/mypre.fish; install_fisher" /repo`,
	}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\n\nwant:\n%s",
			strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

// The order is load-bearing, not cosmetic: chsh refuses a shell that is not
// listed in /etc/shells, so a run that changed the shell first would fail on
// exactly the fresh machine this phase exists for.
func TestFishAddsTheShellsEntryBeforeChangingTheShell(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	shells, chsh := -1, -1
	for i, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			shells = i
		}
		if strings.Contains(op, "chsh") {
			chsh = i
		}
	}
	if shells == -1 || chsh == -1 {
		t.Fatalf("both steps must appear: %v", fake.Ops)
	}
	if shells > chsh {
		t.Errorf("chsh at %d precedes the /etc/shells entry at %d; chsh refuses "+
			"an unlisted shell, so this order fails on a fresh machine", chsh, shells)
	}
}

// The path is passed as $1 and never interpolated into the script text, so a
// prefix containing a quote, a space or $(...) cannot change what root runs.
func TestFishPassesTheShellPathAsAnArgumentNotInsideTheScript(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var op string
	for _, o := range fake.Ops {
		if strings.Contains(o, "/etc/shells") {
			op = o
		}
	}
	// Written in the fake's quoted rendering, which is what makes the second
	// assertion say what it means: the quote closing before `sh` is the script
	// element ending there, so $0 and $1 are separate arguments and not text the
	// shell would parse.
	if !strings.Contains(op, `printf '%s\\n' \"$1\" >> /etc/shells`) {
		t.Errorf("the script must use the positional $1: %s", op)
	}
	if !strings.HasSuffix(op, `>> /etc/shells" sh /usr/bin/fish`) {
		t.Errorf("the path must follow the script as $0 and $1: %s", op)
	}
}

func TestFishSkipsTheShellsEntryWhenItIsAlreadyListed(t *testing.T) {
	fake := &fakeChange{files: map[string][]byte{
		"/etc/shells": []byte("# comment\n/bin/sh\n/bin/bash\n/usr/bin/fish\n"),
	}}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			t.Errorf("must not append an entry that is already present: %s", op)
		}
	}
	if !strings.Contains(out.String(), "already") {
		t.Errorf("the skip must be visible in the phase's output:\n%s", out.String())
	}
}

// An entry is an entry however it is spaced. /etc/shells is a hand-edited file,
// and a line with a trailing space compared verbatim is a line this phase never
// recognises -- so every apply would append another copy of it and the file
// would grow by one line per run. Converging means arriving somewhere and
// staying there.
func TestFishRecognisesAnAlreadyListedEntryAroundWhitespace(t *testing.T) {
	fake := &fakeChange{files: map[string][]byte{
		"/etc/shells": []byte("/bin/sh\n  /usr/bin/fish  \n"),
	}}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			t.Errorf("re-applying appended an entry the file already carries, so "+
				"the file grows by a line per run: %s", op)
		}
	}
}

// An unreadable /etc/shells is not an absent entry. Appending a duplicate line
// is inert; omitting a missing one makes chsh refuse.
func TestFishAddsTheEntryWhenShellsCannotBeRead(t *testing.T) {
	fake := &fakeChange{readFileErr: map[string]bool{"/etc/shells": true}}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			found = true
		}
	}
	if !found {
		t.Errorf("an unreadable /etc/shells must not be read as 'already listed': %v", fake.Ops)
	}
}

func TestFishSkipsChshWhenTheLoginShellIsAlreadyFish(t *testing.T) {
	for _, shell := range []string{"/opt/homebrew/bin/fish", "/usr/local/bin/fish"} {
		fake := &fakeChange{}
		var out bytes.Buffer
		if err := phase.Fish(fishContext(fake, &out, shell)); err != nil {
			t.Fatalf("%s: unexpected error: %v", shell, err)
		}
		for _, op := range fake.Ops {
			if strings.Contains(op, "chsh") {
				t.Errorf("%s: must not plan a shell change: %s", shell, op)
			}
		}
	}
}

// The refusal, and the assertion that it is a refusal rather than a partial
// application.
//
// NOTHING may be recorded, not merely no chsh. The check used to sit inside
// loginShell, which runs after registerShell has already issued
// `sudo /bin/sh -c '... >> /etc/shells'` -- so a run that could not proceed had
// changed the machine under sudo before saying so. The append is idempotent and
// harmless in itself; the property it breaks is the one this whole design rests
// on, that a refusal means nothing was performed.
func TestFishRefusesAnUnknownLoginNameBeforePerformingAnything(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	ctx := fishContext(fake, &out, "/bin/bash")
	ctx.User = ""
	err := phase.Fish(ctx)
	if err == nil {
		t.Fatal("an empty login name must refuse: `sudo chsh -s <shell>` with no " +
			"user argument changes root's shell")
	}
	if len(fake.Ops) != 0 {
		t.Errorf("a refused run performed %v; the /etc/shells append is privileged "+
			"and must not happen on a run that cannot proceed", fake.Ops)
	}
}

// The other direction, and the reason the check is not a bare empty-User test at
// the top of the phase: a machine already running fish issues no chsh, so it
// needs no login name and must not be refused for lacking one.
func TestFishNeedsNoLoginNameWhenTheShellIsAlreadyFish(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	ctx := fishContext(fake, &out, "/opt/homebrew/bin/fish")
	ctx.User = ""
	if err := phase.Fish(ctx); err != nil {
		t.Fatalf("no name is needed when no chsh is due: %v", err)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "chsh") {
			t.Errorf("must not plan a shell change: %s", op)
		}
	}
}

func TestFishRefusesWhenFishIsAbsentAndDoesNothing(t *testing.T) {
	fake := &fakeChange{lookPathErr: map[string]bool{"fish": true}}
	var out bytes.Buffer
	err := phase.Fish(fishContext(fake, &out, "/bin/bash"))
	if err == nil {
		t.Fatal("want a refusal when fish is not installed")
	}
	if !strings.Contains(err.Error(), "packages phase") {
		t.Errorf("the refusal must point at the phase that installs fish: %v", err)
	}
	if len(fake.Ops) != 0 {
		t.Errorf("nothing may run before fish is known to exist: %v", fake.Ops)
	}
}

// --no-config is not tidiness. Sourcing the owner's configuration to install
// plugins runs whatever that configuration does; this invocation needs exactly
// one function from one tracked file and asks for nothing else.
func TestFishInstallsPluginsWithoutLoadingTheUsersConfiguration(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := fake.Ops[len(fake.Ops)-1]
	if !strings.HasPrefix(last, "run fish --no-config -c ") {
		t.Errorf("the plugin install must be last and must use --no-config: %s", last)
	}
	if !strings.Contains(last, "$argv[1]") {
		t.Errorf("the root must reach fish as $argv[1], not interpolated: %s", last)
	}
	if !strings.HasSuffix(last, " /repo") {
		t.Errorf("the resolved root must be the trailing argument: %s", last)
	}
}

func TestFishStopsAtTheFirstFailure(t *testing.T) {
	for _, tc := range []struct{ name, failOn string }{
		{"shells entry", "/bin/sh"},
		{"chsh", "chsh"},
		{"fisher", "fish --no-config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeChange{failOn: tc.failOn}
			var out bytes.Buffer
			if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err == nil {
				t.Fatal("want the refusal to propagate, got nil")
			}
			for _, op := range fake.Ops {
				if strings.Contains(op, tc.failOn) {
					t.Errorf("the failing operation must not be recorded: %s", op)
				}
			}
		})
	}
}

// mypre reads the file the fish phase sources. Every case below reads it from
// disk rather than from a fixture, because the thing under test is the tracked
// file itself -- these are the only assertions this suite can make about a fish
// function it is forbidden to execute.
func mypre(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../../fish/mypre.fish")
	if err != nil {
		t.Fatalf("the fish phase invokes install_fisher from this file: %v", err)
	}
	return string(body)
}

// installFisherBody returns the CODE lines between `function install_fisher`
// and the `end` that closes it -- comments and blanks dropped, because every
// assertion below is about what the function does. A comment quoting the fisher
// source it guards against would otherwise satisfy those assertions on its own.
//
// The scan fails loudly on a missing header or a missing terminator: an empty
// body would let every assertion pass over nothing, which is how a guard rots
// into decoration.
func installFisherBody(t *testing.T) []string {
	t.Helper()
	var body []string
	inside := false
	for _, line := range strings.Split(mypre(t), "\n") {
		if strings.HasPrefix(line, "function install_fisher") {
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if line == "end" {
			if len(body) == 0 {
				t.Fatal("install_fisher has no body at all")
			}
			return body
		}
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		body = append(body, line)
	}
	if !inside {
		t.Fatal("mypre.fish defines no install_fisher; the fish phase calls it by name")
	}
	t.Fatal("install_fisher is never closed by an `end` at column 0")
	return nil
}

// indexOf returns the position of the first line in body containing want, or -1.
func indexOf(body []string, want string) int {
	for i, line := range body {
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

// conditionIndex is indexOf over the lines that DECIDE something -- output
// statements skipped. A refusal message naturally quotes the variable it read
// and the directories it looked in, so without this a guard that printed the
// right words while testing nothing would satisfy every assertion about it.
func conditionIndex(body []string, want string) int {
	for i, line := range body {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "echo ") || strings.HasPrefix(trimmed, "printf ") {
			continue
		}
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

// saysAll reports whether the output statements in body name every one of want.
func saysAll(body []string, want ...string) (missing []string) {
	var said strings.Builder
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "echo ") || strings.HasPrefix(trimmed, "printf ") {
			said.WriteString(line)
		}
	}
	for _, w := range want {
		if !strings.Contains(said.String(), w) {
			missing = append(missing, w)
		}
	}
	return missing
}

// fishfile is the tracked plugin list, and as of the fish_plugins seed row it is
// also what fisher's own record starts out as. Two readers, one file.
//
// fisher reads fish_plugins with `string match --regex '^[^\s]+$'`, so EVERY
// non-blank line without whitespace is taken as a plugin name -- a `#` comment
// would be fetched as a repository. The file therefore carries plugin specs and
// nothing else, and that is a constraint on the file rather than a convention.
func TestFishfileIsNothingButPluginSpecs(t *testing.T) {
	data, err := os.ReadFile("../../../fish/fishfile")
	if err != nil {
		t.Fatalf("install_fisher reads this file from the checkout: %v", err)
	}
	var plugins []string
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != strings.TrimSpace(line) || strings.HasPrefix(line, "#") || line == "" {
			t.Errorf("line %d is %q; fisher would fetch that as a plugin name", i+1, line)
			continue
		}
		plugins = append(plugins, line)
	}
	if len(plugins) == 0 {
		t.Fatal("fishfile is empty; seeding it as fish_plugins would record nothing")
	}
	// First, because a plugin manager that is not in its own record is a plugin
	// manager the next `fisher update` removes.
	if plugins[0] != "jorgebucaran/fisher" {
		t.Errorf("the first plugin is %q, want jorgebucaran/fisher: fisher must "+
			"name itself in the list that becomes its record", plugins[0])
	}
}

// The other half of the same statement. fishfile names fisher, so the command
// must not: `fisher install jorgebucaran/fisher $plugins` names it twice, and
// the tracked list stops being the whole list the moment the command carries an
// entry the file does not.
func TestInstallFisherTakesItsWholePluginListFromTheFile(t *testing.T) {
	body := installFisherBody(t)
	i := indexOf(body, "fisher install")
	if i < 0 {
		t.Fatalf("install_fisher never invokes `fisher install`:\n%s", strings.Join(body, "\n"))
	}
	// The arguments after the verb, not the whole line: the installer's URL on
	// the same line names jorgebucaran/fisher too, and a substring search would
	// read that as the command naming it.
	_, args, _ := strings.Cut(body[i], "fisher install")
	if strings.TrimSpace(args) != "$plugins" {
		t.Errorf("`fisher install%s` installs something other than exactly the "+
			"file's list; naming a plugin here means the tracked list is no longer "+
			"the whole list", args)
	}
}

// The guard, pinned as far as a Go test can pin a fish function it must not
// execute: this asserts the guard's TEXT and its ORDER, not its behaviour. See
// the report for what that leaves uncovered.
//
// The rule is install onto a machine with no plugin files, and leave an existing
// installation alone. Under --no-config -- which is how the fish phase calls this
// function, and which disables universal variables outright -- fisher's record
// $_fisher_plugins is invisible, so bootstrap cannot tell "already installed"
// from "half installed" and must not guess. Letting fisher run anyway is what
// broke the machine in the field: it classifies install-vs-update from that
// invisible record, refuses every plugin as a conflict, installs nothing, and --
// because nothing installed -- reaches `command rm -f $fish_plugins` and deletes
// its own record, which made every retry identical.
//
// The skip is status 0, and that is the assertion this case exists for. Every
// phase must be safe to re-run; a refusal here fails `apply workstation` on a
// perfectly healthy machine, which is why the first version of this guard was
// wrong.
func TestInstallFisherSkipsAMachineThatAlreadyHasPluginFiles(t *testing.T) {
	body := installFisherBody(t)
	invoke := indexOf(body, "fisher install")
	if invoke < 0 {
		t.Fatalf("install_fisher never invokes `fisher install`:\n%s", strings.Join(body, "\n"))
	}
	// Each of the three directories has to be LOOKED IN. Naming them in the
	// message is the other half, asserted separately below: a guard that globs
	// two of the three lets the third through to fisher, and the message would
	// still read as though all three had been checked.
	for _, dir := range []string{"functions", "conf.d", "completions"} {
		i := conditionIndex(body, dir+"/*.fish")
		if i < 0 {
			t.Errorf("nothing looks for a .fish file in %s/, so an existing "+
				"installation there reaches fisher anyway", dir)
			continue
		}
		if i > invoke {
			t.Errorf("%s/ is only examined at line %d, after fisher runs at line %d",
				dir, i, invoke)
		}
	}
	// The skip has to be CONDITIONAL on what the glob found. Without this, a
	// guard whose condition had been replaced by a constant would satisfy every
	// other assertion here -- the glob, the message and the return would all
	// still be present -- while either skipping every machine or none.
	glob := conditionIndex(body, "functions/*.fish")
	if glob >= 0 {
		var name string
		for _, field := range strings.Fields(body[glob])[1:] {
			if !strings.HasPrefix(field, "-") {
				name = field
				break
			}
		}
		if name == "" {
			t.Fatalf("cannot tell what the glob is assigned to: %q", strings.TrimSpace(body[glob]))
		}
		tested := -1
		for i, line := range body[:invoke] {
			if i == glob || conditionIndex([]string{line}, name) < 0 {
				continue
			}
			tested = i
			break
		}
		if tested < 0 {
			t.Errorf("nothing tests %s after the glob fills it, so the skip does not "+
				"depend on whether any plugin file was actually found", name)
		} else {
			// Two shapes that would satisfy every assertion above while inverting
			// or disabling the rule. These are checks on the CONDITION'S TEXT --
			// whether it evaluates as intended is the part no Go test can reach.
			cond := strings.TrimSpace(body[tested])
			// The skip fires when the glob found something. Negated, it would
			// install onto exactly the machines it exists to leave alone, and skip
			// the fresh ones it exists to provision.
			if strings.HasPrefix(cond, "if not ") || strings.HasPrefix(cond, "if !") {
				t.Errorf("the skip is conditioned on %q; inverted, it installs onto a "+
					"machine that already has plugin files and skips the fresh one", cond)
			}
			// A constant conjunct switches the whole guard off in place. The
			// semicolon is trimmed because `if false; and set --query installed[1]`
			// is how that is spelled on one line, and it is the shape a plausible
			// edit takes -- the variable is still tested, so nothing else here
			// notices.
			for _, word := range strings.Fields(cond) {
				if word = strings.Trim(word, ";"); word == "false" || word == "true" {
					t.Errorf("the condition %q carries the constant %q, so it no longer "+
						"depends on what the glob found", cond, word)
				}
			}
		}
	}
	// A skip nobody can see is indistinguishable from a phase that did its work,
	// and the maintenance path is the whole reason skipping is acceptable.
	if missing := saysAll(body[:invoke], "functions", "conf.d", "completions", "fisher update"); missing != nil {
		t.Errorf("the skip never says %v; it must name the three directories it "+
			"found files in and point at `fisher update` from an interactive shell, "+
			"which is where fisher's record is visible", missing)
	}
	// Zero, and before the invocation. This is the correction: a machine whose
	// plugins are already in place is one this phase has nothing to do to, so
	// `apply workstation` must stay green on it.
	ret := indexOf(body[:invoke], "return")
	if ret < 0 {
		t.Fatalf("nothing returns before `fisher install`, so an existing "+
			"installation is handed to fisher:\n%s", strings.Join(body, "\n"))
	}
	if got := strings.TrimSpace(body[ret]); got != "return 0" {
		t.Errorf("the skip is %q, want `return 0`; a non-zero skip fails "+
			"`apply workstation` on a healthy machine, and a bare `return` leaves "+
			"the status to whatever ran last", got)
	}
	// Never clobber. Every file in those directories was plugin-owned on the
	// machine that hit this, but that is a fact about one machine: a hand-written
	// function there would be destroyed with no way back. It is also what makes
	// skipping the only available verdict -- bootstrap cannot inspect its way to
	// a better one.
	for _, destructive := range []string{"rm ", "rm -", "mv "} {
		if i := indexOf(body, destructive); i >= 0 {
			t.Errorf("install_fisher line %d runs %q; an existing installation is "+
				"left exactly as it was found: %q", i, destructive, strings.TrimSpace(body[i]))
		}
	}
}

// git.io is GitHub's retired URL shortener. Measured 2026-08-11: it still
// answers 301 to
// https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish
// -- so this is hardening, not a repair. Provisioning a new machine should not
// route through a service its owner has already announced the end of.
func TestMypreNamesTheFisherInstallerDirectly(t *testing.T) {
	body := mypre(t)
	if strings.Contains(body, "git.io") {
		t.Error("git.io is a retired shortener; name the raw URL it redirects to")
	}
	if !strings.Contains(body,
		"https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish") {
		t.Error("install_fisher must fetch the installer from its canonical URL")
	}
}
