# GithubTeamRepository CRD

`GithubTeamRepository` is a **namespace-scoped** resource that overrides the default repository-level team permissions set by `GithubOrganization`. Use it to grant a team a different (typically more restrictive) permission on a specific set of repositories.

## Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubTeamRepository
metadata:
  name: com--greenhouse-sandbox--eng--overrides
  namespace: default
spec:
  github: com
  organization: greenhouse-sandbox
  team: eng
  repository:
    - greenhouse-secret
  permission: pull
```

## Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `github` | string | Yes | Name of the `Github` resource. |
| `organization` | string | Yes | GitHub organization slug. |
| `team` | string | Yes | GitHub team slug to apply the override for. |
| `repository` | []string | Yes | List of repository names to apply the override to. |
| `permission` | string | Yes | Permission level to grant: `pull`, `push`, `admin`, `maintain`, or `triage`. |

## How It Works

`GithubTeamRepository` resources are read by the `GithubOrganization` controller (not a dedicated controller). When the organization reconciles repository permissions it checks for any `GithubTeamRepository` objects that reference the same `github` and `organization` and applies the override permission instead of the default for the listed repositories.

This allows you to, for example, give the `eng` team `admin` on most repositories while restricting it to `pull` on a specific sensitive repository.
