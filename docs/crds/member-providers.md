# Member Providers

Member providers are the identity sources that `GithubTeam` reads to determine which users should be in a team. Every provider type comes in two scopes:

- **Namespaced** — only accessible from `GithubTeam` resources in the same namespace.
- **Cluster-scoped** — accessible from `GithubTeam` resources in any namespace. Secrets for cluster-scoped providers must be in the operator's namespace.

---

## LDAPGroupProvider / ClusterLDAPGroupProvider

Fetches group membership from an LDAP or Active Directory server.

### Namespaced Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: LDAPGroupProvider
metadata:
  name: engineering-ldap
  namespace: default
spec:
  host: ldap.example.com:636
  baseDN: dc=example,dc=com
  secret: ldap-bind-secret
```

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ldap-bind-secret
  namespace: default
stringData:
  bindDN: "cn=bind,dc=example,dc=com"
  bindPW: "super-secret"
```

### Cluster-scoped Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: ClusterLDAPGroupProvider
metadata:
  name: shared-ldap
spec:
  host: ldap.global.com:636
  baseDN: dc=global,dc=com
  secret: ldap-global-secret  # Must be in the operator's namespace
```

### Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `host` | string | Yes | LDAP server address including port (e.g. `ldap.example.com:636`). |
| `baseDN` | string | Yes | Base DN for group searches. |
| `secret` | string | Yes | Name of a Secret containing `bindDN` and `bindPW`. |

---

## GenericExternalMemberProvider / ClusterGenericExternalMemberProvider

Fetches member lists from a JSON HTTP API. Supports Basic Auth and OAuth2 via a Secret, optional pagination, and configurable field mapping.

### Namespaced Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GenericExternalMemberProvider
metadata:
  name: http-eng
  namespace: default
spec:
  endpoint: https://api.example.com/members
  secret: http-cred
  resultsField: results
  idField: id
  paginated: true
  totalPagesField: total_pages
  pageParam: page
```

**Basic Auth secret:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: http-cred
  namespace: default
stringData:
  username: "api-user"
  password: "api-pass"
```

**OAuth2 secret:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: http-oauth-cred
  namespace: default
stringData:
  username: "GITHUB-GUARD"
  password: "password"
  client_id: "JgDxEDTXp4i4..."
  client_secret: "QpRex0w4NUTR..."
```

### Cluster-scoped Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: ClusterGenericExternalMemberProvider
metadata:
  name: global-http
spec:
  endpoint: https://api.global.com/members
  secret: http-global-secret  # Must be in the operator's namespace
  resultsField: results
  idField: id
```

### Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `endpoint` | string | Yes | Full URL of the HTTP API endpoint. |
| `secret` | string | Yes | Secret containing auth credentials (`username`/`password` for Basic or OAuth2). |
| `resultsField` | string | Yes | JSON field name in the response that contains the array of member objects. |
| `idField` | string | Yes | JSON field name within each member object that holds the user identifier. |
| `paginated` | bool | No | Set to `true` to enable pagination support. |
| `totalPagesField` | string | No | JSON field name that contains the total page count (required when `paginated: true`). |
| `pageParam` | string | No | Query parameter name used to specify the page number (required when `paginated: true`). |

---

## StaticMemberProvider / ClusterStaticMemberProvider

Serves a static list of members defined directly in the CRD spec. No external API calls are made.

### Namespaced Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: StaticMemberProvider
metadata:
  name: static-seed
  namespace: default
spec:
  groups:
    - group: any
      members:
        - johndoe
        - janedoe
```

### Cluster-scoped Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: ClusterStaticMemberProvider
metadata:
  name: global-static
spec:
  groups:
    - group: admins
      members:
        - superuser
```

### Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `groups` | []Group | Yes | List of named groups. |

**Group:**

| Field | Type | Description |
|---|---|---|
| `group` | string | Logical group name (referenced in `GithubTeam` via `externalMemberProvider.static.group`). |
| `members` | []string | List of internal user identifiers (matched against `GithubAccountLink.spec.userID`). |

---

## Referencing Providers in GithubTeam

When referencing a cluster-scoped provider, add `kind: Cluster<ProviderType>`:

```yaml
spec:
  externalMemberProvider:
    ldap:
      kind: ClusterLDAPGroupProvider   # omit for namespaced
      provider: shared-ldap
      group: cn=eng,ou=groups,dc=example,dc=com
```
