# check-status

Analyze the health of the repo-guard controller manager: pod state, recent error logs, Prometheus metrics, the status of all managed CRs, and the label configuration vs. actual operation state. Produce a structured report with findings and recommendations.

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
6. **Label audit** — for each `GithubOrganization` and `GithubTeam`, surface all operational labels and cross-reference them against the actual operation state in `.status.operations`. Flag TTL labels where matching ops are overdue, add/remove labels with stuck failed ops, and dry-run mode active.
7. **Summary & recommendations** — synthesize findings into an **Overall Health** verdict and concrete action items.

---

## Steps in detail

### 1 — Pod health

Run in parallel:

```bash
kubectl get pods -n greenhouse -l control-plane=controller-manager \
  -o custom-columns='NAME:.metadata.name,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,CREATED:.metadata.creationTimestamp,STATUS:.status.phase'

kubectl describe pod -n greenhouse \
  $(kubectl get pods -n greenhouse -l control-plane=controller-manager -o name | head -1 | sed 's|pod/||') \
  | grep -A5 'Last State:\|Reason:\|Message:'
```

Note: pod name must be resolved first before describing it.

### 2 — Error logs

```bash
POD=$(kubectl get pods -n greenhouse -l control-plane=controller-manager -o name | head -1 | sed 's|pod/||')
kubectl logs -n greenhouse "$POD" --tail=200 2>&1 \
  | grep -E '"level":"error"|"level":"warn"|"level":"panic"|ERROR|WARN|panic|ratelimit|rate.limit|rate_limit' \
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
    error:     (.status.error // ""),
    timestamp: (.status.timestamp // "n/a"),
    failedRepoOps:  ([(.status.operations.repoOperations  // [] | .[] | select(.state=="failed"))] | length),
    failedOwnerOps: ([(.status.operations.organizationOwnerOperations // [] | .[] | select(.state=="failed"))] | length),
    failedTeamOps:  ([(.status.operations.teamOperations              // [] | .[] | select(.state=="failed"))] | length),
    pendingRepoOps:  ([(.status.operations.repoOperations  // [] | .[] | select(.state=="pending"))] | length),
    pendingOwnerOps: ([(.status.operations.organizationOwnerOperations // [] | .[] | select(.state=="pending"))] | length),
    pendingTeamOps:  ([(.status.operations.teamOperations              // [] | .[] | select(.state=="pending"))] | length)
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
    error:  (.status.error // ""),
    failed: ([(.status.operations // [] | .[] | select(.state=="failed"))] | length),
    pending: ([(.status.operations // [] | .[] | select(.state=="pending"))] | length)
  } |
  [.ns, .name, .status, "failed=\(.failed)", "pending=\(.pending)"] | @tsv' \
  | column -t
```

### 6 — Label audit

#### 6a — GithubOrganization label inspection

```bash
kubectl get githuborganization -A -o json | jq -r '
  .items[] |
  . as $r |
  {
    ns:   .metadata.namespace,
    name: .metadata.name,
    labels: {
      dryRun:                (.metadata.labels["repo-guard.cloudoperators.dev/dryRun"] // ""),
      addOrganizationOwner:  (.metadata.labels["repo-guard.cloudoperators.dev/addOrganizationOwner"] // ""),
      removeOrganizationOwner: (.metadata.labels["repo-guard.cloudoperators.dev/removeOrganizationOwner"] // ""),
      addTeam:               (.metadata.labels["repo-guard.cloudoperators.dev/addTeam"] // ""),
      removeTeam:            (.metadata.labels["repo-guard.cloudoperators.dev/removeTeam"] // ""),
      addRepositoryTeam:     (.metadata.labels["repo-guard.cloudoperators.dev/addRepositoryTeam"] // ""),
      removeRepositoryTeam:  (.metadata.labels["repo-guard.cloudoperators.dev/removeRepositoryTeam"] // ""),
      cleanOperations:       (.metadata.labels["repo-guard.cloudoperators.dev/cleanOperations"] // ""),
      failedTTL:             (.metadata.labels["repo-guard.cloudoperators.dev/failedTTL"] // ""),
      completedTTL:          (.metadata.labels["repo-guard.cloudoperators.dev/completedTTL"] // "")
    },
    ops: {
      failedOwner: ([(.status.operations.organizationOwnerOperations // [] | .[] | select(.state=="failed"))] | length),
      failedTeam:  ([(.status.operations.teamOperations              // [] | .[] | select(.state=="failed"))] | length),
      failedRepo:  ([(.status.operations.repoOperations              // [] | .[] | select(.state=="failed"))] | length),
      completeOwner: ([(.status.operations.organizationOwnerOperations // [] | .[] | select(.state=="complete"))] | length),
      completeTeam:  ([(.status.operations.teamOperations              // [] | .[] | select(.state=="complete"))] | length),
      completeRepo:  ([(.status.operations.repoOperations              // [] | .[] | select(.state=="complete"))] | length)
    }
  } |
  "\(.ns)/\(.name)\n  labels: dryRun=\(.labels.dryRun) failedTTL=\(.labels.failedTTL) completedTTL=\(.labels.completedTTL)\n         addOwner=\(.labels.addOrganizationOwner) removeOwner=\(.labels.removeOrganizationOwner) addTeam=\(.labels.addTeam) removeTeam=\(.labels.removeTeam) addRepo=\(.labels.addRepositoryTeam) removeRepo=\(.labels.removeRepositoryTeam) clean=\(.labels.cleanOperations)\n  ops:    failedOwner=\(.ops.failedOwner) failedTeam=\(.ops.failedTeam) failedRepo=\(.ops.failedRepo) completeOwner=\(.ops.completeOwner) completeTeam=\(.ops.completeTeam) completeRepo=\(.ops.completeRepo)"
'
```

#### 6b — GithubTeam label inspection

```bash
kubectl get githubteam -A -o json | jq -r '
  .items[] |
  {
    ns:   .metadata.namespace,
    name: .metadata.name,
    labels: {
      dryRun:                  (.metadata.labels["repo-guard.cloudoperators.dev/dryRun"] // ""),
      orphaned:                (.metadata.labels["repo-guard.cloudoperators.dev/orphaned"] // ""),
      addUser:                 (.metadata.labels["repo-guard.cloudoperators.dev/addUser"] // ""),
      removeUser:              (.metadata.labels["repo-guard.cloudoperators.dev/removeUser"] // ""),
      disableInternalUsernames:(.metadata.labels["repo-guard.cloudoperators.dev/disableInternalUsernames"] // ""),
      requireVerifiedEmail:    (.metadata.labels["repo-guard.cloudoperators.dev/require-verified-domain-email"] // ""),
      failedTTL:               (.metadata.labels["repo-guard.cloudoperators.dev/failedTTL"] // ""),
      completedTTL:            (.metadata.labels["repo-guard.cloudoperators.dev/completedTTL"] // ""),
      notfoundTTL:             (.metadata.labels["repo-guard.cloudoperators.dev/notfoundTTL"] // ""),
      skippedTTL:              (.metadata.labels["repo-guard.cloudoperators.dev/skippedTTL"] // "")
    },
    ops: {
      failed:   ([(.status.operations // [] | .[] | select(.state=="failed"))]   | length),
      complete: ([(.status.operations // [] | .[] | select(.state=="complete"))] | length),
      notfound: ([(.status.operations // [] | .[] | select(.state=="notfound"))] | length),
      skipped:  ([(.status.operations // [] | .[] | select(.state=="skipped"))]  | length),
      pending:  ([(.status.operations // [] | .[] | select(.state=="pending"))]  | length)
    }
  } |
  "\(.ns)/\(.name)\n  labels: dryRun=\(.labels.dryRun) orphaned=\(.labels.orphaned) addUser=\(.labels.addUser) removeUser=\(.labels.removeUser)\n         disableInternalUsernames=\(.labels.disableInternalUsernames) requireVerifiedEmail=\(.labels.requireVerifiedEmail)\n         failedTTL=\(.labels.failedTTL) completedTTL=\(.labels.completedTTL) notfoundTTL=\(.labels.notfoundTTL) skippedTTL=\(.labels.skippedTTL)\n  ops:    failed=\(.ops.failed) complete=\(.ops.complete) notfound=\(.ops.notfound) skipped=\(.ops.skipped) pending=\(.ops.pending)"
'
```

#### 6c — Label-vs-operations anomaly detection

For each resource, flag the following conditions:

**GithubOrganization anomalies:**
- `failedTTL` is set but there are failed operations older than the TTL duration (controller stuck / not reconciling).
- `failedTTL` is **not** set but there are failed operations present (no automatic cleanup configured).
- `completedTTL` is set but there are completed operations older than the TTL duration.
- `addOrganizationOwner=true` but there are failed add-owner operations (additions failing).
- `removeOrganizationOwner=true` but there are failed remove-owner operations.
- `addTeam=true` / `removeTeam=true` / `addRepositoryTeam=true` / `removeRepositoryTeam=true` with failed team/repo operations.
- `dryRun=true` — mutations are suppressed; note this prominently.
- `cleanOperations=complete` or `cleanOperations=failed` — one-shot cleanup label present (may have already fired).

**GithubTeam anomalies:**
- `failedTTL` is set but failed operations are present (may be within TTL, but worth surfacing).
- `failedTTL` is **not** set but failed operations exist — no auto-cleanup.
- `notfoundTTL` is set but `notfound` operations exist.
- `skippedTTL` is set but `skipped` operations exist.
- `completedTTL` is set but `complete` operations exist.
- `addUser=true` with failed add-user operations.
- `removeUser=true` with failed remove-user operations.
- `dryRun=true` — mutations suppressed.
- `orphaned=true` — team is marked orphaned; controller will not manage memberships.

```bash
# Quick anomaly scan: resources with failed ops but no failedTTL configured
echo "=== GithubOrganizations with failed ops and no failedTTL ==="
kubectl get githuborganization -A -o json | jq -r '
  .items[] |
  select(
    ((.status.operations.organizationOwnerOperations // [] | map(select(.state=="failed")) | length) +
     (.status.operations.teamOperations              // [] | map(select(.state=="failed")) | length) +
     (.status.operations.repoOperations              // [] | map(select(.state=="failed")) | length)) > 0
    and
    (.metadata.labels["repo-guard.cloudoperators.dev/failedTTL"] // "" | length) == 0
  ) |
  "\(.metadata.namespace)/\(.metadata.name) — has failed ops but failedTTL label is not set"
'

echo "=== GithubTeams with failed ops and no failedTTL ==="
kubectl get githubteam -A -o json | jq -r '
  .items[] |
  select(
    (.status.operations // [] | map(select(.state=="failed")) | length) > 0
    and
    (.metadata.labels["repo-guard.cloudoperators.dev/failedTTL"] // "" | length) == 0
  ) |
  "\(.metadata.namespace)/\(.metadata.name) — has failed ops but failedTTL label is not set"
'

echo "=== Resources in dry-run mode ==="
kubectl get githuborganization -A -o json | jq -r '
  .items[] |
  select(.metadata.labels["repo-guard.cloudoperators.dev/dryRun"] == "true") |
  "GithubOrganization \(.metadata.namespace)/\(.metadata.name) — DRY RUN active (mutations suppressed)"
'
kubectl get githubteam -A -o json | jq -r '
  .items[] |
  select(.metadata.labels["repo-guard.cloudoperators.dev/dryRun"] == "true") |
  "GithubTeam \(.metadata.namespace)/\(.metadata.name) — DRY RUN active (mutations suppressed)"
'

echo "=== Orphaned GithubTeams ==="
kubectl get githubteam -A -o json | jq -r '
  .items[] |
  select(.metadata.labels["repo-guard.cloudoperators.dev/orphaned"] != null) |
  "GithubTeam \(.metadata.namespace)/\(.metadata.name) — orphaned=\(.metadata.labels["repo-guard.cloudoperators.dev/orphaned"]) (membership management disabled)"
'
```

### 7 — GithubAccountLink & provider statuses

```bash
kubectl get githubaccountlink -A --no-headers 2>/dev/null | wc -l
kubectl get ldapgroupprovider -A -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get genericexternalmemberprovider -A -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get staticmemberprovider -A -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get clusterldapgroupprovider -o custom-columns='NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get clustergenericexternalmemberprovider -o custom-columns='NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
kubectl get clusterstaticmemberprovider -o custom-columns='NAME:.metadata.name,STATUS:.status.state' 2>/dev/null
```

### 8 — Summary report

Present the findings in the following structure:

```
## repo-guard Health Report — <timestamp>

### Overall Health: [HEALTHY | DEGRADED | UNHEALTHY]

### Pod
- Status: <Running|CrashLoopBackOff|...>
- Restarts: <N>
- Created: <RFC3339 timestamp>

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

### Label Audit Findings
- <resource> — <anomaly description>
  e.g. "greenhouse/my-org — 3 failed owner ops, no failedTTL configured (ops will persist indefinitely)"
  e.g. "greenhouse/my-team — dryRun=true, mutations suppressed"
  e.g. "greenhouse/my-team — failedTTL=1h but 2 failed ops are >24h old (controller may not be reconciling)"

### Issues Found
1. <issue 1 — resource, description, likely cause>
2. <issue 2 — ...>

### Recommendations
- <actionable recommendation 1>
- <actionable recommendation 2>
```

**Health verdict rules:**
- `HEALTHY` — no errors in logs, all reconcile error ratios < 1%, zero failed CRs, no label anomalies.
- `DEGRADED` — some failed CRs or error ratio 1–10%, or label anomalies present, but no panics or rate-limit loops.
- `UNHEALTHY` — pod restarts, panic logs, error ratio > 10%, or multiple orgs stuck in `failed`/`ratelimited`.

---

## Notes

- All CRDs are in the `repo-guard.cloudoperators.dev` API group.
- Controller names in metrics use PascalCase (e.g. `GithubOrganization`, `GithubTeam`).
- The metrics port is `9443` but is served via the Kubernetes API proxy at `/api/v1/namespaces/greenhouse/pods/<pod>/proxy/metrics`. Use `kubectl get --raw` rather than `kubectl exec wget`.
- Status field paths:
  - Org status: `.status.orgStatus` (values: `complete`, `failed`, `ratelimited`, `pending`)
  - Org error: `.status.error`
  - Org operations: `.status.operations.{organizationOwnerOperations, teamOperations, repoOperations}`
  - Team status: `.status.teamStatus`
  - Team error: `.status.error`
  - Team operations: `.status.operations` (flat `[]GithubUserOperation`)
- Operation states: `pending`, `complete`, `failed`, `skipped`, `notfound` (note: `notfound` and `skipped` only appear in team ops)
- TTL labels take a Go duration string (e.g. `1h`, `24h`, `7d`). An empty or invalid value disables cleanup.
- `dryRun=true` suppresses all GitHub API mutations for that resource — add/remove operations are logged but not executed.
- `orphaned=true` on a GithubTeam disables membership management entirely for that team.
