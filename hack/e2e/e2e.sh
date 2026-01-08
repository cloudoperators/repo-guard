#!/usr/bin/env bash
set -euo pipefail

# End-to-end helper for local k3d-based testing of repo-guard Helm chart
# Commands:
#   up        - create k3d cluster
#   down      - delete k3d cluster
#   install   - generate values from test.env, install CRDs and Helm chart
#   test      - run basic runtime checks against running cluster
#
# Options (env):
#   K3D_CLUSTER    - cluster name (default: repo-guard)
#   NAMESPACE      - namespace (default: default)
#   HELM_RELEASE   - release name (default: repo-guard)
#   CHART_PATH     - path to chart (default: charts/repo-guard)
#   CONTAINER_TOOL - docker or podman (default: docker)
#   E2E_IMAGE_REPO - local image repo name to build/use (default: repo-guard)
#   E2E_IMAGE_TAG  - local image tag to build/use  (default: e2e)
#   E2E_IMAGE      - full image reference (overrides repo+tag)
#   E2E_SKIP_BUILD - if "true", do not build image during `up`
#   E2E_SKIP_IMPORT- if "true", do not import image into k3d during `up`

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)
ROOT_DIR=$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)

K3D_CLUSTER=${K3D_CLUSTER:-repo-guard}
NAMESPACE=${NAMESPACE:-default}
HELM_RELEASE=${HELM_RELEASE:-repo-guard}
CHART_PATH=${CHART_PATH:-${ROOT_DIR}/charts/repo-guard}
VALUES_OUT=${VALUES_OUT:-${SCRIPT_DIR}/values.generated.yaml}
ENV_FILE_DEFAULT=${ENV_FILE_DEFAULT:-${ROOT_DIR}/internal/controller/test.env}
CONTAINER_TOOL=${CONTAINER_TOOL:-docker}
E2E_IMAGE_REPO=${E2E_IMAGE_REPO:-repo-guard}
E2E_IMAGE_TAG=${E2E_IMAGE_TAG:-e2e}
E2E_IMAGE=${E2E_IMAGE:-${E2E_IMAGE_REPO}:${E2E_IMAGE_TAG}}
E2E_TRIGGER_LABEL_KEY=${E2E_TRIGGER_LABEL_KEY:-githubguard.sap/trigger}
# Default to using the in-repo dummy EMP HTTP server for E2E runs.
# Set USE_DUMMY_EMP_HTTP=false to use external Profiles instead.
USE_DUMMY_EMP_HTTP=${USE_DUMMY_EMP_HTTP:-true}

# Default to using a local dummy LDAP server too.
# Set USE_DUMMY_LDAP=false to use external LDAP defined in test.env
USE_DUMMY_LDAP=${USE_DUMMY_LDAP:-true}

# Run dummy servers inside the k3d cluster instead of on localhost (recommended)
# When true, this script will build small images for the dummy servers, import
# them into the k3d cluster, deploy them as Deployments+Services, and point Helm
# values at their in-cluster DNS addresses.
USE_INCLUSTER_DUMMIES=${USE_INCLUSTER_DUMMIES:-true}

DUMMY_EMP_HTTP_PID=""
DUMMY_EMP_HTTP_READY_FILE=""
DUMMY_EMP_HTTP_BASE=""

DUMMY_LDAP_PID=""
DUMMY_LDAP_READY_FILE=""
DUMMY_LDAP_BASE=""

# Default image names for in-cluster dummies (can be overridden by env)
EMP_DUMMY_IMAGE=${EMP_DUMMY_IMAGE:-repo-guard-emp-http-dummy:e2e}
LDAP_DUMMY_IMAGE=${LDAP_DUMMY_IMAGE:-repo-guard-ldap-dummy:e2e}

# --- Minimal logging helpers (early) ---
# Some functions below use these before their full definitions appear later.
# Define lightweight versions if they are not already defined; later, richer
# definitions (if present) can override these.
if ! declare -f ts >/dev/null 2>&1; then
  ts() { date '+%Y-%m-%d %H:%M:%S'; }
fi
if ! declare -f log_info >/dev/null 2>&1; then
  log_info() { echo "[$(ts)] INFO: $*" >&2; }
fi
if ! declare -f log_warn >/dev/null 2>&1; then
  log_warn() { echo "[$(ts)] WARN: $*" >&2; }
fi
if ! declare -f log_error >/dev/null 2>&1; then
  log_error() { echo "[$(ts)] ERROR: $*" >&2; }
fi
if ! declare -f log_step >/dev/null 2>&1; then
  log_step() { echo "[$(ts)] >>> $*" >&2; }
fi

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "ERROR: $1 is required." >&2; exit 1; }
}

check_prereqs() {
  require k3d
  require kubectl
  require helm
  require awk
  require sed
  require ${CONTAINER_TOOL}
  # Local dummy servers need 'go' only when not using in-cluster dummies
  if [[ "${USE_INCLUSTER_DUMMIES}" != "true" ]]; then
    if [[ "${USE_DUMMY_EMP_HTTP}" == "true" ]]; then
      require go
    fi
    if [[ "${USE_DUMMY_LDAP}" == "true" ]]; then
      require go
    fi
  fi
}

# Ensure we are targeting the expected k3d cluster context for tests,
# otherwise ask the user for confirmation before proceeding.
ensure_test_context_or_confirm() {
  local expected_ctx="k3d-${K3D_CLUSTER}"
  local current_ctx
  current_ctx=$(kubectl config current-context 2>/dev/null || true)

  # If kubectl has no current-context, this is suspicious.
  if [[ -z "${current_ctx}" ]]; then
    echo "[$(ts)] WARN: kubectl has no current-context configured." >&2
  fi

  # If current context already matches the expected k3d context, continue silently.
  if [[ "${current_ctx}" == "${expected_ctx}" ]]; then
    return 0
  fi

  # Best-effort: detect if the expected context exists to provide a helpful hint.
  local has_expected="false"
  if kubectl config get-contexts -o name 2>/dev/null | grep -qx "${expected_ctx}"; then
    has_expected="true"
  fi

  echo "[$(ts)] ATTENTION: E2E tests are about to run against kubectl context: '${current_ctx:-<none>}'" >&2
  echo "[$(ts)] Expected k3d context for this run: '${expected_ctx}'" >&2
  if [[ "${has_expected}" == "true" ]]; then
    echo "[$(ts)] HINT: Switch with 'kubectl config use-context ${expected_ctx}' or run 'make e2e-up' to (re)create and export kubeconfig." >&2
  else
    echo "[$(ts)] HINT: Run 'make e2e-up' to create the k3d cluster and export kubeconfig for this script." >&2
  fi

  # Ask for confirmation only if interactive; otherwise, fail safe.
  if [[ -t 0 ]]; then
    read -r -p "Proceed with NON-k3d context '${current_ctx:-<none>}'? [y/N]: " reply
    case "${reply}" in
      y|Y|yes|YES)
        echo "[$(ts)] Proceeding as requested..." >&2
        return 0
        ;;
      *)
        echo "[$(ts)] Aborting per user choice. Please switch to '${expected_ctx}' and re-run." >&2
        exit 1
        ;;
    esac
  else
    echo "[$(ts)] Non-interactive session detected; aborting to avoid running tests against the wrong cluster." >&2
    echo "[$(ts)] Please switch to '${expected_ctx}' (kubectl config use-context ${expected_ctx}) and re-run." >&2
    exit 1
  fi
}

# Ensure kubeconfig exists on disk and export KUBECONFIG for this session
ensure_kubeconfig_env() {
  local kube_file="${SCRIPT_DIR}/.kubeconfig_${K3D_CLUSTER}"
  log_step "Exporting kubeconfig for cluster ${K3D_CLUSTER}"
  # Write kubeconfig content to a stable path for the session
  k3d kubeconfig get "${K3D_CLUSTER}" > "${kube_file}"
  export KUBECONFIG="${kube_file}"
  log_info "KUBECONFIG=${KUBECONFIG}"
}

# Build manager image locally using the selected container tool
build_image() {
  log_step "Building local image: ${E2E_IMAGE}"
  (cd "${ROOT_DIR}" && ${CONTAINER_TOOL} build -t "${E2E_IMAGE}" .)
}

# Import a local image into the k3d cluster so Kubernetes can pull it without a registry
import_image() {
  log_step "Importing image into k3d cluster ${K3D_CLUSTER}: ${E2E_IMAGE}"
  k3d image import -c "${K3D_CLUSTER}" "${E2E_IMAGE}" --mode direct
}

# Helper to read VAR=value from test.env with optional quotes
read_env_var() {
  local name=$1
  local file=${2:-$ENV_FILE_DEFAULT}
  local val
  # support optional spaces around '=' in env file (e.g., VAR = "value")
  # Use [[:space:]] to be robust against tabs etc.
  val=$(grep -E "^${name}[[:space:]]*=" "$file" | tail -n1 | sed -E 's/^[^=]+=//') || true
  # remove surrounding quotes (single or double) and trim spaces
  val=$(echo -n "$val" | sed -E "s/^[[:space:]]*[\"']?//; s/[\"']?[[:space:]]*$//")
  echo -n "$val"
}

cmd_up() {
  section_begin "k3d cluster UP (${K3D_CLUSTER})"
  check_prereqs
  if k3d cluster list | awk 'NR>1 {print $1}' | grep -qx "${K3D_CLUSTER}"; then
    log_info "k3d cluster ${K3D_CLUSTER} already exists"
    # Ensure kubeconfig is written and exported for subsequent kubectl/helm commands
    ensure_kubeconfig_env
    # Optionally build and import image into existing cluster
    if [[ "${E2E_SKIP_BUILD:-false}" != "true" ]]; then
      build_image
    else
      log_info "E2E_SKIP_BUILD=true → skipping image build"
    fi
    if [[ "${E2E_SKIP_IMPORT:-false}" != "true" ]]; then
      import_image
    else
      log_info "E2E_SKIP_IMPORT=true → skipping k3d image import"
    fi
    section_end
    return 0
  fi
  log_step "Creating k3d cluster: ${K3D_CLUSTER}"
  k3d cluster create "${K3D_CLUSTER}" --wait --timeout 120s
  # Write kubeconfig and export KUBECONFIG for this session
  ensure_kubeconfig_env
  # Build and import local image for immediate use
  if [[ "${E2E_SKIP_BUILD:-false}" != "true" ]]; then
    build_image
  else
    log_info "E2E_SKIP_BUILD=true → skipping image build"
  fi
  if [[ "${E2E_SKIP_IMPORT:-false}" != "true" ]]; then
    import_image
  else
    log_info "E2E_SKIP_IMPORT=true → skipping k3d image import"
  fi
  section_end
}

cmd_down() {
  check_prereqs
  section_begin "k3d cluster DOWN (${K3D_CLUSTER})"
  log_step "Deleting k3d cluster: ${K3D_CLUSTER}"
  k3d cluster delete "${K3D_CLUSTER}" || true
  # Best-effort cleanup of kubeconfig file for this cluster
  local kube_file="${SCRIPT_DIR}/.kubeconfig_${K3D_CLUSTER}"
  if [[ -f "${kube_file}" ]]; then
    rm -f "${kube_file}" || true
  fi
  section_end
}

cmd_gen_values() {
  log_step "Generating Helm values from internal/controller/test.env -> ${VALUES_OUT}"
  local -a extra_env=()
  if [[ -n "${DUMMY_EMP_HTTP_BASE}" ]]; then
    extra_env+=(EMP_HTTP_DUMMY_BASE="${DUMMY_EMP_HTTP_BASE}")
  fi
  if [[ -n "${DUMMY_LDAP_BASE}" ]]; then
    extra_env+=(LDAP_HOST_OVERRIDE="${DUMMY_LDAP_BASE}")
  fi
  # With 'set -u' enabled, expanding an empty array triggers an error. Avoid that.
  if ((${#extra_env[@]})); then
    ( env "${extra_env[@]}" bash "${SCRIPT_DIR}/gen-values.sh" "${ROOT_DIR}/internal/controller/test.env" >"${VALUES_OUT}" )
  else
    ( bash "${SCRIPT_DIR}/gen-values.sh" "${ROOT_DIR}/internal/controller/test.env" >"${VALUES_OUT}" )
  fi
  log_info "Generated values file at ${VALUES_OUT}"
}

# Build and import dummy EMP HTTP server image into k3d
build_import_dummy_emp_image() {
  local img="${EMP_DUMMY_IMAGE}"
  log_step "Building dummy EMP HTTP image: ${img}"
  local tmpdir
  tmpdir=$(mktemp -d)
  cat >"${tmpdir}/Dockerfile" <<'EOF'
FROM --platform=$BUILDPLATFORM golang:1.25 as build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/emp-http-server ./hack/emp-http-server

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=build /out/emp-http-server /app/emp-http-server
EXPOSE 18080
USER nonroot
ENTRYPOINT ["/app/emp-http-server"]
EOF
  (${CONTAINER_TOOL} build -f "${tmpdir}/Dockerfile" -t "${img}" "${ROOT_DIR}" >/dev/null)
  rm -rf "${tmpdir}"
  log_step "Importing image into k3d: ${img}"
  k3d image import --cluster "${K3D_CLUSTER}" "${img}" --mode direct >/dev/null
}

# Build and import dummy LDAP server image into k3d
build_import_dummy_ldap_image() {
  local img="${LDAP_DUMMY_IMAGE}"
  log_step "Building dummy LDAP image: ${img}"
  local tmpdir
  tmpdir=$(mktemp -d)
  cat >"${tmpdir}/Dockerfile" <<'EOF'
FROM --platform=$BUILDPLATFORM golang:1.25 as build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/ldap-testserver ./hack/ldap-testserver

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ldap-testserver /app/ldap-testserver
EXPOSE 1389
USER nonroot
ENTRYPOINT ["/app/ldap-testserver"]
EOF
  (${CONTAINER_TOOL} build -f "${tmpdir}/Dockerfile" -t "${img}" "${ROOT_DIR}" >/dev/null)
  rm -rf "${tmpdir}"
  log_step "Importing image into k3d: ${img}"
  k3d image import --cluster "${K3D_CLUSTER}" "${img}" --mode direct >/dev/null
}

# Deploy in-cluster EMP HTTP dummy as Deployment+Service
deploy_incluster_emp_http() {
  local du dp gid uid
  du=$(read_env_var EMP_HTTP_DUMMY_USERNAME)
  dp=$(read_env_var EMP_HTTP_DUMMY_PASSWORD)
  gid=$(read_env_var EMP_HTTP_GROUP_ID)
  uid=$(read_env_var EMP_HTTP_USER_INTERNAL_USERNAME)

  log_step "Deploying in-cluster EMP HTTP dummy (namespace ${NAMESPACE})"
  cat <<EOF | kubectl apply -n "${NAMESPACE}" -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata:
  name: emp-http
spec:
  replicas: 1
  selector:
    matchLabels: {app: emp-http}
  template:
    metadata:
      labels: {app: emp-http}
    spec:
      containers:
        - name: emp-http
          image: ${EMP_DUMMY_IMAGE}
          args: ["-listen", ":18080"]
          ports:
            - name: http
              containerPort: 18080
          env:
            - name: EMP_HTTP_USERNAME
              value: "${du}"
            - name: EMP_HTTP_PASSWORD
              value: "${dp}"
            - name: EMP_HTTP_GROUP_ID
              value: "${gid}"
            - name: EMP_HTTP_USER_INTERNAL_USERNAME
              value: "${uid}"
---
apiVersion: v1
kind: Service
metadata:
  name: emp-http
spec:
  selector:
    app: emp-http
  ports:
    - name: http
      port: 18080
      targetPort: 18080
      protocol: TCP
EOF
  kubectl -n "${NAMESPACE}" rollout status deploy/emp-http --timeout=2m >/dev/null
}

# Deploy in-cluster LDAP dummy as Deployment+Service
deploy_incluster_ldap() {
  local bindDN bindPW baseDN group user
  bindDN=$(read_env_var LDAP_GROUP_PROVIDER_BIND_DN)
  bindPW=$(read_env_var LDAP_GROUP_PROVIDER_BIND_PW)
  baseDN=$(read_env_var LDAP_GROUP_PROVIDER_BASE_DN)
  group=$(read_env_var LDAP_GROUP_PROVIDER_GROUP_NAME)
  user=$(read_env_var LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME)

  log_step "Deploying in-cluster LDAP dummy (namespace ${NAMESPACE})"
  cat <<EOF | kubectl apply -n "${NAMESPACE}" -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ldap-dummy
spec:
  replicas: 1
  selector:
    matchLabels: {app: ldap-dummy}
  template:
    metadata:
      labels: {app: ldap-dummy}
    spec:
      containers:
        - name: ldap
          image: ${LDAP_DUMMY_IMAGE}
          args: ["-listen", ":1389"]
          ports:
            - name: ldap
              containerPort: 1389
          env:
            - name: LDAP_GROUP_PROVIDER_BIND_DN
              value: "${bindDN}"
            - name: LDAP_GROUP_PROVIDER_BIND_PW
              value: "${bindPW}"
            - name: LDAP_GROUP_PROVIDER_BASE_DN
              value: "${baseDN}"
            - name: LDAP_GROUP_PROVIDER_GROUP_NAME
              value: "${group}"
            - name: LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME
              value: "${user}"
---
apiVersion: v1
kind: Service
metadata:
  name: ldap-dummy
spec:
  selector:
    app: ldap-dummy
  ports:
    - name: ldap
      port: 1389
      targetPort: 1389
      protocol: TCP
EOF
  kubectl -n "${NAMESPACE}" rollout status deploy/ldap-dummy --timeout=2m >/dev/null
}

start_dummy_emp_http() {
  if [[ "${USE_DUMMY_EMP_HTTP}" != "true" ]]; then
    return 0
  fi
  # If using in-cluster dummies, do not start local server
  if [[ "${USE_INCLUSTER_DUMMIES}" == "true" ]]; then
    return 0
  fi
  if [[ -n "${DUMMY_EMP_HTTP_PID}" ]]; then
    return 0
  fi
  log_step "Starting dummy Generic HTTP provider server"
  DUMMY_EMP_HTTP_READY_FILE="${SCRIPT_DIR}/.emp_http_ready_${$}.txt"
  # Read dummy credentials and ids from test.env
  local du dp gid uid
  du=$(read_env_var EMP_HTTP_DUMMY_USERNAME)
  dp=$(read_env_var EMP_HTTP_DUMMY_PASSWORD)
  gid=$(read_env_var EMP_HTTP_GROUP_ID)
  uid=$(read_env_var EMP_HTTP_USER_INTERNAL_USERNAME)
  # Start on random localhost port, write base URL to ready file when ready
  # Use pushd/popd to avoid subshell so that $! is set in this shell under 'set -u'
  pushd "${ROOT_DIR}" >/dev/null
  go run ./hack/emp-http-server \
    -listen 127.0.0.1:0 \
    -ready-file "${DUMMY_EMP_HTTP_READY_FILE}" \
    -username "${du}" \
    -password "${dp}" \
    -group "${gid}" \
    -user "${uid}" >/dev/null 2>&1 &
  DUMMY_EMP_HTTP_PID=$!
  popd >/dev/null
  # Wait for ready file to appear and contain URL
  for i in {1..50}; do
    if [[ -s "${DUMMY_EMP_HTTP_READY_FILE}" ]]; then
      DUMMY_EMP_HTTP_BASE=$(cat "${DUMMY_EMP_HTTP_READY_FILE}")
      break
    fi
    sleep 0.1
  done
  if [[ -z "${DUMMY_EMP_HTTP_BASE}" ]]; then
    log_error "Dummy EMP HTTP server did not become ready"
    exit 1
  fi
  log_info "Dummy EMP HTTP base: ${DUMMY_EMP_HTTP_BASE}"
}

stop_dummy_emp_http() {
  if [[ -n "${DUMMY_EMP_HTTP_PID}" ]]; then
    log_step "Stopping dummy Generic HTTP provider server (pid ${DUMMY_EMP_HTTP_PID})"
    kill "${DUMMY_EMP_HTTP_PID}" >/dev/null 2>&1 || true
    wait "${DUMMY_EMP_HTTP_PID}" >/dev/null 2>&1 || true
    DUMMY_EMP_HTTP_PID=""
  fi
  if [[ -n "${DUMMY_EMP_HTTP_READY_FILE}" && -f "${DUMMY_EMP_HTTP_READY_FILE}" ]]; then
    rm -f "${DUMMY_EMP_HTTP_READY_FILE}" || true
    DUMMY_EMP_HTTP_READY_FILE=""
  fi
}

start_dummy_ldap() {
  if [[ "${USE_DUMMY_LDAP}" != "true" ]]; then
    return 0
  fi
  # If using in-cluster dummies, do not start local server
  if [[ "${USE_INCLUSTER_DUMMIES}" == "true" ]]; then
    return 0
  fi
  if [[ -n "${DUMMY_LDAP_PID}" ]]; then
    return 0
  fi
  log_step "Starting dummy LDAP server"
  DUMMY_LDAP_READY_FILE="${SCRIPT_DIR}/.ldap_ready_${$}.txt"
  pushd "${ROOT_DIR}" >/dev/null
  go run ./hack/ldap-testserver \
    -listen 127.0.0.1:0 \
    -ready-file "${DUMMY_LDAP_READY_FILE}" >/dev/null 2>&1 &
  DUMMY_LDAP_PID=$!
  popd >/dev/null
  for i in {1..50}; do
    if [[ -s "${DUMMY_LDAP_READY_FILE}" ]]; then
      DUMMY_LDAP_BASE=$(cat "${DUMMY_LDAP_READY_FILE}")
      break
    fi
    sleep 0.1
  done
  if [[ -z "${DUMMY_LDAP_BASE}" ]]; then
    log_error "Dummy LDAP server did not become ready"
    exit 1
  fi
  log_info "Dummy LDAP host: ${DUMMY_LDAP_BASE}"
}

stop_dummy_ldap() {
  if [[ -n "${DUMMY_LDAP_PID}" ]]; then
    log_step "Stopping dummy LDAP server (pid ${DUMMY_LDAP_PID})"
    kill "${DUMMY_LDAP_PID}" >/dev/null 2>&1 || true
    wait "${DUMMY_LDAP_PID}" >/dev/null 2>&1 || true
    DUMMY_LDAP_PID=""
  fi
  if [[ -n "${DUMMY_LDAP_READY_FILE}" && -f "${DUMMY_LDAP_READY_FILE}" ]]; then
    rm -f "${DUMMY_LDAP_READY_FILE}" || true
    DUMMY_LDAP_READY_FILE=""
  fi
}

cmd_install() {
  section_begin "Install repo-guard Helm release (${HELM_RELEASE})"
  check_prereqs

  # Optionally start local dummy EMP HTTP server
  start_dummy_emp_http
  # Optionally start local dummy LDAP server
  start_dummy_ldap

  # Create namespace if missing
  log_step "Ensuring namespace exists: ${NAMESPACE}"
  kubectl get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl create ns "${NAMESPACE}"

  # If requested, build/import and deploy in-cluster dummy servers and set base URLs
  if [[ "${USE_INCLUSTER_DUMMIES}" == "true" ]]; then
    if [[ "${USE_DUMMY_EMP_HTTP}" == "true" ]]; then
      build_import_dummy_emp_image
      deploy_incluster_emp_http
      # Set base URL for gen-values.sh to use
      DUMMY_EMP_HTTP_BASE="http://emp-http.${NAMESPACE}.svc.cluster.local:18080"
      log_info "Using in-cluster EMP HTTP dummy at ${DUMMY_EMP_HTTP_BASE}"
    fi
    if [[ "${USE_DUMMY_LDAP}" == "true" ]]; then
      build_import_dummy_ldap_image
      deploy_incluster_ldap
      # LDAP must include scheme to avoid ldaps:// default
      DUMMY_LDAP_BASE="ldap://ldap-dummy.${NAMESPACE}.svc.cluster.local:1389"
      log_info "Using in-cluster LDAP dummy at ${DUMMY_LDAP_BASE}"
    fi
  fi

  # Apply external CRD(s): Greenhouse Team CRD
  log_step "Applying Greenhouse Team CRD"
  kubectl apply -f "${ROOT_DIR}/config/crd/external/greenhouse.sap_teams.yaml"

  # Ensure Prometheus Operator CRDs (PodMonitor, PrometheusRule) exist for monitoring templates
  # These come from kube-prometheus-stack upstream CRDs
  log_step "Ensuring Prometheus Operator CRDs (PodMonitor, PrometheusRule) are installed"
  kubectl apply -f "https://raw.githubusercontent.com/prometheus-community/helm-charts/refs/heads/main/charts/kube-prometheus-stack/charts/crds/crds/crd-podmonitors.yaml" || true
  kubectl apply -f "https://raw.githubusercontent.com/prometheus-community/helm-charts/refs/heads/main/charts/kube-prometheus-stack/charts/crds/crds/crd-prometheusrules.yaml" || true

  # Generate values from test.env
  log_step "Generating Helm values from test.env"
  cmd_gen_values

  # Pre-create Greenhouse Team CRs before installing repo-guard (as requested)
  # Read team names from env and apply them so GithubTeam can reference them immediately
  GH_TEAM1=$(read_env_var TEAM_1)
  GH_TEAM2=$(read_env_var TEAM_2)
  GH_OWNER_TEAM=$(read_env_var ORGANIZATION_OWNER_TEAM)
  # Repo default teams (ensure Greenhouse Teams exist if these are GH-backed)
  DEF_PUB_PULL=$(read_env_var DEFAULT_PUBLIC_PULL_TEAM)
  DEF_PUB_PUSH=$(read_env_var DEFAULT_PUBLIC_PUSH_TEAM)
  DEF_PUB_ADMIN=$(read_env_var DEFAULT_PUBLIC_ADMIN_TEAM)
  DEF_PRIV_PULL=$(read_env_var DEFAULT_PRIVATE_PULL_TEAM)
  DEF_PRIV_PUSH=$(read_env_var DEFAULT_PRIVATE_PUSH_TEAM)
  DEF_PRIV_ADMIN=$(read_env_var DEFAULT_PRIVATE_ADMIN_TEAM)
  CUSTOM_PRIV_TEAM=$(read_env_var CUSTOM_PRIVATE_REPO_TEAM)
  if [[ -n "$GH_TEAM1" ]]; then
    log_step "Ensuring Greenhouse Team CR exists: ${GH_TEAM1}"
    apply_greenhouse_team "$GH_TEAM1" || true
  fi
  if [[ -n "$GH_TEAM2" ]]; then
    log_step "Ensuring Greenhouse Team CR exists: ${GH_TEAM2}"
    apply_greenhouse_team "$GH_TEAM2" || true
  fi
  if [[ -n "$GH_OWNER_TEAM" ]]; then
    log_step "Ensuring Greenhouse Team CR exists: ${GH_OWNER_TEAM} (owner team)"
    apply_greenhouse_team "$GH_OWNER_TEAM" || true
  fi
  for t in "$DEF_PUB_PULL" "$DEF_PUB_PUSH" "$DEF_PUB_ADMIN" "$DEF_PRIV_PULL" "$DEF_PRIV_PUSH" "$DEF_PRIV_ADMIN" "$CUSTOM_PRIV_TEAM"; do
    [[ -z "$t" ]] && continue
    # avoid re-applying for ones we already ensured (TEAM_1, TEAM_2, OWNER)
    if [[ "$t" != "$GH_TEAM1" && "$t" != "$GH_TEAM2" && "$t" != "$GH_OWNER_TEAM" ]]; then
      log_step "Ensuring Greenhouse Team CR exists: ${t} (repo default/assignment)"
      apply_greenhouse_team "$t" || true
    fi
  done

  # Install/upgrade Helm release (CRDs under chart/crds will be installed by Helm)
  log_step "Installing Helm chart ${HELM_RELEASE} in namespace ${NAMESPACE}"
  # If Prometheus Operator CRDs are not installed, disable PrometheusRule/PodMonitor templates
  HELM_EXTRA_ARGS=()
  if ! kubectl get crd podmonitors.monitoring.coreos.com >/dev/null 2>&1 || \
     ! kubectl get crd prometheusrules.monitoring.coreos.com >/dev/null 2>&1; then
    log_info "Prometheus Operator CRDs not found; disabling monitoring templates for install"
    HELM_EXTRA_ARGS+=(--set monitoring.enabled=false --set monitoring.podMonitor.enabled=false)
  fi
  helm upgrade --install "${HELM_RELEASE}" "${CHART_PATH}" \
    --namespace "${NAMESPACE}" \
    -f "${VALUES_OUT}" \
    --set manager.image.repository="${E2E_IMAGE_REPO}" \
    --set manager.image.tag="${E2E_IMAGE_TAG}" \
    ${HELM_EXTRA_ARGS[@]:-} \
    --create-namespace \
    --wait --timeout 5m
  section_end
}

cmd_install_dry_run() {
  section_begin "Helm install DRY-RUN (${HELM_RELEASE})"
  check_prereqs
  # In default mode, start the dummy EMP HTTP server so values get a valid endpoint
  start_dummy_emp_http
  # And start dummy LDAP to override LDAP host
  start_dummy_ldap
  # Generate values from test.env to reflect current env (will pick up DUMMY_EMP_HTTP_BASE)
  cmd_gen_values

  log_step "Helm install DRY-RUN for ${HELM_RELEASE} in namespace ${NAMESPACE}"
  log_info "Rendering manifests with --dry-run --debug so you can inspect output"

  HELM_EXTRA_ARGS=()
  # Optionally disable monitoring if Prometheus CRDs are not present, to avoid noise
  if ! kubectl get crd podmonitors.monitoring.coreos.com >/dev/null 2>&1 || \
     ! kubectl get crd prometheusrules.monitoring.coreos.com >/dev/null 2>&1; then
    HELM_EXTRA_ARGS+=(--set monitoring.enabled=false --set monitoring.podMonitor.enabled=false)
  fi

  helm upgrade --install "${HELM_RELEASE}" "${CHART_PATH}" \
    --namespace "${NAMESPACE}" \
    -f "${VALUES_OUT}" \
    --set manager.image.repository="${E2E_IMAGE_REPO}" \
    --set manager.image.tag="${E2E_IMAGE_TAG}" \
    ${HELM_EXTRA_ARGS[@]:-} \
    --create-namespace \
    --dry-run --debug
  # Stop dummy server right after rendering (if it was started)
  stop_dummy_emp_http
  stop_dummy_ldap
  section_end
}

cmd_test() {
  section_begin "E2E tests"
  check_prereqs
  ensure_test_context_or_confirm
  log_step "Running E2E checks in namespace ${NAMESPACE}"

  # Final status reporting with emojis for this test run
  # Set a trap to print a concise failure summary on the first error
  trap 'echo "[$(ts)] ❌  E2E tests FAILED — see logs above for details" >&2; exit 1' ERR

  # 1) Ensure CRDs are present
  log_step "Checking required CRDs exist"
  for crd in githubs.githubguard.sap githuborganizations.githubguard.sap githubteams.githubguard.sap githubteamrepositories.githubguard.sap githubaccountlinks.githubguard.sap staticmemberproviders.githubguard.sap ldapgroupproviders.githubguard.sap genericexternalmemberproviders.githubguard.sap; do
    echo -n "Checking CRD $crd ... "
    kubectl get crd "$crd" >/dev/null
    echo OK
  done

  # 2) Ensure core resources exist (Github, Organization, Teams)
  log_step "Listing Github resources"
  kubectl -n "${NAMESPACE}" get github -o wide

  log_step "Listing GithubOrganization resources"
  kubectl -n "${NAMESPACE}" get githuborganization -o wide || true

  log_step "Listing GithubTeam resources"
  kubectl -n "${NAMESPACE}" get githubteam -o wide || true

  # 3) Verify statuses fields exist (non-empty where available)
  # We only check that the .status is present; the controller may still be reconciling.
  log_step "Verifying status fields (best-effort)"
  kubectl -n "${NAMESPACE}" get github -o json | jq -e '.items | length >= 1' >/dev/null 2>&1 || { echo "No Github resources found"; exit 1; }
  # If jq is not available, skip deep checks
  if command -v jq >/dev/null 2>&1; then
    local github_statuses
    github_statuses=$(kubectl -n "${NAMESPACE}" get github -o json | jq -r '.items[] | "Github: \(.metadata.name) state=\(.status.state // "")"')
    echo "$github_statuses"
    if echo "$github_statuses" | grep -q "state=failed"; then
      echo "ERROR: One or more Github resources are in 'failed' state. Check controller logs." >&2
      kubectl -n "${NAMESPACE}" logs -l control-plane=controller-manager --tail=100 >&2 || true
      exit 1
    fi
    kubectl -n "${NAMESPACE}" get githuborganization -o json 2>/dev/null | jq -r '.items[]? | "GithubOrganization: \(.metadata.name) state=\(.status.orgStatus // "")"' || true
    kubectl -n "${NAMESPACE}" get githubteam -o json 2>/dev/null | jq -r '.items[]? | "GithubTeam: \(.metadata.name) state=\(.status.teamStatus // "")"' || true
  else
    echo "jq not found; skipping JSON status checks"
  fi

  # 4) Metrics check: port-forward one manager pod and curl metrics (robust, no SIGPIPE noise)
  log_step "Checking metrics endpoint"
  POD=$(kubectl -n "${NAMESPACE}" get pods -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}')
  kubectl -n "${NAMESPACE}" port-forward "pod/${POD}" 9443:9443 >/dev/null 2>&1 & PF_PID=$!
  trap 'kill ${PF_PID} >/dev/null 2>&1 || true' EXIT
  sleep 3
  METRICS=""
  for i in 1 2 3 4 5; do
    METRICS=$(curl -fsS http://127.0.0.1:9443/metrics || true)
    [[ -n "$METRICS" ]] && break
    sleep 1
  done
  echo "Sample metrics lines:"
  MET_FILE=$(mktemp)
  # Write once, then read with grep/head to avoid SIGPIPE on printf
  printf "%s" "$METRICS" >"$MET_FILE"
  grep -E "^#|githubguard|controller_runtime_|repo_guard_" "$MET_FILE" | head -n 20 || true
  if grep -qE "controller_runtime_|repo_guard_" "$MET_FILE"; then
    echo "Metrics endpoint looks healthy"
  else
    echo "Metrics endpoint not responsive" >&2
    rm -f "$MET_FILE"
    exit 1
  fi
  rm -f "$MET_FILE"


  # 5) Validate GitHub side using PAT (basic checks)
  log_step "Validating GitHub resources via API"
  GITHUB_API=$(read_env_var GITHUB_V3_API_URL)
  GITHUB_TOKEN=$(read_env_var GITHUB_TOKEN)
  ORG=$(read_env_var ORGANIZATION)
  TEAM_1=$(read_env_var TEAM_1)
  TEAM_2=$(read_env_var TEAM_2)
  OWNER_TEAM=$(read_env_var ORGANIZATION_OWNER_TEAM)
  LDAP_TEAM=$(read_env_var LDAP_GROUP_PROVIDER_TEAM_NAME)
  STATIC_TEAM=$(read_env_var EMP_STATIC_TEAM_NAME)
  HTTP_TEAM=$(read_env_var EMP_HTTP_TEAM_NAME)

  if [[ -z "$GITHUB_TOKEN" ]]; then
    echo "GITHUB_TOKEN missing in ${ENV_FILE_DEFAULT}; skipping GitHub API checks" >&2
  else
    authHeader=( -H "Authorization: token ${GITHUB_TOKEN}" -H "Accept: application/vnd.github+json" )
    echo "Checking organization ${ORG} accessibility"
    http_status=$(curl -sS -o /tmp/org.json -w "%{http_code}" "${GITHUB_API}/orgs/${ORG}" "${authHeader[@]}")
    if [[ "$http_status" != "200" ]]; then
      echo "GitHub org check failed: HTTP ${http_status}" >&2
      cat /tmp/org.json >&2 || true
      exit 1
    fi

    echo "Fetching teams for org ${ORG}"
    http_status=$(curl -sS -o /tmp/teams.json -w "%{http_code}" "${GITHUB_API}/orgs/${ORG}/teams?per_page=100" "${authHeader[@]}")
    if [[ "$http_status" != "200" ]]; then
      echo "GitHub team list failed: HTTP ${http_status}" >&2
      cat /tmp/teams.json >&2 || true
      exit 1
    fi
    TEAMS_JSON=$(cat /tmp/teams.json)
    # Require that defined teams exist
    for t in "$TEAM_1" "$TEAM_2" "$OWNER_TEAM" "$LDAP_TEAM" "$STATIC_TEAM" "$HTTP_TEAM"; do
      [[ -z "$t" ]] && continue
      if command -v jq >/dev/null 2>&1; then
        t_lc=$(printf "%s" "$t" | tr '[:upper:]' '[:lower:]')
        if ! printf "%s" "$TEAMS_JSON" | jq -r '.[] | [.slug, .name] | .[]' | tr '[:upper:]' '[:lower:]' | grep -qx "$t_lc"; then
          echo "Missing team in GitHub: $t" >&2; exit 1
        fi
      else
        # Fallback regex allowing optional spaces after colon
        pattern="\"slug\"[[:space:]]*:[[:space:]]*\"$t\"|\"name\"[[:space:]]*:[[:space:]]*\"$t\""
        echo "$TEAMS_JSON" | grep -qiE "$pattern" || { echo "Missing team in GitHub: $t" >&2; exit 1; }
      fi
    done
    echo "GitHub team presence verified"
  fi

  echo "E2E checks completed"
  # Run the Greenhouse Team membership scenario as part of e2e-test unless skipped
  if [[ "${E2E_SKIP_TEAMS:-false}" == "true" ]]; then
    echo "E2E_SKIP_TEAMS=true set; skipping Greenhouse Team membership scenario"
    # Clear failure trap and report success before exiting this command
    trap - ERR
    echo "[$(ts)] ✅  E2E tests completed successfully 🎉"
    return 0
  fi

  echo "Running e2e scenarios (owner + provider-backed + repository)"
  ensure_jq
  wait_for_controller_readiness
  # Per requested order: owner first, then provider-backed tests, then repo tests
  run_owner_scenario
  run_provider_scenarios
  run_repository_scenarios

  # All steps completed successfully — clear failure trap and print success summary
  trap - ERR
  echo "[$(ts)] ✅  E2E tests completed successfully 🎉"
}

# ------------------------------
# Repository scenarios (public/private team permissions)
# ------------------------------

# Create a repository via GitHub API. Args: <name> <private:true|false>
github_create_repo() {
  local name=$1 priv=$2
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  local body
  body=$(jq -nc --arg n "$name" --argjson p $( [[ "$priv" == "true" ]] && echo true || echo false ) '{name:$n, private:$p}')
  curl -sS -o /tmp/repo-create.json -w "%{http_code}" -X POST \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    -d "$body" \
    "${api}/orgs/${org}/repos"
}

# Fetch repo teams JSON into /tmp/repo-<name>-teams.json; echo HTTP code
github_get_repo_teams_json() {
  local repo=$1
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o "/tmp/repo-${repo}-teams.json" -w "%{http_code}" \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/repos/${org}/${repo}/teams?per_page=100"
}

# Assert a repo has a team with a given permission. Args: <repo> <teamName> <permission>
assert_repo_has_team_permission() {
  local repo=$1 team=$2 perm=$3
  local code
  code=$(github_get_repo_teams_json "$repo")
  if [[ "$code" != "200" ]]; then
    echo "[$(ts)] ERROR: Failed to list teams for repo ${repo} (HTTP ${code})" >&2
    cat "/tmp/repo-${repo}-teams.json" >&2 || true
    return 1
  fi
  if ! jq -e --arg t "$team" --arg p "$perm" '.[] | select((.slug==$t or .name==$t) and (.permission==$p))' "/tmp/repo-${repo}-teams.json" >/dev/null 2>&1; then
    echo "[$(ts)] ERROR: Repo ${repo} missing team '${team}' with permission '${perm}'" >&2
    echo "Teams payload:" >&2
    cat "/tmp/repo-${repo}-teams.json" >&2 || true
    return 1
  fi
  return 0
}

# Wait until GithubOrganization reports some repository team operations OR is complete (best-effort)
wait_for_org_repo_ops() {
  local timeout_sec=${E2E_TIMEOUT:-180}
  local interval=${E2E_INTERVAL:-3}
  local start=$(date +%s)
  local org_name
  org_name=$(get_org_resource_name)
  if [[ -z "$org_name" ]]; then
    echo "[$(ts)] WARN: No GithubOrganization resource found; skipping repo ops wait" >&2
    return 0
  fi

  local PUB=$(read_env_var E2E_REPO_PUBLIC)
  local PRIV=$(read_env_var E2E_REPO_PRIVATE)
  [[ -z "$PUB" ]] && PUB="e2e-pub"
  [[ -z "$PRIV" ]] && PRIV="e2e-priv"
  local TEAM_1=$(read_env_var TEAM_1)
  local TEAM_2=$(read_env_var TEAM_2)
  local OWNER_TEAM=$(read_env_var ORGANIZATION_OWNER_TEAM)

  log_step "Waiting for GithubOrganization repository team operations to appear or status to be complete"
  while true; do
    local org_json
    org_json=$(kubectl -n "${NAMESPACE}" get githuborganization "$org_name" -o json 2>/dev/null) || true
    if [[ -n "$org_json" ]]; then
      local ops_count
      ops_count=$(echo "$org_json" | jq -r '.status.operations.repoOperations | length // 0') || ops_count=0
      local org_status
      org_status=$(echo "$org_json" | jq -r '.status.orgStatus // ""')

      if (( ops_count > 0 )); then
        log_info "Observed ${ops_count} repository team operations"
        return 0
      fi

      if [[ "$org_status" =~ ^[Cc]omplete$ ]]; then
        # If complete, verify that at least one expected permission is already there
        # to be sure we are not just skipping because of a stale complete status
        local code
        code=$(github_get_repo_teams_json "$PUB")
        if [[ "$code" == "200" ]]; then
           if jq -e --arg t "$TEAM_1" '.[] | select(.slug==$t or .name==$t)' "/tmp/repo-${PUB}-teams.json" >/dev/null 2>&1; then
             log_info "GithubOrganization status is complete and repository permissions verified"
             return 0
           fi
        fi
        log_info "GithubOrganization status is complete but repository permissions not yet found (waiting)"
      fi
    fi

    local now=$(date +%s)
    if (( now - start >= timeout_sec )); then
      echo "[$(ts)] WARN: Timeout waiting for repository team operations or complete status (continuing)" >&2
      echo "$org_json" | jq '.status' || true
      return 0
    fi
    sleep "$interval"
  done
}

run_repository_scenarios() {
  ensure_jq
  local ORG=$(read_env_var ORGANIZATION)
  local TOKEN=$(read_env_var GITHUB_TOKEN)
  if [[ -z "$TOKEN" ]]; then
    log_info "Skipping repository scenarios: GITHUB_TOKEN not set"
    return 0
  fi

  local PUB=$(read_env_var E2E_REPO_PUBLIC)
  local PRIV=$(read_env_var E2E_REPO_PRIVATE)
  [[ -z "$PUB" ]] && PUB="e2e-pub"
  [[ -z "$PRIV" ]] && PRIV="e2e-priv"

  local DEF_PUB_PULL=$(read_env_var DEFAULT_PUBLIC_PULL_TEAM)
  local DEF_PUB_PUSH=$(read_env_var DEFAULT_PUBLIC_PUSH_TEAM)
  local DEF_PUB_ADMIN=$(read_env_var DEFAULT_PUBLIC_ADMIN_TEAM)
  local DEF_PRIV_PULL=$(read_env_var DEFAULT_PRIVATE_PULL_TEAM)
  local DEF_PRIV_PUSH=$(read_env_var DEFAULT_PRIVATE_PUSH_TEAM)
  local DEF_PRIV_ADMIN=$(read_env_var DEFAULT_PRIVATE_ADMIN_TEAM)
  local CUSTOM_PRIV_TEAM=$(read_env_var CUSTOM_PRIVATE_REPO_TEAM)
  local CUSTOM_PRIV_PERMISSION=$(read_env_var CUSTOM_PRIVATE_REPO_PERMISSION)
  [[ -z "$CUSTOM_PRIV_PERMISSION" ]] && CUSTOM_PRIV_PERMISSION="push"

  log_step "Repository scenario: creating public repo '${PUB}' and private repo '${PRIV}'"
  local code
  code=$(github_create_repo "$PUB" false)
  if [[ "$code" != "201" && "$code" != "422" ]]; then
    echo "[$(ts)] ERROR: Failed to create public repo ${PUB} (HTTP ${code})" >&2
    cat /tmp/repo-create.json >&2 || true
    exit 1
  fi
  code=$(github_create_repo "$PRIV" true)
  if [[ "$code" != "201" && "$code" != "422" ]]; then
    echo "[$(ts)] ERROR: Failed to create private repo ${PRIV} (HTTP ${code})" >&2
    cat /tmp/repo-create.json >&2 || true
    exit 1
  fi

  wait_for_controller_readiness
  wait_for_org_repo_ops

  log_step "Validating team permissions on public repo '${PUB}'"
  [[ -n "$DEF_PUB_PULL" ]] && assert_repo_has_team_permission "$PUB" "$DEF_PUB_PULL" pull
  [[ -n "$DEF_PUB_PUSH" ]] && assert_repo_has_team_permission "$PUB" "$DEF_PUB_PUSH" push
  [[ -n "$DEF_PUB_ADMIN" ]] && assert_repo_has_team_permission "$PUB" "$DEF_PUB_ADMIN" admin

  log_step "Validating team permissions on private repo '${PRIV}'"
  [[ -n "$DEF_PRIV_PULL" ]] && assert_repo_has_team_permission "$PRIV" "$DEF_PRIV_PULL" pull
  [[ -n "$DEF_PRIV_PUSH" ]] && assert_repo_has_team_permission "$PRIV" "$DEF_PRIV_PUSH" push
  [[ -n "$DEF_PRIV_ADMIN" ]] && assert_repo_has_team_permission "$PRIV" "$DEF_PRIV_ADMIN" admin
  if [[ -n "$CUSTOM_PRIV_TEAM" ]]; then
    assert_repo_has_team_permission "$PRIV" "$CUSTOM_PRIV_TEAM" "$CUSTOM_PRIV_PERMISSION"
  fi

  if [[ "${E2E_KEEP:-true}" != "true" ]]; then
    log_step "Cleaning up test repositories"
    local del
    del=$(github_delete_repo "$PUB")
    if [[ "$del" != "204" && "$del" != "200" && "$del" != "404" ]]; then
      echo "[$(ts)] WARN: Deleting repo ${ORG}/${PUB} returned HTTP ${del}" >&2
      cat /tmp/delrepo.json >&2 || true
    fi
    del=$(github_delete_repo "$PRIV")
    if [[ "$del" != "204" && "$del" != "200" && "$del" != "404" ]]; then
      echo "[$(ts)] WARN: Deleting repo ${ORG}/${PRIV} returned HTTP ${del}" >&2
      cat /tmp/delrepo.json >&2 || true
    fi
  else
    log_info "E2E_KEEP=true; repositories ${PUB}, ${PRIV} are kept"
  fi

  log_step "Repository scenarios completed"
}

usage() {
  cat <<EOF
Usage: $0 <command>
Commands:
  up        Create k3d cluster
  down      Delete k3d cluster
  install-crds  Install only repo-guard CRDs into the cluster
  install   Generate values and install Helm chart
  test      Run basic runtime checks
  teams     Run Greenhouse Team scenario (status-driven membership)
  image-build  Build the repo-guard image locally (no cluster ops)
  github-cleanup  Delete e2e-created GitHub teams and (optionally) repositories

Environment:
  K3D_CLUSTER   Cluster name (default: ${K3D_CLUSTER})
  NAMESPACE     Namespace (default: ${NAMESPACE})
  HELM_RELEASE  Helm release name (default: ${HELM_RELEASE})
  CHART_PATH    Path to chart (default: ${CHART_PATH})
  VALUES_OUT    Generated values path (default: ${VALUES_OUT})
  CONTAINER_TOOL Container CLI to build image (default: ${CONTAINER_TOOL})
  E2E_IMAGE_REPO Image repository for manager (default: ${E2E_IMAGE_REPO})
  E2E_IMAGE_TAG  Image tag for manager (default: ${E2E_IMAGE_TAG})
  E2E_IMAGE      Full image ref (overrides repo+tag) (default: ${E2E_IMAGE})
  E2E_SKIP_BUILD If true, skip image build on up (default: false)
  E2E_SKIP_IMPORT If true, skip k3d image import on up (default: false)
  # github-cleanup flags:
  E2E_DRY_RUN           Dry run; print planned deletions only (default: false)
  E2E_CLEANUP_REPOS     Also delete repositories (requires delete_repo scope) (default: true)
  E2E_REPO_PREFIX       Only delete repos whose name starts with this prefix (required when E2E_CLEANUP_REPOS=true) (default: e2e)
EOF
}

# ===== Pretty logging helpers (Bash 3 compatible) =====
# Toggle via env: E2E_NO_COLOR=1, E2E_NO_EMOJI=1, E2E_COMPACT=1

supports_color() {
  # Disable on non-tty or when NO_COLOR/E2E_NO_COLOR set (use ${VAR-} for set -u safety)
  if [ -n "${E2E_NO_COLOR-}" ] || [ -n "${NO_COLOR-}" ]; then return 1; fi
  [ -t 1 ] || return 1
  return 0
}

# Initialize colors (use variables, no arrays to keep Bash 3.2 compat)
init_colors() {
  if supports_color; then
    C_RESET='\033[0m'
    C_BOLD='\033[1m'
    C_DIM='\033[2m'
    C_CYAN='\033[36m'
    C_GREEN='\033[32m'
    C_YELLOW='\033[33m'
    C_RED='\033[31m'
    C_MAGENTA='\033[35m'
    C_BLUE='\033[34m'
  else
    C_RESET=''
    C_BOLD=''
    C_DIM=''
    C_CYAN=''
    C_GREEN=''
    C_YELLOW=''
    C_RED=''
    C_MAGENTA=''
    C_BLUE=''
  fi
}

init_emojis() {
  if [ -n "${E2E_NO_EMOJI-}" ]; then
    EMJ_OK='[OK]'
    EMJ_FAIL='[X]'
    EMJ_STEP='>'
    EMJ_SECTION='*'
    EMJ_INFO='i'
  else
    EMJ_OK='✅'
    EMJ_FAIL='❌'
    EMJ_STEP='👉'
    EMJ_SECTION='📦'
    EMJ_INFO='ℹ️'
  fi
}

# Horizontal rule and banners
hr() {
  # Usage: hr "Title" (optional)
  # use ${1-} to avoid "unbound variable" when called without args under set -u
  local title="${1-}"
  local line="────────────────────────────────────────────────────────"
  if [ -n "$title" ]; then
    printf "%b%s %s %s%b\n" "$C_DIM" "$line" "$title" "$line" "$C_RESET"
  else
    printf "%b%s%b\n" "$C_DIM" "$line$line" "$C_RESET"
  fi
}

banner() {
  # Usage: banner "Title"
  # use ${1-} for set -u safety
  local title="${1-}"
  printf "%b%s %s %s%b\n" "$C_BOLD$C_BLUE" "====" "$title" "====" "$C_RESET"
}

ts() {
  date +"%Y-%m-%d %H:%M:%S"
}

log_info() {
  echo "[$(ts)] INFO: $*"
}

log_step() {
  echo "[$(ts)] $EMJ_STEP $*"
}

log_expect() {
  echo "[$(ts)] EXPECT: $*"
}

log_observe() {
  echo "[$(ts)] OBSERVED: $*"
}

SECTION_TITLE=""
SECTION_START_TS=0

section_begin() {
  SECTION_TITLE="$1"
  SECTION_START_TS=$(date +%s)
  hr
  printf "%b%s %s%b\n" "$C_BOLD$C_CYAN" "$EMJ_SECTION" "$SECTION_TITLE" "$C_RESET"
  hr
}

section_end() {
  local end=$(date +%s)
  local dur=$(( end - SECTION_START_TS ))
  printf "%b%s Completed in %ss%b\n" "$C_DIM" "$EMJ_OK" "$dur" "$C_RESET"
  hr
}

# Initialize styles once
init_colors
init_emojis

cmd_install_crds() {
  section_begin "Install repo-guard CRDs"
  check_prereqs
  # Ensure cluster exists
  cmd_up
  log_step "Installing repo-guard CRDs from ${CHART_PATH}/crds"
  kubectl apply -f "${CHART_PATH}/crds"
  section_end
}

# ------------------------------
# GitHub cleanup (teams/repos)
# ------------------------------

github_delete_team() {
  local team_slug=$1
  local api org token
  api=$(read_env_var GITHUB_V3_API_URL)
  org=$(read_env_var ORGANIZATION)
  token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o /tmp/delteam.json -w "%{http_code}" -X DELETE \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/orgs/${org}/teams/${team_slug}"
}

github_list_repos_json() {
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o /tmp/repos.json -w "%{http_code}" \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/orgs/${org}/repos?per_page=100"
}

github_delete_repo() {
  local repo_name=$1
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o /tmp/delrepo.json -w "%{http_code}" -X DELETE \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/repos/${org}/${repo_name}"
}

cmd_github_cleanup() {
  ensure_jq
  local token org
  token=$(read_env_var GITHUB_TOKEN)
  org=$(read_env_var ORGANIZATION)
  if [[ -z "$token" || -z "$org" ]]; then
    echo "ERROR: GITHUB_TOKEN and ORGANIZATION must be set in ${ENV_FILE_DEFAULT} for cleanup." >&2
    exit 1
  fi

  local dry=${E2E_DRY_RUN:-false}
  local do_repos=${E2E_CLEANUP_REPOS:-true}
  local repo_prefix=${E2E_REPO_PREFIX:-e2e}
  section_begin "GitHub cleanup for org=${org}"
  log_step "dryRun=${dry}, repos=${do_repos}, repoPrefix=${repo_prefix}"

  # Resolve team names from env
  local TEAM_1 TEAM_2 OWNER_TEAM LDAP_TEAM HTTP_TEAM STATIC_TEAM EMAIL_TEAM
  TEAM_1=$(read_env_var TEAM_1)
  TEAM_2=$(read_env_var TEAM_2)
  OWNER_TEAM=$(read_env_var ORGANIZATION_OWNER_TEAM)
  LDAP_TEAM=$(read_env_var LDAP_GROUP_PROVIDER_TEAM_NAME)
  HTTP_TEAM=$(read_env_var EMP_HTTP_TEAM_NAME)
  STATIC_TEAM=$(read_env_var EMP_STATIC_TEAM_NAME)
  EMAIL_TEAM=$(read_env_var EMAIL_DOMAIN_TEST_TEAM_NAME)

  # Fetch teams once
  local code
  code=$(github_fetch_teams_json)
  if [[ "$code" != "200" ]]; then
    echo "ERROR: Failed to fetch teams for cleanup: HTTP ${code}" >&2
    cat /tmp/teams.json >&2 || true
    exit 1
  fi

  # Build candidate list and delete
  local -a candidates
  for t in "$TEAM_1" "$TEAM_2" "$OWNER_TEAM" "$LDAP_TEAM" "$HTTP_TEAM" "$STATIC_TEAM" "$EMAIL_TEAM"; do
    [[ -z "$t" ]] && continue
    candidates+=("$t")
  done

  if ((${#candidates[@]} == 0)); then
    echo "[$(ts)] INFO: No team names found in env; nothing to delete."
  else
    echo "[$(ts)] INFO: Candidate teams to delete: ${candidates[*]}"
  fi

  local deleted_any=false
  for name in "${candidates[@]}"; do
    local slug
    slug=$(github_find_team "$name")
    if [[ -z "$slug" || "$slug" == "null" ]]; then
      echo "[$(ts)] INFO: Team not found (skip): ${name}"
      continue
    fi
    if [[ "$dry" == "true" ]]; then
      echo "[$(ts)] INFO: DRY-RUN would delete team: ${slug}"
      continue
    fi
    local dcode
    dcode=$(github_delete_team "$slug")
    if [[ "$dcode" == "204" || "$dcode" == "200" ]]; then
      echo "[$(ts)] INFO: Deleted team: ${slug}"
      deleted_any=true
    else
      echo "[$(ts)] ERROR: Failed to delete team ${slug} (HTTP ${dcode})" >&2
      cat /tmp/delteam.json >&2 || true
      exit 1
    fi
  done

  # Optionally delete repositories by prefix
  if [[ "$do_repos" == "true" ]]; then
    if [[ -z "$repo_prefix" ]]; then
      echo "[$(ts)] WARN: E2E_CLEANUP_REPOS=true but E2E_REPO_PREFIX is empty; skipping repo deletion for safety." >&2
    else
      local rcode
      rcode=$(github_list_repos_json)
      if [[ "$rcode" != "200" ]]; then
        echo "[$(ts)] ERROR: Failed to list repos (HTTP ${rcode})" >&2
        cat /tmp/repos.json >&2 || true
        exit 1
      fi
      local repos
      repos=$(jq -r --arg p "$repo_prefix" '.[] | select(.name | startswith($p)) | .name' /tmp/repos.json)
      if [[ -z "$repos" ]]; then
        echo "[$(ts)] INFO: No repositories found with prefix '${repo_prefix}'."
      else
        echo "[$(ts)] INFO: Repositories matching prefix '${repo_prefix}':"
        echo "$repos" | sed 's/^/  - /'
        while IFS= read -r repo; do
          [[ -z "$repo" ]] && continue
          if [[ "$dry" == "true" ]]; then
            echo "[$(ts)] INFO: DRY-RUN would delete repo: ${org}/${repo}"
            continue
          fi
          local delrc
          delrc=$(github_delete_repo "$repo")
          if [[ "$delrc" == "204" || "$delrc" == "200" ]]; then
            echo "[$(ts)] INFO: Deleted repo: ${org}/${repo}"
          else
            echo "[$(ts)] ERROR: Failed to delete repo ${org}/${repo} (HTTP ${delrc})" >&2
            cat /tmp/delrepo.json >&2 || true
            exit 1
          fi
        done <<< "$repos"
      fi
    fi
  fi

    # Revert organization owners to only ORGANIZATION_OWNER_USER
    local OWNER_GH_USER
    OWNER_GH_USER=$(read_env_var ORGANIZATION_OWNER_USER)
    if [[ -n "$OWNER_GH_USER" ]]; then
      log_step "Reverting organization owners to only: ${OWNER_GH_USER}"
      local acode
      acode=$(github_list_org_admins)
      if [[ "$acode" != "200" ]]; then
        echo "[$(ts)] ERROR: Failed to list org admins (HTTP ${acode})" >&2
        cat /tmp/admins.json >&2 || true
        exit 1
      fi
      # Ensure desired owner is admin
      if jq -e --arg u "$OWNER_GH_USER" '.[] | select(.login==$u)' /tmp/admins.json >/dev/null 2>&1; then
        if e2e_verbose_true; then log_observe "Owner already admin: ${OWNER_GH_USER}"; fi
      else
        if [[ "$dry" == "true" ]]; then
          echo "[$(ts)] INFO: DRY-RUN would promote ${OWNER_GH_USER} to org admin"
        else
          local pcode
          pcode=$(github_set_org_membership_role "$OWNER_GH_USER" "admin")
          if [[ "$pcode" != "200" && "$pcode" != "201" ]]; then
            echo "[$(ts)] ERROR: Failed to set admin for ${OWNER_GH_USER} (HTTP ${pcode})" >&2
            cat "/tmp/membership_${OWNER_GH_USER}.json" >&2 || true
            exit 1
          else
            echo "[$(ts)] INFO: Ensured ${OWNER_GH_USER} is org admin (HTTP ${pcode})"
          fi
        fi
        # Refresh admins list after potential change
        acode=$(github_list_org_admins)
        [[ "$acode" == "200" ]] || true
      fi
      # Demote all other admins
      if [[ -s /tmp/admins.json ]]; then
        local others
        others=$(jq -r --arg u "$OWNER_GH_USER" '.[] | select(.login!=$u) | .login' /tmp/admins.json)
        if [[ -z "$others" ]]; then
          if e2e_verbose_true; then log_observe "No other admins to demote"; fi
        else
          while IFS= read -r adm; do
            [[ -z "$adm" ]] && continue
            if [[ "$dry" == "true" ]]; then
              echo "[$(ts)] INFO: DRY-RUN would demote admin to member: ${adm}"
              continue
            fi
            local dcode
            dcode=$(github_set_org_membership_role "$adm" "member")
            if [[ "$dcode" != "200" && "$dcode" != "201" ]]; then
              echo "[$(ts)] ERROR: Failed to demote ${adm} to member (HTTP ${dcode})" >&2
              cat "/tmp/membership_${adm}.json" >&2 || true
              exit 1
            else
              echo "[$(ts)] INFO: Demoted admin to member: ${adm}"
            fi
          done <<< "$others"
        fi
      fi
    else
      log_info "ORGANIZATION_OWNER_USER not set; skipping owners revert"
    fi

  echo "[$(ts)] STEP: GitHub cleanup finished (dryRun=${dry})"
}

# ------------------------------
# Teams scenario (Greenhouse-driven)
# ------------------------------

ensure_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required for teams scenario. Please install jq and retry." >&2
    exit 1
  fi
}

# ------------------------------
# Logging helpers
# ------------------------------

ts() {
  date '+%Y-%m-%d %H:%M:%S'
}

e2e_verbose_true() {
  local v=${E2E_VERBOSE:-true}
  [[ "$v" == "true" || "$v" == "1" || "$v" == "yes" ]]
}

log_step() {
  echo "[$(ts)] STEP: $*"
}

log_info() {
  echo "[$(ts)] INFO: $*"
}

log_expect() {
  echo "[$(ts)] EXPECT: $*"
}

log_observe() {
  echo "[$(ts)] OBSERVED: $*"
}

# Patch a transient trigger label on a GithubTeam to nudge a reconcile
# Usage: bump_trigger_label <githubteam-name>
bump_trigger_label() {
  local gt_name=$1
  local ts_val
  ts_val=$(date +%s)
  if command -v jq >/dev/null 2>&1; then
    local patch
    patch=$(jq -nc --arg k "${E2E_TRIGGER_LABEL_KEY}" --arg v "${ts_val}" '{metadata:{labels:{($k):$v}}}')
    echo "[$(ts)] TRIGGER: Patching label ${E2E_TRIGGER_LABEL_KEY}=${ts_val} on GithubTeam/${gt_name}"
    kubectl -n "${NAMESPACE}" patch githubteam "${gt_name}" --type=merge -p "${patch}" >/dev/null 2>&1 || true
  else
    # Fallback without jq; try simple JSON with the default key
    echo "[$(ts)] TRIGGER: Patching label ${E2E_TRIGGER_LABEL_KEY}=${ts_val} on GithubTeam/${gt_name} (no jq)"
    kubectl -n "${NAMESPACE}" patch githubteam "${gt_name}" --type=merge -p "{\"metadata\":{\"labels\":{\"${E2E_TRIGGER_LABEL_KEY}\":\"${ts_val}\"}}}" >/dev/null 2>&1 || true
  fi
}

get_github_name() {
  kubectl -n "${NAMESPACE}" get github -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true
}

apply_greenhouse_team() {
  local name=$1
  kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: greenhouse.sap/v1alpha1
kind: Team
metadata:
  name: ${name}
spec:
  description: E2E team ${name}
EOF
}

# Patch Greenhouse Team .status.members to the provided comma-separated list of IDs
patch_greenhouse_team_members() {
  local team_name=$1; shift
  local ids_csv=$1
  local json_members
  if [[ -z "$ids_csv" ]]; then
    json_members="[]"
  else
    # build JSON array of objects {"id":"<id>", "email":"<id>@example.com", "firstName":"<id>", "lastName":"<id>"}
    json_members=$(printf '%s' "$ids_csv" | awk -F, '{
      printf "[";
      for(i=1;i<=NF;i++){
        gsub(/^\"|\"$/,"",$i); gsub(/^\s+|\s+$/,"",$i);
        printf (i>1?",":"") "{\"id\":\"" $i "\",\"email\":\"" $i "@example.com\",\"firstName\":\"" $i "\",\"lastName\":\"" $i "\"}"
      }
      printf "]";
    }')
  fi
  log_step "Patching Greenhouse Team status: team=${team_name} members=${ids_csv}"
  kubectl -n "${NAMESPACE}" patch team "${team_name}" --type=merge --subresource=status -p "{\"status\":{\"members\":${json_members}, \"statusConditions\": {\"conditions\": []}}}"
}

mk_gt_name() {
  local github_name=$1 org=$2 team=$3
  # Match Helm template transformations:
  # name: {{ $githubKey }}--{{ $org.organization | replace "/" "-" | lower }}--{{ $team.name | replace "_" "-" | replace " " "-" | lower }}
  local org_sanitized=${org//\//-}
  local team_sanitized=${team//_/ -}
  team_sanitized=${team_sanitized// /-}
  # Lowercase
  org_sanitized=$(echo -n "$org_sanitized" | tr '[:upper:]' '[:lower:]')
  team_sanitized=$(echo -n "$team_sanitized" | tr '[:upper:]' '[:lower:]')
  echo -n "${github_name}--${org_sanitized}--${team_sanitized}"
}

wait_for_resource() {
  local kind=$1 name=$2
  local timeout_sec=${E2E_TIMEOUT:-180}
  local interval=${E2E_INTERVAL:-3}
  local start=$(date +%s)
  while true; do
    if kubectl -n "${NAMESPACE}" get "$kind" "$name" >/dev/null 2>&1; then
      return 0
    fi
    local now=$(date +%s)
    if (( now - start >= timeout_sec )); then
      echo "Timeout waiting for ${kind}/${name} to exist" >&2
      return 1
    fi
    sleep "$interval"
  done
}

# ------------------------------
# Readiness waits and GitHub API helpers
# ------------------------------

wait_for_controller_readiness() {
  local timeout_sec=${E2E_TIMEOUT:-180}
  local interval=${E2E_INTERVAL:-3}
  local start=$(date +%s)
  log_step "Waiting for Github.status.state == running"
  while true; do
    local state
    state=$(kubectl -n "${NAMESPACE}" get github -o json 2>/dev/null | jq -r '.items[0].status.state // ""') || true
    if [[ "$state" == "running" ]]; then break; fi
    local now=$(date +%s)
    if (( now - start >= timeout_sec )); then
      echo "[$(ts)] ERROR: Timeout waiting for Github to be running (last state=${state})" >&2
      kubectl -n "${NAMESPACE}" get github -o json | jq '.' >&2 || true
      exit 1
    fi
    if e2e_verbose_true; then log_observe "Github.status.state=${state:-<empty>} (waiting)"; fi
    sleep "$interval"
  done
  log_info "Github.status.state is running"

  log_step "Waiting for GithubOrganization to converge (status=Complete OR operations queue empty)"
  start=$(date +%s)
  while true; do
    local orgjson
    orgjson=$(kubectl -n "${NAMESPACE}" get githuborganization -o json 2>/dev/null) || true
    if [[ -n "$orgjson" ]] && echo "$orgjson" | jq -e '.items | length > 0' >/dev/null; then
      local status ops
      status=$(echo "$orgjson" | jq -r '.items[0].status.orgStatus // ""')
      ops=$(echo "$orgjson" | jq -r '.items[0].status.operations | length // 0')
      if [[ "$status" =~ ^[Cc]omplete$ || "$ops" == "0" ]]; then
        break
      fi
      if e2e_verbose_true; then log_observe "GithubOrganization.orgStatus=${status:-<empty>} operations=${ops} (waiting)"; fi
    fi
    local now=$(date +%s)
    if (( now - start >= timeout_sec )); then
      echo "[$(ts)] ERROR: Timeout waiting for GithubOrganization to converge" >&2
      echo "$orgjson" | jq '.' >&2 || true
      exit 1
    fi
    sleep "$interval"
  done
  log_info "GithubOrganization convergence reached"
}

github_fetch_teams_json() {
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o /tmp/teams.json -w "%{http_code}" \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/orgs/${org}/teams?per_page=100"
}

github_find_team() {
  local name=$1
  jq -r --arg n "$name" '[.[] | select((.slug==$n) or (.name==$n))][0].slug' /tmp/teams.json
}

github_check_team_member() {
  local team_slug=$1
  local username=$2
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o /tmp/tm.json -w "%{http_code}" \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/orgs/${org}/teams/${team_slug}/memberships/${username}"
}

github_list_org_admins() {
  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)
  curl -sS -o /tmp/admins.json -w "%{http_code}" \
    -H "Authorization: token ${token}" -H "Accept: application/vnd.github+json" \
    "${api}/orgs/${org}/members?role=admin&per_page=100"
}

# Set GitHub organization membership role for a user (admin|member)
# Writes response body to /tmp/membership_<username>.json and prints HTTP status code
github_set_org_membership_role() {
  local username=$1
  local role=$2

  if [[ -z "$username" || -z "$role" ]]; then
    echo "[$(ts)] ERROR: github_set_org_membership_role requires <username> and <role> (admin|member)" >&2
    echo "0"
    return 1
  fi

  if [[ "$role" != "admin" && "$role" != "member" ]]; then
    echo "[$(ts)] ERROR: Invalid role '$role'. Must be 'admin' or 'member'." >&2
    echo "0"
    return 1
  fi

  local api=$(read_env_var GITHUB_V3_API_URL)
  local org=$(read_env_var ORGANIZATION)
  local token=$(read_env_var GITHUB_TOKEN)

  curl -sS -o "/tmp/membership_${username}.json" -w "%{http_code}" \
    -X PUT \
    -H "Authorization: token ${token}" \
    -H "Accept: application/vnd.github+json" \
    -H "Content-Type: application/json" \
    -d "{\"role\":\"${role}\"}" \
    "${api}/orgs/${org}/memberships/${username}"
}

# Return the first GithubOrganization resource name in the namespace
get_org_resource_name() {
  kubectl -n "${NAMESPACE}" get githuborganization -o json 2>/dev/null | jq -r '.items[0].metadata.name // empty' || true
}

# Ensure the GithubOrganization.spec.organizationOwnerTeams includes provided team
ensure_org_has_owner_team() {
  local team=$1
  local org_name
  org_name=$(get_org_resource_name)
  [[ -z "$org_name" ]] && { echo "[$(ts)] WARN: No GithubOrganization resource found to patch owners" >&2; return 0; }
  # If jq says the team exists already, do nothing
  local current
  current=$(kubectl -n "${NAMESPACE}" get githuborganization "$org_name" -o json)
  if printf '%s' "$current" | jq -e --arg t "$team" '.spec.organizationOwnerTeams // [] | index($t) != null' >/dev/null 2>&1; then
    return 0
  fi
  # Compute new array and patch
  local new_array
  new_array=$(printf '%s' "$current" | jq -c --arg t "$team" '(.spec.organizationOwnerTeams // []) + [$t]')
  log_step "Patching GithubOrganization to include owner team '${team}'"
  kubectl -n "${NAMESPACE}" patch githuborganization "$org_name" --type=merge -p "{\"spec\":{\"organizationOwnerTeams\":${new_array}}}"
}

# Wait until GitHub org admins include the given username
wait_for_org_admin() {
  local username=$1
  local timeout_sec=${E2E_TIMEOUT:-180}
  local interval=${E2E_INTERVAL:-3}
  local start=$(date +%s)
  log_step "Waiting for user to be an org admin on GitHub: ${username}"
  while true; do
    local rc
    rc=$(github_list_org_admins)
    if [[ "$rc" == "200" ]]; then
      if jq -e --arg u "$username" '.[] | select(.login==$u)' /tmp/admins.json >/dev/null 2>&1; then
        log_info "Confirmed org admin on GitHub: ${username}"
        return 0
      fi
      if e2e_verbose_true; then log_observe "${username} not in admins yet (retrying)"; fi
    else
      echo "[$(ts)] WARN: Failed to list org admins (HTTP ${rc}); will retry" >&2
    fi
    local now=$(date +%s)
    if (( now - start >= timeout_sec )); then
      echo "[$(ts)] ERROR: Timeout waiting for ${username} to be org admin" >&2
      cat /tmp/admins.json >&2 || true
      return 1
    fi
    sleep "$interval"
  done
}

# Drive owner flow: ensure owner greenhouse team has member, then verify admin on GitHub
run_owner_scenario() {
  ensure_jq
  local OWNER_TEAM OWNER_GH_ID OWNER_GH_USER ORG GITHUB_NAME
  OWNER_TEAM=$(read_env_var ORGANIZATION_OWNER_TEAM)
  OWNER_GH_ID=$(read_env_var ORGANIZATION_OWNER_GREENHOUSE_ID)
  OWNER_GH_USER=$(read_env_var ORGANIZATION_OWNER_USER)
  ORG=$(read_env_var ORGANIZATION)
  GITHUB_NAME=$(get_github_name)

  if [[ -z "$OWNER_TEAM" || -z "$OWNER_GH_USER" ]]; then
    log_info "Owner scenario skipped: ORGANIZATION_OWNER_TEAM or ORGANIZATION_OWNER_USER not set"
    return 0
  fi

  # Fallback for greenhouse id if not given: reuse LDAP internal username
  if [[ -z "$OWNER_GH_ID" ]]; then
    OWNER_GH_ID=$(read_env_var LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME)
  fi

  log_step "Owner scenario: team=${OWNER_TEAM}, greenhouseID=${OWNER_GH_ID}, githubUser=${OWNER_GH_USER}"
  # Ensure Greenhouse Team CR exists (idempotent) and org is configured to use it for owners
  apply_greenhouse_team "$OWNER_TEAM" || true
  ensure_org_has_owner_team "$OWNER_TEAM"
  # Patch status members for owner team
  patch_greenhouse_team_members "$OWNER_TEAM" "$OWNER_GH_ID"

  # Wait for GithubTeam CR to exist (rendered by Helm) and for 1 member to appear in status
  local GT_OWNER
  if [[ -n "$GITHUB_NAME" ]]; then
    GT_OWNER=$(mk_gt_name "$GITHUB_NAME" "$ORG" "$OWNER_TEAM")
    log_info "Waiting for GithubTeam resource: ${GT_OWNER}"
    wait_for_resource githubteam "$GT_OWNER" || true
    wait_for_members_count "$GT_OWNER" 1 || true
  fi

  # Optionally observe Kubernetes-side owner state
  if kubectl -n "${NAMESPACE}" get githuborganization -o json 2>/dev/null | jq -e '.items | length > 0' >/dev/null 2>&1; then
    log_step "Observing GithubOrganization owners state (best-effort)"
    kubectl -n "${NAMESPACE}" get githuborganization -o json | jq -r '.items[0].status.organizationOwners // []' || true
  fi

  # Verify on GitHub that owner user is an org admin (authoritative)
  wait_for_org_admin "$OWNER_GH_USER"

  # --- Add a new member to the same owner team and verify propagation ---
  local NEW_OWNER_GH_ID NEW_OWNER_GH_USER
  NEW_OWNER_GH_ID=$(read_env_var USER_1_GREENHOUSE_ID)
  NEW_OWNER_GH_USER=$(read_env_var USER_1_GITHUB_USERNAME)
  if [[ -n "$NEW_OWNER_GH_ID" && -n "$NEW_OWNER_GH_USER" ]]; then
    log_step "Owner scenario (extend): add second member to owner team -> ${NEW_OWNER_GH_ID}/${NEW_OWNER_GH_USER}"
    # Patch owner team to contain two members now
    patch_greenhouse_team_members "$OWNER_TEAM" "$OWNER_GH_ID,$NEW_OWNER_GH_ID"
    # Wait for Kubernetes GithubTeam status to reflect 2 members
    if [[ -n "$GT_OWNER" ]]; then
      wait_for_members_count "$GT_OWNER" 2 || true
    fi
    # Verify on GitHub the new user also becomes an org admin
    wait_for_org_admin "$NEW_OWNER_GH_USER"
  else
    log_info "Owner scenario (extend): USER_1 mapping not provided; skipping add-second-member step"
  fi
}

wait_for_members_count() {
  local gt_name=$1
  local expected=$2
  local timeout_sec=${E2E_TIMEOUT:-180}
  local interval=${E2E_INTERVAL:-3}
  local start=$(date +%s)
  # Apply a trigger label before starting the expectation wait to provoke a reconcile
  bump_trigger_label "${gt_name}"
  log_expect "GithubTeam/${gt_name} members count == ${expected}"
  local observed=0
  while true; do
    local count
    count=$(kubectl -n "${NAMESPACE}" get githubteam "${gt_name}" -o json 2>/dev/null | jq -r '.status.members | length // 0')
    if [[ "$count" == "$expected" ]]; then
      if e2e_verbose_true; then
        local members
        members=$(kubectl -n "${NAMESPACE}" get githubteam "${gt_name}" -o json 2>/dev/null | jq -c '.status.members // []')
        log_observe "GithubTeam/${gt_name} count=${count} members=${members}"
      fi
      return 0
    fi
    observed=$((observed+1))
    # If observed 10 times without convergence, bump trigger again to nudge reconcile
    if (( observed % 10 == 0 )); then
      log_info "No convergence after ${observed} observations; re-triggering reconcile"
      bump_trigger_label "${gt_name}"
    fi
    local now=$(date +%s)
    if (( now - start >= timeout_sec ));
    then
      echo "[$(ts)] ERROR: Timeout waiting for GithubTeam/${gt_name} members count ${expected}. Last count=${count}" >&2
      kubectl -n "${NAMESPACE}" get githubteam "${gt_name}" -o json | jq '.status' >&2 || true
      # Show last controller logs to help debugging
      if kubectl -n "${NAMESPACE}" get deploy repo-guard-controller-manager >/dev/null 2>&1; then
        echo "[$(ts)] INFO: Last controller logs:" >&2
        kubectl -n "${NAMESPACE}" logs deploy/repo-guard-controller-manager --tail=120 >&2 || true
      fi
      return 1
    fi
    if e2e_verbose_true; then log_observe "GithubTeam/${gt_name} current count=${count} (waiting for ${expected})"; fi
    sleep "$interval"
  done
}

# Extracted helper to run the teams scenario so it can be reused by cmd_test and cmd_teams
run_teams_scenario() {
  # Read inputs
  local ORG TEAM1 TEAM2 USER1 USER2
  ORG=$(read_env_var ORGANIZATION)
  TEAM1=$(read_env_var TEAM_1)
  TEAM2=$(read_env_var TEAM_2)
  USER1=$(read_env_var USER_1)
  USER2=$(read_env_var USER_2)
  local GITHUB_NAME
  GITHUB_NAME=$(get_github_name)
  if [[ -z "$GITHUB_NAME" ]]; then
    echo "ERROR: No Github resource found in namespace ${NAMESPACE}" >&2
    exit 1
  fi

  echo "Using Github=${GITHUB_NAME}, Org=${ORG}, Teams=${TEAM1},${TEAM2}"

  # Ensure Greenhouse Team CRs
  log_step "Ensuring Greenhouse Team CRs exist: ${TEAM1}, ${TEAM2}"
  apply_greenhouse_team "$TEAM1" && log_info "Applied/Ensured Greenhouse Team/${TEAM1}"
  apply_greenhouse_team "$TEAM2" && log_info "Applied/Ensured Greenhouse Team/${TEAM2}"

  # GithubTeam CRs are created by Helm from values (gen-values.sh). Compute names and wait for them.
  local GT1_NAME GT2_NAME
  GT1_NAME=$(mk_gt_name "$GITHUB_NAME" "$ORG" "$TEAM1")
  GT2_NAME=$(mk_gt_name "$GITHUB_NAME" "$ORG" "$TEAM2")
  log_step "Waiting for GithubTeam resources to exist: ${GT1_NAME}, ${GT2_NAME}"
  wait_for_resource githubteam "$GT1_NAME"
  wait_for_resource githubteam "$GT2_NAME"

  log_step "Scenario: drive membership via Greenhouse status for ${GT1_NAME} (TEAM_1=${TEAM1})"

  # 1) Start with 1 member (USER1)
  kubectl -n "${NAMESPACE}" get team "$TEAM1" >/dev/null
  log_step "Patching Greenhouse Team/${TEAM1} status to members=[${USER1}]"
  kubectl -n "${NAMESPACE}" patch team "$TEAM1" --type=merge --subresource=status -p "$(jq -nc --arg u "$USER1" '{status:{members:[{id:$u}]}}')"
  wait_for_members_count "$GT1_NAME" 1

  # 2) Add USER2 -> expect 2 members
  log_step "Patching Greenhouse Team/${TEAM1} status to members=[${USER1}, ${USER2}]"
  kubectl -n "${NAMESPACE}" patch team "$TEAM1" --type=merge --subresource=status -p "$(jq -nc --arg u1 "$USER1" --arg u2 "$USER2" '{status:{members:[{id:$u1},{id:$u2}]}}')"
  wait_for_members_count "$GT1_NAME" 2

  # 3) Remove first (keep USER2) -> expect 1
  log_step "Patching Greenhouse Team/${TEAM1} status to members=[${USER2}] (removing ${USER1})"
  kubectl -n "${NAMESPACE}" patch team "$TEAM1" --type=merge --subresource=status -p "$(jq -nc --arg u2 "$USER2" '{status:{members:[{id:$u2}]}}')"
  wait_for_members_count "$GT1_NAME" 1

  log_step "Scenario: label toggles on ${GT1_NAME}"
  # Disable removal; try to remove member in Greenhouse -> should remain 1
  log_step "Setting label githubguard.sap/removeUser=false on GithubTeam/${GT1_NAME}"
  kubectl -n "${NAMESPACE}" patch githubteam "$GT1_NAME" --type merge -p '{"metadata":{"labels":{"githubguard.sap/removeUser":"false"}}}'
  kubectl -n "${NAMESPACE}" patch team "$TEAM1" --type=merge --subresource=status -p '{"status":{"members":[]}}'
  # Wait briefly and assert still 1
  if e2e_verbose_true; then log_expect "GithubTeam/${GT1_NAME} to remain at count=1 despite empty Greenhouse members (remove disabled)"; fi
  sleep 5
  wait_for_members_count "$GT1_NAME" 1

  # Disable addition; add USER1 back in Greenhouse -> count should stay 1
  log_step "Setting label githubguard.sap/addUser=false on GithubTeam/${GT1_NAME}"
  kubectl -n "${NAMESPACE}" patch githubteam "$GT1_NAME" --type merge -p '{"metadata":{"labels":{"githubguard.sap/addUser":"false"}}}'
  kubectl -n "${NAMESPACE}" patch team "$TEAM1" --type=merge --subresource=status -p "$(jq -nc --arg u1 "$USER1" '{status:{members:[{id:$u1}]}}')"
  if e2e_verbose_true; then log_expect "GithubTeam/${GT1_NAME} to remain at count=1 despite Greenhouse adding ${USER1} (add disabled)"; fi
  wait_for_members_count "$GT1_NAME" 1

  # Re-enable both and converge to two members
  log_step "Re-enabling addUser/removeUser on GithubTeam/${GT1_NAME}"
  kubectl -n "${NAMESPACE}" patch githubteam "$GT1_NAME" --type merge -p '{"metadata":{"labels":{"githubguard.sap/addUser":"true","githubguard.sap/removeUser":"true"}}}'
  kubectl -n "${NAMESPACE}" patch team "$TEAM1" --type=merge --subresource=status -p "$(jq -nc --arg u1 "$USER1" --arg u2 "$USER2" '{status:{members:[{id:$u1},{id:$u2}]}}')"
  wait_for_members_count "$GT1_NAME" 2

  log_step "Quick sanity for ${GT2_NAME}: patch Greenhouse Team/${TEAM2} to one member [${USER1}] and verify"
  kubectl -n "${NAMESPACE}" patch team "$TEAM2" --type=merge --subresource=status -p "$(jq -nc --arg u "$USER1" '{status:{members:[{id:$u}]}}')"
  wait_for_members_count "$GT2_NAME" 1

  if [[ "${E2E_KEEP:-true}" != "true" ]]; then
    log_info "Cleanup note: resources retained for inspection by default (set E2E_KEEP=false to enable auto-cleanup)"
  else
    log_info "E2E_KEEP=true set; leaving all resources"
  fi
  log_step "Teams scenario completed"
}

# ------------------------------
# Provider-backed scenarios (LDAP / HTTP / Static)
# ------------------------------
run_provider_scenarios() {
  local GITHUB_API=$(read_env_var GITHUB_V3_API_URL)
  local ORG=$(read_env_var ORGANIZATION)
  local LDAP_TEAM_NAME=$(read_env_var LDAP_GROUP_PROVIDER_TEAM_NAME)
  local LDAP_USER_GH=$(read_env_var LDAP_GROUP_PROVIDER_USER_GITHUB_USERNAME)
  local HTTP_TEAM_NAME=$(read_env_var EMP_HTTP_TEAM_NAME)
  local HTTP_USER_GH=$(read_env_var EMP_HTTP_USER_GITHUB_USERNAME)
  local STATIC_TEAM_NAME=$(read_env_var EMP_STATIC_TEAM_NAME)
  local STATIC_USER_GH=$(read_env_var EMP_STATIC_USER_GITHUB_USERNAME)

  # Refresh team list and locate slugs
  local code
  code=$(github_fetch_teams_json)
  if [[ "$code" != "200" ]]; then
    echo "Failed to fetch teams for provider scenarios: HTTP ${code}" >&2
    cat /tmp/teams.json >&2 || true
    exit 1
  fi

  local ldap_slug http_slug static_slug
  ldap_slug=$(github_find_team "$LDAP_TEAM_NAME")
  http_slug=$(github_find_team "$HTTP_TEAM_NAME")
  static_slug=$(github_find_team "$STATIC_TEAM_NAME")

  # LDAP team membership check
  if [[ -n "$LDAP_USER_GH" && -n "$ldap_slug" && "$ldap_slug" != "null" ]]; then
    echo "Verifying LDAP team membership on GitHub: ${LDAP_TEAM_NAME} -> ${LDAP_USER_GH}"
    code=$(github_check_team_member "$ldap_slug" "$LDAP_USER_GH")
    if [[ "$code" != "200" && "$code" != "204" ]]; then
      echo "LDAP membership check failed (HTTP ${code}). Response:" >&2
      cat /tmp/tm.json >&2 || true
      exit 1
    fi
  fi

  # Generic HTTP provider team membership check
  if [[ -n "$HTTP_USER_GH" && -n "$http_slug" && "$http_slug" != "null" ]]; then
    echo "Verifying HTTP provider team membership on GitHub: ${HTTP_TEAM_NAME} -> ${HTTP_USER_GH}"
    code=$(github_check_team_member "$http_slug" "$HTTP_USER_GH")
    if [[ "$code" != "200" && "$code" != "204" ]]; then
      echo "HTTP provider membership check failed (HTTP ${code}). Response:" >&2
      cat /tmp/tm.json >&2 || true
      exit 1
    fi
  fi

  # Static provider team membership check
  if [[ -n "$STATIC_USER_GH" && -n "$static_slug" && "$static_slug" != "null" ]]; then
    echo "Verifying Static provider team membership on GitHub: ${STATIC_TEAM_NAME} -> ${STATIC_USER_GH}"
    code=$(github_check_team_member "$static_slug" "$STATIC_USER_GH")
    if [[ "$code" != "200" && "$code" != "204" ]]; then
      echo "Static provider membership check failed (HTTP ${code}). Response:" >&2
      cat /tmp/tm.json >&2 || true
      exit 1
    fi
  fi

  log_info "Provider-backed scenarios passed"
  section_end
}

cmd_teams() {
  section_begin "Teams scenario"
  check_prereqs
  ensure_jq
  # Ensure cluster and namespace
  cmd_up
  log_step "Ensuring namespace exists: ${NAMESPACE}"
  kubectl get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl create ns "${NAMESPACE}"

  # Ensure Helm release installed (basic check: deployment exists)
  if ! kubectl -n "${NAMESPACE}" get deploy -l app.kubernetes.io/name=repo-guard >/dev/null 2>&1; then
    log_info "Helm release not detected; running install first"
    cmd_install
  fi
  run_teams_scenario
  section_end
}

main() {
  cmd=${1:-}
  case "$cmd" in
    up) shift; cmd_up "$@" ;;
    down) shift; cmd_down "$@" ;;
    install-crds) shift; cmd_install_crds "$@" ;;
    install) shift; cmd_install "$@" ;;
    install-dry-run) shift; cmd_install_dry_run "$@" ;;
    teams|teams-test) shift; cmd_teams "$@" ;;
    test) shift; cmd_test "$@" ;;
    image-build) shift; build_image "$@" ;;
    github-cleanup) shift; cmd_github_cleanup "$@" ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
