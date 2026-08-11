// The one white-box case in this directory. Everything else about main is
// asserted through the shim, in package main_test, because everything else
// about main is observable from outside; loginShell is not exported and must
// not be, so it is exercised here.
package main

import (
	"errors"
	"reflect"
	"testing"
)

// fakeCommand stands in for the platform's user database tool. No case in this
// file may execute dscl or getent: both read the accounts of the machine the
// suite happens to run on, so a case that ran either would assert whatever the
// developer's own passwd entry says -- which is a different answer on every
// machine, and no answer at all in a container.
//
// argv records each invocation as its ELEMENTS, never as one joined string.
// Joining is how a broken command hides: `dscl . -read /Users/me UserShell`
// passed as a single argument joins to exactly the same text as the five
// separate arguments that work, and an assertion on that text passes while the
// command is unrunnable.
type fakeCommand struct {
	out  string // what the tool writes to standard output
	err  error  // non-nil when the tool cannot be run, or exits non-zero
	argv [][]string
}

func (f *fakeCommand) read(name string, args ...string) ([]byte, error) {
	f.argv = append(f.argv, append([]string{name}, args...))
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.out), nil
}

// The incident this resolver exists for, on darwin.
//
// $SHELL says /bin/zsh because the run started in a zsh terminal; the account's
// login shell has been fish for months. Reading $SHELL made `check` report
// "the login shell is /bin/zsh, not fish" on a machine that was already
// provisioned exactly as this repository intends.
//
// The argv is asserted as well as the answer. A resolver that asked the wrong
// question would still parse this fixture's output perfectly.
func TestLoginShellReadsTheDarwinUserDatabaseRatherThanSHELL(t *testing.T) {
	fake := &fakeCommand{out: "UserShell: /opt/homebrew/bin/fish\n"}

	got := loginShell(fake.read, "darwin", "nilbot", "/bin/zsh")

	if got != "/opt/homebrew/bin/fish" {
		t.Errorf("login shell = %q, want /opt/homebrew/bin/fish; $SHELL is inherited "+
			"from whatever started this process and describes the session, not the "+
			"account", got)
	}
	want := [][]string{{"dscl", ".", "-read", "/Users/nilbot", "UserShell"}}
	if !reflect.DeepEqual(fake.argv, want) {
		t.Errorf("asked %q, want %q", fake.argv, want)
	}
}

// The same, on linux. getent prints the passwd line itself: seven
// colon-separated fields, the shell last.
func TestLoginShellReadsTheLinuxUserDatabaseRatherThanSHELL(t *testing.T) {
	fake := &fakeCommand{out: "nilbot:x:1000:1000:Ersi Ni,,,:/home/nilbot:/usr/bin/fish\n"}

	got := loginShell(fake.read, "linux", "nilbot", "/bin/bash")

	if got != "/usr/bin/fish" {
		t.Errorf("login shell = %q, want /usr/bin/fish", got)
	}
	want := [][]string{{"getent", "passwd", "nilbot"}}
	if !reflect.DeepEqual(fake.argv, want) {
		t.Errorf("asked %q, want %q", fake.argv, want)
	}
}

// A tool that cannot be run, or that exits non-zero, falls back to the hint.
//
// The assertion is that the hint comes back, not merely that fish does not:
// returning "" here is the failure mode most likely to escape review, and check
// reads an empty shell as "the login shell cannot be determined", which is a
// worse report than the stale one $SHELL gives.
func TestLoginShellFallsBackToSHELLWhenTheToolFails(t *testing.T) {
	for _, plat := range []string{"darwin", "linux"} {
		t.Run(plat, func(t *testing.T) {
			fake := &fakeCommand{err: errors.New("executable file not found in $PATH")}

			got := loginShell(fake.read, plat, "nilbot", "/bin/zsh")

			if got != "/bin/zsh" {
				t.Errorf("login shell = %q, want the $SHELL hint /bin/zsh; an empty "+
					"answer makes check report that the login shell cannot be "+
					"determined at all", got)
			}
			if len(fake.argv) != 1 {
				t.Errorf("asked %d times, want 1: %q", len(fake.argv), fake.argv)
			}
		})
	}
}

// Output the parser does not recognise falls back to the hint too, and each of
// these is something one of the two tools really emits.
//
// A parser that took whatever it found would hand check a path that is not a
// shell -- a home directory, or a fragment of dscl's error text -- and check
// would then report a login-shell failure naming it, which is worse than
// reporting the stale one.
func TestLoginShellFallsBackToSHELLOnOutputItCannotParse(t *testing.T) {
	for _, c := range []struct {
		name, plat, out string
	}{
		// dscl answers an unknown record on stdout and exits non-zero; asserted
		// here as well because the exit status is not what the parser sees.
		{"darwin no such user", "darwin",
			"<dscl_cmd> DS Error: -14136 (eDSRecordNotFound)\n"},
		// The key with nothing after it. dscl prints this shape when the value is
		// on the next line, and an account can have an empty UserShell.
		{"darwin empty value", "darwin", "UserShell:\n"},
		{"darwin empty output", "darwin", ""},
		// A truncated passwd line: six fields, so the last one is the HOME
		// directory. Anything that reached for the final field would return it.
		{"linux truncated line", "linux", "nilbot:x:1000:1000::/home/nilbot\n"},
		// Seven fields with the last one empty -- a real passwd entry, meaning
		// "the system default".
		{"linux empty shell field", "linux", "nilbot:x:1000:1000::/home/nilbot:\n"},
		{"linux empty output", "linux", ""},
		{"linux error text", "linux", "getent: command not found\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeCommand{out: c.out}

			got := loginShell(fake.read, c.plat, "nilbot", "/bin/zsh")

			if got != "/bin/zsh" {
				t.Errorf("login shell = %q, want the $SHELL hint /bin/zsh", got)
			}
			if len(fake.argv) != 1 {
				t.Fatalf("the tool was asked %d times; this case proves nothing if the "+
					"resolver never got as far as parsing: %q", len(fake.argv), fake.argv)
			}
		})
	}
}

// The forms that must still parse. Each of these is the parser being tolerant
// of exactly what the tools do, and none of them may take the fallback -- a
// fallback here would silently report the session's shell as the account's,
// which is the whole defect.
func TestLoginShellParsesTheOutputTheToolsReallyEmit(t *testing.T) {
	for _, c := range []struct {
		name, plat, out, want string
	}{
		{"darwin no trailing newline", "darwin", "UserShell: /bin/zsh", "/bin/zsh"},
		{"darwin trailing whitespace", "darwin", "UserShell: /bin/zsh  \n", "/bin/zsh"},
		{"darwin carriage return", "darwin", "UserShell: /bin/zsh\r\n", "/bin/zsh"},
		{"linux no trailing newline", "linux",
			"nilbot:x:1000:1000::/home/nilbot:/bin/zsh", "/bin/zsh"},
		{"linux trailing whitespace", "linux",
			"nilbot:x:1000:1000::/home/nilbot:/bin/zsh \n", "/bin/zsh"},
		// The GECOS field holds commas and spaces on a normal Debian account, and
		// the home directory is a path: neither may confuse the field count.
		{"linux populated gecos", "linux",
			"nilbot:x:1000:1000:Ersi Ni,Room 1,,:/home/nilbot:/opt/fish/bin/fish",
			"/opt/fish/bin/fish"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeCommand{out: c.out}

			if got := loginShell(fake.read, c.plat, "nilbot", "/bin/bash"); got != c.want {
				t.Errorf("login shell = %q, want %q", got, c.want)
			}
		})
	}
}

// With no login name there is no record to read, and on a platform this tool
// does not support there is no tool to read it with. Both answer the hint
// WITHOUT running anything: `dscl . -read /Users/ UserShell` is a question
// about a record that does not exist, and running a command to be told so is
// still running a command during a `plan`.
func TestLoginShellRunsNothingWithoutANameOrASupportedPlatform(t *testing.T) {
	for _, c := range []struct{ name, plat, user string }{
		{"no login name", "darwin", ""},
		{"unsupported platform", "windows", "nilbot"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeCommand{out: "UserShell: /opt/homebrew/bin/fish\n"}

			got := loginShell(fake.read, c.plat, c.user, "/bin/zsh")

			if got != "/bin/zsh" {
				t.Errorf("login shell = %q, want the $SHELL hint /bin/zsh", got)
			}
			if len(fake.argv) != 0 {
				t.Errorf("ran %q; there is nothing to ask", fake.argv)
			}
		})
	}
}
