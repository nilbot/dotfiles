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
	staged  bool // an earlier run left a staging copy behind
	pending bool
	present []string // the fishState entries that are actually there
}

func fishInspect(q Query) (fishFacts, error) {
	f := fishFacts{
		link:   filepath.Join(q.Home, ".config", "fish"),
		source: filepath.Join(q.Root, "fish"),
	}
	f.staging = f.link + ".bootstrap-migrating"

	// Asked FIRST, and independently of the symlink, because of the one window
	// the staging protocol does not by itself survive: between RemoveAll(link)
	// and Rename there is no ~/.config/fish at all. Keyed on the symlink alone
	// this reports not-pending there, so `migrate` says "nothing to migrate",
	// preflight passes, apply makes a fresh directory and seeds the stub, and
	// BOTH copies of fisher's state -- the checkout's and the staging one -- are
	// orphaned with nothing reporting it. check calls the machine healthy.
	//
	// fishRun's refusal was written for exactly that state and could never be
	// reached, because Pending never let the question be asked. Making the state
	// VISIBLE is the whole job here; see fishRun for why it is not repaired.
	staged, err := q.Read.Lstat(f.staging)
	if err != nil {
		return f, err
	}
	f.staged = staged.Exists
	f.pending = f.staged

	info, err := q.Read.Lstat(f.link)
	if err != nil {
		return f, err
	}
	if !info.IsLink {
		return f, nil
	}
	dest, err := q.Read.Readlink(f.link)
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
		state, err := q.Read.Lstat(filepath.Join(f.source, name))
		if err != nil {
			return f, err
		}
		if state.Exists {
			f.present = append(f.present, name)
		}
	}
	return f, nil
}

func fishPending(q Query) (bool, error) {
	f, err := fishInspect(q)
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
	f, err := fishInspect(c.Query())
	if err != nil {
		return err
	}
	if !f.pending {
		c.logf("      already reconciled: ~/.config/fish is not a symlink into this checkout")
		return nil
	}

	// Read from the same facts Pending answered from, not re-asked here: one
	// account of the machine, which is what stops the two forming separate
	// opinions about it.
	//
	// Refused, and deliberately NOT repaired. Renaming staging into place would
	// finish the job when the run died in the release window -- and would
	// silently install a HALF-COPIED directory when it died mid-copy instead.
	// Those two states are indistinguishable from here, and guessing wrong
	// destroys data that is not in git. Both locations are named so a human can
	// tell them apart, which is something this code cannot do.
	if f.staged {
		return &change.Refusal{
			Path: f.staging,
			Problem: "an interrupted migration left this staging copy behind, so " +
				"fisher's state may be in two places and only you can tell whether " +
				"the copy finished",
			Remediation: "compare it with " + f.source +
				"; keep whichever is complete, remove " + f.staging + ", then retry",
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

func gitconfigInspect(q Query) (gitconfigFacts, error) {
	g := gitconfigFacts{
		path: filepath.Join(q.Home, ".gitconfig"),
		want: filepath.Join(q.Root, newSharedGitconfig),
	}
	info, err := q.Read.Lstat(g.path)
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
	data, err := q.Read.ReadFile(g.path)
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

func gitconfigPending(q Query) (bool, error) {
	g, err := gitconfigInspect(q)
	return g.pending, err
}

func gitconfigRun(c Context) error {
	g, err := gitconfigInspect(c.Query())
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

func gitignoreInspect(q Query) (gitignoreFacts, error) {
	g := gitignoreFacts{
		link: filepath.Join(q.Home, ".gitignore"),
		old:  filepath.Join(q.Root, oldGlobalIgnore),
		want: filepath.Join(q.Root, newGlobalIgnore),
	}
	source, err := q.Read.Lstat(g.want)
	if err != nil {
		return g, err
	}
	g.wantExists = source.Exists

	info, err := q.Read.Lstat(g.link)
	if err != nil {
		return g, err
	}
	if !info.IsLink {
		return g, nil
	}
	dest, err := q.Read.Readlink(g.link)
	if err != nil {
		return g, err
	}
	g.pending = resolveLink(g.link, dest) == g.old
	return g, nil
}

func gitignorePending(q Query) (bool, error) {
	g, err := gitignoreInspect(q)
	return g.pending, err
}

func gitignoreRun(c Context) error {
	g, err := gitignoreInspect(c.Query())
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

// ------------------------------------------------------------- mambaforge
//
// The first RECLAIMING migration, and the only code in this design whose entire
// purpose is destroying something irrecoverable. ~/sdk/mambaforge is 3.5 GB,
// survives from May 2024, and is in no repository: `git show` recovers nothing.
//
// It is reclaimable because uv replaced it. The installation's four environments
// are named by Python version alone (3_9 through 3_12), which is exactly what
// `uv python install` provides, and micromamba is not on PATH -- so the mamba
// block in fish/mypost.fish is already inert.
//
// The KIND is what makes this safe, not the code below: a bare
// `./bootstrap migrate` lists it and runs nothing, so a routine invocation
// cannot reach any of it. The guard is the second line, for the one invocation
// that does.

// mambaforgeTools are the commands whose resolving inside the installation means
// it is still LIVE. Deleting 3.5 GB out from under a working toolchain is the
// failure the guard exists for, and every one of these would break silently
// afterwards -- an interpreter that is simply gone produces "command not found"
// in a shell nobody connects to a reclamation they ran last week.
//
// python and python3 are here even though they are the two most likely to
// resolve somewhere harmless. The cost of listing them is one LookPath; the cost
// of omitting one is unrecoverable.
var mambaforgeTools = []string{"conda", "mamba", "micromamba", "python", "python3", "pip"}

type mambaforgeFacts struct {
	dir     string // ~/sdk/mambaforge
	exists  bool
	pending bool
}

func mambaforgeInspect(q Query) (mambaforgeFacts, error) {
	m := mambaforgeFacts{dir: filepath.Join(q.Home, "sdk", "mambaforge")}
	info, err := q.Read.Lstat(m.dir)
	if err != nil {
		return m, err
	}
	m.exists = info.Exists
	// A REAL directory, so a symlink there is not pending. Removing a symlink
	// reclaims no disk at all, and what it points at is data this migration knows
	// nothing about. Run refuses that shape rather than treating it as done --
	// see mambaforgeRun.
	m.pending = info.IsDir
	return m, nil
}

func mambaforgePending(q Query) (bool, error) {
	m, err := mambaforgeInspect(q)
	return m.pending, err
}

// mambaforgeRun refuses far more readily than it removes, which is the right
// asymmetry for an operation with no undo.
//
// The PATH guard is deliberately NOT part of Pending. A live toolchain does not
// mean there is nothing to reclaim -- it means the reclamation cannot happen
// yet -- so hiding the directory from the listing would make 3.5 GB invisible
// until somebody rediscovered it. Pending answers "is it there"; Run answers
// "may it go". Reader has no LookPath either, so the split is enforced by the
// type rather than by discipline.
func mambaforgeRun(c Context) error {
	m, err := mambaforgeInspect(c.Query())
	if err != nil {
		return err
	}
	if !m.pending {
		if m.exists {
			return &change.Refusal{
				Path: m.dir,
				Problem: "exists and is not a real directory, so it is not the " +
					"installation this reclaims; removing it would free nothing and " +
					"destroy something this migration knows nothing about",
				Remediation: "remove it yourself if you meant to, or reclaim whatever " +
					"it stands for directly",
			}
		}
		c.logf("      already reclaimed: %s is not there", m.dir)
		return nil
	}
	if err := mambaforgeInUse(c.Change, m.dir); err != nil {
		return err
	}
	// Said before it happens rather than after, because there is nothing to say
	// afterwards that would help.
	c.logf("      reclaiming %s; this destroys untracked data and cannot be undone", m.dir)
	return c.Change.RemoveAll(m.dir)
}

// mambaforgeInUse refuses when any guarded tool leads into dir.
//
// It is the load-bearing part of this migration -- the removal itself is one
// call -- so it names both the tool and where it resolves. "refusing to remove
// ~/sdk/mambaforge" without those is a dead end for whoever reads it, and the
// remedy differs completely depending on which tool fired.
func mambaforgeInUse(mach Machine, dir string) error {
	for _, tool := range mambaforgeTools {
		at, inside, err := resolvesInto(mach, tool, dir)
		if err != nil {
			return err
		}
		if inside {
			return &change.Refusal{
				Path: dir,
				Problem: fmt.Sprintf("%q on PATH resolves to %s, inside it, so this "+
					"toolchain is still live", tool, at),
				Remediation: "take it off PATH -- or move whatever still needs it to " +
					"uv -- then retry",
			}
		}
	}
	return nil
}

// maxLinkHops bounds the symlink chain resolvesInto will follow. It is Linux's
// MAXSYMLINKS and above darwin's 32, so any chain the kernel can resolve this
// one can too -- and a chain longer than the kernel's own limit answers ELOOP,
// which is not a tool anything is currently running.
const maxLinkHops = 40

// resolvesInto reports whether looking up name leads into dir, and where.
//
// Every hop is tested, not just the final destination. exec.LookPath answers
// with the entry it found ON PATH and does not resolve symlinks, so a
// ~/.local/bin/python pointing into the installation reads as a path outside it
// -- and comparing LookPath's answer directly would let the removal proceed
// against a live interpreter AND break the link on the way past.
//
// A tool that is not on PATH is not in use, so a LookPath error is not an error
// here. exec.LookPath reports both "no such name" and "found but not
// executable" that way, and neither is something about to be deleted out from
// under a running process.
//
// The comparison is between absolute paths. A relative PATH entry cannot be
// resolved without a working directory, which this package deliberately cannot
// read -- and a PATH holding one is not a shape any machine here has.
func resolvesInto(m Machine, name, dir string) (string, bool, error) {
	path, err := m.LookPath(name)
	if err != nil {
		return "", false, nil
	}
	for hop := 0; hop < maxLinkHops; hop++ {
		if within(dir, path) {
			return path, true, nil
		}
		info, err := m.Lstat(path)
		if err != nil {
			return "", false, err
		}
		if !info.IsLink {
			return "", false, nil
		}
		dest, err := m.Readlink(path)
		if err != nil {
			return "", false, err
		}
		path = resolveLink(path, dest)
	}
	return "", false, nil
}

// within reports whether path is dir or below it.
//
// The separator is part of the prefix, and that is the whole of this function's
// reason to exist: ~/sdk/mambaforge-old begins with ~/sdk/mambaforge and is a
// different directory. Without it the guard refuses over a sibling forever and
// the 3.5 GB is never reclaimed, with a message naming a directory that has
// nothing to do with the tool it found.
func within(dir, path string) bool {
	dir, path = filepath.Clean(dir), filepath.Clean(path)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
