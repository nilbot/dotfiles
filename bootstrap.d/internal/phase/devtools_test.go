package phase_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

// The operations the phase owes a machine, in the only order that works.
const (
	// The RESOLVED brew. fakeChange.LookPath answers /usr/bin/<name>, so this
	// is what the phase gets on a machine that has brew on PATH -- and pinning
	// it here is what makes a regression to the bare name visible.
	opInstallUv    = "run /usr/bin/brew install uv"
	opCheckHooks   = "run bash /repo/git/install-hooks.sh preflight --adopt-owned /repo /home /opt/homebrew/bin/agents"
	opInstallHooks = "run bash /repo/git/install-hooks.sh install --adopt-owned /repo /home /opt/homebrew/bin/agents"
)

func devtoolsCtx(uvOnPath bool) (*fakeChange, phase.Context, *bytes.Buffer) {
	fake := &fakeChange{
		info: map[string]change.FileInfo{
			// The tap's binary at the first Homebrew prefix, which is what
			// resolveAgents probes for. Note that it is NOT on PATH: the fake
			// answers /usr/bin/<name> for everything, and a phase that went
			// looking there would find nothing -- which is the defect this
			// fixture is shaped to catch.
			"/opt/homebrew/bin/agents": {Exists: true},
		},
		links:       map[string]string{},
		lookPathErr: map[string]bool{"uv": !uvOnPath},
	}
	out := &bytes.Buffer{}
	return fake, phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Out: out,
	}, out
}

func TestDevtoolsRunsItsStepsInOrder(t *testing.T) {
	fake, ctx, _ := devtoolsCtx(false)
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("Devtools: %v", err)
	}
	want := []string{opInstallUv, opCheckHooks, opInstallHooks}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

// The ordering, asserted by position rather than by the whole list, so it keeps
// reporting the same finding if a step is ever added above or between.
//
// This is not stylistic. The installer is the only thing that links the four
// hook names and the only thing that writes core.hooksPath, and it refuses
// without touching anything -- so running its preflight first is what makes a
// machine whose global git config is unusable, or whose chain belongs to another
// binary, learn that before a link is written. `install` re-runs the same
// validation internally, which is why the assertion is about the preflight's
// POSITION and not about its presence.
func TestDevtoolsChecksTheHooksBeforeInstallingThem(t *testing.T) {
	fake, ctx, _ := devtoolsCtx(false)
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("Devtools: %v", err)
	}
	check := slices.Index(fake.Ops, opCheckHooks)
	install := slices.Index(fake.Ops, opInstallHooks)
	if check < 0 || install < 0 {
		t.Fatalf("both steps must happen; ops: %v", fake.Ops)
	}
	if check > install {
		t.Errorf("the hooks preflight ran at %d, after the install at %d; a machine "+
			"whose global git config is unusable should learn that before anything "+
			"is linked, not after", check, install)
	}
}

// The delegation is the point of this phase. git/install-hooks.sh validates the
// global config, links ~/.gitattributes, symlinks the four hook names and writes
// core.hooksPath LAST so a partial install cannot activate an incomplete
// directory -- and it is already tested in the module that owns it. A phase that
// reimplemented any of that would be a second, untested, ordering-sensitive
// copy of it.
func TestDevtoolsDelegatesHooksRatherThanInstallingThem(t *testing.T) {
	fake, ctx, _ := devtoolsCtx(false)
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("Devtools: %v", err)
	}
	for _, want := range []string{opCheckHooks, opInstallHooks} {
		if !slices.Contains(fake.Ops, want) {
			t.Errorf("the exact delegation is missing; ops:\n%s\nwant:\n%s",
				strings.Join(fake.Ops, "\n"), want)
		}
	}
	for _, op := range fake.Ops {
		switch {
		case strings.HasPrefix(op, "link "), strings.HasPrefix(op, "seed "):
			t.Errorf("the phase linked something itself (%q); hooks and "+
				"~/.gitattributes belong to git/install-hooks.sh", op)
		case strings.Contains(op, "core.hooksPath"):
			t.Errorf("the phase wrote core.hooksPath itself (%q); the installer "+
				"writes it last so a partial install cannot activate an "+
				"incomplete hooks directory", op)
		}
	}
}

// A machine that has run the packages phase has the tap's binary at a Homebrew
// prefix; one that has not has nothing for the chain to point at. The refusal
// must name every path it looked in -- "no agents binary" on its own is a dead
// end for whoever has to act on it -- and must perform no operation first, so
// the failure cannot leave a machine half-changed.
//
// This is also where the LookPath trap is caught, and the fixture is shaped so
// that the trap is loud rather than lucky. The fake's LookPath answers
// /usr/bin/<name> for any name it does not know, so a resolveAgents that tried
// LookPath("agents") first -- the thing this phase must not do, because
// Homebrew's shellenv is read by the next login shell and not by this process --
// would find /usr/bin/agents and hand the installer a path that is not the
// tap's. Measured: adding that arm makes this case fail with "no agents binary
// at any Homebrew prefix, and the phase proceeded".
func TestDevtoolsRefusesWhenNoAgentsIsInstalled(t *testing.T) {
	fake, ctx, _ := devtoolsCtx(true)
	delete(fake.info, "/opt/homebrew/bin/agents")

	err := phase.Devtools(ctx)
	if err == nil {
		t.Fatal("no agents binary at any Homebrew prefix, and the phase proceeded")
	}
	for _, want := range []string{
		"/opt/homebrew/bin/agents",
		"/usr/local/bin/agents",
		"/home/linuxbrew/.linuxbrew/bin/agents",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s, so it does not say where it "+
				"looked: %v", want, err)
		}
	}
	if len(fake.Ops) != 0 {
		t.Errorf("the phase performed operations before refusing:\n%s",
			strings.Join(fake.Ops, "\n"))
	}
}

// The other branch of the uv decision. Without it, an implementation that
// ignored LookPath entirely would pass every case above.
func TestDevtoolsSkipsUvWhenItIsAlreadyOnPath(t *testing.T) {
	fake, ctx, out := devtoolsCtx(true)
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("Devtools: %v", err)
	}
	want := []string{opCheckHooks, opInstallHooks}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
	// Skipped, not silent: a phase that says nothing about a step it did not
	// take is indistinguishable from one that forgot it.
	if !strings.Contains(out.String(), "uv") {
		t.Errorf("the skip must be visible in the phase's output:\n%s", out.String())
	}
}

// The same finding the fish phase carried, at the third site to have it, and
// the reason this test does not simply trust LookPath: on a fresh Linux box
// Homebrew's installer writes a shellenv line into a profile that only the next
// login shell reads, so brew is installed and unfindable by name in the run that
// installed it. Measured in CI 2026-08-17 on debian:stable-slim -- stage zero
// succeeded, Homebrew landed at /home/linuxbrew/.linuxbrew, the fish phase found
// it there, and this phase died on `exec: "brew": executable file not found in
// $PATH` one phase later.
func TestDevtoolsFindsBrewOutsideThisProcessPATH(t *testing.T) {
	fake := &fakeChange{
		info: map[string]change.FileInfo{
			"/home/linuxbrew/.linuxbrew/bin/brew":   {Exists: true},
			"/home/linuxbrew/.linuxbrew/bin/agents": {Exists: true},
		},
		links: map[string]string{},
		// The WHOLE of what this machine has on PATH: nothing. uv is absent, so
		// the install runs, and brew is absent, so it must be resolved.
		lookPathOnly: map[string]bool{},
	}
	out := &bytes.Buffer{}
	ctx := phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "linux",
		Profile: "workstation", Out: out,
	}
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("brew is installed at the Linuxbrew prefix and the phase did not find it: %v", err)
	}
	// EVERY op, for the reason the fish version of this check records: asserting
	// only that the prefixed path appears SOMEWHERE is what let that fix ship
	// half-done, with one call site resolved and the next still on the bare name.
	for _, op := range fake.Ops {
		for _, field := range strings.Fields(op) {
			if field == "brew" {
				t.Errorf("an op invokes brew by bare name, which does not resolve "+
					"on the machine this phase exists for: %s", op)
			}
		}
	}
	if !strings.Contains(strings.Join(fake.Ops, "\n"), "/home/linuxbrew/.linuxbrew/bin/brew install uv") {
		t.Errorf("the phase did not install uv through the prefixed brew:\n%s",
			strings.Join(fake.Ops, "\n"))
	}
}

// The other direction: the probe widens where it looks, it does not invent a
// path. With brew genuinely absent everywhere, the phase must refuse rather than
// hand a guess to Run.
func TestDevtoolsRefusesWhenNoBrewExistsAnywhere(t *testing.T) {
	fake := &fakeChange{
		info: map[string]change.FileInfo{}, links: map[string]string{},
		lookPathOnly: map[string]bool{},
	}
	out := &bytes.Buffer{}
	if err := phase.Devtools(phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "linux",
		Profile: "workstation", Out: out,
	}); err == nil {
		t.Fatal("no brew anywhere, and the phase proceeded")
	}
	if len(fake.Ops) != 0 {
		t.Errorf("the phase performed operations before refusing:\n%s",
			strings.Join(fake.Ops, "\n"))
	}
}

// Every step is a precondition for the ones after it, so a failure must stop the
// phase rather than be logged and stepped over. The preflight is the
// load-bearing case: continuing past it hands a machine to the install that the
// preflight had just refused.
//
// Which refusal comes back is asserted, not just that one did. Absence of the
// later operations is not enough on its own: the two installer invocations share
// a leading word, so an implementation that swallowed the preflight's error
// would produce an Ops list ending in the install and a refusal from the wrong
// step. The fake names the failing operation -- the command for Run -- so that
// is what tells the two apart.
//
// Each failOn below names exactly one recorded operation, which is why the fake
// matches against the operation as recorded rather than against a bare path.
func TestDevtoolsStopsAtTheFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failOn   string
		wantPath string
		mustNot  []string
	}{
		{"uv", opInstallUv, "/usr/bin/brew", []string{opCheckHooks, opInstallHooks}},
		{"hooks preflight", "install-hooks.sh preflight", "bash",
			[]string{opInstallHooks}},
		{"hooks install", "install-hooks.sh install", "bash", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, ctx, _ := devtoolsCtx(false)
			fake.failOn = tc.failOn
			err := phase.Devtools(ctx)
			var refusal *change.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("want the refusal to propagate, got %T: %v", err, err)
			}
			if refusal.Path != tc.wantPath {
				t.Errorf("the %s step failed but the refusal names %q, not %q; "+
					"its error was swallowed and a later step reported instead",
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
