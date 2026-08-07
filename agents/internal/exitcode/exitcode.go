// Package exitcode holds the process exit codes shared by every agents
// subcommand. A caller -- a git hook, a harness, a shell -- can act on the code
// without knowing which subcommand produced it.
package exitcode

const (
	OK        = 0 // did the thing
	Advisory  = 1 // finished, but the caller should look at the output
	Block     = 2 // the only code that stops work
	Malformed = 3 // input could not be parsed
	Skip      = 4 // not applicable here (not a repo, no .agents/, unknown event)
	NoRecord  = 5 // wanted to record and could not
)
