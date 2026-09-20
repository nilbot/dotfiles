package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestRel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveWithoutManifestIsImplicitV1(t *testing.T) {
	l := Resolve(t.TempDir())
	if l.Schema != SchemaV1 {
		t.Fatalf("schema = %q, want %q", l.Schema, SchemaV1)
	}
	if l.Stores[RoleQNA] != "docs/qna" || l.Stores[RolePlans] != "docs/plans" {
		t.Fatalf("v1 stores = %v", l.Stores)
	}
	if l.ManifestPath != "" || len(l.Problems) != 0 {
		t.Fatalf("v1 layout = %+v", l)
	}
}

func TestResolveV2MapIsCanonical(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}`)
	l := Resolve(root)
	if l.Schema != SchemaV2 {
		t.Fatalf("layout = %+v", l)
	}
	if l.Stores[RoleDesign] != ".context/design" || l.Stores[RoleQNA] != ".context/qna" {
		t.Fatalf("stores = %v", l.Stores)
	}
	if len(l.Problems) != 0 {
		t.Fatalf("problems = %v", l.Problems)
	}
}

func TestResolveRejectsArrayStores(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": ["design", "plans", "journal", "qna"]
}`)
	l := Resolve(root)
	if !hasProblem(l.Problems, ProblemManifestJSON) {
		t.Fatalf("problems = %v, want %s", l.Problems, ProblemManifestJSON)
	}
}

func TestResolveRejectsDuplicateStoreKeys(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": "a/design",
    "design": "b/design",
    "plans": "a/plans",
    "journal": "a/journal",
    "qna": "a/qna"
  }
}`)
	l := Resolve(root)
	if !hasProblem(l.Problems, ProblemRoleDuplicate) {
		t.Fatalf("problems = %v, want %s", l.Problems, ProblemRoleDuplicate)
	}
}

func TestResolveUnknownSchemaIsUnsupportedNotGuessed(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{"schema":"agents.layout/v9","layout_status":"active","stores":{}}`)
	l := Resolve(root)
	if l.Schema != "agents.layout/v9" || !hasProblem(l.Problems, ProblemSchemaUnknown) {
		t.Fatalf("layout = %+v", l)
	}
	if l.Stores != nil {
		t.Fatalf("an unknown schema must not resolve stores: %v", l.Stores)
	}
}

func TestManifestJSONHasNoProfileField(t *testing.T) {
	data, err := MarshalManifest(Manifest{
		Schema: SchemaV2, MinMutVerFloor: MinMutVerFloorV2,
		LayoutStatus: StatusActive,
		Stores: map[string]string{
			RoleDesign: "docs/design", RolePlans: "docs/plans",
			RoleJournal: "docs/journal", RoleQNA: "docs/qna",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"profile"`) {
		t.Fatalf("manifest still has a profile field: %s", data)
	}
}

// A manifest that was found but could not be used must still say where it was:
// ManifestPath is what distinguishes "there is no manifest" (implicit v1) from
// "there is one and it is broken".
func TestResolveProblemReturnsRecordTheManifestPath(t *testing.T) {
	corrupt := t.TempDir()
	writeManifest(t, corrupt, "{not json")
	l := Resolve(corrupt)
	if !hasProblem(l.Problems, ProblemManifestJSON) {
		t.Fatalf("problems = %v, want %s", l.Problems, ProblemManifestJSON)
	}
	if l.ManifestPath != ManifestRel {
		t.Fatalf("an unparseable manifest must report its path: %q, want %q", l.ManifestPath, ManifestRel)
	}

	noSchema := t.TempDir()
	writeManifest(t, noSchema, `{}`)
	m := Resolve(noSchema)
	if !hasProblem(m.Problems, ProblemSchemaUnknown) {
		t.Fatalf("problems = %v, want %s", m.Problems, ProblemSchemaUnknown)
	}
	if m.ManifestPath != ManifestRel {
		t.Fatalf("a schema-less manifest must report its path: %q, want %q", m.ManifestPath, ManifestRel)
	}
	if len(m.Problems) != 1 || m.Problems[0].Detail == "" {
		t.Fatalf("an absent schema needs a non-empty detail: %+v", m.Problems)
	}
}
