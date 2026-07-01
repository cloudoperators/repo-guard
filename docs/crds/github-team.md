# GithubTeam CRD

`GithubTeam` is a **namespace-scoped** resource that maps a desired GitHub team to a membership provider. The controller resolves the member list from the configured provider and syncs it to GitHub.

## Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubTeam
metadata:
  name: com--greenhouse-sandbox--eng
  namespace: default
  labels:
    repo-guard.cloudoperators.dev/addUser: "true"
    repo-guard.cloudoperators.dev/removeUser: "true"
spec:
  github: com
  organization: greenhouse-sandbox
  team: eng
```

## Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `github` | string | Yes | Name of the `Github` resource. |
| `organization` | string | Yes | GitHub organization slug. |
| `team` | string | Yes | GitHub team slug to manage. |
| `greenhouseTeam` | string | No | Greenhouse Team CRD name to use as member source. Mutually exclusive with `externalMemberProvider`. |
| `externalMemberProvider` | object | No | External member source configuration. |

## Member Provider Options

Exactly one of the following should be specified (either `greenhouseTeam` or one key under `externalMemberProvider`):

### Option A — Namespaced LDAP

```yaml
spec:
  externalMemberProvider:
    ldap:
      provider: engineering-ldap   # LDAPGroupProvider name
      group: cn=eng,ou=groups,dc=example,dc=com
```

### Option B — Cluster-scoped LDAP

```yaml
spec:
  externalMemberProvider:
    ldap:
      kind: ClusterLDAPGroupProvider
      provider: shared-ldap
      group: cn=shared,ou=groups,dc=global,dc=com
```

### Option C — Namespaced Static

```yaml
spec:
  externalMemberProvider:
    static:
      provider: static-seed
      group: any
```

### Option D — Cluster-scoped Static

```yaml
spec:
  externalMemberProvider:
    static:
      kind: ClusterStaticMemberProvider
      provider: global-static
      group: admins
```

### Option E — Namespaced HTTP

```yaml
spec:
  externalMemberProvider:
    genericHTTP:
      provider: http-eng
      group: results
```

### Option F — Cluster-scoped HTTP

```yaml
spec:
  externalMemberProvider:
    genericHTTP:
      kind: ClusterGenericExternalMemberProvider
      provider: global-http
      group: results
```

### Option G — Greenhouse

```yaml
spec:
  greenhouseTeam: engineering   # Greenhouse Team CRD name
```

## Labels

See the full [Labels Reference](../operations/labels#githubteam-labels) for all supported labels.

| Key | Effect |
|---|---|
| `repo-guard.cloudoperators.dev/addUser` | Allow adding members. Defaults to allowed when unset. |
| `repo-guard.cloudoperators.dev/removeUser` | Allow removing members. Defaults to allowed when unset. |
| `repo-guard.cloudoperators.dev/dryRun` | Prevent mutations; write planned operations to status. |
| `repo-guard.cloudoperators.dev/disableInternalUsernames` | Filter out members where GreenhouseID matches GithubUsername. |
| `repo-guard.cloudoperators.dev/require-verified-domain-email` | Only allow members with a verified email under the specified domain. |
| `repo-guard.cloudoperators.dev/orphaned` | Set by the controller when the team has no known parent organization. Do not set manually. |
