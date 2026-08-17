package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Command is one node of the tree that dispatch and help both walk. Declaring a
// command and documenting it are the same act, so they cannot diverge -- which
// is the whole reason this type exists. The previous arrangement was a switch
// in main.go and an unrelated string literal beside it, and spec 7 changed four
// lines of that literal by hand in a single workstream.
type Command struct {
	Name     string     // "prune"; the full path comes from traversal
	Summary  string     // one line, shown in the parent's listing
	Usage    string     // "agents trace cache prune --lane <name>"
	Detail   string     // paragraph shown by `agents help <path>`
	Audience []Audience // who invokes this
	Flags    func(*flag.FlagSet)
	Run      func(args []string, io IO) int
	Sub      []*Command
}

// IO is one bundle rather than three parameters because the existing handlers
// have three different signatures and a registry needs one. Most are
// (args, stdout); runHandoff, runHandoffWrite and runHandoffDraft take stdin;
// runHook takes stdin and writes to *stderr*, not stdout, because a harness
// consumes its stdout. Adapting them all to a stdout-only signature would
// silently redirect the hook's diagnostics into a channel the harness parses.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Audience string

// Automated audiences act on the exit code mechanically and cannot read prose,
// so for them the code must be disposition: proceed, stop, or you-called-me-
// wrong. Attentional audiences read the output, and a non-zero code is what
// makes them read it -- which is why `agents init` exits 1 so its state is
// visible, and why `agents review --stats` returns 1 for a successful reading
// that is not a clean pass.
const (
	Git     Audience = "git"
	Harness Audience = "harness"
	CI      Audience = "ci"
	Human   Audience = "human"
	Agent   Audience = "agent"
)

func (a Audience) Automated() bool {
	return a == Git || a == Harness || a == CI
}

// visibleToPeople reports whether this command belongs in the usage text a
// person reads. `hook` is invoked only by a harness and `guard` only by git;
// both sat in the same flat list as `init` before this existed.
func (c *Command) visibleToPeople() bool {
	for _, a := range c.Audience {
		if a == Human {
			return true
		}
	}
	return false
}

// Find walks as deep as the arguments match and returns the deepest command
// plus the arguments that are not part of its path. An unknown subcommand
// resolves to its parent with the unknown token still in rest, so the parent
// reports it -- which is how `agents trace nosuch` keeps exiting 3.
func (c *Command) Find(args []string) (*Command, []string) {
	cur := c
	i := 0
	for i < len(args) {
		next := cur.child(args[i])
		if next == nil {
			break
		}
		cur = next
		i++
	}
	if cur == c {
		return nil, args
	}
	return cur, args[i:]
}

func (c *Command) child(name string) *Command {
	for _, s := range c.Sub {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Walk visits every command in the tree with its full path, excluding the root.
// It is what the coverage check and the README renderer both traverse.
func (c *Command) Walk(fn func(path []string, cmd *Command)) {
	var descend func(prefix []string, node *Command)
	descend = func(prefix []string, node *Command) {
		for _, s := range node.Sub {
			path := append(append([]string{}, prefix...), s.Name)
			fn(path, s)
			descend(path, s)
		}
	}
	descend(nil, c)
}

// RenderUsage writes the top-level listing. With all=false it shows only
// commands a person invokes; with all=true it shows everything.
func RenderUsage(root *Command, w io.Writer, all bool) {
	fmt.Fprintf(w, "usage: %s\n\n", root.Usage)
	width := 0
	var rows []*Command
	for _, c := range root.Sub {
		if !all && !c.visibleToPeople() {
			continue
		}
		rows = append(rows, c)
		if n := len(c.Usage); n > width {
			width = n
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	for _, c := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.Usage, c.Summary)
	}
	if !all {
		fmt.Fprint(w, "\n  agents help --all            include commands invoked by git and harnesses\n")
	}
	fmt.Fprint(w, "\n")
	RenderExitCodes(w)
}

// RenderHelp writes one command's own page. It filters subcommands by audience
// on the same rule RenderUsage applies to the top level: a subcommand only a
// harness or git invokes does not belong on the page a person reads, and
// nesting is no reason for it to escape the filter. Nothing in the tree is
// hidden this way today -- the filter is here so that adding the first nested
// `hook`-alike does not silently leak it into its parent's page, which is the
// shape of omission the whole registry exists to prevent.
func RenderHelp(cmd *Command, path []string, w io.Writer, all bool) {
	fmt.Fprintf(w, "agents %s -- %s\n\n", strings.Join(path, " "), cmd.Summary)
	fmt.Fprintf(w, "usage: %s\n\n", cmd.Usage)
	fmt.Fprintf(w, "%s\n", cmd.Detail)

	var rows []*Command
	hidden := 0
	for _, s := range cmd.Sub {
		if !all && !s.visibleToPeople() {
			hidden++
			continue
		}
		rows = append(rows, s)
	}
	if len(rows) > 0 {
		fmt.Fprint(w, "\nsubcommands:\n")
		width := 0
		for _, s := range rows {
			if n := len(s.Name); n > width {
				width = n
			}
		}
		for _, s := range rows {
			fmt.Fprintf(w, "  %-*s  %s\n", width, s.Name, s.Summary)
		}
	}
	// Filtered content stays discoverable: a reader who can see that something
	// is missing is told where to look, rather than being left with a gap.
	if hidden > 0 {
		fmt.Fprintf(w, "\n  agents help %s --all  include the subcommands invoked by git and harnesses\n",
			strings.Join(path, " "))
	}
}
