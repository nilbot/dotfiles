package layout

import (
	"reflect"
	"strings"
	"testing"
)

// The template table is creation-time convenience only (design §0.4). It
// expands to the canonical role-to-path map, and the name itself has no wire
// identity -- so the only thing that has to hold is that every expansion is a
// layout this binary would accept: an `init --template` that wrote a manifest
// the very next command refuses to mutate would be the worst possible trade.
func TestTemplateDefaultsExpandToValidLayouts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stores map[string]string
	}{
		{TemplateCodeRepo, map[string]string{
			RoleDesign: "docs/design", RolePlans: "docs/plans",
			RoleJournal: "docs/journal", RoleQNA: "docs/qna",
		}},
		{TemplateContentVault, map[string]string{
			RoleDesign: ".context/design", RolePlans: ".context/plans",
			RoleJournal: ".context/journal", RoleQNA: ".context/qna",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stores, ok := templateDefaults(tc.name)
			if !ok {
				t.Fatalf("templateDefaults(%q) is not a known template", tc.name)
			}
			if !reflect.DeepEqual(stores, tc.stores) {
				t.Fatalf("templateDefaults(%q) = %v, want %v", tc.name, stores, tc.stores)
			}
			if ps := Validate(t.TempDir(), mk(stores)); len(ps) != 0 {
				t.Fatalf("templateDefaults(%q) does not pass Validate: %v", tc.name, ps)
			}
		})
	}
}

// custom, and no --template at all, mean no defaults rather than an empty
// layout: the caller must supply all four roles through --stores (design §0.4).
func TestTemplateDefaultsCustomAndEmptyHaveNoDefaults(t *testing.T) {
	for _, name := range []string{TemplateCustom, ""} {
		stores, ok := templateDefaults(name)
		if !ok {
			t.Errorf("templateDefaults(%q) must be a known template", name)
		}
		if stores != nil {
			t.Errorf("templateDefaults(%q) = %v, want no defaults", name, stores)
		}
	}
}

// An unknown name is not custom. Treating it as custom would silently accept a
// typo and then ask for four --stores the caller did not expect to need.
func TestTemplateDefaultsRejectsAnUnknownName(t *testing.T) {
	got, ok := templateDefaults("prose-vault")
	if ok {
		t.Fatalf("templateDefaults(%q) = %v, true; an unknown template must be reported", "prose-vault", got)
	}
	if got != nil {
		t.Fatalf("an unknown template returned stores: %v", got)
	}
}

func TestTemplateStoresExpandsAndOverridesIndividualRoles(t *testing.T) {
	stores, err := TemplateStores(TemplateContentVault, map[string]string{RoleQNA: "notes/qna"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		RoleDesign: ".context/design", RolePlans: ".context/plans",
		RoleJournal: ".context/journal", RoleQNA: "notes/qna",
	}
	if !reflect.DeepEqual(stores, want) {
		t.Fatalf("stores = %v, want %v", stores, want)
	}
}

// The expanded map is the manifest's only store input: the returned map must
// not alias the template table, or one caller's override would rewrite the
// defaults for the next.
func TestTemplateStoresDoesNotMutateTheTemplateTable(t *testing.T) {
	TemplateStores(TemplateContentVault, map[string]string{RoleQNA: "notes/qna"})
	again, err := TemplateStores(TemplateContentVault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again[RoleQNA] != ".context/qna" {
		t.Fatalf("a previous override leaked into the template: %v", again)
	}
}

func TestTemplateStoresCustomNeedsAllFourRoles(t *testing.T) {
	partial := map[string]string{RoleDesign: "notes/design"}
	for _, name := range []string{"", TemplateCustom} {
		_, err := TemplateStores(name, partial)
		if err == nil {
			t.Fatalf("TemplateStores(%q, partial) succeeded; a v2 layout needs all four roles", name)
		}
		for _, role := range []string{RolePlans, RoleJournal, RoleQNA} {
			if !strings.Contains(err.Error(), role) {
				t.Errorf("TemplateStores(%q) error %q does not name the missing %s", name, err, role)
			}
		}
	}

	full := map[string]string{
		RoleDesign: "architecture/design", RolePlans: "architecture/plans",
		RoleJournal: "notes/log", RoleQNA: "notes/qna",
	}
	stores, err := TemplateStores("", full)
	if err != nil {
		t.Fatalf("all four --stores were supplied and were still refused: %v", err)
	}
	if !reflect.DeepEqual(stores, full) {
		t.Fatalf("stores = %v, want %v", stores, full)
	}
}

func TestTemplateStoresRejectsAnUnknownRole(t *testing.T) {
	_, err := TemplateStores(TemplateContentVault, map[string]string{"changelog": "notes/changelog"})
	if err == nil || !strings.Contains(err.Error(), "changelog") {
		t.Fatalf("an unknown role must be named and refused, got %v", err)
	}
}

func TestTemplateStoresRejectsAnUnknownTemplate(t *testing.T) {
	_, err := TemplateStores("prose-vault", nil)
	if err == nil || !strings.Contains(err.Error(), "prose-vault") {
		t.Fatalf("an unknown template must be named and refused, got %v", err)
	}
}
