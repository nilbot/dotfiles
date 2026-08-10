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

// loadRows reads and parses the manifest once for both manifest checks, so one
// fault produces one account of itself rather than two descriptions of it.
//
// The rows are platform-filtered here, exactly as phase.Config filters them:
// asking whether a darwin-only row is satisfied on linux is asking the wrong
// question.
//
// The error is returned rather than flattened to a string because All hands it
// back to its caller: a *manifest.SyntaxError is malformed INPUT, which the
// check verb must answer with 3 like every other verb.
func loadRows(c Context) ([]manifest.Row, error) {
	path := filepath.Join(c.Root, "bootstrap.d", "links.manifest")
	data, err := c.Change.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the manifest: %w", err)
	}
	parsed, err := manifest.Parse(data)
	if err != nil {
		return nil, err
	}
	return manifest.For(parsed, c.Platform), nil
}

func manifestOwners(applicable []manifest.Row, err error) Result {
	if err != nil {
		return Result{Fail, "manifest-owners", err.Error()}
	}
	if dupes := manifest.DuplicateTargets(applicable); len(dupes) > 0 {
		return Result{Fail, "manifest-owners",
			"claimed by more than one row: " + strings.Join(dupes, ", ")}
	}
	return Result{OK, "manifest-owners",
		fmt.Sprintf("%d rows, one owner each", len(applicable))}
}

func manifestKinds(c Context, applicable []manifest.Row, err error) Result {
	if err != nil {
		return Result{Fail, "manifest-kinds", err.Error()}
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

// fishSourceLine matches the one line the seeded stub exists to carry, and
// captures the path it names.
//
// Anchored on both ends: a commented-out line does not start with `source`, a
// mention inside a comment does not either, and `config.fish.bak` does not end
// where the pattern ends. What comes before /fish/ is unconstrained, because the
// stub names the clone location -- but naming it is not the same as it being
// there, which is what resolves exists to settle.
var fishSourceLine = regexp.MustCompile(`^source\s+["']?(.*/fish/config\.fish)["']?$`)

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
	named, dangling := resolves(c, data, fishSourceLine)
	switch {
	case named != "":
		return Result{OK, "fish-source", "the stub sources " + named}
	case dangling != "":
		return Result{Fail, "fish-source", "the stub sources " + dangling +
			", which does not exist, so every shared fish setting is silently " +
			"inactive; point it at this checkout"}
	}
	return Result{Fail, "fish-source", path + " does not source the tracked config, " +
		"so every shared fish setting is silently inactive; restore its " +
		"'source .../fish/config.fish' line"}
}

// resolves scans data for the first line matching pattern whose captured path
// exists, and reports it. When one or more lines matched but none of their paths
// exist, the first such path is returned as dangling instead.
//
// Both silent-failure guards need this and neither is complete without it.
// Matching the text only asks whether the file SAYS the right thing; the failure
// they exist to catch is the file saying it about somewhere that is not there.
// Seed copies these templates verbatim -- no substitution -- so a template
// hardcoding ~/dotfiles is simply wrong on a checkout anywhere else, and both
// git and fish pass over an unreadable include or source without stopping.
func resolves(c Context, data []byte, pattern *regexp.Regexp) (found, dangling string) {
	for _, line := range strings.Split(string(data), "\n") {
		m := pattern.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		path := expandHome(m[1], c.Home)
		info, err := c.Change.Lstat(path)
		if err == nil && info.Exists {
			return path, ""
		}
		if dangling == "" {
			dangling = path
		}
	}
	return "", dangling
}

// expandHome resolves the home-directory spellings these two files use: $HOME
// and ${HOME}, which fish expands, and a leading ~, which git expands. Nothing
// else is substituted, so a line naming a variable this does not know reports as
// unresolvable -- which is the honest answer, since bootstrap cannot confirm it.
//
// A relative path is joined to Home: git resolves a relative include against the
// including file's directory, which is $HOME for ~/.gitconfig.
func expandHome(path, home string) string {
	path = strings.NewReplacer("${HOME}", home, "$HOME", home).Replace(path)
	if path == "~" {
		return home
	}
	if rest, cut := strings.CutPrefix(path, "~/"); cut {
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	return path
}

// sharedGitConfig is the path a healthy ~/.gitconfig includes. It is not a
// manifest row -- the manifest seeds the local file, not the shared one -- so
// this is the only place it is named.
const sharedGitConfig = "git/gitconfig.shared"

// gitconfigIncludeLine matches an actual include, not a mention, and captures
// the path it names.
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
// What comes BEFORE git/ is unconstrained, because the local file names the
// clone location -- but the path is then resolved, because naming a file git
// cannot open is exactly the failure this guard exists for.
var gitconfigIncludeLine = regexp.MustCompile(
	`^path\s*=\s*["']?([^"'#;]*` + regexp.QuoteMeta(sharedGitConfig) + `)["']?(\s|[#;]|$)`)

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
	named, dangling := resolves(c, data, gitconfigIncludeLine)
	switch {
	case named != "":
		return Result{OK, "gitconfig-include", "includes " + named}
	case dangling != "":
		// git passes over an include it cannot open without a word, so this
		// state looks exactly like a healthy one until a shared setting is
		// missed.
		return Result{Fail, "gitconfig-include", "~/.gitconfig includes " + dangling +
			", which does not exist, so every shared git setting is silently " +
			"inactive; point it at this checkout"}
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
