package drift

import (
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// The v2 router points at the manifest and the CLI's resolved view. It must not
// name a store path: a v2 repository may put its stores anywhere, so a router
// that spells docs/ is wrong for every repository that moved.
func TestV2RouterNamesTheManifestNotAStorePath(t *testing.T) {
	for _, want := range []string{".agents/layout.json", "agents.layout/v2", "agents layout path"} {
		if !strings.Contains(layout.V2AgentsMD, want) {
			t.Errorf("v2 router does not name %q", want)
		}
	}
	if strings.Contains(layout.V2AgentsMD, "docs/") {
		t.Error("the v2 router must not hardcode a store path")
	}
}

// The router ships verbatim from design §4.4, so its bytes are pinned, not
// sampled: the substring checks above cannot see a typo anywhere else in the
// template, and a one-character change would otherwise ship to every v2
// repository silently. 1225 bytes, 1219 characters -- the difference is the
// three em dashes -- with the SHA-256 recorded for the design's fence.
func TestV2RouterBytesArePinned(t *testing.T) {
	const (
		wantLen    = 1225
		wantDigest = "c6cecd53b08cb2459d5e846c9f871fee71c5c45a9ecd3b4b9885b54a18adcc66"
	)
	if got := len(layout.V2AgentsMD); got != wantLen {
		t.Errorf("V2AgentsMD is %d bytes, want %d", got, wantLen)
	}
	if got := DigestString(layout.V2AgentsMD); got != wantDigest {
		t.Errorf("V2AgentsMD sha256 = %s, want %s", got, wantDigest)
	}
}

// One function selects the canonical router from the resolved layout (design
// §8.1). A v1 repository keeps DefaultAgentsMD byte-for-byte; a v2 repository
// must digest it as known_legacy rather than current, so carrying the old
// router is a recognized state with an exit instead of permanent drift.
func TestV1RouterIsLegacyForV2(t *testing.T) {
	v1 := layout.Resolve(t.TempDir())
	v2 := layout.Layout{Manifest: layout.Manifest{
		Schema: layout.SchemaV2, MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus: layout.StatusActive,
		Stores: map[string]string{
			layout.RoleDesign:  ".context/design",
			layout.RolePlans:   ".context/plans",
			layout.RoleJournal: ".context/journal",
			layout.RoleQNA:     ".context/qna",
		},
	}}
	// The brief wrote this as an `if` initializer, which scopes digest to that
	// one statement and leaves the two assertions below it uncompilable.
	digest := DigestString(scaffold.DefaultAgentsMD)
	if CanonicalRouterDigestFor(v1) != digest {
		t.Fatal("v1 must keep DefaultAgentsMD as its canonical router")
	}
	if CanonicalRouterDigestFor(v2) == digest {
		t.Fatal("v2 must not accept the v1 router as current")
	}
	if !IsLegacyRouterDigest(digest) {
		t.Fatal("the v1 router must be legacy for a v2 repository")
	}
}
