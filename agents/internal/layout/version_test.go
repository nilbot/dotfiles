package layout

import "testing"

func TestSupportRefusesBelowFloorAndAllowsV1Control(t *testing.T) {
	v2 := mk(storesWith(RoleDesign, "context/design"))
	if ok, reason := Support("v0.5.99", v2); ok || reason != "below_floor" {
		t.Fatalf("v0.5.99 on v2 = (%v, %q), want refused below the floor", ok, reason)
	}
	if ok, reason := Support("v0.6.0", v2); !ok {
		t.Fatalf("v0.6.0 on v2 refused: %q", reason)
	}
	v1 := v1Layout(t.TempDir())
	if ok, reason := Support("v0.5.99", v1); !ok {
		t.Fatalf("v0.5.99 on v1 refused: %q", reason)
	}
	if ok, reason := Support("dev", v2); ok || reason != "unreleased" {
		t.Fatalf("an unstamped build on v2 = (%v, %q), want refused as unreleased", ok, reason)
	}
	if ok, reason := Support("dev", v1); !ok {
		t.Fatalf("an unstamped build on v1 refused: %q", reason)
	}
}

// The three refusals the floor test does not reach: a floor that cannot be
// parsed, a schema this build does not know, and a migration in progress.
func TestSupportRefusesInvalidFloorUnknownSchemaAndAMigration(t *testing.T) {
	bad := mk(storesWith(RoleDesign, "context/design"))
	bad.MinMutVerFloor = "not-a-version"
	if ok, reason := Support("v0.6.0", bad); ok || reason != "invalid" {
		t.Fatalf("an unparseable floor = (%v, %q), want invalid", ok, reason)
	}
	unknown := Layout{Manifest: Manifest{Schema: "agents.layout/v9", LayoutStatus: StatusActive}}
	if ok, reason := Support("v0.6.0", unknown); ok || reason != "unknown_schema" {
		t.Fatalf("an unknown schema = (%v, %q), want unknown_schema", ok, reason)
	}
	migrating := mk(storesWith(RoleDesign, "context/design"))
	migrating.LayoutStatus = StatusMigrating
	if ok, reason := Support("v0.6.0", migrating); ok || reason != "migrating" {
		t.Fatalf("a migrating layout = (%v, %q), want migrating", ok, reason)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.6.0", "0.6.0", 0},
		{"0.5.99", "0.6.0", -1},
		{"v0.6.0", "0.6.0-rc.1", 1},
		{"v0.10.0", "v0.9.9", 1},
	}
	for _, tc := range cases {
		got, err := compareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("compare(%q, %q) = (%d, %v), want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
}
