// Command agents maintains repo-tracked agent context: the .agents/ directory,
// the harness wiring that feeds it, and the git hooks that guard it.
package main

import (
	"fmt"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return exitcode.Malformed
	}
	switch args[0] {
	case "hook":
		return runHook(args[1:], os.Stdin, os.Stderr)
	case "init":
		return runInit(args[1:], os.Stdout)
	case "wire":
		return runWire(args[1:], os.Stdout)
	case "trace":
		return runTrace(args[1:], os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		usage()
		return exitcode.Malformed
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: agents <command> [flags]

commands are registered in this file as they are implemented.
`)
}
