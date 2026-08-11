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
	want := []string{
		`sudo /bin/sh -c printf '%s\n' "$1" >> /etc/shells sh /usr/bin/fish`,
		"sudo chsh -s /usr/bin/fish someone",
		"run fish --no-config -c source $argv[1]/fish/mypre.fish; install_fisher /repo",
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
	if !strings.Contains(op, `printf '%s\n' "$1" >> /etc/shells`) {
		t.Errorf("the script must use the positional $1: %s", op)
	}
	if !strings.HasSuffix(op, `>> /etc/shells sh /usr/bin/fish`) {
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

// git.io is GitHub's retired URL shortener. Measured 2026-08-11: it still
// answers 301 to
// https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish
// -- so this is hardening, not a repair. Provisioning a new machine should not
// route through a service its owner has already announced the end of.
func TestMypreNamesTheFisherInstallerDirectly(t *testing.T) {
	body, err := os.ReadFile("../../../fish/mypre.fish")
	if err != nil {
		t.Fatalf("the fish phase invokes install_fisher from this file: %v", err)
	}
	if strings.Contains(string(body), "git.io") {
		t.Error("git.io is a retired shortener; name the raw URL it redirects to")
	}
	if !strings.Contains(string(body),
		"https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish") {
		t.Error("install_fisher must fetch the installer from its canonical URL")
	}
}
