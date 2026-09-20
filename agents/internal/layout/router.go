package layout

import _ "embed"

// V2AgentsMD is the canonical router for a repository with
// .agents/layout.json. It names the manifest and the CLI's resolved view; it
// must not name a store path. scaffold.DefaultAgentsMD remains the v1 router.
//
// The bytes live in assets/router-v2.md rather than in a Go literal, because
// the router is mostly fenced markup: a raw string cannot hold its backticks,
// and the concatenation that would work is unreadable. The file is the design
// section 4.4 block verbatim, so the two can be compared mechanically.
//
// //go:embed can only initialize a variable, so this is one: nothing assigns to
// it after package initialization, and drift's router tests pin its contents.
//
//go:embed assets/router-v2.md
var V2AgentsMD string
