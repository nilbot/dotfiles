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

// writeExecutable writes a file exec.LookPath will accept, which is what the
// mambaforge guard is tested through: the real Applier's LookPath, over a real
// PATH, rather than a double that could agree with a wrong implementation.
//
// The Chmod is not redundant with the mode os.WriteFile is given. A mode handed
// to a file-CREATING call is masked by the umask and a mode handed to Chmod is
// not, so under `umask 077` the requested 0755 arrives as 0700. That is still
// executable by the owner and so would pass here -- but the same confusion
// produced three defects in Task 9, so the mode is set rather than requested.
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	writeFile(t, path, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// onlyPATH replaces PATH for one case. Without it the developer's own python3
// decides whether the guard fires, so the suite would answer differently on two
// machines -- and the case that proves the removal PROCEEDS would silently stop
// proving it on a machine with conda installed.
func onlyPATH(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
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
	for _, want := range []string{"fish", "gitconfig", "gitignore", "mambaforge"} {
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

// The release window, which is the one interruption the staging protocol does
// not by itself survive.
//
// A run that dies after RemoveAll(link) and before Rename leaves no
// ~/.config/fish at all. Keyed on the symlink alone, Pending answers false
// there: `migrate` prints "nothing to migrate" and exits 0, preflight passes,
// apply makes a fresh directory and seeds the stub, and BOTH copies of fisher's
// state -- the checkout's and the staging directory's -- are orphaned with
// nothing reporting it. check calls the machine healthy.
//
// fishRun's refusal was already written for exactly this state and could never
// be reached, because Pending never let the question be asked.
func TestFishPendingSeesAnAbandonedStagingDirectory(t *testing.T) {
	// The release window, reproduced exactly: staging is complete, the symlink
	// is gone, and ~/.config/fish does not exist.
	window := func(t *testing.T) *fixture {
		t.Helper()
		f := newFixture(t)
		f.oldFishMachine(t)
		staging := filepath.Join(f.home, ".config", "fish.bootstrap-migrating")
		writeFile(t, filepath.Join(staging, "fish_variables"), "SETUVAR fish_color_normal:normal\n")
		if err := os.Remove(filepath.Join(f.home, ".config", "fish")); err != nil {
			t.Fatal(err)
		}
		return f
	}

	t.Run("an abandoned staging directory is pending", func(t *testing.T) {
		f := window(t)
		if !isPending(t, f.ctx(), "fish") {
			t.Error("a machine interrupted in the release window must be pending; " +
				"otherwise both copies of fisher's state are orphaned in silence")
		}
	})
	t.Run("no staging directory is not pending", func(t *testing.T) {
		f := window(t)
		if err := os.RemoveAll(filepath.Join(f.home, ".config", "fish.bootstrap-migrating")); err != nil {
			t.Fatal(err)
		}
		if isPending(t, f.ctx(), "fish") {
			t.Error("with no staging directory and no symlink there is nothing to migrate")
		}
	})

	// The refusal fishRun already carried, now reachable. It must name both
	// places fisher's state can be, because deciding what to keep needs both.
	t.Run("run refuses and names both locations", func(t *testing.T) {
		f := window(t)
		before := tree(t, f.home)

		err := named(t, "fish").Run(f.ctx())
		var refusal *change.Refusal
		if !errors.As(err, &refusal) {
			t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
		}
		staging := filepath.Join(f.home, ".config", "fish.bootstrap-migrating")
		for _, want := range []string{staging, filepath.Join(f.root, "fish")} {
			if !strings.Contains(refusal.Error(), want) {
				t.Errorf("the refusal must name %s:\n%s", want, refusal.Error())
			}
		}
		// Refusing is the whole job. Renaming staging into place would be a guess
		// about whether it is complete, on data that is not in git.
		if after := tree(t, f.home); after != before {
			t.Errorf("a refused migration changed the home:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})

	// And the verb surfaces it, rather than reporting nothing to do.
	t.Run("a bare migrate surfaces it", func(t *testing.T) {
		f := window(t)
		err := Run(f.ctx(), "")
		if err == nil {
			t.Fatalf("a bare migrate must not report success here:\n%s", f.out.String())
		}
		if strings.Contains(f.out.String(), "nothing to migrate") {
			t.Errorf("a bare migrate reported nothing to do over orphaned state:\n%s",
				f.out.String())
		}
	})
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

// ----------------------------------------------------------- mambaforge

// The six tools §8.1 names, stated HERE rather than read from the
// implementation's own list. A guard tested against the list it consults would
// pass with any five of them, which is the mutation this spelling catches.
var mambaforgeGuarded = []string{"conda", "mamba", "micromamba", "python", "python3", "pip"}

// mambaforgeMachine builds ~/sdk/mambaforge in the shape the repo owner's really
// has it: four environments named by Python version alone -- exactly what
// `uv python install` provides, which is why this is reclaimable at all.
//
// bin/ is deliberately EMPTY of anything the guard looks for, so each case
// decides for itself which tool is on PATH and where it resolves. The real one
// is 3.5 GB and none of it is in git; every case builds its own under a temp
// HOME and none may touch the machine's.
func (f *fixture) mambaforgeMachine(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(f.home, "sdk", "mambaforge")
	writeExecutable(t, filepath.Join(dir, "bin", "activate"))
	writeFile(t, filepath.Join(dir, "conda-meta", "history"), "# created 2024-05\n")
	for _, env := range []string{"3_9", "3_10", "3_11", "3_12"} {
		writeFile(t, filepath.Join(dir, "envs", env, "lib", "python.so"), "not really\n")
	}
	return dir
}

// The kind is the whole safety story, and it is declared in exactly one place.
// A mambaforge declared Reconciling would be run by a bare migrate -- the one
// thing this migration must never do -- and nothing else in the design would
// notice.
func TestMambaforgeIsTheOnlyReclaimingMigration(t *testing.T) {
	if got := named(t, "mambaforge").Kind; got != Reclaiming {
		t.Errorf("mambaforge is %q, want %q; a reconciling migration is run by a bare migrate",
			got, Reclaiming)
	}
	if got := strings.Join(Names(All(), Reclaiming), ","); got != "mambaforge" {
		t.Errorf("reclaiming = %q, want \"mambaforge\"", got)
	}
	// The other three are unchanged, so preflight still refuses on exactly what
	// it refused on before -- see TestAMambaforgeMachineHasNothingReconcilingPending
	// and, for what preflight then does with that,
	// phase.TestPreflightAllowsAPendingReclamation.
	if got := strings.Join(Names(All(), Reconciling), ","); got != "fish,gitconfig,gitignore" {
		t.Errorf("reconciling = %q, want \"fish,gitconfig,gitignore\"", got)
	}
}

func TestMambaforgePendingBothDirections(t *testing.T) {
	t.Run("the directory is pending", func(t *testing.T) {
		f := newFixture(t)
		f.mambaforgeMachine(t)
		if !isPending(t, f.ctx(), "mambaforge") {
			t.Error("a ~/sdk/mambaforge holding 3.5 GB of untracked data is reclaimable")
		}
	})
	t.Run("an absent directory is not pending", func(t *testing.T) {
		f := newFixture(t)
		if isPending(t, f.ctx(), "mambaforge") {
			t.Error("a machine with no ~/sdk/mambaforge has nothing to reclaim")
		}
	})
	t.Run("a reclaimed machine is not pending", func(t *testing.T) {
		f := newFixture(t)
		f.mambaforgeMachine(t)
		onlyPATH(t, t.TempDir())
		mustRun(t, f.ctx(), "mambaforge")
		if isPending(t, f.ctx(), "mambaforge") {
			t.Error("the migration must not still be pending after it ran; a bare " +
				"migrate would advertise a reclamation that already happened")
		}
	})
	t.Run("a regular file is not pending", func(t *testing.T) {
		f := newFixture(t)
		writeFile(t, filepath.Join(f.home, "sdk", "mambaforge"), "not an installation\n")
		if isPending(t, f.ctx(), "mambaforge") {
			t.Error("a regular file at that path is not the installation this reclaims")
		}
	})
	t.Run("a symlink is not pending", func(t *testing.T) {
		f := newFixture(t)
		elsewhere := filepath.Join(t.TempDir(), "mambaforge")
		mkdirAll(t, elsewhere)
		symlink(t, elsewhere, filepath.Join(f.home, "sdk", "mambaforge"))
		if isPending(t, f.ctx(), "mambaforge") {
			t.Error("removing a symlink reclaims no disk at all, so a machine that " +
				"has one is not what this migration is for")
		}
	})
}

// The removal itself, and the case that proves the guard is not a blanket
// refusal: a python on PATH that resolves OUTSIDE the directory must not stop
// it, or the migration could never run on any real machine.
func TestMambaforgeReclaimsTheDirectory(t *testing.T) {
	f := newFixture(t)
	dir := f.mambaforgeMachine(t)
	keep := filepath.Join(f.home, ".config", "starship.toml")
	writeFile(t, keep, "# machine-local\n")

	system := filepath.Join(t.TempDir(), "bin")
	for _, tool := range mambaforgeGuarded {
		writeExecutable(t, filepath.Join(system, tool))
	}
	onlyPATH(t, system)

	mustRun(t, f.ctx(), "mambaforge")

	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("%s is still there (%v); nothing was reclaimed", dir, err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the migration took %s with it: %v", keep, err)
	}
}

// The guard, one case per tool. Deleting 3.5 GB out from under a live toolchain
// is the failure it exists for, and the tool must be NAMED: "refusing to remove
// ~/sdk/mambaforge" with no reason is a dead end for whoever reads it.
//
// Each subtest puts exactly one tool inside the directory, so the refusal names
// the tool the case is about rather than whichever the loop happened to reach
// first.
func TestMambaforgeRefusesWhileAGuardedToolResolvesInsideIt(t *testing.T) {
	for _, tool := range mambaforgeGuarded {
		t.Run(tool, func(t *testing.T) {
			f := newFixture(t)
			dir := f.mambaforgeMachine(t)
			live := filepath.Join(dir, "bin", tool)
			writeExecutable(t, live)
			onlyPATH(t, filepath.Join(dir, "bin"))
			before := tree(t, f.home)

			err := named(t, "mambaforge").Run(f.ctx())
			var refusal *change.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
			}
			for _, want := range []string{tool, live, dir} {
				if !strings.Contains(refusal.Error(), want) {
					t.Errorf("the refusal must name %s:\n%s", want, refusal.Error())
				}
			}
			if refusal.Remediation == "" {
				t.Error("a refusal must name its remediation")
			}
			// The whole point. A guard that refused and deleted anyway would be
			// worse than no guard, because the message would say it had not.
			if after := tree(t, f.home); after != before {
				t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s",
					before, after)
			}
		})
	}
}

// exec.LookPath does not resolve symlinks: it answers with the entry it found on
// PATH. So a ~/bin/python pointing into the installation reads as a path outside
// it, and a guard comparing LookPath's answer directly would let the removal
// proceed against a toolchain that is live -- and break the link as well.
func TestMambaforgeRefusesThroughASymlinkOnPATH(t *testing.T) {
	f := newFixture(t)
	dir := f.mambaforgeMachine(t)
	inside := filepath.Join(dir, "bin", "python3")
	writeExecutable(t, inside)

	bin := filepath.Join(f.home, "bin")
	symlink(t, inside, filepath.Join(bin, "python3"))
	onlyPATH(t, bin)
	before := tree(t, f.home)

	err := named(t, "mambaforge").Run(f.ctx())
	var refusal *change.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
	}
	if !strings.Contains(refusal.Error(), inside) {
		t.Errorf("the refusal must name where the tool actually resolves:\n%s", refusal.Error())
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A PATH entry whose DIRECTORY component is a symlink into the installation.
//
// Lstat follows all but the final component, so Lstat("$HOME/condabin/python3")
// with condabin -> <install>/bin reports a plain regular file: IsLink is false.
// A guard that followed links from the leaf only stops on hop zero and lets the
// removal proceed against a live interpreter. Measured on darwin.
func TestMambaforgeRefusesThroughASymlinkedPATHDirectory(t *testing.T) {
	f := newFixture(t)
	dir := f.mambaforgeMachine(t)
	inside := filepath.Join(dir, "bin", "python3")
	writeExecutable(t, inside)

	// The shape conda's own installer writes: a stable name in $HOME pointing at
	// whichever installation is current.
	condabin := filepath.Join(f.home, "condabin")
	symlink(t, filepath.Join(dir, "bin"), condabin)
	onlyPATH(t, condabin)
	before := tree(t, f.home)

	err := named(t, "mambaforge").Run(f.ctx())
	var refusal *change.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
	}
	if !strings.Contains(refusal.Error(), inside) {
		t.Errorf("the refusal must name where the tool actually resolves:\n%s", refusal.Error())
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The mirror image: the INSTALLATION is reached through a symlinked ancestor,
// while PATH names the other route. The two spellings share no prefix at all, so
// no amount of resolving the tool alone would find the overlap -- the target
// directory has to be resolved too.
//
// This is the dangerous direction. ~/sdk/mambaforge is still pending (Lstat
// follows the symlinked ancestor and reports a real directory), so RemoveAll
// would delete the real installation straight through the link.
func TestMambaforgeRefusesThroughASymlinkedAncestor(t *testing.T) {
	f := newFixture(t)
	// The real installation, somewhere else entirely.
	elsewhere := filepath.Join(t.TempDir(), "volume", "sdk")
	install := filepath.Join(elsewhere, "mambaforge")
	writeFile(t, filepath.Join(install, "conda-meta", "history"), "# created 2024-05\n")
	inside := filepath.Join(install, "bin", "conda")
	writeExecutable(t, inside)
	// ~/sdk is the symlink, so ~/sdk/mambaforge is the migration's target and the
	// installation at the same time.
	symlink(t, elsewhere, filepath.Join(f.home, "sdk"))
	// PATH names the other route, which shares no prefix with ~/sdk/mambaforge.
	onlyPATH(t, filepath.Join(install, "bin"))

	if !isPending(t, f.ctx(), "mambaforge") {
		t.Fatal("~/sdk/mambaforge must still be pending here, or this case is not " +
			"exercising the dangerous shape at all")
	}

	err := named(t, "mambaforge").Run(f.ctx())
	var refusal *change.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
	}
	if !strings.Contains(refusal.Error(), inside) {
		t.Errorf("the refusal must name the live tool:\n%s", refusal.Error())
	}
	if _, err := os.Stat(filepath.Join(install, "conda-meta", "history")); err != nil {
		t.Errorf("the reclamation deleted the installation through the symlink: %v", err)
	}
}

// A path the guard cannot resolve must REFUSE, not proceed. "I gave up" read as
// "nothing is in use" fails in the one direction that costs 3.5 GB, and the user
// pays one message for the other direction.
//
// Both shapes go through a double rather than the filesystem, and deliberately:
// the kernel's own MAXSYMLINKS is 32 on darwin, so a chain long enough to exhaust
// the budget makes exec.LookPath itself fail before the resolver is ever reached.
// A relative PATH entry would need the test to chdir. Neither is reachable from a
// fixture, and an unreachable branch in this guard is one nobody has checked.
func TestMambaforgeRefusesWhatItCannotResolve(t *testing.T) {
	for _, tc := range []struct {
		name  string
		at    string
		loop  bool
		wants string
	}{
		{"a symlink loop", "/nowhere/bin/python3", true, "too many symbolic links"},
		{"a relative PATH entry", "relbin/python3", false, "relative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			dir := f.mambaforgeMachine(t)
			onlyPATH(t, t.TempDir())
			c := f.ctx()
			c.Change = lookPathAt{
				Interface: change.NewApplier(&f.out, f.root),
				tool:      "python3", at: tc.at, loop: tc.loop,
			}
			before := tree(t, f.home)

			err := named(t, "mambaforge").Run(c)
			var refusal *change.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
			}
			if !strings.Contains(refusal.Error(), tc.wants) {
				t.Errorf("the refusal must say why it could not decide (%q):\n%s",
					tc.wants, refusal.Error())
			}
			if _, err := os.Stat(filepath.Join(dir, "conda-meta", "history")); err != nil {
				t.Errorf("an undecidable guard deleted 3.5 GB anyway: %v", err)
			}
			if after := tree(t, f.home); after != before {
				t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s",
					before, after)
			}
		})
	}
}

// The third fail-closed route: the INSTALLATION's own path cannot be resolved.
//
// One failure there makes every comparison below it untrustworthy, so it must
// refuse before any tool is looked up at all. It had no case until the reviewer
// pointed out that replacing the branch with `resolvedDir, _ := resolvePath(...)`
// left the suite green -- an unexercised branch in front of an irrecoverable
// delete, presented as held.
//
// ~/sdk is made a link to itself while ~/sdk/mambaforge stays a real directory,
// so the migration is still pending and the refusal is about resolution rather
// than about the shape of the target.
func TestMambaforgeRefusesWhenItsOwnPathCannotBeResolved(t *testing.T) {
	f := newFixture(t)
	dir := f.mambaforgeMachine(t)
	onlyPATH(t, t.TempDir())
	// The double must name the path in its RESOLVED spelling. By the time the
	// walk reaches this component it has already rewritten the prefix -- on
	// darwin t.TempDir() lives under /var, which is itself a link to /private/var
	// -- so a double keyed on the raw path never matches and the case passes
	// while exercising nothing. It did exactly that on the first run.
	resolvedHome, err := filepath.EvalSymlinks(f.home)
	if err != nil {
		t.Fatal(err)
	}
	c := f.ctx()
	c.Change = loopingComponent{
		Interface: change.NewApplier(&f.out, f.root),
		dir:       filepath.Join(resolvedHome, "sdk"),
	}
	before := tree(t, f.home)

	if pending, err := named(t, "mambaforge").Pending(c.Query()); err != nil || !pending {
		t.Fatalf("the migration must still be pending here (%v, %v), or the refusal "+
			"below would be about the target's shape rather than about resolution",
			pending, err)
	}

	err = named(t, "mambaforge").Run(c)
	var refusal *change.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
	}
	if !strings.Contains(refusal.Error(), "cannot be resolved") {
		t.Errorf("the refusal must say it could not decide:\n%s", refusal.Error())
	}
	if _, err := os.Stat(filepath.Join(dir, "conda-meta", "history")); err != nil {
		t.Errorf("an undecidable guard deleted 3.5 GB anyway: %v", err)
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A sibling whose name merely BEGINS with the target's is not inside it.
// strings.HasPrefix without the separator says otherwise, and the machine then
// keeps 3.5 GB forever with a refusal naming a directory that has nothing to do
// with it.
func TestMambaforgeIgnoresASimilarlyNamedSibling(t *testing.T) {
	f := newFixture(t)
	dir := f.mambaforgeMachine(t)
	sibling := filepath.Join(f.home, "sdk", "mambaforge-old", "bin")
	writeExecutable(t, filepath.Join(sibling, "conda"))
	onlyPATH(t, sibling)

	mustRun(t, f.ctx(), "mambaforge")

	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("%s survived (%v); a sibling directory blocked the reclamation", dir, err)
	}
	if _, err := os.Stat(filepath.Join(sibling, "conda")); err != nil {
		t.Errorf("the migration removed the sibling's conda: %v", err)
	}
}

// Named on a machine that has nothing to reclaim: a no-op that succeeds, so
// `./bootstrap migrate mambaforge` twice is not an error the second time.
func TestMambaforgeOnAReclaimedMachineSaysSo(t *testing.T) {
	f := newFixture(t)
	onlyPATH(t, t.TempDir())
	mustRun(t, f.ctx(), "mambaforge")
	if !strings.Contains(f.out.String(), "already reclaimed") {
		t.Errorf("a reclamation with nothing to do must say so:\n%s", f.out.String())
	}
}

// Anything at that path which is not a real directory is refused rather than
// removed. Both halves of the branch are covered: RemoveAll on the symlink would
// take the LINK, reclaim nothing, and report success -- a silent wrong outcome on
// the one verb in this design whose job is destroying something -- and a regular
// file there is simply not the installation this reclaims.
func TestMambaforgeRefusesAPathThatIsNotADirectory(t *testing.T) {
	path := func(f *fixture) string { return filepath.Join(f.home, "sdk", "mambaforge") }

	t.Run("a symlink", func(t *testing.T) {
		f := newFixture(t)
		elsewhere := filepath.Join(t.TempDir(), "mambaforge")
		writeFile(t, filepath.Join(elsewhere, "conda-meta", "history"), "# created 2024-05\n")
		symlink(t, elsewhere, path(f))
		onlyPATH(t, t.TempDir())
		before := tree(t, f.home)

		err := named(t, "mambaforge").Run(f.ctx())
		var refusal *change.Refusal
		if !errors.As(err, &refusal) {
			t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
		}
		if !strings.Contains(refusal.Error(), path(f)) {
			t.Errorf("the refusal must name the path:\n%s", refusal.Error())
		}
		if after := tree(t, f.home); after != before {
			t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
		if _, err := os.Stat(filepath.Join(elsewhere, "conda-meta", "history")); err != nil {
			t.Errorf("the migration followed the symlink and destroyed what it "+
				"pointed at: %v", err)
		}
	})

	// The half the self-review reworded the message for, and which nothing was
	// asserting.
	t.Run("a regular file", func(t *testing.T) {
		f := newFixture(t)
		writeFile(t, path(f), "not an installation\n")
		onlyPATH(t, t.TempDir())
		before := tree(t, f.home)

		err := named(t, "mambaforge").Run(f.ctx())
		var refusal *change.Refusal
		if !errors.As(err, &refusal) {
			t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
		}
		if !strings.Contains(refusal.Error(), path(f)) {
			t.Errorf("the refusal must name the path:\n%s", refusal.Error())
		}
		if refusal.Remediation == "" {
			t.Error("a refusal must name its remediation")
		}
		if after := tree(t, f.home); after != before {
			t.Errorf("a refused reclamation changed the home:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})
}

// The rule, over the REAL migration rather than the fake Task 9 wired the
// mechanism with: a bare migrate lists it, with the exact command, and deletes
// nothing.
func TestBareMigrateListsMambaforgeAndDeletesNothing(t *testing.T) {
	f := newFixture(t)
	dir := f.mambaforgeMachine(t)
	// A PATH with nothing on it, so the listing cannot be mistaken for the guard
	// quietly declining to run: only the kind may stop this.
	onlyPATH(t, t.TempDir())
	before := tree(t, f.home)

	if err := Run(f.ctx(), ""); err != nil {
		t.Fatalf("bare migrate: %v", err)
	}
	if !strings.Contains(f.out.String(), "./bootstrap migrate mambaforge") {
		t.Errorf("a bare migrate must name the exact command:\n%s", f.out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "conda-meta", "history")); err != nil {
		t.Fatalf("a bare migrate destroyed untracked data: %v", err)
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("a bare migrate changed the home:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The INPUT preflight sees on a machine whose only pending migration is the
// reclamation: something reclaiming due, nothing reconciling.
//
// This states the fact, not the behaviour. It cannot call Preflight -- this
// package cannot import internal/phase, which imports it -- so what preflight
// then DOES with that input is pinned where it belongs, by
// phase.TestPreflightAllowsAPendingReclamation. Read together they say: this
// shape occurs, and preflight passes it.
func TestAMambaforgeMachineHasNothingReconcilingPending(t *testing.T) {
	f := newFixture(t)
	f.mambaforgeMachine(t)

	due, err := Pending(f.ctx().Query())
	if err != nil {
		t.Fatal(err)
	}
	if len(Names(due, Reclaiming)) != 1 {
		t.Fatalf("mambaforge is not pending on this fixture, so this case proves nothing: %v", due)
	}
	if names := Names(due, Reconciling); len(names) != 0 {
		t.Errorf("preflight would refuse this machine over %v; nothing reconciling is due",
			names)
	}
}

// lookPathAt is an Applier that reports one guarded tool at a chosen path, and
// optionally makes that path a symlink to itself.
//
// It exists for the two shapes a fixture cannot produce. The kernel's own
// MAXSYMLINKS is 32 on darwin and 40 on Linux, so a chain long enough to exhaust
// the resolver's budget makes exec.LookPath fail first and the branch is never
// entered; and a relative PATH entry would need the case to chdir the whole test
// binary. Both branches decide whether an irrecoverable delete proceeds, so
// neither may go unexercised.
type lookPathAt struct {
	change.Interface
	tool string
	at   string
	loop bool
}

func (l lookPathAt) LookPath(name string) (string, error) {
	if name == l.tool {
		return l.at, nil
	}
	return "", errNotOnPATH
}

func (l lookPathAt) Lstat(path string) (change.FileInfo, error) {
	if l.loop && path == l.at {
		return change.FileInfo{Exists: true, IsLink: true}, nil
	}
	return l.Interface.Lstat(path)
}

func (l lookPathAt) Readlink(path string) (string, error) {
	if l.loop && path == l.at {
		return l.at, nil
	}
	return l.Interface.Readlink(path)
}

var errNotOnPATH = errors.New("not on PATH")

// loopingComponent is an Applier that reports one DIRECTORY as a symlink to
// itself while everything under it stays real, so a case can make the
// installation's own path unresolvable without changing what Lstat says about
// the installation. The filesystem cannot be made to hold that combination.
type loopingComponent struct {
	change.Interface
	dir string
}

func (l loopingComponent) Lstat(path string) (change.FileInfo, error) {
	if path == l.dir {
		return change.FileInfo{Exists: true, IsLink: true}, nil
	}
	return l.Interface.Lstat(path)
}

func (l loopingComponent) Readlink(path string) (string, error) {
	if path == l.dir {
		return l.dir, nil
	}
	return l.Interface.Readlink(path)
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
