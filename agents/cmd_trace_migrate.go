package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

// runTraceMigrate moves a previously tracked index into the machine-local store.
//
// Three steps, in an order chosen so that nothing is ever removed before it is
// safe to remove: copy the records into the store, then unstage the tracked
// directory, then drop the merge=union attribute that only existed to let two
// branches append to the same tracked file on the same day.
//
// It stops short of committing. What else is in flight is not this command's
// business, and a tool that commits on the user's behalf during a migration is
// how unrelated work ends up in a migration commit.
func runTraceMigrate(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace migrate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	apply := fs.Bool("yes", false, "actually migrate; without this it only reports")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	rc, agentsDir, store, code := storeHere(stdout)
	if code != exitcode.OK {
		return code
	}

	tracked := filepath.Join(agentsDir, "reports", "traces")
	entries, err := os.ReadDir(tracked)
	if os.IsNotExist(err) {
		fmt.Fprintln(stdout, "no tracked trace index here; nothing to migrate")
		return exitcode.OK
	}
	if err != nil {
		fmt.Fprintf(stdout, "agents trace migrate: %v\n", err)
		return exitcode.NoRecord
	}
	files := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files++
		}
	}

	if !*apply {
		fmt.Fprintf(stdout, "would copy %d tracked daily file(s) into %s\n", files, tableCell(store))
		fmt.Fprintf(stdout, "would run `git rm -r --cached -- %s`\n", tableCell(relTo(rc.Root, tracked)))
		fmt.Fprintln(stdout, "would drop the .agents/reports/traces merge=union attribute")
		fmt.Fprintln(stdout, "nothing was changed; re-run with --yes")
		return exitcode.Advisory
	}

	moved, err := trace.MigrateTrackedIndex(agentsDir, store)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace migrate: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintf(stdout, "copied %d daily file(s) into %s\n", moved, tableCell(store))

	// Only now, with the content safe in the store.
	if out, err := repo.Git(rc.Root, "rm", "-r", "--cached", "--quiet", "--", relTo(rc.Root, tracked)); err != nil {
		fmt.Fprintf(stdout, "agents trace migrate: git rm failed: %v\n%s\n", err, out)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, "unstaged the tracked trace index")

	if dropped, err := dropMergeUnionAttribute(rc.Root); err != nil {
		fmt.Fprintf(stdout, "agents trace migrate: %v\n", err)
		return exitcode.NoRecord
	} else if dropped {
		fmt.Fprintln(stdout, "dropped the merge=union attribute; nothing tracked appends concurrently now")
	}

	fmt.Fprintln(stdout, "review and commit the removal yourself; this command does not commit")
	return exitcode.OK
}

// dropMergeUnionAttribute removes the one line that existed only for the
// tracked index, leaving every other attribute in place.
func dropMergeUnionAttribute(root string) (bool, error) {
	path := filepath.Join(root, ".gitattributes")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var out []string
	dropped := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ".agents/reports/traces/") && strings.Contains(line, "merge=union") {
			dropped = true
			continue
		}
		out = append(out, line)
	}
	if !dropped {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// relTo renders a path the way git wants to be given it.
func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
