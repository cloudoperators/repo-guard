# Labels Reference

Labels control the behavior of Repo Guard controllers. All labels live under `metadata.labels` of the corresponding resource. Unless otherwise noted, labels must be explicitly set to `"true"` to enable an action — omitting them disables the operation.

---

## GithubOrganization Labels

| Key | Allowed Values | Description | Default |
|---|---|---|---|
| `repo-guard.cloudoperators.dev/addOrganizationOwner` | `"true"` / `"false"` | Allows the controller to add missing organization owners. | Disabled |
| `repo-guard.cloudoperators.dev/removeOrganizationOwner` | `"true"` / `"false"` | Allows the controller to remove extra organization owners. | Disabled |
| `repo-guard.cloudoperators.dev/addTeam` | `"true"` / `"false"` | Allows the controller to create missing teams defined by policy. | Disabled |
| `repo-guard.cloudoperators.dev/removeTeam` | `"true"` / `"false"` | Allows the controller to remove teams that are out of policy. | Disabled |
| `repo-guard.cloudoperators.dev/addRepositoryTeam` | `"true"` / `"false"` | Allows setting default team permissions on repositories. | Disabled |
| `repo-guard.cloudoperators.dev/removeRepositoryTeam` | `"true"` / `"false"` | Allows removing default team permissions from repositories. | Disabled |
| `repo-guard.cloudoperators.dev/removeOrganizationMember` | `"true"` / `"dryRun"` / `"false"` | Removes org members not in any team. `"dryRun"` previews in `.status` without executing. All team-member fetches must succeed; failure on any team (non-rate-limit) blocks removals. No removals if the org has zero teams. | Disabled |
| `repo-guard.cloudoperators.dev/removeRepositoryDirectCollaborator` | `"true"` / `"dryRun"` / `"false"` | Removes all direct repository collaborators so access is team-managed only. `"dryRun"` previews without executing. Org owners and `spec.protectedMembers` are exempt. | Disabled |
| `repo-guard.cloudoperators.dev/dryRun` | `"true"` / `"false"` | When `"true"`, no changes are made to GitHub; status shows planned operations. | `"false"` |
| `repo-guard.cloudoperators.dev/cleanOperations` | `"complete"` / `"failed"` | In dryRun, purge completed or failed operations from status. Removed automatically after cleanup. | Not set |
| `repo-guard.cloudoperators.dev/failedTTL` | Go duration (e.g. `1h`, `30m`) | Clears failed operations and failed status after the duration since last status timestamp. | Not set |
| `repo-guard.cloudoperators.dev/completedTTL` | Go duration (e.g. `24h`) | Clears completed operations after the duration since last status timestamp. | Not set |

**Annotation:**

| Key | Description |
|---|---|
| `repo-guard.cloudoperators.dev/skipDefaultRepositoryTeams` | Comma-separated list of repository names to skip when applying default team permissions. |

---

## GithubTeam Labels

| Key | Allowed Values | Description | Default |
|---|---|---|---|
| `repo-guard.cloudoperators.dev/addUser` | `"true"` / `"false"` | Controls add member operations. Set `"false"` to disable; allowed when unset or `"true"`. | Allowed when unset |
| `repo-guard.cloudoperators.dev/removeUser` | `"true"` / `"false"` | Controls remove member operations. Set `"false"` to disable; allowed when unset or `"true"`. | Allowed when unset |
| `repo-guard.cloudoperators.dev/dryRun` | `"true"` / `"false"` | When `"true"`, no member changes are made; status shows planned operations. | `"false"` |
| `repo-guard.cloudoperators.dev/disableInternalUsernames` | `"true"` / `"false"` | Filters out members where GreenhouseID == GithubUsername (avoids leaking internal IDs). | `"false"` |
| `repo-guard.cloudoperators.dev/require-verified-domain-email` | `<domain>` | Only allows members with a verified email under this domain (from their `GithubAccountLink`). | Not set |
| `repo-guard.cloudoperators.dev/orphaned` | `"true"` | Set by the controller when the team is considered orphaned. **Do not set manually.** | Controller-managed |
| `repo-guard.cloudoperators.dev/failedTTL` | Go duration | Clears failed operations and error after the duration since last status timestamp. | Not set |
| `repo-guard.cloudoperators.dev/completedTTL` | Go duration | Clears completed operations after the duration since last status timestamp. | Not set |
| `repo-guard.cloudoperators.dev/notfoundTTL` | Go duration | Clears operations in `notfound` state after the duration since last status timestamp. | Not set |
| `repo-guard.cloudoperators.dev/skippedTTL` | Go duration | Clears operations in `skipped` state after the duration since last status timestamp. | Not set |

---

## GithubAccountLink Annotations

`GithubAccountLink` uses annotations (not labels) for the email-check mechanism. The two annotation keys defined in the API types are:

| Key | Kind | Description |
|---|---|---|
| `repo-guard.cloudoperators.dev/email-check-config` | Annotation | Multi-org email check configuration — a JSON object mapping org name to `{"domain": "...", "enabled": true, "ttl": "24h"}`. Set by the user. |
| `repo-guard.cloudoperators.dev/email-check-results` | Annotation | Controller-managed multi-org email check results — a JSON object written by the controller after performing the verification. |
