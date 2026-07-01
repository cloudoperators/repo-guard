# Getting Started

## Prerequisites

- A running Kubernetes cluster (v1.25+)
- `kubectl` and `helm` installed
- A GitHub App installed on your organization with the required permissions
- The operator's namespace created (default: `repo-guard`)

## Installation

Install Repo Guard using the Helm chart published to GHCR:

```bash
helm install repo-guard oci://ghcr.io/cloudoperators/charts/repo-guard \
  --namespace repo-guard \
  --create-namespace \
  --set github.secret=github-com-secret
```

Create the GitHub App secret in the operator namespace before (or alongside) the install:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-com-secret
  namespace: repo-guard
stringData:
  privateKey: |
    -----BEGIN RSA PRIVATE KEY-----
    ...
    -----END RSA PRIVATE KEY-----
```

## Quick Start

Once the operator is running, apply these resources in order to manage your first GitHub team:

**Step 1 — Register your GitHub App:**

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: Github
metadata:
  name: com
spec:
  webURL: https://github.com
  v3APIURL: https://api.github.com
  integrationID: 420328
  clientUserAgent: repo-guard
  secret: github-com-secret
```

**Step 2 — Declare your organization:**

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubOrganization
metadata:
  name: com--my-org
  namespace: default
  labels:
    repo-guard.cloudoperators.dev/addTeam: "true"
    repo-guard.cloudoperators.dev/dryRun: "false"
spec:
  github: com
  organization: my-org
  installationID: 12345678
```

**Step 3 — Create a team with a static member list:**

```yaml
apiVersion: repo-guard.cloudoperators.dev/v1
kind: StaticMemberProvider
metadata:
  name: my-team-members
  namespace: default
spec:
  groups:
    - group: members
      members:
        - johndoe
        - janedoe
---
apiVersion: repo-guard.cloudoperators.dev/v1
kind: GithubTeam
metadata:
  name: com--my-org--my-team
  namespace: default
  labels:
    repo-guard.cloudoperators.dev/addUser: "true"
    repo-guard.cloudoperators.dev/removeUser: "true"
spec:
  github: com
  organization: my-org
  team: my-team
  externalMemberProvider:
    static:
      provider: my-team-members
      group: members
```

The operator will reconcile and create/update the `my-team` GitHub team with the declared members.

## Next Steps

- Read [Architecture](./architecture) to understand the reconciliation flow.
- Explore the [CRD reference](../crds/github) for full spec documentation.
- See [Labels Reference](../operations/labels) for all available control labels.
