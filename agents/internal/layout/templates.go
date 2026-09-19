package layout

import (
	"fmt"
	"strings"
)

// templateDefaults is the CLI template table (design §0.4). A template name is
// creation-time convenience only: it expands to the four role paths, and the
// name is never written to the manifest, so it has no wire identity and needs
// no registry. ok reports whether the name is a template at all -- an unknown
// name is a clear CLI error, never silently treated as custom.
//
// The paths are the canonical expansion, not a shorthand: the manifest records
// the expanded map and there is no persisted store_root or profile.
func templateDefaults(name string) (map[string]string, bool) {
	switch name {
	case TemplateCodeRepo:
		return map[string]string{
			RoleDesign: "docs/design", RolePlans: "docs/plans",
			RoleJournal: "docs/journal", RoleQNA: "docs/qna",
		}, true
	case TemplateContentVault:
		return map[string]string{
			RoleDesign: ".context/design", RolePlans: ".context/plans",
			RoleJournal: ".context/journal", RoleQNA: ".context/qna",
		}, true
	case TemplateCustom, "":
		return nil, true
	default:
		return nil, false // unknown template: the CLI reports an error
	}
}

// TemplateStores expands a CLI template name plus --stores overrides into the
// expanded role-to-path map a v2 manifest records (design §0.4). Each override
// replaces one role's path, overriding the template default or supplying the
// role when there is no template; after that every one of the four roles must
// be present, because v2 has no partial layout (design §4.3).
//
// Every error it returns is a malformed invocation -- an unknown template, a
// role outside the four, a missing role -- and the CLI reports it as such
// rather than writing a manifest: a name this binary does not recognise is a
// typo, not a layout to record.
func TemplateStores(name string, overrides map[string]string) (map[string]string, error) {
	defaults, ok := templateDefaults(name)
	if !ok {
		return nil, fmt.Errorf("unknown template %q: the templates are %s, %s, and %s, or no --template at all with all four --stores",
			name, TemplateCodeRepo, TemplateContentVault, TemplateCustom)
	}

	// A fresh map, never the table's own: the expansion is the caller's to
	// record, and an override must not rewrite the defaults for the next call.
	stores := make(map[string]string, len(roleNames))
	for role, path := range defaults {
		stores[role] = path
	}
	for role, path := range overrides {
		if !validRole(role) {
			return nil, fmt.Errorf("unknown store role %q: the roles are %s", role, strings.Join(Roles(), ", "))
		}
		stores[role] = path
	}

	var missing []string
	for _, role := range Roles() {
		if _, ok := stores[role]; !ok {
			missing = append(missing, role)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no store for %s: a v2 layout needs all four roles, a template supplies defaults, and --stores <role>=<path> supplies one",
			strings.Join(missing, ", "))
	}
	return stores, nil
}
