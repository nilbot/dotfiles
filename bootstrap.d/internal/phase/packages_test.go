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
	opAptUpdate  = "sudo apt-get update"
	opAptInstall = "sudo apt-get install -y build-essential curl file git"
	opPacman     = "sudo pacman -S --needed --noconfirm base-devel curl file git"
	// The installer script is one argv element, and the fake quotes it because a
	// bare join could not say so: `-c` takes the whole `/bin/bash -c "$(curl
	// ...)"` string, and an implementation that split it into words would render
	// identically here while passing the outer bash a command it cannot run.
	opInstallBrew = `run /bin/bash -c ` +
		`"/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""`
)

// brewOnPath is what fakeChange.LookPath answers for a command it can resolve,
// and therefore the path the bundle runs through on a machine that already has
// Homebrew.
const brewOnPath = "/usr/bin/brew"

// The three prefixes Homebrew installs to, in the order the phase probes them.
const (
	brewAppleSilicon = "/opt/homebrew/bin/brew"
	brewIntel        = "/usr/local/bin/brew"
	brewLinux        = "/home/linuxbrew/.linuxbrew/bin/brew"
)

// opBundleVia is the last operation of every successful run. The phase invokes
// brew by the path it RESOLVED rather than by name, so which path appears here
// is the assertion rather than an incidental detail.
func opBundleVia(brew string) string {
	return "run " + brew + " bundle --file /repo/bootstrap.d/Brewfile"
}

// installedAt is the state a successful Homebrew install leaves behind: a brew
// at one of the three prefixes, and still nothing on PATH. That combination is
// the whole point -- the installer appends a shellenv line to a shell profile,
// which cannot alter the PATH of the process that ran it.
func installedAt(fake *fakeChange, path string) {
	fake.info[path] = change.FileInfo{Exists: true, IsRegular: true}
}

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
	if want := []string{opBundleVia(brewOnPath)}; strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
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
	want := []string{opAptUpdate, opAptInstall, opBundleVia(brewOnPath)}
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
	want := []string{opPacman, opBundleVia(brewOnPath)}
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
//
// This is also the fresh-machine case, and the one an earlier draft of this
// phase got wrong. The installer appends a `shellenv` line to a shell PROFILE,
// which the next login shell reads; it cannot change the PATH of the process
// that ran it, and Machine.LookPath is exec.LookPath over exactly that inherited
// PATH. So `brew` is still not on PATH one statement after installing it, and a
// bundle invoked by bare name dies with "executable file not found in $PATH" on
// precisely the machine this phase exists for.
func TestPackagesInstallsHomebrewWhenItIsAbsent(t *testing.T) {
	fake, ctx, _ := packagesCtx("darwin", "brew")
	installedAt(fake, brewAppleSilicon)
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	want := []string{opInstallBrew, opBundleVia(brewAppleSilicon)}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s\n\nthe bundle must run through the path the "+
			"installer wrote, not the bare name: brew is not on this process's PATH "+
			"and cannot be, so `run brew bundle` fails on every fresh machine",
			strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

// Each of the three prefixes Homebrew actually installs to, and the order they
// are probed in. LookPath is retried first and still fails here -- that is the
// whole premise -- so what is asserted is the Lstat probe alone.
//
// The last case is the ordering: with two prefixes populated the FIRST in the
// list wins, so a reordering of brewLocations is a visible change rather than a
// silent one.
func TestPackagesResolvesBrewByProbingAfterTheInstaller(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platform  string
		installed []string
		want      string
	}{
		{"apple silicon", "darwin", []string{brewAppleSilicon}, brewAppleSilicon},
		{"intel", "darwin", []string{brewIntel}, brewIntel},
		{"linux", "linux", []string{brewLinux}, brewLinux},
		{"both mac prefixes populated, first wins", "darwin",
			[]string{brewIntel, brewAppleSilicon}, brewAppleSilicon},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, ctx, out := packagesCtx(tc.platform, "brew", "pacman")
			for _, path := range tc.installed {
				installedAt(fake, path)
			}
			if err := phase.Packages(ctx); err != nil {
				t.Fatalf("Packages: %v", err)
			}
			if last := fake.Ops[len(fake.Ops)-1]; last != opBundleVia(tc.want) {
				t.Errorf("bundle ran as %q, want %q", last, opBundleVia(tc.want))
			}
			// Reported, not silent. A machine whose brew is off PATH is a machine
			// whose next shell behaves differently from this one, and the operator
			// should be able to see that in the log rather than infer it.
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("the resolved path must appear in the phase's output:\n%s",
					out.String())
			}
		})
	}
}

// The installer reporting success while brew is nowhere means something upstream
// changed. Guessing past that is worse than stopping: the next step would hand a
// fabricated path to `brew bundle`, whose failure would name the guess instead of
// the cause.
//
// All three locations must be named. A reader who hits this needs to know where
// bootstrap looked before they can tell a moved prefix from a broken install.
func TestPackagesRefusesWhenBrewCannotBeResolvedAfterInstalling(t *testing.T) {
	fake, ctx, _ := packagesCtx("darwin", "brew")
	err := phase.Packages(ctx)
	if err == nil {
		t.Fatal("an installer that produced no usable brew must be refused, not " +
			"followed by a bundle through a path nobody verified")
	}
	for _, path := range []string{brewAppleSilicon, brewIntel, brewLinux} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("the refusal must name %s: %v", path, err)
		}
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "bundle") {
			t.Errorf("the bundle ran despite brew being unresolvable: %q", op)
		}
	}
}

// An unreadable prefix must not end the search.
//
// Returning on the first Lstat error means an EACCES on /opt -- which a hardened
// Linux box can have, and which is an error rather than an "absent" answer --
// stops the walk before it ever reaches /home/linuxbrew, on the platform where
// that is the only prefix that can be right. The failure is total (the phase
// refuses) on a machine where Homebrew is installed and findable, which makes it
// worth a case of its own.
func TestPackagesProbesPastAnUnreadablePrefix(t *testing.T) {
	// Every case puts the unreadable prefix BEFORE the one holding brew. A prefix
	// after the winner is never probed at all, so it would prove nothing.
	for _, tc := range []struct {
		name       string
		unreadable []string
		installed  string
		want       string
	}{
		{"EACCES on /opt, brew under linuxbrew",
			[]string{brewAppleSilicon}, brewLinux, brewLinux},
		{"EACCES on the first two, brew under linuxbrew",
			[]string{brewAppleSilicon, brewIntel}, brewLinux, brewLinux},
		{"EACCES on /opt, brew at the very next prefix",
			[]string{brewAppleSilicon}, brewIntel, brewIntel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, ctx, out := packagesCtx("linux", "brew", "pacman")
			fake.lstatErr = map[string]bool{}
			for _, path := range tc.unreadable {
				fake.lstatErr[path] = true
			}
			installedAt(fake, tc.installed)
			if err := phase.Packages(ctx); err != nil {
				t.Fatalf("an unreadable prefix must not end the search: %v", err)
			}
			if last := fake.Ops[len(fake.Ops)-1]; last != opBundleVia(tc.want) {
				t.Errorf("bundle ran as %q, want %q", last, opBundleVia(tc.want))
			}
			// Not lost, either. A prefix bootstrap could not read is something the
			// operator should be able to see, even on a run that succeeded.
			for _, path := range tc.unreadable {
				if !strings.Contains(out.String(), path) {
					t.Errorf("the unreadable prefix %s must be reported:\n%s",
						path, out.String())
				}
			}
		})
	}
}

// Every prefix unreadable is still a refusal: an error is not evidence that brew
// is there.
func TestPackagesRefusesWhenEveryPrefixIsUnreadable(t *testing.T) {
	fake, ctx, _ := packagesCtx("linux", "brew", "pacman")
	fake.lstatErr = map[string]bool{
		brewAppleSilicon: true, brewIntel: true, brewLinux: true,
	}
	err := phase.Packages(ctx)
	if err == nil {
		t.Fatal("three unreadable prefixes are not three usable brews")
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "bundle") {
			t.Errorf("the bundle ran through an unverified path: %q", op)
		}
	}
}

// ACCEPTED LIMITATION, pinned deliberately so a later change cannot flip it
// silently.
//
// `plan` cannot preview a machine that has no Homebrew yet. This fixture is that
// machine exactly: no brew on PATH, none of the three prefixes populated, and --
// under `plan` -- an installer that Planner.Run records without executing. The
// phase then finds nothing to resolve and stops with exit 2, where before it
// would have printed a plan.
//
// This is not an oversight and must not be "fixed" by falling back to the bare
// name. The phase cannot distinguish "the installer ran and produced nothing"
// from "the installer was recorded, not run"; the only general way to tell them
// apart is a per-operation "was this performed" signal on Machine, which is a
// mode flag every phase could then branch on -- the erosion the dry-run
// invariant exists to prevent. The message is worded to be true either way, and
// naming all three prefixes is what lets a reader tell a moved prefix from a
// machine that simply has no Homebrew.
func TestPackagesRefusesUnderPlanOnAMachineWithNoHomebrew(t *testing.T) {
	// darwin, so stage zero is not what stops the run: the only thing missing
	// here is Homebrew itself.
	fake, ctx, _ := packagesCtx("darwin", "brew")

	err := phase.Packages(ctx)
	if err == nil {
		t.Fatal("a machine with no resolvable Homebrew must stop the phase; " +
			"planning a bundle against a brew that does not exist reports work " +
			"that cannot happen")
	}
	for _, path := range []string{brewAppleSilicon, brewIntel, brewLinux} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("the message must name %s so a moved prefix is "+
				"distinguishable from an absent Homebrew: %v", path, err)
		}
	}
	// The installer was reached -- this is the fresh-machine path, not an early
	// exit somewhere above it.
	if !slices.Contains(fake.Ops, opInstallBrew) {
		t.Errorf("the installer step must have been reached; ops: %v", fake.Ops)
	}
	if slices.Contains(fake.Ops, opBundleVia(brewOnPath)) {
		t.Error("the bundle must not run through the bare name; that is the defect " +
			"this refusal replaced")
	}
}

// The already-installed path must NOT probe. PATH is the machine's own answer to
// "which brew", and a fixed list could disagree with it -- a Homebrew under
// /opt/homebrew on a machine deliberately using /usr/local would be silently
// swapped underneath the operator.
//
// The fixture makes the two answers differ: brew resolves on PATH to
// /usr/bin/brew AND a brew exists at the first probe location. Only an
// implementation that probes when it should not can produce the second.
func TestPackagesPrefersPathOverProbingWhenBrewIsAlreadyInstalled(t *testing.T) {
	fake, ctx, _ := packagesCtx("darwin")
	installedAt(fake, brewAppleSilicon)
	if err := phase.Packages(ctx); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	want := []string{opBundleVia(brewOnPath)}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s\n\nLookPath answered %s; probing overrode the "+
			"machine's own answer", strings.Join(fake.Ops, "\n"),
			strings.Join(want, "\n"), brewOnPath)
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
	//
	// "already" is required, not merely the word Homebrew. The two outcomes an
	// operator has to tell apart are "it was here, nothing was done" and "it was
	// installed just now, and your next shell will see a PATH this one does not";
	// a message that fits both reports neither.
	log := strings.ToLower(out.String())
	if !strings.Contains(log, "homebrew") || !strings.Contains(log, "already") {
		t.Errorf("the skip must be visible in the phase's output, and distinguishable "+
			"from having installed Homebrew:\n%s", out.String())
	}
}

// `brew bundle` is last on every path, and it names the Brewfile by absolute
// path. Both halves matter: the phase has no cwd concept -- Machine has no cd
// and no shell -- so a relative --file would resolve against wherever the user
// happened to invoke ./bootstrap from, and a bundle that runs before its own
// prerequisites is a bundle against a Homebrew that may not exist yet.
func TestPackagesBundlesLastOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platform  string
		absent    []string
		installed string // where the installer put brew, when it had to run
		want      string
	}{
		{"darwin, brew present", "darwin", nil, "", brewOnPath},
		{"darwin, brew absent", "darwin", []string{"brew"}, brewAppleSilicon, brewAppleSilicon},
		{"debian", "linux", []string{"pacman"}, "", brewOnPath},
		{"debian, brew absent", "linux", []string{"pacman", "brew"}, brewLinux, brewLinux},
		{"arch", "linux", []string{"apt-get"}, "", brewOnPath},
		{"arch, brew absent", "linux", []string{"apt-get", "brew"}, brewLinux, brewLinux},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, ctx, _ := packagesCtx(tc.platform, tc.absent...)
			if tc.installed != "" {
				installedAt(fake, tc.installed)
			}
			if err := phase.Packages(ctx); err != nil {
				t.Fatalf("Packages: %v", err)
			}
			if len(fake.Ops) == 0 {
				t.Fatal("the phase performed nothing")
			}
			// opBundleVia spells the absolute Brewfile path, so matching it asserts
			// both halves at once.
			if last := fake.Ops[len(fake.Ops)-1]; last != opBundleVia(tc.want) {
				t.Errorf("last op is %q, want %q; ops: %v", last, opBundleVia(tc.want), fake.Ops)
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
			[]string{opAptInstall, opBundleVia(brewOnPath)}},
		{"apt install", "linux", []string{"pacman"}, "sudo apt-get install", "apt-get",
			[]string{opBundleVia(brewOnPath)}},
		{"pacman", "linux", []string{"apt-get"}, "sudo pacman", "pacman",
			[]string{opBundleVia(brewOnPath)}},
		{"homebrew installer", "darwin", []string{"brew"}, "Homebrew/install", "/bin/bash",
			[]string{opBundleVia(brewOnPath), opBundleVia(brewAppleSilicon)}},
		{"bundle", "darwin", nil, "bundle", brewOnPath, nil},
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

// requiredFormulae are the formulae something else in this repo calls. Each is
// here because code breaks without it, and carries the reason why.
//
// This is deliberately NOT the Brewfile's full contents. The Brewfile is the
// record of what this workstation installs and is meant to be edited directly;
// pinning every line would mean editing Go in another module to add a CLI tool,
// and would report an unimportant addition with the same shape and urgency as
// dropping the login shell -- training the reader to sync the list without
// reading it, which is how the change that mattered would get waved through.
var requiredFormulae = []struct{ name, why string }{
	{"fish", "the fish phase installs plugins with it and makes it the login shell"},
	{"gitleaks", "`agents guard --staged` shells out to it"},
	{"uv", "the devtools phase calls it"},
}

// The Brewfile is an AUDIT of the deleted brew/*.list files. What this pins is
// what the audit DECIDED -- the couplings that must hold and the retirements
// that must not silently reverse -- each with the reason it was decided, which
// a set comparison cannot carry.
func TestBrewfileCarriesTheAudit(t *testing.T) {
	body := readBrewfile(t)

	for _, req := range requiredFormulae {
		if !slices.Contains(body.brews, req.name) {
			t.Errorf("%q is missing: %s. brews: %v", req.name, req.why, body.brews)
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

// A formula declared twice is harmless to `brew bundle`, but it is always a
// mistake and it is invisible to any check that compares sets -- the duplicate
// collapses into the set and only a count notices, which is not what a count is
// read for. One reached master this way: an edit meant to drop a formula
// replaced it with the name of one already declared below.
func TestBrewfileDeclaresNothingTwice(t *testing.T) {
	body := readBrewfile(t)
	for _, group := range []struct {
		what  string
		names []string
	}{
		{"formula", body.brews},
		{"cask", body.casks},
	} {
		if len(group.names) == 0 {
			t.Fatalf("no %s lines parsed; this guard would pass without checking anything",
				group.what)
		}
		seen := make(map[string]bool, len(group.names))
		for _, name := range group.names {
			if seen[name] {
				t.Errorf("%s %q is declared more than once", group.what, name)
			}
			seen[name] = true
		}
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
