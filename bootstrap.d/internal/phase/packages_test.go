package phase_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

// Every operation this phase may perform, spelled exactly as the fake records
// it. Nothing here runs a package manager: fakeChange appends a string and
// returns, so a test that "installs Homebrew" installs a line in a slice.
const (
	opAptUpdate   = "sudo apt-get update"
	opAptInstall  = "sudo apt-get install -y build-essential curl file git"
	opPacman      = "sudo pacman -S --needed --noconfirm base-devel curl file git"
	opInstallBrew = `run /bin/bash -c /bin/bash -c ` +
		`"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
	opBrewBundle = "run brew bundle --file /repo/bootstrap.d/Brewfile"
)

// packagesCtx builds the phase's world. absent names the commands LookPath must
// fail for, which is the only input that decides which branch runs.
func packagesCtx(platform string, absent ...string) (*fakeChange, phase.Context, *bytes.Buffer) {
	missing := map[string]bool{}
	for _, name := range absent {
		missing[name] = true
	}
	fake := &fakeChange{
		info:        map[string]change.FileInfo{},
		links:       map[string]string{},
		lookPathErr: missing,
	}
	out := &bytes.Buffer{}
	return fake, phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: platform,
		Profile: "workstation", Out: out,
	}, out
}

// Stage zero is Homebrew's own prerequisites, and on darwin they arrive with the
// Xcode command line tools -- which this phase does not manage. So the step does
// not run at all, rather than running something macOS-shaped.
//
// The negative is asserted as well as the op list, because the failure worth
// catching is a phase that reaches a native package manager on the wrong
// platform. `sudo apt-get install build-essential` on macOS is not a no-op that
// prints a warning; it is a phase that has misidentified the machine it is on.
func TestPackagesSkipsStageZeroOnDarwin(t *testing.T) {
	fake, ctx, _ := packagesCtx("darwin")
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if want := []string{opBrewBundle}; strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
	for _, op := range fake.Ops {
		for _, native := range []string{"apt-get", "pacman", "sudo "} {
			if strings.Contains(op, native) {
				t.Errorf("darwin reached a native package manager: %q contains %q",
					op, native)
			}
		}
	}
}

// The other half of the same decision. Without it an implementation that skipped
// stage zero unconditionally would pass every darwin case above, and a Linux
// machine would be handed to `brew` with no compiler on it.
func TestPackagesRunsAptStageZeroOnDebian(t *testing.T) {
	fake, ctx, _ := packagesCtx("linux", "pacman")
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	want := []string{opAptUpdate, opAptInstall, opBrewBundle}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
	// The update has to precede the install or the install resolves against an
	// index that may be months old, which on Debian is the difference between a
	// working install and a 404 for every package named.
	if update, install := slices.Index(fake.Ops, opAptUpdate), slices.Index(fake.Ops, opAptInstall); update > install {
		t.Errorf("apt-get update ran at %d, after the install at %d", update, install)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "pacman") {
			t.Errorf("a Debian machine reached pacman: %q", op)
		}
	}
}

func TestPackagesRunsPacmanStageZeroOnArch(t *testing.T) {
	fake, ctx, _ := packagesCtx("linux", "apt-get")
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	want := []string{opPacman, opBrewBundle}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "apt-get") {
			t.Errorf("an Arch machine reached apt-get: %q", op)
		}
	}
}

// Neither present is a machine this phase cannot provision, and it must say so
// before running anything -- a Homebrew install that proceeds without a C
// toolchain fails later, further from the cause, having already touched the box.
//
// Both names are required in the message. "no package manager found" tells the
// reader nothing about which distributions are supported, and this is precisely
// the moment they need to know.
func TestPackagesRefusesWhenNoNativePackageManagerIsPresent(t *testing.T) {
	fake, ctx, _ := packagesCtx("linux", "apt-get", "pacman")
	err := phase.Packages(ctx)
	if err == nil {
		t.Fatal("a Linux machine with neither apt-get nor pacman must be refused")
	}
	for _, name := range []string{"apt-get", "pacman"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal must name %s: %v", name, err)
		}
	}
	if len(fake.Ops) > 0 {
		t.Errorf("nothing may happen before the refusal; ops: %v", fake.Ops)
	}
}

// The installer is the one place this design executes remote code, so the
// condition it runs under is worth pinning from both sides.
func TestPackagesInstallsHomebrewWhenItIsAbsent(t *testing.T) {
	fake, ctx, _ := packagesCtx("darwin", "brew")
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	want := []string{opInstallBrew, opBrewBundle}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

// The skip. An implementation that ignored LookPath would re-run the installer
// on every apply of every already-provisioned machine -- remote code fetched and
// executed for nothing.
func TestPackagesSkipsTheInstallerWhenBrewIsPresent(t *testing.T) {
	fake, ctx, out := packagesCtx("darwin")
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "curl") || strings.Contains(op, "Homebrew/install") {
			t.Errorf("the installer ran on a machine that already has brew: %q", op)
		}
	}
	// Skipped, not silent: a phase that says nothing about a step it did not take
	// is indistinguishable from one that forgot it.
	if !strings.Contains(strings.ToLower(out.String()), "homebrew") {
		t.Errorf("the skip must be visible in the phase's output:\n%s", out.String())
	}
}

// `brew bundle` is last on every path, and it names the Brewfile by absolute
// path. Both halves matter: the phase has no cwd concept -- Machine has no cd
// and no shell -- so a relative --file would resolve against wherever the user
// happened to invoke ./bootstrap from, and a bundle that runs before its own
// prerequisites is a bundle against a Homebrew that may not exist yet.
func TestPackagesBundlesLastOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		absent   []string
	}{
		{"darwin, brew present", "darwin", nil},
		{"darwin, brew absent", "darwin", []string{"brew"}},
		{"debian", "linux", []string{"pacman"}},
		{"debian, brew absent", "linux", []string{"pacman", "brew"}},
		{"arch", "linux", []string{"apt-get"}},
		{"arch, brew absent", "linux", []string{"apt-get", "brew"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, ctx, _ := packagesCtx(tc.platform, tc.absent...)
			if err := phase.Packages(ctx); err != nil {
				t.Fatalf("Packages: %v", err)
			}
			if len(fake.Ops) == 0 {
				t.Fatal("the phase performed nothing")
			}
			// opBrewBundle spells the absolute path, so matching it asserts both
			// halves at once.
			if last := fake.Ops[len(fake.Ops)-1]; last != opBrewBundle {
				t.Errorf("last op is %q, want %q; ops: %v", last, opBrewBundle, fake.Ops)
			}
		})
	}
}

// Every step is a precondition for the ones after it: the bundle needs Homebrew,
// and on Linux Homebrew needs the toolchain stage zero installs. A failure must
// stop the phase rather than be logged and stepped over.
//
// Which refusal comes back is asserted, not just that one did. Absence of the
// later operations is not enough on its own -- a swallowed error followed by a
// failure in the next step produces the same Ops list -- so the fake names the
// failing operation and that is what tells the two apart.
func TestPackagesStopsAtTheFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		absent   []string
		failOn   string
		wantPath string
		mustNot  []string
	}{
		{"apt update", "linux", []string{"pacman"}, "sudo apt-get update", "apt-get",
			[]string{opAptInstall, opBrewBundle}},
		{"apt install", "linux", []string{"pacman"}, "sudo apt-get install", "apt-get",
			[]string{opBrewBundle}},
		{"pacman", "linux", []string{"apt-get"}, "sudo pacman", "pacman",
			[]string{opBrewBundle}},
		{"homebrew installer", "darwin", []string{"brew"}, "Homebrew/install", "/bin/bash",
			[]string{opBrewBundle}},
		{"bundle", "darwin", nil, "run brew bundle", "brew", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, ctx, _ := packagesCtx(tc.platform, tc.absent...)
			fake.failOn = tc.failOn
			err := phase.Packages(ctx)
			var refusal *change.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("want the refusal to propagate, got %T: %v", err, err)
			}
			if refusal.Path != tc.wantPath {
				t.Errorf("the %s step failed but the refusal names %q, not %q; its "+
					"error was swallowed and a later step reported instead",
					tc.name, refusal.Path, tc.wantPath)
			}
			for _, op := range tc.mustNot {
				if slices.Contains(fake.Ops, op) {
					t.Errorf("%q ran after the %s step failed; ops: %v",
						op, tc.name, fake.Ops)
				}
			}
		})
	}
}

// The Brewfile is an AUDIT of brew/*.list, not a translation of it, so the four
// decisions that make it an audit are asserted rather than left to a reader
// diffing two files by eye.
func TestBrewfileCarriesTheAudit(t *testing.T) {
	body := readBrewfile(t)

	for _, want := range []string{"gitleaks", "uv"} {
		if !slices.Contains(body.brews, want) {
			t.Errorf("%q is missing; it is required by tooling that never declared it "+
				"-- gitleaks by 'agents guard --staged', uv by the devtools phase. "+
				"brews: %v", want, body.brews)
		}
	}
	for _, gone := range []string{"micromamba", "youtube-dl"} {
		if slices.Contains(body.brews, gone) {
			t.Errorf("%q survived the audit; micromamba is superseded by uv and "+
				"youtube-dl has been unmaintained since 2021", gone)
		}
	}
	// The live fork, named explicitly: dropping youtube-dl without it would be a
	// removal rather than an audit.
	if !slices.Contains(body.brews, "yt-dlp") {
		t.Errorf("yt-dlp must replace youtube-dl; brews: %v", body.brews)
	}
	if !slices.Contains(body.casks, "font-symbols-only-nerd-font") {
		t.Errorf("the nerd font cask must replace the vendored install-font-linux.sh; "+
			"casks: %v", body.casks)
	}
}

// Casks are macOS-only, and `brew bundle` does not skip a cask on Linux -- it
// aborts the whole bundle. One unguarded line therefore costs every package
// below it, on the platform that has the least chance of noticing.
func TestBrewfileGuardsEveryCaskWithOSMac(t *testing.T) {
	body := readBrewfile(t)
	if len(body.casks) == 0 {
		t.Fatal("no cask lines found; this guard would pass without checking anything")
	}
	for _, line := range body.unguardedCasks {
		t.Errorf("cask line %q is outside the `if OS.mac?` guard; on Linux it does "+
			"not merely fail, it aborts the entire bundle", line)
	}
}

type brewfile struct {
	brews          []string
	casks          []string
	unguardedCasks []string
}

// readBrewfile parses the file the phase names, from the tree it lives in. The
// point is that the FILE is audited, not a copy of its text pasted into a test:
// a Brewfile edited without this test being edited is exactly the drift that
// makes an audit stop meaning anything.
func readBrewfile(t *testing.T) brewfile {
	t.Helper()
	path := filepath.Join("..", "..", "Brewfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the packages phase names this file by absolute path and "+
			"`brew bundle` reads it: %v", err)
	}

	var parsed brewfile
	guarded := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "", strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "if OS.mac?"):
			guarded = true
			continue
		case line == "end":
			guarded = false
			continue
		}
		name, ok := quotedArgument(line)
		if !ok {
			t.Errorf("unrecognised Brewfile line %q; this test cannot vouch for a "+
				"line it does not understand", line)
			continue
		}
		switch {
		case strings.HasPrefix(line, "brew "):
			parsed.brews = append(parsed.brews, name)
		case strings.HasPrefix(line, "cask "):
			parsed.casks = append(parsed.casks, name)
			if !guarded {
				parsed.unguardedCasks = append(parsed.unguardedCasks, line)
			}
		default:
			t.Errorf("unrecognised Brewfile directive %q", line)
		}
	}
	return parsed
}

// quotedArgument returns what a `brew "x"` or `cask "x"` line names.
func quotedArgument(line string) (string, bool) {
	open := strings.Index(line, `"`)
	if open < 0 {
		return "", false
	}
	rest := line[open+1:]
	closing := strings.Index(rest, `"`)
	if closing < 0 {
		return "", false
	}
	return rest[:closing], true
}
