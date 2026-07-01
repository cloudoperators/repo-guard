# GithubOrganization CRD

`GithubOrganization` is a **namespace-scoped** resource that represents a GitHub organization managed by Repo Guard. It controls organization-level policies: default repository team permissions, organization owner enforcement, and team lifecycle.

## Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubOrganization
metadata:
  name: com--greenhouse-sandbox
  namespace: default
  labels:
    repo-guard.cloudoperators.dev/addTeam: "true"
    repo-guard.cloudoperators.dev/removeTeam: "true"
    repo-guard.cloudoperators.dev/addOrganizationOwner: "true"
    repo-guard.cloudoperators.dev/removeOrganizationOwner: "true"
    repo-guard.cloudoperators.dev/addRepositoryTeam: "true"
    repo-guard.cloudoperators.dev/removeRepositoryTeam: "true"
    repo-guard.cloudoperators.dev/dryRun: "false"
spec:
  github: com
  organization: greenhouse-sandbox
  organizationOwnerTeams:
    - org-admins
  defaultPublicRepositoryTeams:
    - team: public-pull-team
      permission: pull
    - team: public-push-team
      permission: push
    - team: public-admin-team
      permission: admin
  defaultPrivateRepositoryTeams:
    - team: private-pull-team
      permission: pull
    - team: private-push-team
      permission: push
    - team: private-admin-team
      permission: admin
  defaultInternalRepositoryTeams:
    - team: internal-pull-team
      permission: pull
  installationID: 43715277
```

## Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `github` | string | Yes | Name of the `Github` (cluster-scoped) resource to use for API access. |
| `organization` | string | Yes | GitHub organization slug. |
| `installationID` | integer | Yes | GitHub App installation ID for this organization. |
| `organizationOwnerTeams` | []string | No | List of GitHub team slugs whose members should be organization owners. |
| `defaultPublicRepositoryTeams` | []TeamPermission | No | Default team permissions applied to every public repository. |
| `defaultPrivateRepositoryTeams` | []TeamPermission | No | Default team permissions applied to every private repository. |
| `defaultInternalRepositoryTeams` | []TeamPermission | No | Default team permissions applied to every internal repository. |
| `protectedMembers` | []string | No | GitHub logins exempt from `removeOrganizationMember` and `removeRepositoryDirectCollaborator`. |

### TeamPermission

| Field | Type | Description |
|---|---|---|
| `team` | string | GitHub team slug. |
| `permission` | string | One of `pull`, `push`, `admin`, `maintain`, `triage`. |

## Labels

See the full [Labels Reference](../operations/labels#githuborganization-labels) for all supported labels.

| Key | Effect |
|---|---|
| `repo-guard.cloudoperators.dev/addTeam` | Create missing teams. |
| `repo-guard.cloudoperators.dev/removeTeam` | Delete out-of-policy teams. |
| `repo-guard.cloudoperators.dev/addOrganizationOwner` | Add missing org owners. |
| `repo-guard.cloudoperators.dev/removeOrganizationOwner` | Remove extra org owners. |
| `repo-guard.cloudoperators.dev/addRepositoryTeam` | Apply default repo team permissions. |
| `repo-guard.cloudoperators.dev/removeRepositoryTeam` | Remove default repo team permissions. |
| `repo-guard.cloudoperators.dev/removeOrganizationMember` | Remove org members not in any team (`"dryRun"` supported). |
| `repo-guard.cloudoperators.dev/removeRepositoryDirectCollaborator` | Remove direct repo collaborators (`"dryRun"` supported). |
| `repo-guard.cloudoperators.dev/dryRun` | Prevent all mutations; write planned operations to status. |

## Annotations

| Key | Description |
|---|---|
| `repo-guard.cloudoperators.dev/skipDefaultRepositoryTeams` | Comma-separated list of repository names to skip when applying default team permissions. |
