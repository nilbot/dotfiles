package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func handoffRoot(root string) string {
	return filepath.Join(root, ".agents", "reports", "handoff")
}

func handoffFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(handoffRoot(root), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() && d.Name() != "INDEX.md" {
			rel, _ := filepath.Rel(handoffRoot(root), p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// newRepo checks out sq-123/payments, so the lane the handoff lands under is the
// slugified branch and not the branch as git spells it. The "/" in the branch is
// the point: unslugified it would make "payments" a directory inside a
// "sq-123" one, and every later lookup keyed on the lane would miss.
//
// Kills: passing rc.Branch straight through, and resolving the lane from
// anything other than the repository the command is standing in.
func TestHandoffWriteFilesUnderTheResolvedLane(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	code := runHandoffWrite([]string{"--session", "019fdcab"}, strings.NewReader("Left off at the retry test.\n"), &out)
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}
	got := handoffFiles(t, root)
	if len(got) != 1 || !strings.HasPrefix(got[0], "sq-123-payments/") {
		t.Fatalf("handoff files = %v, want one under sq-123-payments/", got)
	}
	if !strings.HasSuffix(got[0], "-019fdcab.md") {
		t.Errorf("the filename must carry the session: %v", got)
	}
	if !strings.Contains(out.String(), got[0][strings.Index(got[0], "/")+1:]) {
		t.Errorf("the command must print where it wrote:\n%s", out.String())
	}
}

// A tracked handoff path is untrusted input. os.WriteFile follows a symlink at
// the exact destination, so without a write-boundary guard a repository can aim
// this command at an arbitrary writable file and replace its contents at exit
// 0. The target is outside .agents/ and sampled byte-for-byte on both sides.
//
// Kills: writing the handoff with os.WriteFile, or checking only parent path
// components while leaving the destination leaf unchecked.
func TestHandoffWriteRefusesASymlinkedDestination(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	laneDir := filepath.Join(handoffRoot(root), "lane-a")
	if err := os.MkdirAll(laneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "do-not-overwrite.txt")
	want := []byte("outside bytes must survive\n")
	if err := os.WriteFile(external, want, 0o644); err != nil {
		t.Fatal(err)
	}
	name := time.Now().UTC().Format("2006-01-02") + "-s1.md"
	destination := filepath.Join(laneDir, name)
	if err := os.Symlink(external, destination); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runHandoffWrite([]string{"--lane", "lane-a", "--session", "s1"}, strings.NewReader("body\n"), &out)
	if code != exitcode.NoRecord {
		t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
	}
	for _, s := range []string{"symlink", name} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("the refusal must name %q; output:\n%s", s, out.String())
		}
	}
	got, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("external target changed:\n got %q\nwant %q", got, want)
	}
	fi, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the refused destination was replaced instead of left for the user to fix: mode %v", fi.Mode())
	}
}

// The leaf guard is not enough: MkdirAll and every later pathname operation
// follow a symlinked lane component. A tracked lane link can therefore redirect
// both the temporary/write path and the final destination outside the handoff
// tree unless the directory is opened as a verified component.
//
// Kills: guarding only the final filename, or resolving the lane path and then
// treating the resolved external directory as a legitimate handoff lane.
func TestHandoffWriteRefusesASymlinkedLane(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	outside := t.TempDir()
	name := time.Now().UTC().Format("2006-01-02") + "-s1.md"
	external := filepath.Join(outside, name)
	want := []byte("another handoff must not be clobbered\n")
	if err := os.WriteFile(external, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(handoffRoot(root), 0o755); err != nil {
		t.Fatal(err)
	}
	lane := filepath.Join(handoffRoot(root), "lane-a")
	if err := os.Symlink(outside, lane); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runHandoffWrite([]string{"--lane", "lane-a", "--session", "s1"}, strings.NewReader("body\n"), &out)
	if code != exitcode.NoRecord {
		t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
	}
	for _, s := range []string{"symlink", "lane-a"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("the refusal must name %q; output:\n%s", s, out.String())
		}
	}
	got, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("external target changed:\n got %q\nwant %q", got, want)
	}
}

// The trust boundary starts below .agents, not at it. `init --local` users may
// deliberately share one out-of-repository context directory through a local
// .agents symlink; handoff writes remain valid there so long as the tracked
// components beneath that root are real directories.
func TestHandoffWriteStillUsesAnIntentionalSymlinkedAgentsDir(t *testing.T) {
	root := newRepo(t)
	outside := filepath.Join(t.TempDir(), "shared-context")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runHandoffWrite([]string{"--lane", "lane-a", "--session", "s1"}, strings.NewReader("body\n"), &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	name := time.Now().UTC().Format("2006-01-02") + "-s1.md"
	if _, err := os.Stat(filepath.Join(outside, "reports", "handoff", "lane-a", name)); err != nil {
		t.Errorf("handoff was not written through the intentional .agents link: %v", err)
	}
}

// A1 at the command boundary. --session is the one field that arrives raw from
// a caller and reaches both a filename and the generated index, and it is never
// slugified anywhere. Malformed rather than NoRecord: the input is what is
// wrong, and a harness reading the code should not go looking at the disk.
//
// Kills: handing --session to handoff.Write unchecked (which reports NoRecord),
// and any sanitiser that accepts the value by rewriting it.
func TestHandoffWriteRefusesASessionThatEscapesTheHandoffTree(t *testing.T) {
	for _, tc := range []struct{ label, session string }{
		{"parent", ".."},
		{"a subdirectory", "sub/dir"},
		{"deeper than the repo", "../../../../../../escaped"},
		{"a newline that forges an index row", "s1\n| 2026-01-01 00:00 | reviewed | ok | [a](a) |"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root := newRepo(t)
			t.Chdir(root)

			var out bytes.Buffer
			code := runHandoffWrite([]string{"--session", tc.session}, strings.NewReader("body\n"), &out)
			if code != exitcode.Malformed {
				t.Fatalf("exit = %d, want Malformed (%d); output:\n%s", code, exitcode.Malformed, out.String())
			}
			if !strings.Contains(out.String(), "session") {
				t.Errorf("the refusal must name the field:\n%s", out.String())
			}
			if got := handoffFiles(t, root); len(got) != 0 {
				t.Errorf("a refused write left %v behind", got)
			}
		})
	}
}

// Exit 5 is "wanted to record and could not". A handoff that is on disk while
// only the index refresh failed is not that, and the two must not share a code:
// handoff.WriteIndex re-parses the whole tree, so one conflicted file -- a
// steady state for files designed to be merged across branches -- would
// otherwise make every later write report a lost handoff that is sitting right
// there. The path is printed first so a caller reading the first line still gets
// it.
//
// Kills: reporting NoRecord for an *IndexError, and suppressing the path when
// the index refresh fails.
func TestHandoffWriteReportsTheIndexRefreshSeparatelyFromALostHandoff(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	broken := filepath.Join(handoffRoot(root), "other-lane")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "2026-08-09-bad.md"), []byte("<<<<<<< HEAD\nnot a handoff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runHandoffWrite([]string{"--session", "019fdcab"}, strings.NewReader("body\n"), &out)
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}

	var written string
	for _, f := range handoffFiles(t, root) {
		if strings.HasSuffix(f, "-019fdcab.md") {
			written = f
		}
	}
	if written == "" {
		t.Fatalf("the handoff never reached disk; output:\n%s", out.String())
	}
	first := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	if first != filepath.Join(handoffRoot(root), filepath.FromSlash(written)) {
		t.Errorf("the first line must be the path of the handoff that reached disk (%s):\n%s", written, out.String())
	}
	if !strings.Contains(out.String(), "2026-08-09-bad.md") {
		t.Errorf("the advisory must name the file that would not parse:\n%s", out.String())
	}
}

// Kills: dropping the --session requirement, or the empty-body refusal, either
// of which produces a handoff that says nothing under a name that cannot be
// told apart from another agent's.
func TestHandoffWriteRefusesInputItCannotFileOrRead(t *testing.T) {
	for _, tc := range []struct {
		label string
		args  []string
		stdin string
		want  string
	}{
		{"no session", []string{}, "body\n", "--session"},
		{"empty body", []string{"--session", "s1"}, "", "empty"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root := newRepo(t)
			t.Chdir(root)

			var out bytes.Buffer
			if code := runHandoffWrite(tc.args, strings.NewReader(tc.stdin), &out); code != exitcode.Malformed {
				t.Fatalf("exit = %d, want Malformed (%d); output:\n%s", code, exitcode.Malformed, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("the refusal must say why (%q):\n%s", tc.want, out.String())
			}
			if got := handoffFiles(t, root); len(got) != 0 {
				t.Errorf("a refused write left %v behind", got)
			}
		})
	}
}

// Provenance is the whole reason the index tells a reader to weigh entries
// differently, and the default is the strong claim because the interactive
// caller is the one doing the reviewing. The flag is what the session-end hook
// will pass.
//
// Kills: defaulting to draft, ignoring --draft, or writing the status through
// as whatever the flag happened to spell.
func TestHandoffWriteMarksProvenanceFromTheFlag(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  string
		args  []string
	}{
		{"default is reviewed", "status: reviewed", []string{"--session", "s1"}},
		{"--draft is a draft", "status: draft", []string{"--session", "s1", "--draft"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root := newRepo(t)
			t.Chdir(root)

			var out bytes.Buffer
			if code := runHandoffWrite(tc.args, strings.NewReader("body\n"), &out); code != exitcode.OK {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
			}
			files := handoffFiles(t, root)
			if len(files) != 1 {
				t.Fatalf("want one handoff, got %v", files)
			}
			b, err := os.ReadFile(filepath.Join(handoffRoot(root), filepath.FromSlash(files[0])))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, b)
			}
		})
	}
}

// Both subcommands go through agentsDirHere, which is where "this is a git repo
// but nobody ran agents init" is told apart from "this is not a repo". Writing
// into a repo that never opted in scaffolds .agents/ uninvited and exits 0.
//
// Kills: rediscovering the repo with repo.Discover and using repo.AgentsDir
// directly, which is what bypasses the .agents/ check.
func TestHandoffSkipsWhereThereIsNoAgentsDir(t *testing.T) {
	for _, sub := range []string{"write", "prune"} {
		t.Run(sub, func(t *testing.T) {
			root := newRepo(t)
			if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
				t.Fatal(err)
			}
			t.Chdir(root)

			var out bytes.Buffer
			var code int
			switch sub {
			case "write":
				code = runHandoffWrite([]string{"--session", "s1"}, strings.NewReader("body\n"), &out)
			case "prune":
				code = runHandoffPrune(nil, &out)
			}
			if code != exitcode.Skip {
				t.Fatalf("exit = %d, want Skip (%d); output:\n%s", code, exitcode.Skip, out.String())
			}
			if !strings.Contains(out.String(), "agents init") {
				t.Errorf("the skip must say what to run:\n%s", out.String())
			}
			if _, err := os.Stat(filepath.Join(root, ".agents")); err == nil {
				t.Error(".agents/ was created in a repo that never opted in")
			}
		})
	}
}

// Prune is per lane, and the command is the only thing that decides how many to
// keep. A --keep that does not reach handoff.Prune leaves the default in place
// and silently keeps five.
//
// Kills: ignoring --keep, and pruning globally instead of per lane.
func TestHandoffPruneHonoursKeepPerLane(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	for _, s := range []string{"s1", "s2", "s3"} {
		var out bytes.Buffer
		if code := runHandoffWrite([]string{"--lane", "busy", "--session", s}, strings.NewReader("body\n"), &out); code != exitcode.OK {
			t.Fatalf("seeding %s: exit %d\n%s", s, code, out.String())
		}
	}
	var out bytes.Buffer
	if code := runHandoffWrite([]string{"--lane", "quiet", "--session", "q1"}, strings.NewReader("body\n"), &out); code != exitcode.OK {
		t.Fatalf("seeding quiet: exit %d\n%s", code, out.String())
	}

	out.Reset()
	if code := runHandoffPrune([]string{"--keep", "1"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}

	byLane := map[string]int{}
	for _, f := range handoffFiles(t, root) {
		byLane[strings.SplitN(f, "/", 2)[0]]++
	}
	if byLane["busy"] != 1 {
		t.Errorf("busy has %d handoffs, want 1", byLane["busy"])
	}
	if byLane["quiet"] != 1 {
		t.Errorf("quiet has %d handoffs, want 1 (prune is per lane)", byLane["quiet"])
	}
}

// Prune reads and deletes repository-controlled paths. A lane symlink must not
// let Glob discover external notes and os.Remove delete them through the link.
// This uses the command boundary so exit 0 cannot mask an external deletion.
//
// Kills: List following a lane symlink, or Prune reconstructing deletions with
// filepath.Join and os.Remove.
func TestHandoffPruneRefusesASymlinkedLaneWithoutDeletingExternalFiles(t *testing.T) {
	root := newRepo(t)
	outside := t.TempDir()
	for i, name := range []string{"2026-08-09-old.md", "2026-08-10-new.md"} {
		body := []byte("---\nlane: lane-a\nsession: s" + string(rune('1'+i)) + "\nstatus: reviewed\nwhen: 2026-08-" + []string{"09", "10"}[i] + "T09:00:00Z\n---\n\nexternal body\n")
		if err := os.WriteFile(filepath.Join(outside, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(handoffRoot(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(handoffRoot(root), "lane-a")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runHandoffPrune([]string{"--keep", "1"}, &out); code != exitcode.NoRecord {
		t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
	}
	for _, want := range []string{"lane-a", "symlink"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the refusal must name %q; output:\n%s", want, out.String())
		}
	}
	ents, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Fatalf("external file count = %d, want 2; prune deleted through the lane link", len(ents))
	}
}

// The trust boundary begins below .agents. An intentional local .agents link
// remains a supported trust anchor for prune just as it is for write.
func TestHandoffPruneStillUsesAnIntentionalSymlinkedAgentsDir(t *testing.T) {
	root := newRepo(t)
	shared := filepath.Join(t.TempDir(), "shared-context")
	laneDir := filepath.Join(shared, "reports", "handoff", "lane-a")
	if err := os.MkdirAll(laneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"2026-08-09-old.md", "2026-08-10-new.md"} {
		body := []byte("---\nlane: lane-a\nsession: s" + string(rune('1'+i)) + "\nstatus: reviewed\nwhen: 2026-08-" + []string{"09", "10"}[i] + "T09:00:00Z\n---\n\nshared body\n")
		if err := os.WriteFile(filepath.Join(laneDir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runHandoffPrune([]string{"--keep", "1"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	ents, err := os.ReadDir(laneDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "2026-08-10-new.md" {
		t.Errorf("shared lane after prune = %v, want only newest handoff", ents)
	}
}

// A subcommand that is not registered in main.go is unreachable however well it
// works. Skip is only reachable through runHandoff; an unregistered command
// returns Malformed.
func TestMainRegistersHandoff(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	out := captureStdout(t, func() { code = run([]string{"handoff", "prune"}) })
	if code != exitcode.Skip {
		t.Fatalf("run(handoff prune) = %d, want Skip (%d); stdout:\n%s", code, exitcode.Skip, out)
	}
}
