package migrate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

// ------------------------------------------------------------------- fish
//
// Spec §7 inverted ~/.config/fish: it was a symlink into the checkout, and it
// becomes a real machine-local directory holding a seeded stub. Fisher has been
// writing its generated state into the checkout through that symlink ever since
// the machine was provisioned, and NONE of that state is in git.

// fishState names exactly what fish and fisher generate, and deliberately not
// "everything in the directory". The tracked files -- config.fish, alias.fish,
// mypre, mypost, fishfile -- stay in the checkout, and config.fish in particular
// must NOT arrive at ~/.config/fish/config.fish: Seed never overwrites, so apply
// would then skip the stub, and the file left in its place sources
// (status dirname)/alias.fish, which the fish-source check reports as a failure
// with no obvious cause.
//
// themes/ is here and is not one of §7's five .gitignore lines. Measured on the
// repo owner's checkout: fish/themes/ exists, is empty, and appears in neither
// `git ls-files` nor `git status --ignored` -- git does not track empty
// directories, so it was invisible to the survey that produced that list. It is
// where `fish_config theme save` writes, so on a machine that has saved one it
// holds real state. Absent entries are skipped, so naming it costs nothing where
// it is not there.
//
// Two other untracked things in that directory are deliberately NOT here:
// .DS_Store, and a config.fish.bak.<epoch> some installer left behind. Neither
// is fish configuration, and moving debris into a freshly made ~/.config/fish
// would be the migration inventing state rather than relocating it.
var fishState = []string{
	"fish_variables", "fish_plugins", "functions", "completions", "conf.d", "themes",
}

// fishFacts is the one account of the machine that fishPending and fishRun both
// read.
type fishFacts struct {
	link    string // ~/.config/fish
	source  string // <root>/fish, where the state has been accumulating
	staging string
	pending bool
	present []string // the fishState entries that are actually there
}

func fishInspect(c Context) (fishFacts, error) {
	f := fishFacts{
		link:   filepath.Join(c.Home, ".config", "fish"),
		source: filepath.Join(c.Root, "fish"),
	}
	f.staging = f.link + ".bootstrap-migrating"

	info, err := c.Change.Lstat(f.link)
	if err != nil {
		return f, err
	}
	if !info.IsLink {
		return f, nil
	}
	dest, err := c.Change.Readlink(f.link)
	if err != nil {
		return f, err
	}
	// Exactly this checkout's fish directory, not any symlink. A link pointing
	// somewhere else is a machine this migration knows nothing about, and
	// dismantling it would be acting on a guess about data that is not in git.
	// Apply still refuses that path, naming "move it aside deliberately".
	if resolveLink(f.link, dest) != f.source {
		return f, nil
	}
	f.pending = true

	for _, name := range fishState {
		state, err := c.Change.Lstat(filepath.Join(f.source, name))
		if err != nil {
			return f, err
		}
		if state.Exists {
			f.present = append(f.present, name)
		}
	}
	return f, nil
}

func fishPending(c Context) (bool, error) {
	f, err := fishInspect(c)
	return f.pending, err
}

// fishRun copies before it removes, and that ordering is the whole point.
//
// Everything lands in a staging directory first; only when every copy has
// succeeded is anything released. An interrupt before that leaves the symlink
// and the checkout exactly as they were, which matters because fish_variables
// and the installed plugin set cannot be recovered from anywhere.
//
// The release itself is a remove of the symlink -- which destroys nothing, the
// data is still in the checkout -- immediately followed by a rename, so the
// window in which ~/.config/fish is absent is a single syscall wide.
func fishRun(c Context) error {
	f, err := fishInspect(c)
	if err != nil {
		return err
	}
	if !f.pending {
		c.logf("      already reconciled: ~/.config/fish is not a symlink into this checkout")
		return nil
	}

	staged, err := c.Change.Lstat(f.staging)
	if err != nil {
		return err
	}
	if staged.Exists {
		// Refused rather than cleared. It holds a copy of data that is not in
		// git, made by a run that did not finish, and this design does not
		// delete such a thing on a guess.
		return &change.Refusal{
			Path:    f.staging,
			Problem: "an interrupted migration left this staging copy behind",
			Remediation: "compare it with " + f.source +
				", remove it once you are satisfied nothing is only there, then retry",
		}
	}
	if err := c.Change.Dir(f.staging); err != nil {
		return err
	}
	// Every copy completes before anything at all is removed.
	for _, name := range f.present {
		if err := c.Change.Copy(filepath.Join(f.source, name),
			filepath.Join(f.staging, name)); err != nil {
			return err
		}
	}

	// From here on things are destroyed. The symlink first, which costs nothing
	// -- the state it points at is still in the checkout, and the staging copy
	// holds it too.
	if err := c.Change.RemoveAll(f.link); err != nil {
		return err
	}
	if err := c.Change.Rename(f.staging, f.link); err != nil {
		return err
	}
	for _, name := range f.present {
		if err := c.Change.RemoveAll(filepath.Join(f.source, name)); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------- gitconfig
//
// §8 renamed git/gitconfig.symlink to git/gitconfig.shared. Every machine
// provisioned before that has a ~/.gitconfig including the old path, and git
// passes over an include it cannot open WITHOUT A WORD -- so every shared git
// setting silently disappears. It is a seed row, so apply will never rewrite it.

const (
	oldSharedGitconfig = "git/gitconfig.symlink"
	newSharedGitconfig = "git/gitconfig.shared"
)

// oldIncludeLine matches an actual include of the pre-rename path and captures
// the path itself, so the rewrite can replace the value and nothing else.
//
// It must be a `path` setting: a comment begins with # or ;, so it cannot reach
// this pattern. That is not fussiness -- the seeded template names the shared
// config in its header COMMENT as well as in its [include] block, so a
// substring test would report this migration pending forever, rewriting nothing
// on every run.
//
// The path must END at the old name: "gitconfig.symlink" and
// "gitconfig.symlink.disabled" are different files. What comes before git/ is
// unconstrained, because the local file names this machine's checkout.
var oldIncludeLine = regexp.MustCompile(
	`^\s*path\s*=\s*["']?([^"'#;]*` + regexp.QuoteMeta(oldSharedGitconfig) + `)["']?(?:\s|[#;]|$)`)

type gitconfigFacts struct {
	path    string // ~/.gitconfig
	want    string // the resolved path the include should name
	lines   []string
	matches []int // indexes into lines
	pending bool
}

func gitconfigInspect(c Context) (gitconfigFacts, error) {
	g := gitconfigFacts{
		path: filepath.Join(c.Home, ".gitconfig"),
		want: filepath.Join(c.Root, newSharedGitconfig),
	}
	info, err := c.Change.Lstat(g.path)
	if err != nil {
		return g, err
	}
	// A regular file, so a symlinked ~/.gitconfig -- the layout 37f00a0 left
	// behind -- is never rewritten. ReadFile and WriteFile both follow symlinks,
	// so rewriting one would edit tracked, published content: precisely the
	// fault §1's rule was written about.
	if !info.IsRegular {
		return g, nil
	}
	data, err := c.Change.ReadFile(g.path)
	if err != nil {
		return g, err
	}
	// Split and rejoined on "\n" with only the matched span replaced, which is
	// the identity transform on every byte this migration is not repointing.
	// The file holds whatever `git config --global` has written; losing any of
	// it is a real loss.
	g.lines = strings.Split(string(data), "\n")
	for i, line := range g.lines {
		if oldIncludeLine.MatchString(line) {
			g.matches = append(g.matches, i)
		}
	}
	g.pending = len(g.matches) > 0
	return g, nil
}

func gitconfigPending(c Context) (bool, error) {
	g, err := gitconfigInspect(c)
	return g.pending, err
}

func gitconfigRun(c Context) error {
	g, err := gitconfigInspect(c)
	if err != nil {
		return err
	}
	if !g.pending {
		c.logf("      already reconciled: ~/.gitconfig does not include " + oldSharedGitconfig)
		return nil
	}
	lines := append([]string(nil), g.lines...)
	for _, i := range g.matches {
		loc := oldIncludeLine.FindStringSubmatchIndex(lines[i])
		if loc == nil {
			// Unreachable: the indexes came from this same pattern. Stated as an
			// error anyway, because the alternative is skipping a line in
			// silence and reporting the migration done.
			return fmt.Errorf("%s:%d matched the include pattern and then did not", g.path, i+1)
		}
		lines[i] = lines[i][:loc[2]] + g.want + lines[i][loc[3]:]
	}
	return c.Change.WriteFile(g.path, []byte(strings.Join(lines, "\n")))
}

// -------------------------------------------------------------- gitignore
//
// The other half of §8's rename. ~/.gitignore is a genuine symlink, so apply
// refuses it on every existing machine once git/gitignore_global.symlink is
// gone. The refusal is correct; the remedy should not be manual when the other
// two renames get one.

const (
	oldGlobalIgnore = "git/gitignore_global.symlink"
	newGlobalIgnore = "git/gitignore_global"
)

type gitignoreFacts struct {
	link    string // ~/.gitignore
	old     string
	want    string
	pending bool
	// wantExists is not part of pending. A checkout missing the renamed file is
	// a broken checkout, not a machine that no longer needs migrating, and Run
	// says so rather than Pending quietly answering no.
	wantExists bool
}

func gitignoreInspect(c Context) (gitignoreFacts, error) {
	g := gitignoreFacts{
		link: filepath.Join(c.Home, ".gitignore"),
		old:  filepath.Join(c.Root, oldGlobalIgnore),
		want: filepath.Join(c.Root, newGlobalIgnore),
	}
	source, err := c.Change.Lstat(g.want)
	if err != nil {
		return g, err
	}
	g.wantExists = source.Exists

	info, err := c.Change.Lstat(g.link)
	if err != nil {
		return g, err
	}
	if !info.IsLink {
		return g, nil
	}
	dest, err := c.Change.Readlink(g.link)
	if err != nil {
		return g, err
	}
	g.pending = resolveLink(g.link, dest) == g.old
	return g, nil
}

func gitignorePending(c Context) (bool, error) {
	g, err := gitignoreInspect(c)
	return g.pending, err
}

func gitignoreRun(c Context) error {
	g, err := gitignoreInspect(c)
	if err != nil {
		return err
	}
	if !g.pending {
		c.logf("      already reconciled: ~/.gitignore does not point at " + oldGlobalIgnore)
		return nil
	}
	// Checked BEFORE the old link is removed. Link would refuse a missing source
	// too, but by then ~/.gitignore would already be gone, and a machine with no
	// global ignore file at all is worse than one pointing at the wrong name.
	if !g.wantExists {
		return &change.Refusal{
			Path:        g.want,
			Problem:     "the renamed global ignore file is not in this checkout",
			Remediation: "restore it, or update this checkout, then retry",
		}
	}
	if err := c.Change.RemoveAll(g.link); err != nil {
		return err
	}
	return c.Change.Link(g.want, g.link)
}
