// Command scaffoldv1 writes the v1 layout into the directory named by its
// first argument, by calling scaffold.Create -- the same call `agents init`
// makes in a repository with no manifest.
//
// It exists because the migration tests in package layout need exactly the
// bytes scaffold writes and cannot import scaffold to ask for them: scaffold
// imports layout, so a test file in package layout that imported scaffold would
// be an import cycle, which Go rejects outright ("import cycle not allowed in
// test"). A testdata program is the one place on the far side of that edge the
// go command will still build for a test, and it keeps the fixture honest: the
// router, the store READMEs, and the two frozen v1 skill texts are produced by
// their writer rather than copied into a test where they could silently drift.
//
// It is test scaffolding, never production code: testdata/ is ignored by
// `go build ./...` and `go vet ./...`, and nothing but a test runs it.
package main

import (
	"fmt"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scaffoldv1 <repository-root>")
		os.Exit(2)
	}
	if err := scaffold.Create(os.Args[1], false); err != nil {
		fmt.Fprintf(os.Stderr, "scaffold %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}
