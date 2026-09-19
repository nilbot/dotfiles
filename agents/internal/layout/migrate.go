package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// Per-move state (design §0.5). "moving" is written immediately before
// `git mv`, so a crash inside the rename is identifiable; the filesystem stays
// the truth and every state is recoverable from it.
const (
	MovePending = "pending"
	MoveMoving  = "moving"
	MoveDone    = "done"
)

// Migration phases, each the last stage that completed. The journal is removed
// only when `layout_status` becomes `active`.
const (
	PhasePlanned = "planned" // journal written, nothing moved yet
	PhaseMoved   = "moved"   // every move is done
	PhasePruned  = "pruned"  // emptied source directories removed
	PhaseRouter  = "router"  // v2 router written
)

// MigrateOptions is one `agents layout migrate` invocation. RouterState is
// `drift.RouterState` as a string, asked by the CLI because the planner does
// not read the router: only `current` and `known_legacy` are boilerplate whose
// replacement is provably content-free.
type MigrateOptions struct {
	Template    string
	Stores      map[string]string
	Archive     string
	Running     string
	RouterState string
}

// LinkCandidate is one markdown link the migration will break: a link in a
// moving store whose target resolves into a moving store. The planner reports
// it and never rewrites it; the migrating-fleet-context skill does (design
// §9.5). Old and New are repository-relative, so the report does not depend on
// the spelling the author chose.
type LinkCandidate struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// Blocker is one reason a plan is refused. A blocker is a report, not a crash:
// PlanMigration returns it in a Plan with a nil error, and the CLI prints one
// Advisory line per blocker.
type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Counts is the plan's own summary, carried by both the human and the JSON
// report (design §9.2).
type Counts struct {
	Move    int `json:"move"`
	Keep    int `json:"keep"`
	Blocked int `json:"blocked"`
	Links   int `json:"links"`
}

// Plan is a migration plan: what would move, what blocks it, and what it would
// leave behind. It is a dry run until Task 11's apply reconciles it, so the
// planner writes nothing at all -- no manifest, no store, no index entry.
type Plan struct {
	Repo   string `json:"repo"`
	DryRun bool   `json:"dry_run"`
	// Phase is the migration phase this report describes: planned for a dry
	// run or a fresh apply; for --resume, the journal phase the run continued
	// from (design §7.2).
	Phase          string          `json:"phase"`
	From           Layout          `json:"from"`
	To             Layout          `json:"to"`
	RouterAction   string          `json:"router"`
	Archive        string          `json:"archive,omitempty"`
	Moves          []Move          `json:"moves"`
	LinkCandidates []LinkCandidate `json:"link_candidates"`
	Blockers       []Blocker       `json:"blockers"`
	Counts         Counts          `json:"counts"`
}

// PlanMigration reads a v1 repository and reports the v2 layout it would
// become: one move per store, the archive it keeps, the links that would break,
// and every reason it refuses.
//
// A refusal is a plan, not an error: an invalid target, an unsupported
// binary, a router the skill has not reconciled, a missing or symlinked store,
// an existing target, residue in docs/, an archive inside a move source, and an
// ignored .agents/ all come back in Plan.Blockers with a nil error, because
// each is something the operator fixes and re-runs. The error return is
// reserved for a source that cannot be read at all -- a repository that is not
// a valid v1 layout, or an I/O failure that makes the plan meaningless.
//
// The git preconditions of design §9.1 -- clean tree, not on a protected
// branch, no operation in progress -- are the CLI's, checked before this is
// called: this function asks git nothing, and in particular never inspects the
// tree it is planning against.
func PlanMigration(root string, opts MigrateOptions) (Plan, error) {
	from := Resolve(root)
	if from.Schema != SchemaV1 || len(from.Problems) > 0 {
		return Plan{}, fmt.Errorf("migration source is not a valid v1 layout")
	}
	to := targetLayout(from, opts)
	// One header for every exit. A refusal is still a dry run that wrote
	// nothing, and a JSON consumer reads `dry_run`, `repo`, and `counts.blocked`
	// before it reads the blockers: an early exit that left `"dry_run": false`
	// and `"blocked": 0` beside its reasons would read as a plan already
	// applied. finishPlan sets the counts from what the plan holds, so no exit
	// can forget either.
	p := Plan{
		Repo: root, DryRun: true, Phase: PhasePlanned, From: from, To: to,
		RouterAction: opts.RouterState + " -> canonical v2",
		Archive:      to.Archive,
	}
	if ps := Validate(root, to); len(ps) > 0 {
		p.Blockers = blockersFromProblems(ps)
		return finishPlan(p), nil
	}
	if ok, reason := Support(opts.Running, to); !ok {
		p.Blockers = []Blocker{{
			Code: reason, Detail: "this binary must not write the target layout",
		}}
		return finishPlan(p), nil
	}
	if opts.RouterState != "current" && opts.RouterState != "known_legacy" {
		p.Blockers = append(p.Blockers, Blocker{Code: "router_not_clean",
			Detail: "run the migrating-fleet-context skill to reconcile the root router first"})
	}
	// Decision 6 and design §0.7: a manifest in an ignored .agents/ is
	// machine-local and a clone would silently fall back to v1. Both paths are
	// asked, because `/.agents/**` is invisible to the directory query alone.
	// An ignored .agents/ is already V16's finding through Validate above, so
	// what this reaches is the fail-closed case: trackedness cannot be asked at
	// all -- a source that is not a repository -- and an unanswered question
	// must not read as a pass.
	if ignored, err := AgentsIgnored(root); err != nil {
		p.Blockers = append(p.Blockers, Blocker{Code: "local_agents",
			Detail: "cannot determine whether .agents/ is ignored: " + err.Error()})
	} else if ignored {
		p.Blockers = append(p.Blockers, Blocker{Code: "local_agents",
			Detail: ".agents/ is ignored, so a manifest here would be machine-local; drop the ignore rule first"})
	}
	for _, role := range Roles() {
		src, _ := Path(from, role)
		dst, _ := Path(to, role)
		// The symlink question is asked of Lstat, which reports the link and not
		// what it points at: a store that is a symlink is a redirect the move
		// would follow, and design §9.1 requires a real directory. Asking IsDir
		// first would report every symlinked store as merely missing, and the
		// remedy -- replace the link with the directory -- would never be said.
		info, err := os.Lstat(filepath.Join(root, src))
		switch {
		case err != nil:
			p.Blockers = append(p.Blockers, Blocker{Code: "store_missing", Detail: role + "=" + src})
			continue
		case info.Mode()&os.ModeSymlink != 0:
			p.Blockers = append(p.Blockers, Blocker{Code: "store_symlink", Detail: role + "=" + src})
			continue
		case !info.IsDir():
			p.Blockers = append(p.Blockers, Blocker{Code: "store_missing", Detail: role + "=" + src})
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, dst)); err == nil {
			p.Blockers = append(p.Blockers, Blocker{Code: "target_exists", Detail: role + "=" + dst})
			continue
		}
		digest, files, size, err := digestTree(root, src)
		if err != nil {
			p.Blockers = append(p.Blockers, Blocker{Code: "store_unreadable", Detail: role + "=" + src + ": " + err.Error()})
			continue
		}
		// The archive is never a move source or destination, refused plans
		// included: `Moves` and `Blockers` are read together, and a plan that
		// lists the overlap it refuses contradicts itself. The refusal already
		// names the store, and this runs after the per-store findings above so
		// that one run still reports them too.
		if to.Archive != "" && (pathPrefix(to.Archive, src) || pathPrefix(src, to.Archive) ||
			pathPrefix(to.Archive, dst) || pathPrefix(dst, to.Archive)) {
			continue
		}
		p.Moves = append(p.Moves, Move{
			Role: role, From: src, To: dst, State: MovePending,
			Files: files, Bytes: size, Digest: digest,
		})
	}
	p.Blockers = append(p.Blockers, residueBlockers(root, to.Archive)...)
	p.Blockers = append(p.Blockers, archiveInSourceBlockers(to.Archive, from)...)
	p.LinkCandidates = scanLinkCandidates(root, p.Moves, from, to)
	return finishPlan(p), nil
}

// finishPlan sets a plan's counts from what it actually holds, so every exit --
// accepted or refused -- reports `counts.blocked` as the number of blockers it
// carries and `counts.move`/`counts.links` as the lists beside them.
func finishPlan(p Plan) Plan {
	p.Counts = Counts{Move: len(p.Moves), Blocked: len(p.Blockers), Links: len(p.LinkCandidates)}
	return p
}

// digestTree is the source identity recorded at plan time: every entry — files,
// symlinks by target, directories by name — over sorted slash-separated
// relative paths, hashing path, size, and contents. Deliberately not a git tree
// oid: a file ignored by .gitignore is invisible to git but is still carried by
// `git mv`. `files` counts non-directory entries; `bytes` counts regular-file
// bytes.
func digestTree(root, rel string) (string, int, int64, error) {
	type record struct {
		rel    string
		kind   string
		target string
		data   []byte
	}
	var records []record
	base := filepath.Join(root, rel)
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		rec := record{rel: filepath.ToSlash(relPath)}
		switch {
		case d.IsDir():
			rec.kind = "d"
		case d.Type()&os.ModeSymlink != 0:
			rec.kind = "l"
			if rec.target, err = os.Readlink(path); err != nil {
				return err
			}
		case !d.Type().IsRegular():
			// A named pipe, socket, or device is not content this migration can
			// carry: it has no bytes to hash, no content identity to verify the
			// move against, and `git mv` cannot record it either. It is refused
			// rather than hashed as a fourth kind, and it is refused *before* any
			// read because os.ReadFile on a FIFO blocks on open until a writer
			// appears -- a planner that hangs forever is the worst outcome
			// available to it.
			return fmt.Errorf("%s is a %s, not a regular file this migration can carry",
				filepath.ToSlash(relPath), entryKind(d.Type()))
		default:
			rec.kind = "f"
			if rec.data, err = os.ReadFile(path); err != nil {
				return err
			}
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].rel < records[j].rel })
	h := sha256.New()
	var files int
	var total int64
	for _, rec := range records {
		switch rec.kind {
		case "d":
			fmt.Fprintf(h, "d %s\n", rec.rel)
		case "l":
			fmt.Fprintf(h, "l %s %s\n", rec.rel, rec.target)
			files++
		default:
			fmt.Fprintf(h, "f %s %d\n", rec.rel, len(rec.data))
			h.Write(rec.data)
			files++
			total += int64(len(rec.data))
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), files, total, nil
}

// entryKind names an entry that is neither a directory, a symlink, nor a
// regular file, for the refusal that turns it away.
func entryKind(mode fs.FileMode) string {
	switch {
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeCharDevice != 0:
		return "character device"
	case mode&os.ModeDevice != 0:
		return "device"
	case mode&os.ModeIrregular != 0:
		return "irregular file"
	default:
		return mode.Type().String()
	}
}

// docsResidue lists entries left in docs/ that are neither a v1 source store nor
// the declared archive. The planner refuses them up front; apply re-checks the
// same list after the moves, because docs/ can gain an entry mid-flight and
// silently leaving a shell behind is the failure this design exists to prevent.
func docsResidue(root, archive string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, role := range Roles() {
		allowed[role] = true
	}
	// The declared archive is compared cleaned, because `--archive ./docs/archive`
	// and `--archive docs/archive/` are the same place and only one of them is a
	// spelling Validate rejects.
	declared := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(archive)), "/")
	// A declared archive may be nested (docs/archive/plans), and the directories
	// under docs/ that exist only to hold it are part of it: refusing
	// docs/archive would make that declaration unsatisfiable, since the operator
	// cannot both keep the archive and empty its parent.
	nested := map[string]bool{}
	for _, ancestor := range docsArchiveAncestors(declared) {
		nested[ancestor] = true
	}
	var out []string
	for _, e := range entries {
		rel := "docs/" + e.Name()
		if allowed[e.Name()] || (archive != "" && (rel == declared || nested[rel])) {
			continue
		}
		out = append(out, rel)
	}
	return out
}

// docsArchiveAncestors lists the directories between docs/ and a declared
// archive: the chain that exists only to hold it.
func docsArchiveAncestors(declared string) []string {
	var out []string
	for dir := filepath.ToSlash(filepath.Dir(declared)); strings.HasPrefix(dir, "docs/"); dir = filepath.ToSlash(filepath.Dir(dir)) {
		out = append(out, dir)
	}
	return out
}

// residueBlockers names every entry in docs/ that is neither one of the four v1
// source stores nor the declared archive. The migration removes docs/ only when
// it is empty, so without this the anti-shell promise fails silently and the
// operator reads a clean success with docs/ still present.
func residueBlockers(root, archive string) []Blocker {
	var out []Blocker
	for _, rel := range docsResidue(root, archive) {
		out = append(out, Blocker{Code: "docs_residue",
			Detail: rel + " is neither a v1 store nor the declared archive; move it out of docs/, delete it, or declare it, then re-run"})
	}
	return out
}

// archiveInSourceBlockers refuses when the declared or derived archive equals or
// sits inside a v1 source store. V12 validates the target layout only, so without
// this `git mv docs/plans .context/plans` would drag the archive along and leave
// the manifest naming a path that no longer exists.
//
// It asks the source layout's stores, not the moves that survived the per-store
// checks: one run names every offending store, including one that is already
// blocked as missing or symlinked, so the operator is not sent around the loop
// once per refusal.
func archiveInSourceBlockers(archive string, from Layout) []Blocker {
	if archive == "" {
		return nil
	}
	var out []Blocker
	for _, role := range Roles() {
		src, ok := Path(from, role)
		if !ok {
			continue
		}
		if pathPrefix(src, archive) || pathPrefix(archive, src) {
			out = append(out, Blocker{Code: "archive_not_in_source",
				Detail: archive + " overlaps the move source " + src + "; move the archive out of the store first, or declare a different archive"})
		}
	}
	return out
}

// targetLayout resolves the layout the migration would record: the CLI's
// template expansion with --stores overrides on top (design §0.4), the declared
// archive, or the source's own. The template name itself is never persisted.
//
// A malformed invocation -- no template and not all four roles, a role outside
// the four, or a name this binary does not know -- falls back to the raw
// override map rather than becoming an error, so Validate's per-role findings
// still name exactly what is missing or unknown, one blocker each.
// The CLI rejects an unknown template itself, before this is reached.
func targetLayout(from Layout, opts MigrateOptions) Layout {
	stores, err := TemplateStores(opts.Template, opts.Stores)
	if err != nil {
		stores = opts.Stores
	}
	archive := opts.Archive
	if archive == "" {
		archive = from.Archive
	}
	return Layout{Manifest: Manifest{
		Schema: SchemaV2, MinMutVerFloor: MinMutVerFloorV2,
		LayoutStatus: StatusActive, Archive: archive, Stores: stores,
	}}
}

// blockersFromProblems renders the target layout's validation problems as
// blockers, keeping every one of them: Validate already reports one problem per
// offending role or path, which is the granularity the operator fixes at.
func blockersFromProblems(ps []Problem) []Blocker {
	out := make([]Blocker, 0, len(ps))
	for _, p := range ps {
		out = append(out, Blocker{Code: p.Code, Detail: strings.TrimSpace(p.Path + " " + p.Detail)})
	}
	return out
}

// scanLinkCandidates reports every markdown link inside a moving store whose
// target resolves under a moving store, mapped to where that target is going.
// It reads and writes nothing: design §9.5 has the CLI report the candidates
// and the migrating-fleet-context skill rewrite them, and a planner that
// rewrote prose would be reporting a plan it had already executed.
//
// Only markdown is read, only content outside fenced code blocks is read, and
// only repository paths are candidates: a URL, a mailto, and a bare fragment
// name no store and survive the move. The moves are the plan's authority for
// what actually changes on disk, and the two layouts say which roles they
// belong to; a move whose role either layout does not declare has no mapping to
// report.
func scanLinkCandidates(root string, moves []Move, from, to Layout) []LinkCandidate {
	var out []LinkCandidate
	for _, m := range moves {
		if _, ok := Path(from, m.Role); !ok {
			continue
		}
		if _, ok := Path(to, m.Role); !ok {
			continue
		}
		base := filepath.Join(root, filepath.FromSlash(m.From))
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			// An unreadable tree or file is already the digest's finding
			// (store_unreadable); a link report is not where a plan fails.
			if err != nil {
				return nil
			}
			if d.IsDir() || !isMarkdown(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			dir := repoDir(rel)
			var f fence
			for i, line := range strings.Split(string(data), "\n") {
				if !f.content(line) {
					continue
				}
				for _, link := range markdownLinks(line) {
					old, ok := resolveLinkTarget(dir, link.target)
					if !ok {
						continue
					}
					for _, mv := range moves {
						if !pathPrefix(mv.From, old) {
							continue
						}
						out = append(out, LinkCandidate{
							File: rel, Line: i + 1,
							Old: old, New: mapStorePath(mv.From, mv.To, old),
						})
						break
					}
				}
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Old < out[j].Old
	})
	return out
}

// isMarkdown reports whether a file name is markdown, the only prose a link
// report reads: a `](...)` in any other file is not a markdown link.
func isMarkdown(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

// repoDir is filepath.Dir for a slash-separated repository-relative path.
func repoDir(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return "."
}

// mapStorePath re-spells a path that lives under a moving store with that
// store's new root.
func mapStorePath(from, to, path string) string {
	from = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(from)), "/")
	to = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(to)), "/")
	path = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(path)), "/")
	if path == from {
		return to
	}
	return to + strings.TrimPrefix(path, from)
}

// resolveLinkTarget resolves one markdown link target against the directory of
// the file that holds it -- not against the repository root, because a relative
// link is relative to its own file -- and reports the repository-relative path
// it names. Absolute URLs, bare fragments, and absolute filesystem paths are
// not repository paths and are not candidates; a fragment or query on a real
// path names a place inside the file, and the file is what a store move
// relocates.
func resolveLinkTarget(dir, target string) (string, bool) {
	if target == "" || strings.HasPrefix(target, "#") || filepath.IsAbs(target) {
		return "", false
	}
	switch {
	case strings.HasPrefix(target, "http://"),
		strings.HasPrefix(target, "https://"),
		strings.HasPrefix(target, "mailto:"):
		return "", false
	}
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return "", false
	}
	return filepath.ToSlash(filepath.Join(dir, filepath.FromSlash(target))), true
}

// fence tracks a fenced code block while a file is read line by line. A link
// inside a fence is an example: the planner must not report it and the skill
// must not rewrite it, and both read markdown through this type so the two
// cannot disagree about what a fence contains.
type fence struct {
	char byte
	n    int
}

// content reports whether a line is file content rather than a fence marker or
// a line inside a fenced block.
func (f *fence) content(line string) bool {
	char, n := fenceRun(strings.TrimSpace(line))
	switch {
	case f.char == 0:
		if n == 0 {
			return true
		}
		f.char, f.n = char, n
		return false
	case n > 0 && char == f.char && n >= f.n:
		f.char, f.n = 0, 0
		return false
	default:
		return false
	}
}

// fenceRun reports the character and length of a line that opens or closes a
// fenced code block: three or more backticks or tildes, with an info string
// allowed after them.
func fenceRun(trimmed string) (byte, int) {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == trimmed[0] {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return trimmed[0], n
}

// markdownLink is one inline `](target)` occurrence on a line. start and end
// delimit the target, so a caller that rewrites a link keeps the link text, an
// optional `"title"`, and every other byte of the line.
type markdownLink struct {
	target string
	start  int
	end    int
}

// markdownLinks returns the inline links on one line of markdown. The target
// ends at whitespace, at the closing parenthesis, or at the `>` of the
// angle-bracket form `<target with spaces>`; anything between the target and
// the closing parenthesis is a title and is part of the syntax, not of the
// target. Only the inline form is read: a reference definition (`[x]: y`) is
// not reached by the forms this report is about.
func markdownLinks(line string) []markdownLink {
	var out []markdownLink
	for i := 0; i+1 < len(line); i++ {
		if line[i] != ']' || line[i+1] != '(' {
			continue
		}
		start := i + 2
		angle := start < len(line) && line[start] == '<'
		if angle {
			start++
		}
		j := start
		for j < len(line) {
			c := line[j]
			if angle {
				if c == '>' {
					break
				}
			} else if c == ')' || c == ' ' || c == '\t' {
				break
			}
			j++
		}
		target := line[start:j]
		if angle && j < len(line) && line[j] == '>' {
			j++
		}
		end := j
		for j < len(line) && line[j] != ')' {
			j++
		}
		if j == len(line) || target == "" {
			i = j
			continue
		}
		out = append(out, markdownLink{target: target, start: start, end: end})
		i = j
	}
	return out
}

// ---------------------------------------------------------------------------
// Apply, resume, and abort: the reconciliation engine (design §0.5, §9.3, §9.4)
// ---------------------------------------------------------------------------

// reconcileHook is the crash-matrix seam (design §10): nil in production, set by
// tests to fail once between two writes. It exists so the crash boundaries are
// enumerable rather than asserted -- every write in the engine sits between two
// named hook calls, and TestResumeCrashMatrix walks the names. Nothing on a
// production path assigns it (R4): a seam that armed itself would make a real
// migration fail on an error no operator caused.
var reconcileHook func(step string, moveIndex int) error

// hook fires the crash seam when a test armed one.
func hook(step string, moveIndex int) error {
	if reconcileHook == nil {
		return nil
	}
	return reconcileHook(step, moveIndex)
}

// MoveError is a per-move refusal: both paths, the reason, and the remedy.
// There is no --force; the operator resolves the filesystem and re-runs.
type MoveError struct {
	Move   Move
	Reason string
	Remedy string
}

func (e *MoveError) Error() string {
	return fmt.Sprintf("%s -> %s: %s\n  remedy: %s", e.Move.From, e.Move.To, e.Reason, e.Remedy)
}

// ResidueError is a named blocker, not a crash: the layout completed but docs/
// retained entries that are neither a v1 source store nor the declared archive.
// The CLI prints one line per entry and returns Advisory.
type ResidueError struct{ Entries []string }

func (e *ResidueError) Error() string {
	return fmt.Sprintf("docs/ still holds %d entr(y/ies) after the migration: %s",
		len(e.Entries), strings.Join(e.Entries, ", "))
}

// ApplyMigration is plan-if-absent plus reconcile: it creates the backup tag,
// freezes the plan into the journal, and then runs the same reconciliation
// resume runs. There is no second code path to drift out of step.
func ApplyMigration(root string, p Plan, backupTag string) error {
	if len(p.Blockers) > 0 {
		return fmt.Errorf("plan has %d blocker(s)", len(p.Blockers))
	}
	if backupTag == "" {
		return errors.New("--backup-tag is required with --apply")
	}
	if out, err := repo.Git(root, "tag", "-a", backupTag, "-m", "pre-layout-v2 backup"); err != nil {
		return fmt.Errorf("git tag: %v: %s", err, out)
	}
	// The journal exists before the first store is touched: the phase, the frozen
	// v1 source, the backup tag, and one content identity per move. That write is
	// what makes a crash recoverable at all (design §9.3 step 2).
	m := p.To.Manifest
	m.LayoutStatus = StatusMigrating
	m.Migration = &Migration{
		From: MigrationFrom{
			Schema:  p.From.Schema,
			Stores:  cloneStores(p.From.Stores),
			Archive: p.From.Archive,
		},
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		BackupTag:       backupTag,
		CreatedManifest: true,
		Phase:           PhasePlanned,
		Moves:           append([]Move(nil), p.Moves...),
	}
	if err := WriteManifest(root, m); err != nil {
		return err
	}
	return reconcileMigration(root)
}

// ResumeMigration reconciles an existing journal. It never re-plans: the frozen
// plan in the manifest is the only authority on what moves where.
func ResumeMigration(root string) error {
	l := Resolve(root)
	if l.LayoutStatus != StatusMigrating || l.Migration == nil {
		return errors.New("no migrating layout to resume")
	}
	if len(l.Problems) > 0 {
		return fmt.Errorf("manifest or journal is invalid: %d problem(s); run `agents layout validate`", len(l.Problems))
	}
	return reconcileMigration(root)
}

// AbortMigration deletes a manifest this migration created while nothing has
// moved. Every later phase refuses: there the remedy is --resume --apply, not a
// delete that would strand moved trees. Aborting is a mutation, so the CLI
// applies the same version guard as every other write before calling this; a
// binary below min_mut_ver_floor never reaches it, and the safe-by-construction
// state is what lets the human delete the manifest by hand instead.
//
// Every refusal names the phase and the retry (R3): the phase is the state that
// makes the delete safe, so an operator who cannot see it cannot tell a refusal
// from a bug. The only write is the manifest's removal -- no store, no
// .gitattributes, no tag is touched -- and the backup tag is left as the record.
func AbortMigration(root string) error {
	l := Resolve(root)
	if l.LayoutStatus != StatusMigrating || l.Migration == nil {
		return errors.New("no migrating layout to abort")
	}
	m := *l.Migration
	if m.Phase != PhasePlanned {
		return fmt.Errorf("abort refused: phase is %s, not %s; run `agents layout migrate --resume --apply`", m.Phase, PhasePlanned)
	}
	if !m.CreatedManifest {
		return fmt.Errorf("abort refused: phase is %s but this run did not create the manifest; restore it from the backup tag %s, or run `agents layout migrate --resume --apply`", m.Phase, m.BackupTag)
	}
	for _, mv := range m.Moves {
		if mv.State != MovePending {
			return fmt.Errorf("abort refused: phase is %s but move %s is %s, not %s; run `agents layout migrate --resume --apply`",
				m.Phase, mv.From, mv.State, MovePending)
		}
	}
	return os.Remove(filepath.Join(root, ManifestRel))
}

// reconcileMigration is the single mutation engine for apply and resume. Each
// iteration advances exactly one phase, and every step is idempotent, so a crash
// between any two writes is recoverable by running the same function again.
func reconcileMigration(root string) error {
	for {
		l := Resolve(root)
		if l.Migration == nil {
			return errors.New("no migration journal")
		}
		m := l.Manifest
		switch l.Migration.Phase {
		case PhasePlanned:
			if err := runMoves(root, &m); err != nil {
				return err
			}
		case PhaseMoved:
			if err := pruneSources(root, &m); err != nil {
				return err
			}
		case PhasePruned:
			if err := reconcileGitattributes(root); err != nil {
				return err
			}
			if err := writeV2Router(root); err != nil {
				return err
			}
			m.Migration.Phase = PhaseRouter
			if err := WriteManifest(root, m); err != nil {
				return err
			}
		case PhaseRouter:
			m.LayoutStatus = StatusActive
			m.Migration = nil
			if err := WriteManifest(root, m); err != nil {
				return err
			}
			// The layout is complete; the anti-shell promise may not be. A named
			// blocker with an Advisory exit, never a clean success.
			if residue := docsResidue(root, m.Archive); len(residue) > 0 {
				return &ResidueError{Entries: residue}
			}
			return nil
		default:
			return fmt.Errorf("manifest phase %q is not one of %s|%s|%s|%s",
				l.Migration.Phase, PhasePlanned, PhaseMoved, PhasePruned, PhaseRouter)
		}
	}
}

// runMoves reconciles every move against the filesystem, in journal order but
// independently: a later move that already landed does not imply an earlier one
// did, and no prefix of the list is assumed (design §0.5).
//
// The classification is filesystem-first, and the journal's `state` is only a
// hint. Source present and destination absent is a move still to make; source
// absent and destination present is a move that landed, verified against the
// digest the journal recorded; both present is a copy or a divergence and
// neither present is a loss, and both refuse rather than guess.
func runMoves(root string, m *Manifest) error {
	for i := range m.Migration.Moves {
		mv := &m.Migration.Moves[i]
		src := filepath.Join(root, mv.From)
		dst := filepath.Join(root, mv.To)
		srcPresent, dstPresent := pathExists(src), pathExists(dst)

		switch {
		case srcPresent && !dstPresent:
			if err := hook("before-move", i); err != nil {
				return err
			}
			// The source is re-verified against the identity the journal froze
			// before anything moves (design §9.3 step 3). The destination check
			// below is the other half: it catches a tree that changed while the
			// rename ran; this one catches a tree that changed between the plan
			// and the apply, while the recovery is still only a restore.
			got, _, _, err := digestTree(root, mv.From)
			if err != nil {
				return fmt.Errorf("%s: %w", mv.From, err)
			}
			if got != mv.Digest {
				return &MoveError{Move: *mv, Reason: fmt.Sprintf(
					"before the move, %s has %s but the journal recorded %s: the tree changed under the migration",
					mv.From, got, mv.Digest), Remedy: moveRemedy(m, mv)}
			}
			mv.State = MoveMoving
			if err := WriteManifest(root, *m); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if out, err := repo.Git(root, "mv", mv.From, mv.To); err != nil {
				return fmt.Errorf("git mv %s -> %s: %v: %s", mv.From, mv.To, err, out)
			}
			if err := hook("after-move", i); err != nil {
				return err
			}
			// The recorded identity is verified on both sides of the move: a
			// rename is atomic on one filesystem, so a mismatch here means the
			// tree changed under the migration and the journal must not record
			// `done`.
			got, _, _, err = digestTree(root, mv.To)
			if err != nil {
				return fmt.Errorf("%s: %w", mv.To, err)
			}
			if got != mv.Digest {
				return &MoveError{Move: *mv, Reason: fmt.Sprintf(
					"after `git mv`, %s has %s but the journal recorded %s: the tree changed under the migration",
					mv.To, got, mv.Digest), Remedy: moveRemedy(m, mv)}
			}
			if err := hook("before-journal", i); err != nil {
				return err
			}
			if err := stageRename(root, mv); err != nil {
				return err
			}
			mv.State = MoveDone
			if err := WriteManifest(root, *m); err != nil {
				return err
			}
			if err := hook("after-journal", i); err != nil {
				return err
			}

		case !srcPresent && dstPresent:
			got, _, _, err := digestTree(root, mv.To)
			if err != nil {
				return fmt.Errorf("%s: %w", mv.To, err)
			}
			if got != mv.Digest {
				return &MoveError{Move: *mv, Reason: fmt.Sprintf(
					"digest mismatch: the journal recorded %s (%d file(s)) but %s has %s",
					mv.Digest, mv.Files, mv.To, got), Remedy: moveRemedy(m, mv)}
			}
			if err := stageRename(root, mv); err != nil {
				return err
			}
			if mv.State != MoveDone {
				mv.State = MoveDone
				if err := WriteManifest(root, *m); err != nil {
					return err
				}
			}

		case srcPresent && dstPresent:
			fromDigest, _, _, err := digestTree(root, mv.From)
			if err != nil {
				return err
			}
			toDigest, _, _, err := digestTree(root, mv.To)
			if err != nil {
				return err
			}
			reason := fmt.Sprintf("both %s and %s exist", mv.From, mv.To)
			if fromDigest == toDigest {
				reason += "; their contents are identical, which is a copy, not a move"
			} else {
				reason += fmt.Sprintf("; their contents differ (%s vs %s)", fromDigest, toDigest)
			}
			// The journal's own digest is named as well: it is the reference the
			// two paths are being judged against, and it tells the operator which
			// side still matches what was planned.
			reason += fmt.Sprintf("; the journal recorded %s for %s", mv.Digest, mv.From)
			return &MoveError{Move: *mv, Reason: reason, Remedy: moveRemedy(m, mv)}

		default:
			return &MoveError{Move: *mv, Reason: fmt.Sprintf("neither %s nor %s exists: the tree was lost or already reconciled", mv.From, mv.To), Remedy: moveRemedy(m, mv)}
		}
	}
	if err := hook("phase-moved", 0); err != nil {
		return err
	}
	m.Migration.Phase = PhaseMoved
	return WriteManifest(root, *m)
}

// stageRename makes the index agree with the working tree for one move. `git mv`
// is itself two operations -- a filesystem rename and an index update -- so a
// crash between them leaves the tree moved and the index stale, still naming the
// old path. `git add -A` over both paths is idempotent and repeatable, and
// rename detection happens at diff/commit time, so the displayed rename and the
// recorded history are unaffected.
//
// The source path is passed only when the index or the worktree still has it.
// Measured with git 2.54.0: `git add -A -- <from> <to>` exits 128 with
// "pathspec ... did not match any files" once `git mv` has completed its index
// update as well as its rename, which is exactly the state a successful move
// leaves behind -- so the unconditional two-path form would make resume fail on
// the very crash it exists to recover from.
func stageRename(root string, mv *Move) error {
	paths := []string{mv.To}
	if pathExists(filepath.Join(root, mv.From)) {
		paths = append([]string{mv.From}, paths...)
	} else if tracked, err := repo.IsTracked(root, mv.From); err != nil {
		return fmt.Errorf("git ls-files --error-unmatch -- %s: %w", mv.From, err)
	} else if tracked {
		paths = append([]string{mv.From}, paths...)
	}
	if out, err := repo.Git(root, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return fmt.Errorf("git add -A -- %s: %v: %s", strings.Join(paths, " "), err, out)
	}
	return nil
}

// moveRemedy is the per-refusal remedy line, and one function serves every row
// because the operator's question is the same in all of them: which of these two
// paths is the tree I mean to keep?
//
// It names both paths, and it offers the non-destructive recovery first: the
// source is the tracked side, so `git restore --source=<tag> -- <source>` brings
// the tagged tree back (design §9.4). `rm -rf` is mentioned only for a path the
// operator has already confirmed by hand is the stray copy -- never as the first
// action and never as the whole instruction. In the both-present and
// digest-mismatch rows the destination can hold real work, and error text that
// prescribes deleting it is data loss by instruction (R1).
func moveRemedy(m *Manifest, mv *Move) string {
	tag := m.Migration.BackupTag
	return fmt.Sprintf(
		"source %s, destination %s: the source is the tracked side, so `git restore --source=%s -- %s` "+
			"brings the tagged tree back. Remove a path only once you have already confirmed by hand that it "+
			"is the stray copy -- `git rm -r -- <path>` when it is tracked, `rm -rf <path>` when it is not; "+
			"the destination can hold real work, so never delete either side on this tool's word alone. "+
			"Then re-run `agents layout migrate --resume --apply`",
		mv.From, mv.To, tag, mv.From)
}

// pruneSources removes directories the moves emptied. ENOENT and ENOTEMPTY are
// not failures: the first is a re-run of this step, the second means the
// directory still holds something, which the residue check reports rather than
// deletes. The archive is never a move source, so a docs/ that holds it is left
// standing by the same rule (design §9.5).
func pruneSources(root string, m *Manifest) error {
	if err := hook("phase-pruned", 0); err != nil {
		return err
	}
	for _, mv := range m.Migration.Moves {
		_ = os.Remove(filepath.Join(root, filepath.Dir(mv.From))) // empty directories only
	}
	if entries, err := os.ReadDir(filepath.Join(root, "docs")); err == nil && len(entries) == 0 {
		if err := os.Remove(filepath.Join(root, "docs")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	m.Migration.Phase = PhasePruned
	return WriteManifest(root, *m)
}

// writeV2Router is idempotent: a router already equal to the v2 text is left
// alone, so re-running after a crash writes nothing new.
func writeV2Router(root string) error {
	if err := hook("phase-router", 0); err != nil {
		return err
	}
	path := filepath.Join(root, "AGENTS.md")
	if current, err := os.ReadFile(path); err == nil && string(current) == V2AgentsMD {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(V2AgentsMD), 0o644)
}

// manifestLinguistLine keeps the manifest visible in PR review. `.agents/**` is
// collapsed by scaffold's blanket rule, and gitattributes is last-match-wins, so
// this one line is what stops the single .agents/ file a reviewer has to read
// from being hidden (design §10, Attributes). It is spelled here exactly as
// scaffold's v2GitattributesLines spells it, and a test in this package and a
// test in scaffold both pin the string.
const manifestLinguistLine = ".agents/layout.json -linguist-generated"

// reconcileGitattributes adds the manifest's linguist exception to the
// repository's tracked .gitattributes. scaffold writes it for a repository
// created as v2; a migrated repository becomes v2 here, and this is its only
// writer.
//
// It is the same idempotent shape scaffold's appendMissingLines uses -- append
// only what is not already present -- because scaffold cannot be imported from
// this package (scaffold imports layout) and because a reconcile that duplicated
// the line would rewrite the file on every resume. It runs at the `pruned`
// phase, after the last abortable state, so a migration that ends in `--abort`
// leaves the still-v1 repository's attributes untouched (R2).
func reconcileGitattributes(root string) error {
	path := filepath.Join(root, ".gitattributes")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == manifestLinguistLine {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var b strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	b.WriteString(manifestLinguistLine + "\n")
	_, err = f.WriteString(b.String())
	return err
}

// pathExists reports whether anything -- file, directory, or symlink -- is at
// path. It asks Lstat, not Stat: a store that is a symlink is present, and the
// planner has already refused it.
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// cloneStores copies the frozen source map into the journal, so a later
// mutation of the plan cannot rewrite what resume compares against.
func cloneStores(stores map[string]string) map[string]string {
	out := make(map[string]string, len(stores))
	for role, path := range stores {
		out[role] = path
	}
	return out
}
