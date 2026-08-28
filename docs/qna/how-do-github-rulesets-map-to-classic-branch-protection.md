# How do GitHub Rulesets map to Classic Branch Protection?

## Context

GitHub replaced "Classic Branch Protection Rules" with **Repository Rulesets** under **Settings $\rightarrow$ Rules $\rightarrow$ Rulesets**.
While classic branch protection remains for legacy configurations, modern repositories use Rulesets to define granular, dynamic, and composable protection policies.

## Key Differences & Architecture

| Concept | Classic Branch Protection | GitHub Rulesets |
|---|---|---|
| **Location in UI** | `Settings -> Branches` | `Settings -> Rules -> Rulesets` |
| **Branch Targeting** | Hardcoded string/glob (e.g. `master`) | Dynamic target `~DEFAULT_BRANCH`, `~ALL`, or glob filters |
| **Evaluation Mode** | Immediate binary enforce | `Active`, `Evaluate` (dry-run/logging mode), `Disabled` |
| **Bypass Control** | "Include administrators" toggle | Granular `bypass_actors` (Roles, Teams, Deploy Keys, Apps) |
| **CLI Support** | `gh api /repos/.../branches/.../protection` | `gh ruleset list`, `gh ruleset check <branch>`, `gh api` |

---

## Mapping Ruleset JSON to Classic Protection

Here is the exact schema mapping to enforce *"Require Pull Request + Status Checks + No Force Push + No Deletion"* on the default branch:

```json
{
  "name": "Protect default branch",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "include": ["~DEFAULT_BRANCH"],
      "exclude": []
    }
  },
  "rules": [
    {
      "type": "deletion"
    },
    {
      "type": "non_fast_forward"
    },
    {
      "type": "pull_request",
      "parameters": {
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": false,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": false
      }
    },
    {
      "type": "required_status_checks",
      "parameters": {
        "strict_required_status_checks_policy": true,
        "required_status_checks": [
          { "context": "gate" }
        ]
      }
    }
  ]
}
```

---

## Managing Rulesets via GitHub CLI (`gh`)

### 1. View Applied Rules on a Branch
```bash
gh ruleset check master --repo nilbot/dotfiles
```

### 2. List All Rulesets
```bash
gh ruleset list --repo nilbot/dotfiles
```

### 3. Create a Ruleset via CLI
```bash
gh api --method POST /repos/:owner/:repo/rulesets \
  -H "Accept: application/vnd.github+json" \
  --input ruleset.json
```

### 4. Delete or Update a Ruleset
```bash
# Update
gh api --method PUT /repos/:owner/:repo/rulesets/<ruleset-id> --input updated.json

# Delete
gh api --method DELETE /repos/:owner/:repo/rulesets/<ruleset-id>
```
