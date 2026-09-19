package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
