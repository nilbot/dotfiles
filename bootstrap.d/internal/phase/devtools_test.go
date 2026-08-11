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
	opInstallUv    = "run brew install uv"
	opCheckHooks   = "run bash /repo/git/install-hooks.sh preflight /repo /home /home/bin/agents"
	opMakeBinDir   = "dir /home/bin"
	opBuildAgents  = "run go build -C /repo/agents -trimpath -o /home/bin/agents ."
	opInstallHooks = "run bash /repo/git/install-hooks.sh install /repo /home /home/bin/agents"
)

func devtoolsCtx(uvOnPath bool) (*fakeChange, phase.Context, *bytes.Buffer) {
	fake := &fakeChange{
		info:        map[string]change.FileInfo{},
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
	want := []string{opInstallUv, opCheckHooks, opMakeBinDir, opBuildAgents, opInstallHooks}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

// The ordering, asserted by position rather than by the whole list, so it keeps
// reporting the same finding if a fourth step is ever added above or between.
//
// This is not stylistic. install-hooks.sh symlinks four hook names AT the agents
// binary and refuses unless it is an executable regular file, so a phase that
// installed hooks first would either refuse on a machine that has never built
// agents, or -- worse, on a machine that has -- point the chain at a stale
// binary and report success.
func TestDevtoolsBuildsTheBinaryBeforePointingHooksAtIt(t *testing.T) {
	fake, ctx, _ := devtoolsCtx(false)
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("Devtools: %v", err)
	}
	build := slices.Index(fake.Ops, opBuildAgents)
	hooks := slices.Index(fake.Ops, opInstallHooks)
	if build < 0 || hooks < 0 {
		t.Fatalf("both steps must happen; ops: %v", fake.Ops)
	}
	if build > hooks {
		t.Errorf("hooks were installed at %d, before the build at %d; the installer "+
			"points four hook names at a binary that does not exist yet", hooks, build)
	}
	if dir := slices.Index(fake.Ops, opMakeBinDir); dir < 0 || dir > build {
		t.Errorf("%s must be created before the build writes into it; ops: %v",
			opMakeBinDir, fake.Ops)
	}

	// The hooks preflight is a check that costs nothing and refuses without
	// touching anything, so its whole value is in WHERE it sits. Running it
	// after the build would still catch a bad ~/.gitconfig, a foreign
	// core.hooksPath or a hooks link pointing somewhere else -- but only after
	// compiling a Go module, which is the one part of this phase that takes
	// real time. Position, not presence, is what is asserted.
	check := slices.Index(fake.Ops, opCheckHooks)
	if check < 0 {
		t.Fatalf("the hooks preflight must run; ops: %v", fake.Ops)
	}
	if check > build {
		t.Errorf("the hooks preflight ran at %d, after the build at %d; a machine "+
			"whose global git config is unusable should learn that in a second, "+
			"not after compiling the agents module", check, build)
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

// The other branch of the uv decision. Without it, an implementation that
// ignored LookPath entirely would pass every case above.
func TestDevtoolsSkipsUvWhenItIsAlreadyOnPath(t *testing.T) {
	fake, ctx, out := devtoolsCtx(true)
	if err := phase.Devtools(ctx); err != nil {
		t.Fatalf("Devtools: %v", err)
	}
	want := []string{opCheckHooks, opMakeBinDir, opBuildAgents, opInstallHooks}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
	// Skipped, not silent: a phase that says nothing about a step it did not
	// take is indistinguishable from one that forgot it.
	if !strings.Contains(out.String(), "uv") {
		t.Errorf("the skip must be visible in the phase's output:\n%s", out.String())
	}
}

// Every step is a precondition for the ones after it, so a failure must stop the
// phase rather than be logged and stepped over. The build is the load-bearing
// case: continuing past it hands install-hooks.sh a binary that was never
// written.
//
// Which refusal comes back is asserted, not just that one did. Absence of the
// later operations is not enough on its own: ~/bin is a PREFIX of the binary
// path every later step names, so a swallowed Dir error would produce an
// identical Ops list and a refusal from the next step instead. The fake names
// the failing operation -- the path for Dir, the command for Run -- so that is
// what tells the two apart.
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
		{"uv", "run brew install uv", "brew",
			[]string{opCheckHooks, opMakeBinDir, opBuildAgents, opInstallHooks}},
		// The reason the preflight moved ahead of the build: a machine that
		// cannot take the hooks must not pay for a compile to find out.
		{"hooks preflight", "install-hooks.sh preflight", "bash",
			[]string{opMakeBinDir, opBuildAgents, opInstallHooks}},
		{"bin directory", "dir /home/bin", "/home/bin",
			[]string{opBuildAgents, opInstallHooks}},
		{"build", "run go build", "go", []string{opInstallHooks}},
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
