package migrate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

// The tests in this package are the only code in the design that exercises
// destruction, so every one of them builds its own checkout and its own $HOME
// under t.TempDir(). Nothing here may read, let alone write, the machine's real
// home: the data these migrations move -- fish_variables and the installed
// plugin set -- is not in git and cannot be recovered.

type fixture struct {
	root string // a checkout
	home string
	out  bytes.Buffer
}

// newFixture builds the smallest checkout the three migrations name, plus an
// empty home with a ~/.config to hang things off.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{root: filepath.Join(dir, "dotfiles"), home: filepath.Join(dir, "home")}
	writeFile(t, filepath.Join(f.root, "fish", "config.fish"), "# tracked shared config\n")
	writeFile(t, filepath.Join(f.root, "fish", "alias.fish"), "# tracked aliases\n")
	writeFile(t, filepath.Join(f.root, "git", "gitignore_global"), "*.o\n")
	writeFile(t, filepath.Join(f.root, "git", "gitconfig.shared"), "[core]\n\tpager = less\n")
	mkdirAll(t, filepath.Join(f.home, ".config"))
	return f
}

func (f *fixture) ctx() Context {
	return Context{
		Change: change.NewApplier(&f.out, f.root),
		Root:   f.root,
		Home:   f.home,
		Out:    &f.out,
	}
}

// oldFishMachine is the shape every machine provisioned before Task 7 has:
// ~/.config/fish is a symlink into the checkout, and fisher has been writing
// its generated state there ever since.
func (f *fixture) oldFishMachine(t *testing.T) {
	t.Helper()
	writeFile(t, filepath.Join(f.root, "fish", "fish_variables"), "SETUVAR fish_color_normal:normal\n")
	writeFile(t, filepath.Join(f.root, "fish", "fish_plugins"), "jorgebucaran/fisher\nIlanCosman/tide@v6\n")
	writeFile(t, filepath.Join(f.root, "fish", "functions", "fisher.fish"), "function fisher\nend\n")
	writeFile(t, filepath.Join(f.root, "fish", "completions", "fisher.fish"), "complete -c fisher\n")
	writeFile(t, filepath.Join(f.root, "fish", "conf.d", "tide.fish"), "set -g tide_left_prompt_items\n")
	writeFile(t, filepath.Join(f.root, "fish", "themes", "mine.theme"), "fish_color_normal normal\n")
	// Debris that is NOT fish configuration and must stay where it is. Both are
	// in the repo owner's real fish/ today.
	writeFile(t, filepath.Join(f.root, "fish", ".DS_Store"), "\x00")
	writeFile(t, filepath.Join(f.root, "fish", "config.fish.bak.1784491210"), "# an installer's backup\n")
	symlink(t, filepath.Join(f.root, "fish"), filepath.Join(f.home, ".config", "fish"))
}

func (f *fixture) oldGitignoreMachine(t *testing.T) {
	t.Helper()
	symlink(t, filepath.Join(f.root, "git", "gitignore_global.symlink"),
		filepath.Join(f.home, ".gitignore"))
}

// oldGitconfig is modelled on the real file: the shared path is named twice,
// once in a header COMMENT and once in the [include] block, with real
// machine-local settings around both. Only the include may be rewritten.
const oldGitconfig = `# Machine-local git configuration.  NOT tracked by dotfiles.
#
# Put anything worth sharing in the checkout's
# ~/dotfiles/git/gitconfig.symlink.  Put identity and secrets in
# ~/etc/extras.secret/gitconfig.

[include]
	path = ~/dotfiles/git/gitconfig.symlink

[user]
	name = A Real Person
	email = someone@example.com

[credential "https://github.com"]
	helper = osxkeychain
`

func (f *fixture) oldGitconfigMachine(t *testing.T) {
	t.Helper()
	writeFile(t, filepath.Join(f.home, ".gitconfig"), oldGitconfig)
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// tree records every path under dir with its kind, contents and link target, so
// a case asserting "nothing changed" catches a rewritten file and a repointed
// symlink as well as a created or deleted one.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			dest, _ := os.Readlink(path)
			b.WriteString("link " + rel + " -> " + dest + "\n")
		case info.IsDir():
			b.WriteString("dir  " + rel + "\n")
		default:
			data, _ := os.ReadFile(path)
			b.WriteString("file " + rel + " " + string(data) + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// named returns the one migration with this name, failing the test when the set
// no longer contains it.
func named(t *testing.T, name string) Migration {
	t.Helper()
	for _, m := range All() {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no migration named %q; All() has %d", name, len(All()))
	return Migration{}
}

func isPending(t *testing.T, c Context, name string) bool {
	t.Helper()
	got, err := named(t, name).Pending(c.Query())
	if err != nil {
		t.Fatalf("%s Pending: %v", name, err)
	}
	return got
}

func mustRun(t *testing.T, c Context, name string) {
	t.Helper()
	if err := named(t, name).Run(c); err != nil {
		t.Fatalf("%s Run: %v", name, err)
	}
}

// The other two of the three interfaces, pinned as phase.Machine and
// check.Machine are.
//
// Machine is the WIDE one -- it is where the four destructive operations are
// allowed to live -- so the assertion that matters is the opposite of the one
// phase makes: change.Interface must satisfy it exactly, with nothing left over
// on either side. A method added to change.Interface and not reflected here
// would be a capability the migrations silently could not reach; one added here
// and not there would not compile.
//
// Reader is the narrow one, and it is what makes preflight's question askable
// by a phase that cannot destroy anything. Widening it would hand that
// capability back through the door this split exists to close.
func TestTheTwoMigrateInterfacesAreThirteenAndThree(t *testing.T) {
	var _ Machine = change.Interface(nil)
	var _ Reader = change.Interface(nil)

	machine := reflect.TypeOf((*Machine)(nil)).Elem()
	if machine.NumMethod() != 13 {
		t.Errorf("migrate.Machine has %d methods, want 13 -- change.Interface entire",
			machine.NumMethod())
	}
	iface := reflect.TypeOf((*change.Interface)(nil)).Elem()
	if machine.NumMethod() != iface.NumMethod() {
		t.Errorf("migrate.Machine has %d methods and change.Interface has %d; a "+
			"migration cannot reach an operation that is not on both",
			machine.NumMethod(), iface.NumMethod())
	}

	reader := reflect.TypeOf((*Reader)(nil)).Elem()
	for _, method := range []string{"Copy", "Rename", "RemoveAll", "WriteFile",
		"Dir", "Link", "Seed", "Run", "Sudo"} {
		if _, found := reader.MethodByName(method); found {
			t.Errorf("migrate.Reader exposes %s; deciding whether a migration "+
				"applies is a read, and preflight asks that question", method)
		}
	}
	if reader.NumMethod() != 3 {
		t.Errorf("migrate.Reader has %d methods, want 3 (Lstat, Readlink, ReadFile)",
			reader.NumMethod())
	}
}

// Every migration is declared exactly once and carries both functions. A
// Migration with a nil Pending would panic the moment preflight asked.
func TestEveryMigrationIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range All() {
		if m.Name == "" {
			t.Error("a migration has no name")
		}
		if seen[m.Name] {
			t.Errorf("two migrations are named %q", m.Name)
		}
		seen[m.Name] = true
		if m.Kind != Reconciling && m.Kind != Reclaiming {
			t.Errorf("%s has kind %q, want reconciling or reclaiming", m.Name, m.Kind)
		}
		if m.Pending == nil || m.Run == nil {
			t.Errorf("%s is missing Pending or Run", m.Name)
		}
	}
	for _, want := range []string{"fish", "gitconfig", "gitignore"} {
		if !seen[want] {
			t.Errorf("the %s migration is missing", want)
		}
	}
}

// ---------------------------------------------------------------- fish

// Both directions, and the three shapes that must NOT be pending. A false
// positive runs the migration twice; a false negative leaves apply refusing the
// .config/fish row forever with no remedy at all.
func TestFishPendingBothDirections(t *testing.T) {
	t.Run("old machine is pending", func(t *testing.T) {
		f := newFixture(t)
		f.oldFishMachine(t)
		if !isPending(t, f.ctx(), "fish") {
			t.Error("a ~/.config/fish symlinked into the checkout must be pending")
		}
	})
	t.Run("migrated machine is not pending", func(t *testing.T) {
		f := newFixture(t)
		f.oldFishMachine(t)
		mustRun(t, f.ctx(), "fish")
		if isPending(t, f.ctx(), "fish") {
			t.Error("the migration must not still be pending after it ran")
		}
	})
	t.Run("a real directory is not pending", func(t *testing.T) {
		f := newFixture(t)
		mkdirAll(t, filepath.Join(f.home, ".config", "fish"))
		if isPending(t, f.ctx(), "fish") {
			t.Error("a real ~/.config/fish is already what this migration produces")
		}
	})
	t.Run("an absent path is not pending", func(t *testing.T) {
		f := newFixture(t)
		if isPending(t, f.ctx(), "fish") {
			t.Error("a machine with no ~/.config/fish has nothing to migrate")
		}
	})
	t.Run("a symlink elsewhere is not pending", func(t *testing.T) {
		f := newFixture(t)
		other := filepath.Join(t.TempDir(), "somebody elses fish")
		mkdirAll(t, other)
		symlink(t, other, filepath.Join(f.home, ".config", "fish"))
		if isPending(t, f.ctx(), "fish") {
			t.Error("a symlink outside this checkout is not this migration's business")
		}
	})
	// A relative link is the same link. Comparing Readlink's raw answer against
	// an absolute path would report this as pointing somewhere else -- a false
	// negative, which leaves apply refusing .config/fish forever.
	t.Run("a relative symlink into the checkout is pending", func(t *testing.T) {
		f := newFixture(t)
		rel, err := filepath.Rel(filepath.Join(f.home, ".config"), filepath.Join(f.root, "fish"))
		if err != nil {
			t.Fatal(err)
		}
		symlink(t, rel, filepath.Join(f.home, ".config", "fish"))
		if !isPending(t, f.ctx(), "fish") {
			t.Errorf("a ~/.config/fish -> %s is the same link, spelled relatively", rel)
		}
	})
}

// The state that is not in git ends up in a real ~/.config/fish, and leaves the
// checkout. The tracked files stay where they are.
func TestFishMovesGeneratedStateOutOfTheCheckout(t *testing.T) {
	f := newFixture(t)
	f.oldFishMachine(t)
	before := map[string]string{}
	for _, rel := range []string{
		"fish_variables", "fish_plugins",
		filepath.Join("functions", "fisher.fish"),
		filepath.Join("completions", "fisher.fish"),
		filepath.Join("conf.d", "tide.fish"),
		// themes/ is not one of §7's five .gitignore lines, and is moved anyway:
		// git does not track empty directories, so the survey that produced that
		// list could not see it. `fish_config theme save` writes here.
		filepath.Join("themes", "mine.theme"),
	} {
		before[rel] = readFile(t, filepath.Join(f.root, "fish", rel))
	}

	mustRun(t, f.ctx(), "fish")

	config := filepath.Join(f.home, ".config", "fish")
	info, err := os.Lstat(config)
	if err != nil {
		t.Fatalf("~/.config/fish is gone: %v", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("~/.config/fish is %s, want a real directory", info.Mode())
	}
	for rel, want := range before {
		if got := readFile(t, filepath.Join(config, rel)); got != want {
			t.Errorf("~/.config/fish/%s = %q, want %q", rel, got, want)
		}
		if _, err := os.Lstat(filepath.Join(f.root, "fish", rel)); !os.IsNotExist(err) {
			t.Errorf("%s is still in the checkout (%v); the migration did not finish", rel, err)
		}
	}
	for _, dir := range []string{"functions", "completions", "conf.d", "themes"} {
		if _, err := os.Lstat(filepath.Join(f.root, "fish", dir)); !os.IsNotExist(err) {
			t.Errorf("%s/ is still in the checkout; the migration did not finish", dir)
		}
	}
	for _, tracked := range []string{"config.fish", "alias.fish"} {
		if _, err := os.Stat(filepath.Join(f.root, "fish", tracked)); err != nil {
			t.Errorf("the migration removed the tracked %s: %v", tracked, err)
		}
	}
	// Debris stays put. Moving it would be the migration inventing state in a
	// directory it just created, and a .DS_Store in ~/.config/fish is noise a
	// human then has to explain to themselves.
	for _, debris := range []string{".DS_Store", "config.fish.bak.1784491210"} {
		if _, err := os.Stat(filepath.Join(f.root, "fish", debris)); err != nil {
			t.Errorf("the migration removed %s, which is not its business: %v", debris, err)
		}
		if _, err := os.Lstat(filepath.Join(config, debris)); !os.IsNotExist(err) {
			t.Errorf("%s was moved into ~/.config/fish; it is not fish configuration", debris)
		}
	}
}

// Carried from Task 7's review. The old tracked config.fish must NOT be copied
// to ~/.config/fish/config.fish: Seed never overwrites, so apply would then skip
// the stub, and the surviving file's `source (status dirname)/alias.fish` does
// not match the fish-source check's pattern -- a machine that reports fail with
// no obvious cause.
func TestFishLeavesNoTrackedConfigInTheSeededLocation(t *testing.T) {
	f := newFixture(t)
	f.oldFishMachine(t)
	mustRun(t, f.ctx(), "fish")

	stub := filepath.Join(f.home, ".config", "fish", "config.fish")
	if _, err := os.Lstat(stub); !os.IsNotExist(err) {
		t.Errorf("%s exists (%v); the seed row can no longer place the stub", stub, err)
	}
}

// The ordering that matters, asserted without injecting anything: nothing may be
// removed until every copy has succeeded. None of what is being moved is in git.
func TestFishCopiesEverythingBeforeItRemovesAnything(t *testing.T) {
	f := newFixture(t)
	f.oldFishMachine(t)

	rec := &recorder{Interface: change.NewApplier(&f.out, f.root)}
	c := f.ctx()
	c.Change = rec
	mustRun(t, c, "fish")

	lastCopy, firstRemove := -1, -1
	for i, op := range rec.ops {
		switch {
		case strings.HasPrefix(op, "copy "):
			lastCopy = i
		case strings.HasPrefix(op, "remove ") && firstRemove < 0:
			firstRemove = i
		}
	}
	if lastCopy < 0 || firstRemove < 0 {
		t.Fatalf("the fixture produced no copies or no removals, so this proves nothing: %v", rec.ops)
	}
	if firstRemove < lastCopy {
		t.Errorf("removal %d happens before the last copy %d:\n%s",
			firstRemove, lastCopy, strings.Join(rec.ops, "\n"))
	}
}

// The same ordering seen from the other side: a copy that fails must leave the
// machine exactly as it was. This is the case that decides whether an interrupted
// run is recoverable, so it asserts on the filesystem rather than on a log.
func TestFishLeavesTheOldStateIntactWhenACopyFails(t *testing.T) {
	f := newFixture(t)
	f.oldFishMachine(t)
	before := tree(t, filepath.Join(f.root, "fish"))

	c := f.ctx()
	// conf.d is copied last, so everything before it has already succeeded --
	// which is exactly the state a remove-as-you-go implementation would have
	// half-destroyed by now.
	c.Change = failingCopy{Interface: change.NewApplier(&f.out, f.root), on: "conf.d"}
	if err := named(t, "fish").Run(c); err == nil {
		t.Fatal("a failed copy must fail the migration")
	}

	if after := tree(t, filepath.Join(f.root, "fish")); after != before {
		t.Errorf("an interrupted migration changed the checkout:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	link := filepath.Join(f.home, ".config", "fish")
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("~/.config/fish is no longer the original symlink (%v, %v); the "+
			"machine is half-migrated", info, err)
	}
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if dest != filepath.Join(f.root, "fish") {
		t.Errorf("~/.config/fish points at %s, want the checkout %s",
			dest, filepath.Join(f.root, "fish"))
	}
}

// A machine that already has a real ~/.config/fish full of its own state must
// not be touched at all. This is the catastrophic case: RemoveAll on a real
// directory here would take fish_variables and the whole plugin set with it.
func TestFishTouchesNothingWhenNotPending(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.home, ".config", "fish", "fish_variables"), "SETUVAR precious:data\n")
	writeFile(t, filepath.Join(f.home, ".config", "fish", "config.fish"), "# the stub\n")
	before := tree(t, f.home)

	mustRun(t, f.ctx(), "fish")

	if after := tree(t, f.home); after != before {
		t.Errorf("a migration that is not pending changed the home:\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}

// ------------------------------------------------------------ gitconfig

func TestGitconfigPendingBothDirections(t *testing.T) {
	t.Run("old include is pending", func(t *testing.T) {
		f := newFixture(t)
		f.oldGitconfigMachine(t)
		if !isPending(t, f.ctx(), "gitconfig") {
			t.Error("a ~/.gitconfig including the pre-rename path must be pending")
		}
	})
	t.Run("migrated file is not pending", func(t *testing.T) {
		f := newFixture(t)
		f.oldGitconfigMachine(t)
		mustRun(t, f.ctx(), "gitconfig")
		if isPending(t, f.ctx(), "gitconfig") {
			t.Errorf("still pending after the rewrite:\n%s",
				readFile(t, filepath.Join(f.home, ".gitconfig")))
		}
	})
	t.Run("a mention in a comment is not pending", func(t *testing.T) {
		f := newFixture(t)
		// The real seeded file names the shared config in its header comment as
		// well as in its include. A Contains test would call this pending
		// forever, and the migration would rewrite nothing every time.
		writeFile(t, filepath.Join(f.home, ".gitconfig"),
			"# shared settings used to live in ~/dotfiles/git/gitconfig.symlink\n"+
				"[user]\n\tname = A Real Person\n")
		if isPending(t, f.ctx(), "gitconfig") {
			t.Error("a comment naming the old path is not an include of it")
		}
	})
	t.Run("no gitconfig is not pending", func(t *testing.T) {
		f := newFixture(t)
		if isPending(t, f.ctx(), "gitconfig") {
			t.Error("a machine with no ~/.gitconfig has nothing to rewrite")
		}
	})
	t.Run("a symlinked gitconfig is not pending", func(t *testing.T) {
		f := newFixture(t)
		// The pre-37f00a0 layout. Rewriting through this symlink would write
		// into the checkout, which is the fault the seed row exists to prevent.
		writeFile(t, filepath.Join(f.root, "git", "gitconfig.symlink"), oldGitconfig)
		symlink(t, filepath.Join(f.root, "git", "gitconfig.symlink"),
			filepath.Join(f.home, ".gitconfig"))
		if isPending(t, f.ctx(), "gitconfig") {
			t.Error("a symlinked ~/.gitconfig is not a machine-local file to rewrite")
		}
	})
}

// One line changes and every other byte survives. This file accumulates whatever
// `git config --global` wrote; losing any of it is a real loss.
func TestGitconfigRewritesExactlyTheIncludeLine(t *testing.T) {
	f := newFixture(t)
	f.oldGitconfigMachine(t)
	mustRun(t, f.ctx(), "gitconfig")

	got := readFile(t, filepath.Join(f.home, ".gitconfig"))
	want := strings.Replace(oldGitconfig,
		"\tpath = ~/dotfiles/git/gitconfig.symlink\n",
		"\tpath = "+filepath.Join(f.root, "git", "gitconfig.shared")+"\n", 1)
	if got != want {
		t.Errorf("the rewrite is not byte-for-byte:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// Stated separately so a fixture change cannot quietly turn the comparison
	// above into a tautology.
	if !strings.Contains(got, "email = someone@example.com") {
		t.Error("a machine-local setting was lost")
	}
	if !strings.Contains(got, "# ~/dotfiles/git/gitconfig.symlink.") &&
		!strings.Contains(got, "~/dotfiles/git/gitconfig.symlink.  Put identity") {
		t.Error("the header comment was rewritten; only the include line may change")
	}
}

// The include line's own spelling survives too: only the PATH VALUE inside it
// changes. Added because a mutation that replaced the whole line with a
// canonical "\tpath = <want>" passed the case above -- the fixture happened to
// be written in exactly that spelling, so "byte-for-byte" was asserted about
// every line except the one being rewritten.
//
// git accepts all four of these. A machine whose file uses any of them must come
// out the other side with its quoting, spacing and trailing comment intact.
func TestGitconfigPreservesTheIncludeLinesOwnFormatting(t *testing.T) {
	for _, spelling := range []string{
		"\tpath = ~/dotfiles/git/gitconfig.symlink",
		"  path=~/dotfiles/git/gitconfig.symlink",
		`	path = "~/dotfiles/git/gitconfig.symlink"`,
		"\tpath = ~/dotfiles/git/gitconfig.symlink  ; kept until the rename settles",
	} {
		t.Run(strings.TrimSpace(spelling), func(t *testing.T) {
			f := newFixture(t)
			writeFile(t, filepath.Join(f.home, ".gitconfig"),
				"[include]\n"+spelling+"\n\n[user]\n\tname = A Real Person\n")

			mustRun(t, f.ctx(), "gitconfig")

			want := strings.Replace(spelling, "~/dotfiles/git/gitconfig.symlink",
				filepath.Join(f.root, "git", "gitconfig.shared"), 1)
			got := readFile(t, filepath.Join(f.home, ".gitconfig"))
			line := strings.Split(got, "\n")[1]
			if line != want {
				t.Errorf("the include line came out as %q, want %q", line, want)
			}
		})
	}
}

func TestGitconfigTouchesNothingWhenNotPending(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.home, ".gitconfig"), "[user]\n\tname = A Real Person\n")
	before := tree(t, f.home)

	mustRun(t, f.ctx(), "gitconfig")

	if after := tree(t, f.home); after != before {
		t.Errorf("a migration that is not pending rewrote the file:\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}

// ------------------------------------------------------------ gitignore

func TestGitignorePendingBothDirections(t *testing.T) {
	t.Run("pre-rename target is pending", func(t *testing.T) {
		f := newFixture(t)
		f.oldGitignoreMachine(t)
		if !isPending(t, f.ctx(), "gitignore") {
			t.Error("a ~/.gitignore pointing at the pre-rename path must be pending")
		}
	})
	t.Run("retargeted link is not pending", func(t *testing.T) {
		f := newFixture(t)
		f.oldGitignoreMachine(t)
		mustRun(t, f.ctx(), "gitignore")
		if isPending(t, f.ctx(), "gitignore") {
			t.Error("the migration must not still be pending after it ran")
		}
	})
	t.Run("absent is not pending", func(t *testing.T) {
		f := newFixture(t)
		if isPending(t, f.ctx(), "gitignore") {
			t.Error("a machine with no ~/.gitignore has nothing to retarget")
		}
	})
	t.Run("a regular file is not pending", func(t *testing.T) {
		f := newFixture(t)
		writeFile(t, filepath.Join(f.home, ".gitignore"), "*.log\n")
		if isPending(t, f.ctx(), "gitignore") {
			t.Error("a hand-written ~/.gitignore is not this migration's business")
		}
	})
	t.Run("a symlink elsewhere is not pending", func(t *testing.T) {
		f := newFixture(t)
		symlink(t, filepath.Join(f.root, "git", "gitattributes"),
			filepath.Join(f.home, ".gitignore"))
		if isPending(t, f.ctx(), "gitignore") {
			t.Error("a symlink to something else is not the pre-rename target")
		}
	})
	t.Run("a relative symlink at the old target is pending", func(t *testing.T) {
		f := newFixture(t)
		rel, err := filepath.Rel(f.home, filepath.Join(f.root, "git", "gitignore_global.symlink"))
		if err != nil {
			t.Fatal(err)
		}
		symlink(t, rel, filepath.Join(f.home, ".gitignore"))
		if !isPending(t, f.ctx(), "gitignore") {
			t.Errorf("a ~/.gitignore -> %s is the same link, spelled relatively", rel)
		}
	})
}

func TestGitignoreRetargetsTheSymlink(t *testing.T) {
	f := newFixture(t)
	f.oldGitignoreMachine(t)
	mustRun(t, f.ctx(), "gitignore")

	link := filepath.Join(f.home, ".gitignore")
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("~/.gitignore is no longer a symlink: %v", err)
	}
	want := filepath.Join(f.root, "git", "gitignore_global")
	if dest != want {
		t.Errorf("~/.gitignore -> %s, want %s", dest, want)
	}
	if got := readFile(t, link); got != "*.o\n" {
		t.Errorf("the retargeted link reads %q, so it does not resolve", got)
	}
}

// The link is removed before the new one is made, so a checkout missing the
// renamed file would leave the machine with no ~/.gitignore at all. Refusing
// first is the only ordering that cannot lose it.
func TestGitignoreRefusesWhenTheRenamedSourceIsMissing(t *testing.T) {
	f := newFixture(t)
	f.oldGitignoreMachine(t)
	if err := os.Remove(filepath.Join(f.root, "git", "gitignore_global")); err != nil {
		t.Fatal(err)
	}
	before := tree(t, f.home)

	err := named(t, "gitignore").Run(f.ctx())
	var refusal *change.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
	}
	if refusal.Remediation == "" {
		t.Error("a refusal must name its remediation")
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("a refused migration changed the home:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// ------------------------------------------------------------ the verb

// A bare migrate runs every pending reconciling migration, and one invocation is
// enough: nothing is left pending afterwards.
func TestBareRunReconcilesEverything(t *testing.T) {
	f := newFixture(t)
	f.oldFishMachine(t)
	f.oldGitconfigMachine(t)
	f.oldGitignoreMachine(t)

	pend, err := Pending(f.ctx().Query())
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 3 {
		t.Fatalf("%d migrations pending on a fully old machine, want 3", len(pend))
	}

	if err := Run(f.ctx(), ""); err != nil {
		t.Fatalf("bare migrate: %v", err)
	}
	pend, err = Pending(f.ctx().Query())
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 0 {
		var names []string
		for _, m := range pend {
			names = append(names, m.Name)
		}
		t.Errorf("still pending after a bare migrate: %s", strings.Join(names, ", "))
	}
}

func TestBareRunOnACleanMachineSaysSo(t *testing.T) {
	f := newFixture(t)
	if err := Run(f.ctx(), ""); err != nil {
		t.Fatalf("a clean machine must not be an error: %v", err)
	}
	if !strings.Contains(f.out.String(), "nothing to migrate") {
		t.Errorf("a bare migrate with nothing to do must say so:\n%s", f.out.String())
	}
}

func TestRunOneByName(t *testing.T) {
	f := newFixture(t)
	f.oldGitignoreMachine(t)
	f.oldGitconfigMachine(t)

	if err := Run(f.ctx(), "gitignore"); err != nil {
		t.Fatal(err)
	}
	if isPending(t, f.ctx(), "gitignore") {
		t.Error("the named migration did not run")
	}
	if !isPending(t, f.ctx(), "gitconfig") {
		t.Error("naming one migration must not run the others")
	}
}

// Preflight refuses on the reconciling migrations only, and this is the case
// that holds it. Refusing on a reclaiming one would deadlock every apply on a
// machine that still has the thing being reclaimed: a bare migrate deliberately
// never runs one, so the remedy the refusal names would not clear it.
func TestNamesFiltersByKind(t *testing.T) {
	ms := []Migration{
		{Name: "fish", Kind: Reconciling},
		{Name: "mambaforge", Kind: Reclaiming},
		{Name: "gitignore", Kind: Reconciling},
	}
	got := strings.Join(Names(ms, Reconciling), ",")
	if got != "fish,gitignore" {
		t.Errorf("reconciling = %q, want \"fish,gitignore\"", got)
	}
	if got := strings.Join(Names(ms, Reclaiming), ","); got != "mambaforge" {
		t.Errorf("reclaiming = %q, want \"mambaforge\"", got)
	}
}

func TestUnknownNameIsAnError(t *testing.T) {
	f := newFixture(t)
	err := Run(f.ctx(), "mambaforgee")
	var unknown *UnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("want a *migrate.UnknownError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "mambaforgee") {
		t.Errorf("the error must name what was asked for: %v", err)
	}
	if !strings.Contains(err.Error(), "fish") {
		t.Errorf("the error must list what is available: %v", err)
	}
}

// The reclaiming mechanism, built now and exercised now. Task 10 adds the first
// real reclaiming migration; until then this fake is the only thing that can
// prove a bare migrate LISTS one instead of running it, and the alternative --
// wiring the mechanism and testing it later -- is how a safety property ships
// untested.
func TestBareRunListsReclaimingMigrationsAndRunsNone(t *testing.T) {
	f := newFixture(t)
	ran := false
	reclaiming := Migration{
		Name:    "mambaforge",
		Kind:    Reclaiming,
		Pending: func(Query) (bool, error) { return true, nil },
		Run:     func(Context) error { ran = true; return nil },
	}

	if err := run(f.ctx(), "", []Migration{reclaiming}); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("a bare migrate ran a reclaiming migration; it destroys untracked data")
	}
	out := f.out.String()
	if !strings.Contains(out, "./bootstrap migrate mambaforge") {
		t.Errorf("a bare migrate must list the exact command for each reclaiming "+
			"migration:\n%s", out)
	}
}

func TestANamedReclaimingMigrationRuns(t *testing.T) {
	f := newFixture(t)
	ran := false
	reclaiming := Migration{
		Name:    "mambaforge",
		Kind:    Reclaiming,
		Pending: func(Query) (bool, error) { return true, nil },
		Run:     func(Context) error { ran = true; return nil },
	}
	if err := run(f.ctx(), "mambaforge", []Migration{reclaiming}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("naming a reclaiming migration must run it")
	}
}

// A migration that is not pending is not listed, so a bare migrate does not
// advertise a reclamation that has already happened.
func TestBareRunListsOnlyPendingReclaimingMigrations(t *testing.T) {
	f := newFixture(t)
	done := Migration{
		Name:    "mambaforge",
		Kind:    Reclaiming,
		Pending: func(Query) (bool, error) { return false, nil },
		Run:     func(Context) error { t.Fatal("must not run"); return nil },
	}
	if err := run(f.ctx(), "", []Migration{done}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.out.String(), "migrate mambaforge") {
		t.Errorf("a reclamation that is already done must not be advertised:\n%s", f.out.String())
	}
}

// ------------------------------------------------------------ test doubles

var errInjected = errors.New("injected copy failure")

// failingCopy is an Applier whose Copy of one named entry fails, so a case can
// interrupt the migration exactly where an interrupt is most dangerous.
type failingCopy struct {
	change.Interface
	on string
}

func (f failingCopy) Copy(source, target string) error {
	if filepath.Base(source) == f.on {
		return errInjected
	}
	return f.Interface.Copy(source, target)
}

// recorder is an Applier that also records the order of the two operations whose
// relative order is the whole safety property.
type recorder struct {
	change.Interface
	ops []string
}

func (r *recorder) Copy(source, target string) error {
	r.ops = append(r.ops, "copy "+source)
	return r.Interface.Copy(source, target)
}

func (r *recorder) RemoveAll(path string) error {
	r.ops = append(r.ops, "remove "+path)
	return r.Interface.RemoveAll(path)
}
