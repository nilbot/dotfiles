package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
)

// platform reports what was detected. main refuses an unsupported OS before a
// Context is ever built, so this arm is reachable only by a direct caller --
// but a check that cannot fail is not a check.
func platform(c Context) Result {
	switch c.Platform {
	case "darwin", "linux":
		return Result{OK, "platform", c.Platform}
	}
	return Result{Fail, "platform",
		fmt.Sprintf("unsupported operating system %q", c.Platform)}
}

// rows loads the applicable manifest rows, or returns the one sentence that
// explains why it could not. Both manifest checks call it, so a manifest that
// cannot be read or parsed produces the same account in both places rather than
// two descriptions of one fault.
//
// The rows are platform-filtered first, exactly as phase.Config filters them:
// asking whether a darwin-only row is satisfied on linux is asking the wrong
// question.
func rows(c Context) ([]manifest.Row, string) {
	path := filepath.Join(c.Root, "bootstrap.d", "links.manifest")
	data, err := c.Change.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("cannot read the manifest: %v", err)
	}
	parsed, err := manifest.Parse(data)
	if err != nil {
		return nil, err.Error()
	}
	return manifest.For(parsed, c.Platform), ""
}

func manifestOwners(c Context) Result {
	applicable, problem := rows(c)
	if problem != "" {
		return Result{Fail, "manifest-owners", problem}
	}
	if dupes := manifest.DuplicateTargets(applicable); len(dupes) > 0 {
		return Result{Fail, "manifest-owners",
			"claimed by more than one row: " + strings.Join(dupes, ", ")}
	}
	return Result{OK, "manifest-owners",
		fmt.Sprintf("%d rows, one owner each", len(applicable))}
}

func manifestKinds(c Context) Result {
	applicable, problem := rows(c)
	if problem != "" {
		return Result{Fail, "manifest-kinds", problem}
	}
	var findings []string
	for _, row := range applicable {
		if p := rowProblem(c, row); p != "" {
			findings = append(findings, "~/"+row.Target+" "+p)
		}
	}
	if len(findings) > 0 {
		head := fmt.Sprintf("%d of %d rows are not as declared:",
			len(findings), len(applicable))
		return Result{Fail, "manifest-kinds", head + "\n" + strings.Join(findings, "\n")}
	}
	return Result{OK, "manifest-kinds",
		fmt.Sprintf("%d rows, each present and of its declared kind", len(applicable))}
}

// rowProblem is the single place that decides what a row's Kind requires of its
// target, and it switches on manifest's own constants rather than on strings of
// its own. A kind added to manifest and not handled here fails loudly in the
// default arm instead of being skipped in silence -- the same reasoning, and the
// same default arm, as phase.Config.
func rowProblem(c Context, row manifest.Row) string {
	target := filepath.Join(c.Home, row.Target)
	info, err := c.Change.Lstat(target)
	if err != nil {
		return err.Error()
	}
	switch row.Kind {
	case manifest.KindLink:
		if !info.IsLink {
			return describe(info) + ", want a symlink"
		}
		dest, err := c.Change.Readlink(target)
		if err != nil {
			return err.Error()
		}
		if want := filepath.Join(c.Root, row.Source); dest != want {
			return fmt.Sprintf("points at %s, want %s", dest, want)
		}
	case manifest.KindSeed:
		// IsRegular, so a symlink fails here: a seeded path is machine-local by
		// definition, and a symlink means another program's writes are landing
		// in the repository.
		if !info.IsRegular {
			return describe(info) + ", want a regular file"
		}
	case manifest.KindDir:
		if !info.IsDir {
			return describe(info) + ", want a real directory"
		}
	default:
		return fmt.Sprintf("has unknown kind %q", row.Kind)
	}
	return ""
}

func describe(info change.FileInfo) string {
	switch {
	case !info.Exists:
		return "is missing"
	case info.IsDir:
		return "is a real directory"
	case info.IsLink:
		return "is a symlink"
	case info.IsRegular:
		return "is a regular file"
	}
	return "is neither a file, a directory nor a symlink"
}

// fishSourceLine matches the one line the seeded stub exists to carry.
//
// Anchored on both ends: a commented-out line does not start with `source`, a
// mention inside a comment does not either, and `config.fish.bak` does not end
// where the pattern ends. What comes before /fish/ is unconstrained -- the stub
// necessarily names the clone location, which is the one fact allowed to vary
// per machine.
var fishSourceLine = regexp.MustCompile(`^source .*/fish/config\.fish$`)

// fishSource is the first of the two silent-total-failure guards. Strip the
// source line from the stub and every shared fish setting stops applying, with
// no error from fish, from bootstrap, or from anything else.
func fishSource(c Context) Result {
	path := filepath.Join(c.Home, ".config", "fish", "config.fish")
	data, err := c.Change.ReadFile(path)
	if err != nil {
		return Result{Fail, "fish-source", fmt.Sprintf(
			"cannot read the fish stub, so every shared fish setting is inactive: %v", err)}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if fishSourceLine.MatchString(strings.TrimSpace(line)) {
			return Result{OK, "fish-source", "the stub sources the tracked config"}
		}
	}
	return Result{Fail, "fish-source", path + " does not source the tracked config, " +
		"so every shared fish setting is silently inactive; restore its " +
		"'source .../fish/config.fish' line"}
}

// sharedGitConfig is the path a healthy ~/.gitconfig includes. It is not a
// manifest row -- the manifest seeds the local file, not the shared one -- so
// this is the only place it is named.
const sharedGitConfig = "git/gitconfig.shared"

// gitconfigIncludeLine matches an actual include, not a mention.
//
// A plain substring test looked sufficient and is not, measured: §8 renames the
// path in gitconfig.local.template's own header COMMENT as well as in its
// [include] block, so the seeded file names the shared config twice. Delete the
// include and the surviving comment satisfies a Contains -- the guard defeated
// by the very file it guards.
//
// So the line must be a `path` setting (comments begin with # or ;, and cannot
// reach this), and the path must END at the shared config: "gitconfig.shared"
// and "gitconfig.shared.disabled" are different files. What follows the value
// may be a quote, whitespace, a trailing comment, or nothing.
//
// What comes BEFORE git/ is deliberately unconstrained. The local file names
// the clone location, which is the one fact allowed to vary per machine.
var gitconfigIncludeLine = regexp.MustCompile(
	`^path\s*=.*` + regexp.QuoteMeta(sharedGitConfig) + `(["'\s;#]|$)`)

// gitconfigInclude is the second silent-total-failure guard, and the one §8
// creates deliberately: the rename leaves every existing machine's ~/.gitconfig
// including a path that no longer exists. git reports nothing for a missing
// include, so every shared setting simply vanishes.
func gitconfigInclude(c Context) Result {
	path := filepath.Join(c.Home, ".gitconfig")
	data, err := c.Change.ReadFile(path)
	if err != nil {
		return Result{Fail, "gitconfig-include", fmt.Sprintf(
			"cannot read ~/.gitconfig, so every shared git setting is inactive: %v", err)}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if gitconfigIncludeLine.MatchString(strings.TrimSpace(line)) {
			return Result{OK, "gitconfig-include", "includes " + sharedGitConfig}
		}
	}
	return Result{Fail, "gitconfig-include", path + " does not include " + sharedGitConfig +
		", so every shared git setting is silently inactive; run './bootstrap migrate'"}
}

// loginShell answers spec §10's check 6: $SHELL is fish, and that same path is
// registered in /etc/shells.
//
// $SHELL describes the session, not the account, so immediately after the fish
// phase runs chsh this still reports the old shell until the next login. The
// detail says so rather than the status softening, because the two cases -- chsh
// never ran, and chsh ran a minute ago -- are indistinguishable from here and
// only one of them is fine.
func loginShell(c Context) Result {
	if c.Shell == "" {
		return Result{Fail, "login-shell",
			"SHELL is unset, so the login shell cannot be determined"}
	}
	// Base, not a suffix test: "/usr/local/bin/notfish" ends in the letters
	// without being fish.
	if filepath.Base(c.Shell) != "fish" {
		return Result{Fail, "login-shell", fmt.Sprintf(
			"the login shell is %s, not fish; run './bootstrap apply workstation', "+
				"then log in again", c.Shell)}
	}
	const shells = "/etc/shells"
	data, err := c.Change.ReadFile(shells)
	if err != nil {
		return Result{Fail, "login-shell",
			fmt.Sprintf("cannot read the registered shell list: %v", err)}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == c.Shell {
			return Result{OK, "login-shell", c.Shell + ", listed in " + shells}
		}
	}
	return Result{Fail, "login-shell",
		c.Shell + " is not listed in " + shells + "; run './bootstrap apply workstation'"}
}

func agentsOnPath(c Context) Result {
	path, err := c.Change.LookPath("agents")
	if err != nil {
		return Result{Fail, "agents",
			"not on PATH; the devtools phase builds it -- run './bootstrap apply workstation'"}
	}
	return Result{OK, "agents", path}
}

// packages asks Homebrew whether the Brewfile is satisfied. `brew bundle check`
// reads and reports; it installs nothing, which is why a check may run it.
//
// The absent-Brewfile arm comes first and deliberately does not reach brew:
// Task 12 creates the file, so until then it genuinely does not exist, and
// handing a missing path to `brew bundle check` produces an error about the path
// where the honest answer is that the phase which creates it has not run.
func packages(c Context) Result {
	brewfile := filepath.Join(c.Root, "bootstrap.d", "Brewfile")
	info, err := c.Change.Lstat(brewfile)
	if err != nil {
		return Result{Fail, "packages", err.Error()}
	}
	if !info.Exists {
		return Result{Fail, "packages",
			"the packages phase has not run: " + brewfile + " does not exist"}
	}
	if _, err := c.Change.LookPath("brew"); err != nil {
		return Result{Fail, "packages",
			"Homebrew is not installed; run './bootstrap apply workstation'"}
	}
	if err := c.Change.Run("brew", "bundle", "check", "--file", brewfile); err != nil {
		return Result{Fail, "packages",
			"some Brewfile entries are not installed; run './bootstrap apply workstation'"}
	}
	return Result{OK, "packages", "every Brewfile entry is installed"}
}
