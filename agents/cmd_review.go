package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/memory"
	"github.com/nilbot/dotfiles/agents/internal/queue"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

// runReview is where an unreviewed draft becomes tracked knowledge, or stops
// existing.
//
// There is deliberately no --keep --all. Pending drafts are surfaced to agents,
// and an agent able to promote in bulk closes the review loop with no human in
// it, which is the whole thing this design is for. Requiring one id per
// promotion leaves an agent exactly one move -- show the drafts and ask -- so
// the selecting happens in conversation, where the context is, while the queue
// is what lets that conversation survive being interrupted for three days.
func runReview(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stdout)
	laneFlag := fs.String("lane", "", "only this lane")
	show := fs.String("show", "", "print one draft")
	keep := fs.String("keep", "", "promote one draft: write, reindex, and commit it")
	bin := fs.String("bin", "", "delete one draft")
	edit := fs.String("edit", "", "open one draft in $EDITOR")
	stats := fs.Bool("stats", false, "report whether the capture instruction is working")
	since := fs.String("since", "", "with --stats: window, e.g. 7d")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdout, usageFor("review"))
		return exitcode.Malformed
	}
	chosen := 0
	for _, f := range []string{*show, *keep, *bin, *edit} {
		if f != "" {
			chosen++
		}
	}
	if chosen > 1 {
		fmt.Fprintln(stdout, "agents review: --show, --keep, --bin and --edit are one at a time")
		return exitcode.Malformed
	}

	rc, agentsDir, store, code := storeHere(stdout)
	if code != exitcode.OK {
		return code
	}

	if *stats {
		return reviewStats(rc, store, *laneFlag, *since, stdout)
	}
	switch {
	case *show != "":
		return reviewShow(store, *show, stdout)
	case *bin != "":
		return reviewBin(store, *bin, stdout)
	case *edit != "":
		return reviewEdit(store, *edit, stdout)
	case *keep != "":
		return reviewKeep(rc, agentsDir, store, *keep, stdout)
	default:
		return reviewList(store, *laneFlag, stdout)
	}
}

func reviewList(store, lane string, stdout io.Writer) int {
	drafts, err := queue.List(store)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	shown := 0
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tWHEN\tSUBJECT")
	for _, d := range drafts {
		if lane != "" && d.Lane != lane {
			continue
		}
		shown++
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			tableCell(d.ID), tableCell(d.Kind),
			d.When.UTC().Format("2006-01-02 15:04"), tableCell(d.Subject))
	}
	_ = tw.Flush()
	if shown == 0 {
		fmt.Fprintln(stdout, "no drafts pending review")
		return exitcode.OK
	}
	// Advisory, not OK: there is something here to decide about, and a script
	// has to be able to tell "nothing pending" from "several waiting".
	fmt.Fprintf(stdout, "%d draft(s) pending; `agents review --show <id>` to read one\n", shown)
	return exitcode.Advisory
}

// reviewStats answers the question §3a exists to have answered: does an agent
// record anything when it is asked properly?
//
// The denominator is sessions that DID WORK, taken from the trace index, not
// sessions that drafted. Counting only the ones that drafted would report a
// draft rate of 100% forever and measure nothing. A session is counted as
// having worked when it produced at least one `stop` record -- the weakest
// available evidence that a turn completed, chosen because anything stronger
// (files changed, commits made) excludes the read-only debugging session that
// is exactly where the valuable conclusions come from.
func reviewStats(rc *repo.Context, store, lane, since string, stdout io.Writer) int {
	var window time.Duration
	if since != "" {
		d, err := trace.ParseSince(since)
		if err != nil {
			fmt.Fprintf(stdout, "agents review: --since %q: %v\n", since, err)
			return exitcode.Malformed
		}
		window = d
	}
	now := time.Now().UTC()
	var cutoff time.Time
	if window > 0 {
		cutoff = now.Add(-window)
	}

	// --lane slices per scenario when the experiment runs one branch per
	// scenario, which is what makes expected-versus-actual mechanical instead
	// of inferred from subject lines.
	res, err := trace.Query(store, trace.Filter{Event: "stop", Lane: lane, Since: window}, now)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	sessions := make([]string, 0, len(res.Records))
	for _, r := range res.Records {
		sessions = append(sessions, r.SessionID)
	}

	st, err := queue.SummarizeLane(store, sessions, lane, cutoff)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}

	scope := "all recorded history"
	if window > 0 {
		scope = "the last " + since
	}
	if lane != "" {
		scope += ", lane " + tableCell(lane)
	}
	fmt.Fprintf(stdout, "capture instruction, over %s\n\n", scope)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "sessions that did work\t%d\n", st.Sessions)
	fmt.Fprintf(tw, "…of those, drafted something\t%d\n", st.SessionsDrafted)
	fmt.Fprintf(tw, "draft rate\t%.0f%%\n", 100*st.DraftRate())
	fmt.Fprintf(tw, "drafts written\t%d\n", st.Drafted)
	fmt.Fprintf(tw, "…promoted\t%d\n", st.Promoted)
	fmt.Fprintf(tw, "…binned\t%d\n", st.Binned)
	fmt.Fprintf(tw, "…still pending\t%d\n", st.Pending)
	if age := st.PendingAge(time.Now()); age >= reportablePendingAge {
		fmt.Fprintf(tw, "oldest pending\t%s\n", humanAge(age))
	}
	if st.Promoted+st.Binned > 0 {
		fmt.Fprintf(tw, "promotion rate\t%.0f%%\n", 100*st.PromotionRate())
	}
	_ = tw.Flush()

	// Said before the verdict below, and independently of it, because it is a
	// different question. Everything else here grades capture; this grades
	// review, which has no trigger at all -- capture was given an instruction
	// that measurably works and review was left on the same muscle memory
	// `agents save` died of. A queue nobody drains is that failure, and it is
	// worse than forgetting to capture: the material was written and is now one
	// re-clone from gone, since the queue is untracked and machine-local.
	if age := st.PendingAge(time.Now()); age >= staleQueueAge {
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "the oldest pending draft has waited %s. Review is not happening on its\n", humanAge(age))
		fmt.Fprintln(stdout, "own, and the queue is untracked -- a re-clone takes it. Drain it with")
		fmt.Fprintln(stdout, "`agents review --keep <id>` or `--bin <id>`.")
	}

	// The reading, spelled out, because a bare rate invites the wrong
	// conclusion. The baseline is zero: twenty sessions under the previous
	// instruction produced no handoffs at all.
	fmt.Fprintln(stdout)
	switch {
	case st.Sessions == 0:
		fmt.Fprintln(stdout, "no sessions recorded in this window; nothing can be concluded")
		return exitcode.Advisory
	case st.SessionsDrafted == 0:
		fmt.Fprintln(stdout, "nothing drafted. This is the result that would justify §3b, then §3c —")
		fmt.Fprintln(stdout, "but revise the instruction's wording and re-measure first: a cheap trigger")
		fmt.Fprintln(stdout, "revised twice still costs less than the gate.")
		return exitcode.Advisory
	case st.Promoted+st.Binned == 0:
		// Drafting is not the result. A draft nobody has judged is not
		// evidence the instruction produces anything worth having, and saying
		// otherwise here would be the readout marking its own homework.
		fmt.Fprintf(stdout, "%d draft(s) written and none reviewed yet, so nothing is settled.\n", st.Drafted)
		fmt.Fprintln(stdout, "Read them with `agents review --show <id>`: three bullets restating the diff")
		fmt.Fprintln(stdout, "are a failure even at a 100% draft rate.")
		return exitcode.Advisory
	case st.PromotionRate() < 0.34:
		fmt.Fprintln(stdout, "the instruction fires but produces material you mostly discard.")
		fmt.Fprintln(stdout, "That is a wording problem, not an argument for a stronger trigger.")
		return exitcode.Advisory
	default:
		fmt.Fprintln(stdout, "the instruction is producing material you keep; §3c is not justified.")
		return exitcode.OK
	}
}

// staleQueueAge is when a pending draft stops being "not got to yet" and starts
// being evidence that review does not happen without being asked. Seven days is
// a guess, and deliberately a loose one: it is the threshold for *reporting* a
// suspicion, nothing acts on it, and the cost of being wrong is one paragraph.
// If real use shows drafts routinely sitting a fortnight and then still being
// promoted, the number is wrong rather than the queue.
const staleQueueAge = 7 * 24 * time.Hour

// reportablePendingAge is the floor for saying anything about the queue's age.
// A draft written minutes ago is not a backlog, and "oldest pending 0 hours" is
// noise that trains the reader to skip the line that matters at 0 days.
const reportablePendingAge = 24 * time.Hour

// humanAge renders a backlog age in days, which is the only unit this number is
// read in. Callers gate on reportablePendingAge, so it never sees under a day.
func humanAge(d time.Duration) string {
	if days := int(d.Hours() / 24); days >= 2 {
		return fmt.Sprintf("%d days", days)
	}
	return "1 day"
}

func reviewShow(store, id string, stdout io.Writer) int {
	d, err := queue.Get(store, id)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintf(stdout, "id:      %s\nkind:    %s\nlane:    %s\nsession: %s\nwhen:    %s\n",
		tableCell(d.ID), tableCell(d.Kind), tableCell(d.Lane), tableCell(d.Session),
		d.When.UTC().Format(time.RFC3339))
	if d.Subject != "" {
		fmt.Fprintf(stdout, "subject: %s\n", tableCell(d.Subject))
	}
	if d.Kind == queue.KindMemory {
		fmt.Fprintf(stdout, "name:    %s\ntype:    %s\ndesc:    %s\n",
			tableCell(d.Name), tableCell(d.Type), tableCell(d.Description))
	}
	fmt.Fprintf(stdout, "\n%s\n", d.Body)
	return exitcode.OK
}

func reviewBin(store, id string, stdout io.Writer) int {
	d, err := queue.Get(store, id)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	if err := queue.Remove(store, id); err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	// Nothing TRACKED records the rejection -- a draft nobody wanted is not a
	// decision the repository needs to carry. The machine-local event log does
	// record it, because "drafted then binned" and "never drafted" are
	// different answers to whether the instruction works, and an empty queue
	// cannot tell them apart.
	_ = queue.AppendEvent(store, queue.Event{
		Event: queue.EventBinned, Session: d.Session,
		Lane: d.Lane, Kind: d.Kind, DraftID: id,
	})
	fmt.Fprintf(stdout, "binned %s\n", tableCell(id))
	return exitcode.OK
}

func reviewEdit(store, id string, stdout io.Writer) int {
	path, err := queue.Path(store, id)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	if _, err := queue.Get(store, id); err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		fmt.Fprintf(stdout, "agents review: $EDITOR is unset; the draft is at %s\n", tableCell(path))
		return exitcode.Malformed
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	// Re-read so a mangled edit is reported now rather than at promotion.
	d, err := queue.Get(store, id)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: the edited draft no longer parses: %v\n", err)
		return exitcode.Malformed
	}
	if err := queue.Validate(d); err != nil {
		fmt.Fprintf(stdout, "agents review: the edited draft is incomplete: %v\n", err)
		return exitcode.Malformed
	}
	fmt.Fprintf(stdout, "edited %s; `agents review --keep %s` to promote it\n", tableCell(id), tableCell(id))
	return exitcode.OK
}

// reviewKeep promotes one draft in a single act.
//
// The order is chosen so that nothing reaches the tracked tree before it has
// been validated, and nothing leaves the queue before it is committed. A
// failure at any step leaves the draft recoverable, which is the property that
// makes "promotion is one act" true rather than merely convenient.
func reviewKeep(rc *repo.Context, agentsDir, store, id string, stdout io.Writer) int {
	d, err := queue.Get(store, id)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	if err := queue.Validate(d); err != nil {
		// Malformed, and the queue is untouched. Synthesising the missing
		// fields here would put a guessed slug and description into the tracked
		// tree at the one moment nobody is reading carefully.
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.Malformed
	}

	op, err := repo.InProgress(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}
	if op != "" {
		fmt.Fprintf(stdout, "agents review: a `git %s` is in progress, and a commit scoped to .agents/ is not safe during one. %s, then promote again.\n", op, inProgressRemedy(op))
		return exitcode.NoRecord
	}

	if d.Kind == queue.KindMemory {
		if warn := offDefaultBranchWarning(rc); warn != "" {
			// Warn and proceed. Refusing would block the case where the
			// knowledge came FROM this branch's work and belongs in its merge;
			// silently retargeting the default branch would commit to a branch
			// the user is not on.
			fmt.Fprintln(stdout, warn)
		}
	}

	path, err := promote(agentsDir, d)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: %v\n", err)
		return exitcode.NoRecord
	}

	subject := d.Subject
	if subject == "" {
		subject = fmt.Sprintf("%s: promote a reviewed draft from %s", d.Kind, d.Lane)
	}
	if out, err := repo.Git(rc.Root, "add", "--", ".agents"); err != nil {
		fmt.Fprintf(stdout, "agents review: git add: %v\n%s", err, out)
		return exitcode.NoRecord
	}
	out, err := repo.Git(rc.Root, "commit", "-m", subject, "--", ".agents")
	fmt.Fprint(stdout, out)
	if err != nil {
		fmt.Fprintf(stdout, "agents review: the draft is at %s and still queued; nothing was lost\n", tableCell(path))
		return exitcode.NoRecord
	}

	// Only now. Everything above can fail without costing the draft.
	if err := queue.Remove(store, id); err != nil {
		fmt.Fprintf(stdout, "agents review: promoted, but the queued copy could not be removed: %v\n", err)
		return exitcode.Advisory
	}
	_ = queue.AppendEvent(store, queue.Event{
		Event: queue.EventPromoted, Session: d.Session,
		Lane: d.Lane, Kind: d.Kind, DraftID: id,
	})
	fmt.Fprintf(stdout, "promoted %s -> %s\n", tableCell(id), tableCell(path))
	return exitcode.OK
}

// promote writes the draft into the tracked tree and regenerates the index it
// belongs to, in one operation, so the normal path never leaves a stale index
// for the pre-commit guard to block on.
func promote(agentsDir string, d queue.Draft) (string, error) {
	switch d.Kind {
	case queue.KindHandoff:
		// StatusReviewed, because a human selected it. The generated index
		// tells its reader that `draft` means nobody has checked it, and a
		// promoted note has been checked -- that is what promotion is.
		path, err := handoff.Write(agentsDir, d.Lane, d.Session, handoff.StatusReviewed, d.Body, d.When)
		var stale *handoff.IndexError
		if errors.As(err, &stale) {
			return path, fmt.Errorf("%w; fix that file and run `agents index`", err)
		}
		return path, err
	case queue.KindMemory:
		dir := filepath.Join(agentsDir, "memory")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		fm := memory.Frontmatter{Name: d.Name, Description: d.Description}
		fm.Metadata.Type = d.Type
		head, err := yaml.Marshal(fm)
		if err != nil {
			return "", err
		}
		var b bytes.Buffer
		b.WriteString("---\n")
		b.Write(head)
		b.WriteString("---\n\n")
		b.WriteString(strings.TrimRight(d.Body, "\n"))
		b.WriteString("\n")
		path := filepath.Join(dir, d.Name+".md")
		if _, err := os.Lstat(path); err == nil {
			return "", fmt.Errorf("a memory entry named %q already exists; edit it or rename the draft rather than overwriting curated knowledge", d.Name)
		}
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			return "", err
		}
		if err := memory.WriteIndex(dir); err != nil {
			return path, err
		}
		return path, nil
	default:
		return "", fmt.Errorf("draft kind %q cannot be promoted", d.Kind)
	}
}

// offDefaultBranchWarning names the branch a repo-wide memory entry is about to
// land on, when that is not where the rest of the repository can see it.
func offDefaultBranchWarning(rc *repo.Context) string {
	head, err := repo.Git(rc.Root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(head)
	if branch == "" || branch == "HEAD" {
		return ""
	}
	def, err := repo.Git(rc.Root, "config", "--get", "init.defaultBranch")
	fallback := strings.TrimSpace(def)
	if err != nil || fallback == "" {
		fallback = "main"
	}
	if branch == fallback || branch == "main" || branch == "master" {
		return ""
	}
	return fmt.Sprintf("agents review: a memory entry is repo-wide knowledge and this promotion lands it on %q;"+
		" it stays invisible to every other lane until that branch merges, and is lost if the branch is abandoned", tableCell(branch))
}
