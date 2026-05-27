# check-status

Analyze the health of the repo-guard controller manager: pod state, recent error logs, Prometheus metrics, and the status of all managed CRs. Produce a structured report with findings and recommendations.

## Usage

```
/check-status
```

## What this command does

1. **Pod health** — check the controller manager pod's phase, restarts, and age.
2. **Recent logs** — tail the last 200 lines, extract ERROR/WARN entries and surface distinct error patterns.
3. **Metrics** — scrape `/metrics` via `kubectl get --raw` and summarise reconcile rates, error ratios, and operation counts.
4. **GithubOrganization statuses** — list every CR by namespace/name/status, highlight `failed` and `ratelimited` ones, and show their pending/failed operation counts.
5. **GithubTeam statuses** — list failed/ratelimited teams with failed operation counts.
6. **Summary & recommendations** — synthesize findings into an **Overall Health** verdict and concrete action items.

---

## Steps in detail

### 1 — Pod health

Run in parallel:

```bash
kubectl get pods -n greenhouse -l control-plane=controller-manager \
  -o custom-columns='NAME:.metadata.name,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,AGE:.metadata.creationTimestamp,STATUS:.status.phase'

kubectl describe pod -n greenhouse \
  $(kubectl get pods -n greenhouse -l control-plane=controller-manager -o name | head -1 | sed 's|pod/||') \
  | grep -A5 'Last State:\|Reason:\|Message:'
```

Note: pod name must be resolved first before describing it.

### 2 — Error logs

```bash
POD=$(kubectl get pods -n greenhouse -l control-plane=controller-manager -o name | head -1 | sed 's|pod/||')
kubectl logs -n greenhouse "$POD" --tail=200 2>&1 \
  | grep -E '"level":"error"|ERROR|WARN|panic|ratelimit|rate.limit|rate_limit' \
  | head -60
```

Group errors by:
- **Distinct error messages** (strip variable parts like resource names, timestamps).
- **Affected resource** (`GithubOrganization`, `GithubTeam`, etc.).
- **GitHub API errors** (404, 403, 429, 5xx patterns).

### 3 — Metrics

```bash
POD=$(kubectl get pods -n greenhouse -l control-plane=controller-manager -o name | head -1 | sed 's|pod/||')
kubectl get --raw "/api/v1/namespaces/greenhouse/pods/${POD}/proxy/metrics" \
  | grep -E '^repo_guard_(controller_reconcile_total|githuborganization_status|githubteam_status|githuborganization_operations|githubteam_operations)' \
  | sort
```

From the output compute and display:

**Reconcile totals by controller and result:**
```
controller              | success | error | requeue
GithubOrganization      |   ...   |  ...  |   ...
GithubTeam              |   ...   |   0   |   ...
```

**Error ratio** = `error / (success + error + requeue)` — flag any controller with ratio > 1%.

**GithubOrganization status distribution** — count resources per status value (complete/failed/ratelimited/pending/dry-run).

**GithubTeam status distribution** — same.

**Org operations with non-zero failed counts** — list label sets where `state="failed"` > 0.

### 4 — GithubOrganization CR statuses

```bash
kubectl get githuborganization -A -o json | jq -r '
  .items[] |
  {
    ns:        .metadata.namespace,
    name:      .metadata.name,
    status:    (.status.orgStatus // "unknown"),
    timestamp: (.status.timestamp // "n/a"),
    failedRepoOps:  ([(.status.operations.repoOperations  // [] | .[] | select(.state=="failed")) | {repo:.repo, op:.operation}] | length),
    failedOwnerOps: ([(.status.operations.organizationOwnerOperations // [] | .[] | select(.state=="failed"))] | length),
    failedTeamOps:  ([(.status.operations.teamOperations              // [] | .[] | select(.state=="failed"))] | length),
    pendingRepoOps:  ([(.status.operations.repoOperations  // [] | .[] | select(.state=="pending")) ] | length),
    pendingOwnerOps: ([(.status.operations.organizationOwnerOperations // [] | .[] | select(.state=="pending")) ] | length),
    pendingTeamOps:  ([(.status.operations.teamOperations              // [] | .[] | select(.state=="pending")) ] | length)
  } |
  [.ns, .name, .status, (.timestamp[0:19] // "n/a"),
   "failedRepo=\(.failedRepoOps) failedOwner=\(.failedOwnerOps) failedTeam=\(.failedTeamOps)",
   "pendingRepo=\(.pendingRepoOps) pendingOwner=\(.pendingOwnerOps) pendingTeam=\(.pendingTeamOps)"] |
  @tsv' | column -t
```

For each **failed** organization, show the first 5 distinct failed operation errors (from the status Operations list).

### 5 — GithubTeam CR statuses

```bash
kubectl get githubteam -A -o json | jq -r '
  .items[] |
  select(.status.teamStatus != "complete") |
  {
    ns:     .metadata.namespace,
    name:   .metadata.name,
    status: .status.teamStatus,
    failed: ([(.status.operations // [] | .[] | select(.state=="failed"))] | length),
    pending: ([(.status.operations // [] | .[] | select(.state=="pending"))] | length)
  } |
  [.ns, .name, .status, "failed=\(.failed)", "pending=\(.pending)"] | @tsv' \
  | column -t
```

### 6 — GithubAccountLink & provider statuses

```bash
kubectl get githubaccountlink -A --no-headers 2>/dev/null | wc -l
kubectl get ldapgroupprovider -A -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get genericexternalmemberprovider -A -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get staticmemberprovider -A -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
```

### 7 — Summary report

Present the findings in the following structure:

```
## repo-guard Health Report — <timestamp>

### Overall Health: [HEALTHY | DEGRADED | UNHEALTHY]

### Pod
- Status: <Running|CrashLoopBackOff|...>
- Restarts: <N>
- Age: <duration>

### Reconcile Metrics (since pod start)
| Controller          | Success | Error | Requeue | Error% |
|---------------------|---------|-------|---------|--------|
| GithubOrganization  |  ...    |  ...  |   ...   |  ...%  |
| GithubTeam          |  ...    |   0   |   ...   |   0%   |
| ...                 |         |       |         |        |

### Resource Status Summary
| Kind                 | Total | complete | failed | ratelimited | pending |
|----------------------|-------|----------|--------|-------------|---------|
| GithubOrganization   |  ...  |   ...    |  ...   |     ...     |   ...   |
| GithubTeam           |  ...  |   ...    |  ...   |     ...     |   ...   |

### Issues Found
1. <issue 1 — resource, description, likely cause>
2. <issue 2 — ...>

### Recommendations
- <actionable recommendation 1>
- <actionable recommendation 2>
```

**Health verdict rules:**
- `HEALTHY` — no errors in logs, all reconcile error ratios < 1%, zero failed CRs.
- `DEGRADED` — some failed CRs or error ratio 1–10%, but no panics or rate-limit loops.
- `UNHEALTHY` — pod restarts, panic logs, error ratio > 10%, or multiple orgs stuck in `failed`/`ratelimited`.

---

## Notes

- The metrics port is `9443` but is served via the Kubernetes API proxy at `/api/v1/namespaces/greenhouse/pods/<pod>/proxy/metrics`. Use `kubectl get --raw` rather than `kubectl exec wget`.
- All CRDs are in the `repo-guard.cloudoperators.dev` API group.
- Controller names in metrics use PascalCase (e.g. `GithubOrganization`, `GithubTeam`).
- The status field for orgs is `.status.orgStatus` and for teams `.status.teamStatus`.
- Operations are nested under `.status.operations.{repoOperations,organizationOwnerOperations,teamOperations}` for orgs, and `.status.operations` (flat array) for teams.