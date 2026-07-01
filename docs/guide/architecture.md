# Architecture

## System Overview

Repo Guard runs as a Kubernetes operator — a collection of controllers that each watch specific CRD types and reconcile desired state against GitHub's API.

```mermaid
flowchart LR
    direction TB
    subgraph crds[Custom Resources]
      G[(Github)]
      GO[(GithubOrganization)]
      GT[(GithubTeam)]
      GTR[(GithubTeamRepository)]
      GAL[(GithubAccountLink)]
      LDAP[(LDAPGroupProvider)]
      CLDAP[(ClusterLDAPGroupProvider)]
      GEMP[(GenericExternalMemberProvider)]
      CGEMP[(ClusterGenericExternalMemberProvider)]
      SMP[(StaticMemberProvider)]
      CSMP[(ClusterStaticMemberProvider)]
    end

    subgraph ctrl[Controllers]
      C_G[Github Controller]
      C_GO[GithubOrganization Controller]
      C_GT[GithubTeam Controller]
      C_GAL[GithubAccountLink Controller]
      C_LDAP[LDAP/AD Provider Controller]
      C_GEMP[Generic HTTP Provider Controller]
      C_SMP[Static Provider Controller]
    end

  subgraph providers[Member Sources]
    Greenhouse[(Greenhouse Team CRD)]
    LDAPSrv[(LDAP/AD)]
    HTTP[(Generic HTTP JSON API)]
    Static[(Static list in CRD)]
  end

  GitHub[(GitHub Cloud / Enterprise)]

  G --> C_G
  GO --> C_GO
  GT --> C_GT
  GTR --> C_GO
  GAL --> C_GAL
  LDAP --> C_LDAP
  CLDAP --> C_LDAP
  GEMP --> C_GEMP
  CGEMP --> C_GEMP
  SMP --> C_SMP
  CSMP --> C_SMP

  C_GT -->|resolves members| Greenhouse
  C_GT -->|resolves members| C_LDAP
  C_GT -->|resolves members| C_GEMP
  C_GT -->|resolves members| C_SMP

  C_LDAP --> LDAPSrv
  C_GEMP --> HTTP
  C_SMP --> Static

  C_GT -->|manage members| GitHub
  C_GO -->|manage org owners, teams, repo & team permissions| GitHub
```

## Resource Relationships

```mermaid
graph TD
  G[Github] -->|owns| GO[GithubOrganization]
  GO -->|owns| GT[GithubTeam]
  GO -->|reads| GTR[GithubTeamRepository]
  GT -->|resolves members| Greenhouse[Greenhouse Team]
  GT -->|resolves members via| LDAP[LDAPGroupProvider]
  GT -->|resolves members via| GEMP[Generic HTTP Provider]
  GT -->|resolves members via| SMP[StaticMemberProvider]
  GAL[GithubAccountLink] -->|maps internal -> github| GT
  GT -->|resolves members via| CLDAP[ClusterLDAPGroupProvider]
  GT -->|resolves members via| CGEMP[ClusterGeneric HTTP Provider]
  GT -->|resolves members via| CSMP[ClusterStaticMemberProvider]
```

## How Reconciliation Works

```mermaid
sequenceDiagram
  participant User as You (apply CRDs)
  participant K8s as Kubernetes API
  participant Ctrl as Controllers
  participant GH as GitHub
  User->>K8s: Apply Github, GithubOrganization, Team, Providers
  K8s-->>Ctrl: Watch events
  Ctrl->>Ctrl: Resolve members (Greenhouse/LDAP/HTTP/Static)
  Ctrl->>GH: Ensure team exists + membership set
  Ctrl->>GH: Ensure repo permissions (defaults + GithubTeamRepository)
  Ctrl->>GH: Ensure org owners
```

## Controller Overview

| Controller | CRDs Watched | Responsibility |
|---|---|---|
| **Github** | `Github` | Validates GitHub App connectivity and surfaces status. |
| **GithubOrganization** | `GithubOrganization`, `GithubTeamRepository` | Manages org owners, team creation/deletion, default repo team permissions. |
| **GithubTeam** | `GithubTeam` | Resolves member list from a provider and syncs team membership on GitHub. |
| **GithubAccountLink** | `GithubAccountLink` | Maps internal user IDs to GitHub user IDs and performs email domain verification. |
| **LDAP Provider** | `LDAPGroupProvider`, `ClusterLDAPGroupProvider` | Periodically fetches group membership from LDAP/AD. |
| **Generic HTTP Provider** | `GenericExternalMemberProvider`, `ClusterGenericExternalMemberProvider` | Fetches member lists from a JSON HTTP API. |
| **Static Provider** | `StaticMemberProvider`, `ClusterStaticMemberProvider` | Serves an in-CRD static list; no external calls needed. |

## Rate Limiting & Backoff

When GitHub returns a rate-limit error the controller extracts the reset timestamp from the error message and requeues the resource with a `RequeueAfter` duration set to the reset time. This avoids busy-looping while still converging as soon as possible.

## Dry Run

Every mutable CRD supports the `repo-guard.cloudoperators.dev/dryRun: "true"` label. When set, the controller logs all intended operations and writes them to `.status` but makes no API calls to GitHub. This is useful for previewing the impact of a new policy before activating it.
