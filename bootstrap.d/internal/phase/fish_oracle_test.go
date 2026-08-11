package phase_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle: install_fisher is RUN, in a sandbox, and judged by what it did.
//
// Every other assertion about this function reads its text -- which glob it
// names, whether a token appears before another one. That is all a Go test can
// normally do with fish, and it is not enough. Two defects reached review with
// the text suite fully green:
//
//   - inverting `not contains -- --force $argv` to `contains -- --force $argv`.
//     The text rules pin --force's POSITION and never its POLARITY. Inverted,
//     `install_fisher` (how the phase calls it) stops guarding and hands fisher
//     the colliding state that deleted its own plugin record in the field; and
//     `fish_reset_all`, which passes --force, starts guarding AFTER its own
//     rm -rf, so one leftover themes/ file makes it skip -- three directories
//     destroyed and nothing rebuilt.
//   - narrowing fish_reset_all's rm to a single directory. The text rules assert
//     an rm -rf exists, does not say themes, and precedes the rebuild -- never
//     WHAT it removes.
//
// Both are one token. Both are caught here in under a second, because behaviour
// is not a proxy for behaviour.
//
// "No test may execute fish against the real machine" is honoured: env is
// replaced wholesale, HOME and XDG_CONFIG_HOME are a t.TempDir, and curl and
// fisher are stubs on a PATH that does not reach the developer's own. Nothing
// here can touch ~/.config/fish. The real `fish` binary is the point -- a fish
// stub would be one more thing standing in for the language, which is the
// defect this file exists to close.
func fishOracle(t *testing.T) (run func(cmd string) (string, int), root string) {
	t.Helper()
	fish, err := exec.LookPath("fish")
	if err != nil {
		// Loud, not silent. A machine without fish cannot answer these
		// questions, and pretending otherwise is how a suite reports coverage
		// it does not have.
		t.Skip("fish is not on PATH; the behavioural guard cannot run here")
	}
	sandbox := t.TempDir()
	bin := filepath.Join(sandbox, "bin")
	cfg := filepath.Join(sandbox, "cfg")
	mustMkdir(t, bin)
	mustMkdir(t, filepath.Join(cfg, "fish"))

	// Both stubs announce themselves and do nothing. `curl | source` in the
	// real function defines fisher; here it defines a fisher that reports its
	// argv, so "was fisher invoked, and with what" is observable.
	mustWrite(t, filepath.Join(bin, "curl"), "#!/bin/sh\necho 'function fisher; echo \"FISHER: $argv\"; end'\n")
	mustWrite(t, filepath.Join(bin, "fisher"), "#!/bin/sh\necho \"FISHER: $@\"\n")

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	// mypre.fish resolves fishfile relative to its own directory, so both must
	// be copied together for the function to read the real plugin list.
	for _, name := range []string{"mypre.fish", "fishfile"} {
		data, err := os.ReadFile(filepath.Join(repo, "fish", name))
		if err != nil {
			t.Fatalf("the tracked fish/%s is what this exercises: %v", name, err)
		}
		mustWrite(t, filepath.Join(sandbox, name), string(data))
	}

	return func(cmd string) (string, int) {
		t.Helper()
		c := exec.Command(fish, "--no-config", "-c",
			"source "+filepath.Join(sandbox, "mypre.fish")+"; "+cmd)
		// Replaced, not extended: an inherited PATH would reach the real curl
		// and the developer's own fish configuration.
		c.Env = []string{
			"HOME=" + sandbox,
			"XDG_CONFIG_HOME=" + cfg,
			"PATH=" + bin + ":/usr/bin:/bin",
		}
		out, err := c.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running install_fisher: %v", err)
		}
		return string(out), code
	}, cfg
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// plant creates a file the guard should notice, under $XDG_CONFIG_HOME/fish.
func plant(t *testing.T, cfg, rel string) {
	t.Helper()
	full := filepath.Join(cfg, "fish", rel)
	mustMkdir(t, filepath.Dir(full))
	mustWrite(t, full, "# planted\n")
}

// The install path and the skip path, decided by what is on disk.
//
// The themes case is not incidental: fisher's own conflict test globs four
// directories, so a theme file collides just as a function does. A guard
// watching three would wave that machine through to the failure it exists to
// prevent.
func TestInstallFisherInstallsOnlyOnAMachineWithNoPluginFiles(t *testing.T) {
	for _, tc := range []struct {
		name       string
		plantFiles []string
		wantFisher bool
	}{
		{"fresh machine", nil, true},
		{"a function present", []string{"functions/x.fish"}, false},
		{"a completion present", []string{"completions/x.fish"}, false},
		{"a conf.d file present", []string{"conf.d/x.fish"}, false},
		{"a theme present", []string{"themes/mine.theme"}, false},
		// Not *.fish, because fisher's conflict test does not restrict by suffix.
		{"a non-fish file present", []string{"conf.d/README"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, cfg := fishOracle(t)
			for _, f := range tc.plantFiles {
				plant(t, cfg, f)
			}
			out, code := run("install_fisher")
			if code != 0 {
				t.Errorf("exit %d; every phase must be safe to re-run, so a machine "+
					"this phase has nothing to do to must stay green:\n%s", code, out)
			}
			called := strings.Contains(out, "FISHER:")
			if called != tc.wantFisher {
				t.Errorf("fisher called = %v, want %v -- with %v on disk:\n%s",
					called, tc.wantFisher, tc.plantFiles, out)
			}
			if tc.wantFisher && !strings.Contains(out, "jorgebucaran/fisher") {
				t.Errorf("the install must carry the tracked plugin list:\n%s", out)
			}
		})
	}
}

// --force, by polarity rather than by position.
//
// This is the assertion the text suite could not make. Inverting the condition
// swaps these two answers, and nothing that reads the source noticed.
func TestInstallFisherForceInstallsDespiteExistingPluginFiles(t *testing.T) {
	run, cfg := fishOracle(t)
	plant(t, cfg, "functions/x.fish")

	out, code := run("install_fisher")
	if strings.Contains(out, "FISHER:") {
		t.Fatalf("without --force a populated machine must be left alone:\n%s", out)
	}

	out, code = run("install_fisher --force")
	if code != 0 {
		t.Errorf("--force exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "FISHER:") {
		t.Errorf("--force must install despite the files that make the guard skip; "+
			"without this, fish_reset_all cannot rebuild after its own rm -rf:\n%s", out)
	}
}

// The documented recovery path, end to end.
//
// fish_reset_all is what the skip message tells the operator to run, and it is
// the only remedy for a machine whose plugin files exist with no record. It
// must clear the three directories fisher owns, LEAVE the user's themes alone,
// and rebuild -- and it rebuilds only because it forces past the guard.
//
// Both halves are load-bearing and neither was pinned before: narrowing the rm
// leaves conf.d and completions behind for fisher to collide with, and clearing
// themes destroys a hand-written theme with no way back.
func TestFishResetAllClearsFisherDirectoriesRebuildsAndSparesThemes(t *testing.T) {
	run, cfg := fishOracle(t)
	for _, f := range []string{"functions/old.fish", "completions/old.fish", "conf.d/old.fish"} {
		plant(t, cfg, f)
	}
	plant(t, cfg, "themes/mine.theme")

	out, _ := run("fish_reset_all")

	for _, gone := range []string{"functions/old.fish", "completions/old.fish", "conf.d/old.fish"} {
		if _, err := os.Stat(filepath.Join(cfg, "fish", gone)); err == nil {
			t.Errorf("%s survived the reset; anything fisher owns that is left behind "+
				"is a file it will refuse to install over, which is the state this "+
				"whole guard exists to escape:\n%s", gone, out)
		}
	}
	theme := filepath.Join(cfg, "fish", "themes", "mine.theme")
	if _, err := os.Stat(theme); err != nil {
		t.Errorf("the reset deleted the user's theme: themes/ is fish's own " +
			"user-theme directory, and a hand-written theme there has no way back")
	}
	if !strings.Contains(out, "FISHER:") {
		t.Errorf("the reset must rebuild after clearing; a reset that removes the "+
			"plugins and does not reinstall them leaves the machine worse than the "+
			"collision it was run to fix:\n%s", out)
	}
}
