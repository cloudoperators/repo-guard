# GithubAccountLink CRD

`GithubAccountLink` is a **cluster-scoped** resource that maps an internal user identity (e.g. an employee ID from an LDAP directory) to a GitHub user ID. It is also the mechanism for verifying that a GitHub account has a verified email under a specific domain, which can be required by `GithubTeam`.

## Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubAccountLink
metadata:
  name: com-jdoe # Cluster scoped
spec:
  userID: jdoe
  githubUserID: "2042059"
  github: com
```

## Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `userID` | string | Yes | Internal user identifier (e.g. employee ID, LDAP UID). |
| `githubUserID` | string | Yes | The GitHub numeric user ID (as a string). |
| `github` | string | Yes | Name of the `Github` resource that owns this link. |

## Email Verification

### Multi-organization (Recommended)

Configure per-org email domain checks via the `repo-guard.cloudoperators.dev/email-check-config` annotation:

```json
{
  "my-org": { "domain": "example.com", "enabled": true, "ttl": "24h" }
}
```

The controller writes results to `repo-guard.cloudoperators.dev/email-check-results`:

```json
{
  "my-org": {
    "domain": "example.com",
    "status": "verified",
    "timestamp": "2024-01-15T10:00:00Z"
  }
}
```

Possible `status` values: `verified`, `not-part-of-org`, `no`.

### Single-organization (Legacy)

The `repo-guard.cloudoperators.dev/require-verified-domain-email` label is supported on `GithubTeam` (not on `GithubAccountLink` itself) to enforce an email-domain requirement for team membership. See [GithubTeam labels](./github-team#labels).

## Annotations

| Key | Description |
|---|---|
| `repo-guard.cloudoperators.dev/email-check-config` | Multi-org email check configuration. JSON object mapping org name to `{"domain": "...", "enabled": true, "ttl": "24h"}`. Set by the user. |
| `repo-guard.cloudoperators.dev/email-check-results` | Controller-managed multi-org email check results written after verification. |

## Enforcing Email Verification on a Team

Once `GithubAccountLink` resources have the email-check results populated, configure the team to enforce the requirement:

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubTeam
metadata:
  labels:
    repo-guard.cloudoperators.dev/require-verified-domain-email: "example.com"
```

Members without a verified email under `example.com` will be excluded from the team.
