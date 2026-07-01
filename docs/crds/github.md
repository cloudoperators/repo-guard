# Github CRD

`Github` is a **cluster-scoped** resource that registers a GitHub App installation with the operator. All other Repo Guard resources reference a `Github` by name.

## Example

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: Github
metadata:
  name: com # Cluster scoped, no namespace
spec:
  webURL: https://github.com
  v3APIURL: https://api.github.com
  integrationID: 420328
  clientUserAgent: repo-guard
  secret: github-com-secret # Secret must be in the operator's namespace
```

## Spec Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `webURL` | string | Yes | Base URL for the GitHub web UI (e.g. `https://github.com` or your GHES URL). |
| `v3APIURL` | string | Yes | GitHub REST API v3 base URL. |
| `integrationID` | integer | Yes | GitHub App ID (the numeric ID of the App itself, found on the App's settings page). Not to be confused with the per-org installation ID, which lives in `GithubOrganization.spec.installationID`. |
| `clientUserAgent` | string | No | User-agent string sent with API requests. |
| `secret` | string | Yes | Name of the Kubernetes Secret (in the operator's namespace) containing the GitHub App credentials (`privateKey`, `clientID`, `clientSecret`). |

## Secret Format

The referenced Secret must contain three keys read by the controller:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-com-secret
  namespace: repo-guard   # Must be in the operator's namespace
stringData:
  privateKey: |
    -----BEGIN RSA PRIVATE KEY-----
    ...
    -----END RSA PRIVATE KEY-----
  clientID: "Iv1.xxxxxxxxxxxx"
  clientSecret: "your-oauth-client-secret"
```

## GitHub Enterprise

For GitHub Enterprise Server, set both `webURL` and `v3APIURL` to your GHES endpoints:

```yaml
spec:
  webURL: https://github.mycompany.com
  v3APIURL: https://github.mycompany.com/api/v3
  integrationID: 1
  secret: ghes-secret
```
