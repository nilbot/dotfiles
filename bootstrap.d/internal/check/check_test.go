package check_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/check"
	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
)

// The manifest every case is checked against, unless it supplies its own. One
// row of each kind, so a case can break exactly one and leave the others sound.
const testManifest = `
link    tmux/tmux.conf                 .tmux.conf     *
seed    git/gitconfig.local.template   .gitconfig     *
dir     -                              .config/fish   *
`

const manifestPath = "/repo/bootstrap.d/links.manifest"

// healthy is a machine on which every check passes: every manifest row present
// and of its declared kind, both silent-failure guards intact, fish the login
// shell, agents on PATH, a Brewfile in the checkout.
//
// Cases break one thing about it and assert on that one check, which is what
// keeps them from passing for the wrong reason.
func healthy() *fakeChange {
	return &fakeChange{
		info: map[string]change.FileInfo{
			"/home/.tmux.conf":           {Exists: true, IsLink: true},
			"/home/.gitconfig":           {Exists: true, IsRegular: true},
			"/home/.config/fish":         {Exists: true, IsDir: true},
			"/repo/bootstrap.d/Brewfile": {Exists: true, IsRegular: true},
			// The two files the stubs below point at. Both guards resolve what
			// they matched, so a healthy machine is one where those paths are
			// actually there -- not merely named.
			"/home/dotfiles/fish/config.fish":     {Exists: true, IsRegular: true},
			"/home/dotfiles/git/gitconfig.shared": {Exists: true, IsRegular: true},
		},
		links: map[string]string{"/home/.tmux.conf": "/repo/tmux/tmux.conf"},
		files: map[string][]byte{
			manifestPath:                     []byte(testManifest),
			"/home/.config/fish/config.fish": []byte("# stub\nsource $HOME/dotfiles/fish/config.fish\n"),
			"/home/.gitconfig":               []byte("[include]\n\tpath = ~/dotfiles/git/gitconfig.shared\n"),
			"/etc/shells":                    []byte("/bin/sh\n/opt/homebrew/bin/fish\n"),
		},
	}
}

func ctx(fake *fakeChange, profile string) check.Context {
	return check.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: profile, Shell: "/opt/homebrew/bin/fish",
	}
}

// all runs every check and fails the case on an unexpected error. All's error
// reports one thing only -- a manifest that does not parse -- so any case that
// is not about that must not see one. The two that are call check.All directly.
func all(t *testing.T, c check.Context) []check.Result {
	t.Helper()
	results, err := check.All(c)
	if err != nil {
		t.Fatalf("unexpected error from All: %v", err)
	}
	return results
}

// find returns the one result with this name. A missing name is fatal: a case
// that silently checked nothing is worse than one that fails.
func find(t *testing.T, results []check.Result, name string) check.Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no check named %q in %v", name, names(results))
	return check.Result{}
}

func names(results []check.Result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Name)
	}
	return out
}

// assertStatus asserts on Name and Status only. Detail is prose for a human and
// Write's column layout is not a contract; pinning either would make every
// wording change a test failure.
func assertStatus(t *testing.T, results []check.Result, name string, want check.Status) check.Result {
	t.Helper()
	got := find(t, results, name)
	if got.Status != want {
		t.Errorf("%s = %s (%s), want %s", name, got.Status, got.Detail, want)
	}
	return got
}

func TestAllReportsTheEightChecks(t *testing.T) {
	got := strings.Join(names(all(t, ctx(healthy(), "workstation"))), ",")
	want := "platform,manifest-owners,manifest-kinds,fish-source,gitconfig-include," +
		"login-shell,agents,packages"
	if got != want {
		t.Errorf("checks = %s, want %s", got, want)
	}
}

func TestAHealthyMachinePassesEveryCheck(t *testing.T) {
	results := all(t, ctx(healthy(), "workstation"))
	for _, r := range results {
		if r.Status != check.OK {
			t.Errorf("%s = %s (%s), want ok", r.Name, r.Status, r.Detail)
		}
	}
	if code := check.ExitCode(results); code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
}

// Rule 1: the three machine-wide checks cover state the dotfiles profile
// deliberately does not manage. Reporting them as failures would make every
// container run report three problems that are not problems.
func TestMachineChecksAreNotApplicableUnderDotfiles(t *testing.T) {
	// A machine on which all three genuinely fail, so the n/a comes from the
	// profile and not from the machine happening to be healthy.
	fake := healthy()
	fake.lookPathErr = map[string]bool{"agents": true, "brew": true}
	delete(fake.info, "/repo/bootstrap.d/Brewfile")
	c := ctx(fake, "dotfiles")
	c.Shell = "/bin/zsh"

	results := all(t, c)
	for _, name := range []string{"login-shell", "agents", "packages"} {
		assertStatus(t, results, name, check.NA)
	}
	if code := check.ExitCode(results); code != 0 {
		t.Errorf("exit %d; n/a must never affect the exit code", code)
	}
}

// The other half of rule 1, and the half that regresses silently: under
// workstation the same three must produce a real verdict. Without this case a
// check that always reported n/a would pass the case above.
func TestMachineChecksReportRealVerdictsUnderWorkstation(t *testing.T) {
	fake := healthy()
	fake.lookPathErr = map[string]bool{"agents": true, "brew": true}
	delete(fake.info, "/repo/bootstrap.d/Brewfile")
	c := ctx(fake, "workstation")
	c.Shell = "/bin/zsh"

	results := all(t, c)
	for _, name := range []string{"login-shell", "agents", "packages"} {
		assertStatus(t, results, name, check.Fail)
	}
	if code := check.ExitCode(results); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

func TestManifestKindsFailsWhenALinkTargetIsARegularFile(t *testing.T) {
	fake := healthy()
	fake.info["/home/.tmux.conf"] = change.FileInfo{Exists: true, IsRegular: true}

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "manifest-kinds", check.Fail)
	if !strings.Contains(got.Detail, ".tmux.conf") {
		t.Errorf("the finding must name the row it is about: %s", got.Detail)
	}
}

func TestManifestKindsFailsWhenASeedTargetIsASymlink(t *testing.T) {
	fake := healthy()
	fake.info["/home/.gitconfig"] = change.FileInfo{Exists: true, IsLink: true}

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "manifest-kinds", check.Fail)
	if !strings.Contains(got.Detail, ".gitconfig") {
		t.Errorf("the finding must name the row it is about: %s", got.Detail)
	}
}

func TestManifestKindsFailsWhenADirTargetIsASymlink(t *testing.T) {
	// The failure §7 exists to end: ~/.config/fish a symlink into the repo.
	fake := healthy()
	fake.info["/home/.config/fish"] = change.FileInfo{Exists: true, IsLink: true}

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "manifest-kinds", check.Fail)
	if !strings.Contains(got.Detail, ".config/fish") {
		t.Errorf("the finding must name the row it is about: %s", got.Detail)
	}
}

func TestManifestKindsFailsWhenALinkPointsSomewhereElse(t *testing.T) {
	fake := healthy()
	fake.links["/home/.tmux.conf"] = "/somewhere/else/tmux.conf"

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "manifest-kinds", check.Fail)
	if !strings.Contains(got.Detail, "/somewhere/else/tmux.conf") {
		t.Errorf("the finding must name where the link actually points: %s", got.Detail)
	}
}

func TestManifestKindsFailsWhenATargetIsMissing(t *testing.T) {
	fake := healthy()
	delete(fake.info, "/home/.tmux.conf")

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "manifest-kinds", check.Fail)
	if !strings.Contains(got.Detail, ".tmux.conf") {
		t.Errorf("the finding must name the missing row: %s", got.Detail)
	}
}

func TestManifestOwnersFailsWhenTwoRowsClaimOnePath(t *testing.T) {
	fake := healthy()
	fake.files[manifestPath] = []byte(testManifest +
		"link    starship.toml   .tmux.conf   *\n")

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "manifest-owners", check.Fail)
	if !strings.Contains(got.Detail, ".tmux.conf") {
		t.Errorf("the finding must name the contested path: %s", got.Detail)
	}
}

// A manifest that does not parse is bad INPUT, which is exit 3 everywhere else
// in this program. All surfaces it as a *manifest.SyntaxError so the check verb
// can answer 3 too; reporting it only as a Fail would make the same typo answer
// 3 to apply and 2 to check.
//
// The results are still complete and still say so, because a report that goes
// blank on the one input it cannot parse is no report.
func TestMalformedManifestIsReportedAsMalformedInput(t *testing.T) {
	fake := healthy()
	fake.files[manifestPath] = []byte("hardlink  a  b  *\n")

	results, err := check.All(ctx(fake, "dotfiles"))

	var syntax *manifest.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("All returned %v, want a *manifest.SyntaxError so check can exit 3", err)
	}
	for _, name := range []string{"manifest-owners", "manifest-kinds"} {
		got := assertStatus(t, results, name, check.Fail)
		if !strings.Contains(got.Detail, "hardlink") {
			t.Errorf("%s must name the offending kind: %s", name, got.Detail)
		}
	}
}

// A manifest that cannot be READ is not malformed input -- the machine is in a
// state bootstrap cannot work from, which is a Fail and exit 2. Without this
// case the arm above could be widened to any error and nothing would notice.
func TestAnUnreadableManifestIsNotMalformedInput(t *testing.T) {
	fake := healthy()
	fake.readErr = map[string]bool{manifestPath: true}

	results, err := check.All(ctx(fake, "dotfiles"))

	var syntax *manifest.SyntaxError
	if errors.As(err, &syntax) {
		t.Errorf("an unreadable manifest is not a syntax error: %v", err)
	}
	assertStatus(t, results, "manifest-kinds", check.Fail)
	if code := check.ExitCode(results); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

// The first silent total failure: a stub that lost its source line leaves every
// shared fish setting inactive with no error anywhere.
func TestFishSourceFailsWithoutTheSourceLine(t *testing.T) {
	fake := healthy()
	fake.files["/home/.config/fish/config.fish"] =
		[]byte("# stub\n# --- installer-managed blocks appear below this line ---\n")

	assertStatus(t, all(t, ctx(fake, "dotfiles")), "fish-source", check.Fail)
}

func TestFishSourceFailsWhenTheStubIsAbsent(t *testing.T) {
	fake := healthy()
	fake.readErr = map[string]bool{"/home/.config/fish/config.fish": true}

	assertStatus(t, all(t, ctx(fake, "dotfiles")), "fish-source", check.Fail)
}

// A source line for something else must not satisfy the check: the shared
// config is the only thing this guard is about.
func TestFishSourceFailsOnAnUnrelatedSourceLine(t *testing.T) {
	fake := healthy()
	fake.files["/home/.config/fish/config.fish"] =
		[]byte("source $HOME/.config/fish/conf.d/other.fish\n")

	assertStatus(t, all(t, ctx(fake, "dotfiles")), "fish-source", check.Fail)
}

func TestFishSourcePassesOnTheSeededStub(t *testing.T) {
	assertStatus(t, all(t, ctx(healthy(), "dotfiles")), "fish-source", check.OK)
}

// The second silent total failure, and the one §8 creates deliberately: an
// existing machine's ~/.gitconfig names the pre-rename path, which after the
// rename includes a file that is not there. Every shared git setting vanishes
// and git says nothing.
func TestGitconfigIncludeFailsOnThePreRenamePath(t *testing.T) {
	fake := healthy()
	fake.files["/home/.gitconfig"] =
		[]byte("[include]\n\tpath = ~/dotfiles/git/gitconfig.symlink\n")

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "gitconfig-include", check.Fail)
	if !strings.Contains(got.Detail, "migrate") {
		t.Errorf("the finding must name the migration that fixes it: %s", got.Detail)
	}
}

// The hole a plain substring test leaves, found by mutation and realistic
// rather than contrived: Task 8 renames the path in gitconfig.local.template's
// own header COMMENT as well as in its [include] block, so the file this check
// guards mentions the shared config twice. Delete the include and the comment
// alone would satisfy a Contains -- the guard defeated by the very file it
// guards. Only an actual `path =` setting counts.
func TestGitconfigIncludeFailsWhenOnlyACommentNamesTheSharedConfig(t *testing.T) {
	fake := healthy()
	fake.files["/home/.gitconfig"] = []byte(
		"# Put anything worth sharing in ~/dotfiles/git/gitconfig.shared.\n" +
			"[user]\n\tname = someone\n")

	assertStatus(t, all(t, ctx(fake, "dotfiles")), "gitconfig-include", check.Fail)
}

// A longer path that merely starts with the shared config's name is a different
// file. Contains cannot tell them apart.
func TestGitconfigIncludeFailsOnASuffixedVariant(t *testing.T) {
	fake := healthy()
	fake.files["/home/.gitconfig"] =
		[]byte("[include]\n\tpath = ~/dotfiles/git/gitconfig.shared.disabled\n")

	assertStatus(t, all(t, ctx(fake, "dotfiles")), "gitconfig-include", check.Fail)
}

// The spellings git accepts for the same include, none of which may be rejected.
func TestGitconfigIncludeAcceptsTheFormsGitAccepts(t *testing.T) {
	for _, line := range []string{
		"\tpath = ~/dotfiles/git/gitconfig.shared",
		"\tpath=~/dotfiles/git/gitconfig.shared",
		"\tpath = \"~/dotfiles/git/gitconfig.shared\"",
		"\tpath = ~/dotfiles/git/gitconfig.shared # the shared settings",
		// A relocated clone: the local file names the clone location, which is
		// exactly the one fact that is allowed to vary per machine.
		"\tpath = /opt/checkout/git/gitconfig.shared",
	} {
		fake := healthy()
		fake.info["/opt/checkout/git/gitconfig.shared"] =
			change.FileInfo{Exists: true, IsRegular: true}
		fake.files["/home/.gitconfig"] = []byte("[include]\n" + line + "\n")
		if got := find(t, all(t, ctx(fake, "dotfiles")), "gitconfig-include"); got.Status != check.OK {
			t.Errorf("%q = %s (%s), want ok", line, got.Status, got.Detail)
		}
	}
}

// The dry-run invariant, in the type system rather than in anyone's memory.
//
// A check runs during `plan` and holds an Applier, whose method set can create
// symlinks and elevate. Machine omits those, so a check that reached for one
// would not compile. The architecture test cannot see this -- it constrains
// imports, not method calls.
func TestMachineCannotMutate(t *testing.T) {
	// change.Interface must satisfy Machine, or every call site would need a
	// wrapper and the narrowing would not be free.
	var _ check.Machine = change.Interface(nil)

	machine := reflect.TypeOf((*check.Machine)(nil)).Elem()
	for _, method := range []string{"Dir", "Link", "Seed", "Sudo"} {
		if _, found := machine.MethodByName(method); found {
			t.Errorf("check.Machine exposes %s; a check must not be able to mutate", method)
		}
	}
	if machine.NumMethod() != 5 {
		t.Errorf("check.Machine has %d methods, want 5 (Lstat, Readlink, ReadFile, "+
			"LookPath, Run); widening it widens what a check can do during a plan",
			machine.NumMethod())
	}
}

// Matching a path string is not the same as the path being there, and the
// difference is the whole failure this guard exists for.
//
// Seed copies gitconfig.local.template verbatim -- no substitution -- so the
// hardcoded "~/dotfiles/…" it carries is wrong on every clone that is not at
// ~/dotfiles, which root() explicitly supports. git ignores a missing include
// without a word, so every shared setting goes inert while the check, matching
// only the text, reports ok. The guard defeated by the exact failure it names.
func TestGitconfigIncludeFailsWhenTheIncludedFileDoesNotExist(t *testing.T) {
	fake := healthy()
	fake.files["/home/.gitconfig"] =
		[]byte("[include]\n\tpath = ~/elsewhere/git/gitconfig.shared\n")

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "gitconfig-include", check.Fail)
	if !strings.Contains(got.Detail, "/home/elsewhere/git/gitconfig.shared") {
		t.Errorf("the finding must name the path that does not resolve: %s", got.Detail)
	}
}

// The same hole in the other guard: a stale absolute source line surviving a
// clone move leaves fish sourcing nothing. fish reports a missing source, but
// only into a shell nobody is reading at login time.
func TestFishSourceFailsWhenTheSourcedFileDoesNotExist(t *testing.T) {
	fake := healthy()
	fake.files["/home/.config/fish/config.fish"] =
		[]byte("source $HOME/elsewhere/fish/config.fish\n")

	got := assertStatus(t, all(t, ctx(fake, "dotfiles")), "fish-source", check.Fail)
	if !strings.Contains(got.Detail, "/home/elsewhere/fish/config.fish") {
		t.Errorf("the finding must name the path that does not resolve: %s", got.Detail)
	}
}

// The spellings of $HOME these two files actually use, all of which must resolve
// rather than be reported as missing.
func TestTheGuardsExpandTheHomeSpellingsTheseFilesUse(t *testing.T) {
	for _, tc := range []struct{ name, line, resolves string }{
		{"fish-source", "source $HOME/dotfiles/fish/config.fish", "/home/dotfiles/fish/config.fish"},
		{"fish-source", "source ${HOME}/dotfiles/fish/config.fish", "/home/dotfiles/fish/config.fish"},
		{"fish-source", "source /abs/checkout/fish/config.fish", "/abs/checkout/fish/config.fish"},
		{"gitconfig-include", "\tpath = ~/dotfiles/git/gitconfig.shared", "/home/dotfiles/git/gitconfig.shared"},
		{"gitconfig-include", "\tpath = /abs/checkout/git/gitconfig.shared", "/abs/checkout/git/gitconfig.shared"},
	} {
		fake := healthy()
		fake.info[tc.resolves] = change.FileInfo{Exists: true, IsRegular: true}
		switch tc.name {
		case "fish-source":
			fake.files["/home/.config/fish/config.fish"] = []byte(tc.line + "\n")
		default:
			fake.files["/home/.gitconfig"] = []byte("[include]\n" + tc.line + "\n")
		}
		if got := find(t, all(t, ctx(fake, "dotfiles")), tc.name); got.Status != check.OK {
			t.Errorf("%q = %s (%s), want ok", tc.line, got.Status, got.Detail)
		}
	}
}

func TestGitconfigIncludeFailsWhenTheFileIsAbsent(t *testing.T) {
	fake := healthy()
	fake.readErr = map[string]bool{"/home/.gitconfig": true}

	assertStatus(t, all(t, ctx(fake, "dotfiles")), "gitconfig-include", check.Fail)
}

func TestGitconfigIncludePassesOnTheSharedPath(t *testing.T) {
	assertStatus(t, all(t, ctx(healthy(), "dotfiles")), "gitconfig-include", check.OK)
}

// Rule 2: between this task and Task 12 the Brewfile genuinely does not exist.
// Handing a missing path to `brew bundle check` produces a confusing error where
// the honest answer is that the phase which creates it has not run.
func TestPackagesFailsWhenTheBrewfileIsAbsent(t *testing.T) {
	fake := healthy()
	delete(fake.info, "/repo/bootstrap.d/Brewfile")

	got := assertStatus(t, all(t, ctx(fake, "workstation")), "packages", check.Fail)
	if !strings.Contains(got.Detail, "the packages phase has not run") {
		t.Errorf("the finding must say the packages phase has not run: %s", got.Detail)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "brew") {
			t.Errorf("brew must not be invoked without a Brewfile: %s", op)
		}
	}
}

func TestPackagesFailsWhenBrewBundleCheckFails(t *testing.T) {
	fake := healthy()
	fake.runErr = map[string]bool{"brew": true}

	assertStatus(t, all(t, ctx(fake, "workstation")), "packages", check.Fail)
}

func TestLoginShellFailsWhenTheShellIsNotFish(t *testing.T) {
	c := ctx(healthy(), "workstation")
	c.Shell = "/bin/zsh"

	got := assertStatus(t, all(t, c), "login-shell", check.Fail)
	if !strings.Contains(got.Detail, "/bin/zsh") {
		t.Errorf("the finding must name the shell it found: %s", got.Detail)
	}
}

// Ends in "fish" is not the same as "is fish": a path ending in the letters
// would otherwise pass.
func TestLoginShellFailsOnAShellMerelyEndingInFish(t *testing.T) {
	c := ctx(healthy(), "workstation")
	c.Shell = "/usr/local/bin/notfish"

	assertStatus(t, all(t, c), "login-shell", check.Fail)
}

func TestLoginShellFailsWhenFishIsNotInEtcShells(t *testing.T) {
	fake := healthy()
	fake.files["/etc/shells"] = []byte("/bin/sh\n/bin/zsh\n")

	got := assertStatus(t, all(t, ctx(fake, "workstation")), "login-shell", check.Fail)
	if !strings.Contains(got.Detail, "/etc/shells") {
		t.Errorf("the finding must name the file that is missing the entry: %s", got.Detail)
	}
}

func TestLoginShellFailsWhenShellIsUnset(t *testing.T) {
	c := ctx(healthy(), "workstation")
	c.Shell = ""

	assertStatus(t, all(t, c), "login-shell", check.Fail)
}

func TestAgentsFailsWhenNotOnPath(t *testing.T) {
	fake := healthy()
	fake.lookPathErr = map[string]bool{"agents": true}

	assertStatus(t, all(t, ctx(fake, "workstation")), "agents", check.Fail)
}

func TestPlatformReportsTheDetectedPlatform(t *testing.T) {
	got := assertStatus(t, all(t, ctx(healthy(), "dotfiles")), "platform", check.OK)
	if !strings.Contains(got.Detail, "darwin") {
		t.Errorf("the platform check must report what it detected: %s", got.Detail)
	}
}

func TestPlatformFailsOnAnUnsupportedOperatingSystem(t *testing.T) {
	c := ctx(healthy(), "dotfiles")
	c.Platform = "plan9"

	assertStatus(t, all(t, c), "platform", check.Fail)
}

func TestExitCodeMapsToTheSharedTable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []check.Result
		want    int
	}{
		{"a failure blocks", []check.Result{
			{Status: check.OK, Name: "a"}, {Status: check.Warn, Name: "b"},
			{Status: check.Fail, Name: "c"},
		}, 2},
		{"a warning is advisory", []check.Result{
			{Status: check.OK, Name: "a"}, {Status: check.Warn, Name: "b"},
			{Status: check.NA, Name: "c"},
		}, 1},
		{"ok and n/a are healthy", []check.Result{
			{Status: check.OK, Name: "a"}, {Status: check.NA, Name: "b"},
		}, 0},
		{"nothing checked is healthy", nil, 0},
	} {
		if got := check.ExitCode(tc.results); got != tc.want {
			t.Errorf("%s: exit %d, want %d", tc.name, got, tc.want)
		}
	}
}

// Write is for a human, so only the two stable parts are asserted -- every
// check's name and its status. Column layout is deliberately not a contract.
func TestWriteReportsEveryCheckAndItsStatus(t *testing.T) {
	var out bytes.Buffer
	results := all(t, ctx(healthy(), "dotfiles"))
	check.Write(&out, results)

	for _, r := range results {
		var found bool
		for _, line := range strings.Split(out.String(), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == string(r.Status) && fields[1] == r.Name {
				found = true
			}
		}
		if !found {
			t.Errorf("Write omits %s %s:\n%s", r.Status, r.Name, out.String())
		}
	}
}

// fakeChange satisfies change.Interface with no I/O at all. Copied from
// internal/phase's test rather than shared: forty lines duplicated across two
// test packages is cheaper than a testing package the production build carries.
type fakeChange struct {
	info        map[string]change.FileInfo
	links       map[string]string
	files       map[string][]byte
	readErr     map[string]bool // paths ReadFile reports as unreadable
	lookPathErr map[string]bool
	runErr      map[string]bool // commands Run reports as failing
	Ops         []string
}

func (f *fakeChange) Lstat(p string) (change.FileInfo, error) { return f.info[p], nil }
func (f *fakeChange) Readlink(p string) (string, error)       { return f.links[p], nil }
func (f *fakeChange) ReadFile(p string) ([]byte, error) {
	if f.readErr[p] {
		return nil, errNotFound
	}
	data, ok := f.files[p]
	if !ok {
		return nil, errNotFound
	}
	return data, nil
}

func (f *fakeChange) LookPath(n string) (string, error) {
	if f.lookPathErr[n] {
		return "", errNotFound
	}
	return "/usr/bin/" + n, nil
}
func (f *fakeChange) Dir(p string) error { f.Ops = append(f.Ops, "dir "+p); return nil }
func (f *fakeChange) Link(s, t string) error {
	f.Ops = append(f.Ops, "link "+t+" -> "+s)
	return nil
}
func (f *fakeChange) Seed(s, t string) error {
	f.Ops = append(f.Ops, "seed "+t+" from "+s)
	return nil
}
func (f *fakeChange) Run(n string, a ...string) error {
	f.Ops = append(f.Ops, "run "+n+" "+strings.Join(a, " "))
	if f.runErr[n] {
		return errNotFound
	}
	return nil
}
func (f *fakeChange) Sudo(n string, a ...string) error {
	f.Ops = append(f.Ops, "sudo "+n+" "+strings.Join(a, " "))
	return nil
}

var errNotFound = errors.New("not found")
