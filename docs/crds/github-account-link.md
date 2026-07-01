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

Use the `repo-guard.cloudoperators.dev/require-verified-domain-email: <domain>` label on the `GithubAccountLink` for single-org verification.

## Labels & Annotations

| Key | Kind | Allowed Values | Description |
|---|---|---|---|
| `repo-guard.cloudoperators.dev/require-verified-domain-email` | Label | `<domain>` | Legacy single-org: request email verification for this domain. |
| `repo-guard.cloudoperators.dev/check-email-status` | Label | `true`/`false` | Controller-managed: whether the user passed the email requirement. |
| `repo-guard.cloudoperators.dev/email-check-config` | Annotation | JSON object | Multi-org email check configuration (see above). |
| `repo-guard.cloudoperators.dev/email-check-results` | Annotation | JSON object | Controller-managed multi-org email check results. |
| `repo-guard.cloudoperators.dev/check-email-timestamp` | Annotation | RFC3339 | Last email verification check time. |
| `repo-guard.cloudoperators.dev/check-email-ttl` | Annotation | Go duration | How long the email verification result stays valid. |
| `repo-guard.cloudoperators.dev/skippedTTL` | Annotation | Go duration | How long a skipped user operation remains in status. |

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
