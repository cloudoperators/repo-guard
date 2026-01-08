#!/usr/bin/env bash
set -euo pipefail

# Generate Helm values.yaml for charts/repo-guard from a test.env file
# Usage: gen-values.sh path/to/test.env > values.generated.yaml

ENV_FILE=${1:-}
if [[ -z "${ENV_FILE}" || ! -f "${ENV_FILE}" ]]; then
  echo "Usage: $0 path/to/test.env" >&2
  exit 1
fi

# Helper to read VAR=value lines, strip surrounding quotes
# Helper to read single-line values, preferring TEST_ prefix if present
read_var() {
  local name=$1
  local val=""
  # Try TEST_ prefix first
  val=$(grep -E "^TEST_${name}[[:space:]]*=" "${ENV_FILE}" | tail -n1 | sed -E 's/^[^=]+=//') || true
  if [[ -z "${val}" ]]; then
    val=$(grep -E "^${name}[[:space:]]*=" "${ENV_FILE}" | tail -n1 | sed -E 's/^[^=]+=//') || true
  fi
  # remove surrounding quotes if present and trim spaces
  val=$(echo -n "${val}" | sed -E "s/^[[:space:]]*[\"']?//; s/[\"']?[[:space:]]*$//")
  echo -n "${val}"
}

# Helper to read possibly multi-line values (e.g., PEM in single or double quotes, or heredoc)
read_var_multiline() {
  local name=$1
  # Try TEST_ prefix first
  local val
  val=$(read_var_multiline_raw "TEST_${name}")
  [[ -n "$val" ]] && { printf "%s" "$val"; return; }
  read_var_multiline_raw "${name}"
}

read_var_multiline_raw() {
  local name=$1
  local started=0 q="" val="" is_heredoc=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    if (( started == 0 )); then
      # Handle various assignments: VAR=VAL, VAR = VAL, VAR="VAL", VAR='VAL', VAR<<EOF
      if [[ "$line" =~ ^[[:space:]]*${name}([[:space:]]*=[[:space:]]*|<<)(.*) ]]; then
        local op="${BASH_REMATCH[1]}"
        local s="${BASH_REMATCH[2]}"
        
        # Check for heredoc
        if [[ "$op" == "<<" ]]; then
          is_heredoc=1
          started=1
          q=$(printf "%s" "$s" | sed -E 's/[[:space:]]+$//')
          continue
        fi

        # Check for quoted
        if [[ "$s" == \"* ]]; then
          q='"'
          s="${s:1}"
          started=1
          if [[ "$s" == *\" ]]; then
            printf "%s" "${s%\"}"
            return
          fi
          val="$s"
        elif [[ "$s" == \'* ]]; then
          q="'"
          s="${s:1}"
          started=1
          if [[ "$s" == *\' ]]; then
            printf "%s" "${s%\'}"
            return
          fi
          val="$s"
        else
          # Single line unquoted
          printf "%s" "$s"
          return
        fi
      fi
    else
      if (( is_heredoc == 1 )); then
         if [[ "$line" == "$q" ]]; then
           printf "%s" "$val"
           return
         fi
         if [[ -z "$val" ]]; then val="$line"
         else val+=$'\n'"$line"
         fi
      else
        # Multi-line quoted
        if [[ "$line" == *"$q" ]]; then
          val+=$'\n'"${line%$q}"
          printf "%s" "$val"
          return
        else
          val+=$'\n'"$line"
        fi
      fi
    fi
  done < "$ENV_FILE"
}

# GitHub App conf
GITHUB_WEB_URL=$(read_var GITHUB_WEB_URL)
GITHUB_V3_API_URL=$(read_var GITHUB_V3_API_URL)
GITHUB_INTEGRATION_ID=$(read_var GITHUB_INTEGRATION_ID)
GITHUB_TOKEN=$(read_var GITHUB_TOKEN)
GITHUB_CLIENT_ID=$(read_var GITHUB_CLIENT_ID)
GITHUB_CLIENT_SECRET=$(read_var GITHUB_CLIENT_SECRET)
GITHUB_PRIVATE_KEY=$(read_var_multiline GITHUB_PRIVATE_KEY)
GITHUB_INSTALLATION_ID=$(read_var GITHUB_INSTALLATION_ID)

# Org and teams
ORGANIZATION=$(read_var ORGANIZATION)
TEAM_1=$(read_var TEAM_1)
TEAM_2=$(read_var TEAM_2)
OWNER_TEAM=$(read_var ORGANIZATION_OWNER_TEAM)
OWNER_GH_ID=$(read_var ORGANIZATION_OWNER_GITHUB_USERID)
OWNER_GH_GREENHOUSE_ID=$(read_var ORGANIZATION_OWNER_GREENHOUSE_ID)

# LDAP provider
LDAP_NAME=$(read_var LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME)
LDAP_HOST=$(read_var LDAP_GROUP_PROVIDER_HOST)
LDAP_BASE_DN=$(read_var LDAP_GROUP_PROVIDER_BASE_DN)
LDAP_BIND_DN_RAW=$(read_var LDAP_GROUP_PROVIDER_BIND_DN)
LDAP_BIND_PW_RAW=$(read_var LDAP_GROUP_PROVIDER_BIND_PW)
# Allow override from environment (used by local dummy LDAP server)
if [[ -n "${LDAP_HOST_OVERRIDE:-}" ]]; then
  LDAP_HOST="${LDAP_HOST_OVERRIDE}"
fi
# Optionally strip backslashes from LDAP DN/PW (macOS shell escaping etc.)
# Set LDAP_REMOVE_BACKSLASHES=false to disable.
if [[ "${LDAP_REMOVE_BACKSLASHES:-true}" == "true" ]]; then
  LDAP_BIND_DN=${LDAP_BIND_DN_RAW//\\/}
  LDAP_BIND_PW=${LDAP_BIND_PW_RAW//\\/}
else
  LDAP_BIND_DN=${LDAP_BIND_DN_RAW}
  LDAP_BIND_PW=${LDAP_BIND_PW_RAW}
fi
LDAP_TEAM_NAME=$(read_var LDAP_GROUP_PROVIDER_TEAM_NAME)
LDAP_GROUP_NAME=$(read_var LDAP_GROUP_PROVIDER_GROUP_NAME)

# Generic External Member Provider (HTTP)
EMP_HTTP_NAME=$(read_var EMP_HTTP_KUBERNETES_RESOURCE_NAME)
EMP_HTTP_TEAM_NAME=$(read_var EMP_HTTP_TEAM_NAME)

# Simplified: prefer values defined directly in test.env under EMP_HTTP_*.
# If endpoints are not defined but a dummy base is provided by e2e.sh, derive them.
EMP_HTTP_ENDPOINT=$(read_var EMP_HTTP_ENDPOINT)
EMP_HTTP_TEST_CONNECTION_URL=$(read_var EMP_HTTP_TEST_CONNECTION_URL)
EMP_HTTP_USERNAME=$(read_var EMP_HTTP_USERNAME)
EMP_HTTP_PASSWORD_RAW=$(read_var EMP_HTTP_PASSWORD)
EMP_HTTP_GROUP_ID=$(read_var EMP_HTTP_GROUP_ID)

# Derive endpoints from dummy base if not explicitly set
if [[ -z "${EMP_HTTP_ENDPOINT}" || -z "${EMP_HTTP_TEST_CONNECTION_URL}" ]]; then
  if [[ -n "${EMP_HTTP_DUMMY_BASE:-}" ]]; then
    base=${EMP_HTTP_DUMMY_BASE%/}
    if [[ -z "${EMP_HTTP_ENDPOINT}" ]]; then
      EMP_HTTP_ENDPOINT="${base}/api/sp/groups/{group}/users.json"
    fi
    if [[ -z "${EMP_HTTP_TEST_CONNECTION_URL}" ]]; then
      EMP_HTTP_TEST_CONNECTION_URL="${base}/api/sp/search.json"
    fi
  fi
fi

# Optionally strip backslashes from EMP_HTTP_PASSWORD (macOS shell escaping etc.)
# Set EMP_HTTP_REMOVE_BACKSLASHES=false to disable.
if [[ "${EMP_HTTP_REMOVE_BACKSLASHES:-true}" == "true" ]]; then
  EMP_HTTP_PASSWORD=${EMP_HTTP_PASSWORD_RAW//\\/}
else
  EMP_HTTP_PASSWORD=${EMP_HTTP_PASSWORD_RAW}
fi

# Static provider
STATIC_NAME=$(read_var EMP_STATIC_KUBERNETES_RESOURCE_NAME)
STATIC_TEAM_NAME=$(read_var EMP_STATIC_TEAM_NAME)

# Repository defaults and assignments
E2E_REPO_PREFIX=$(read_var E2E_REPO_PREFIX)
if [[ -z "$E2E_REPO_PREFIX" ]]; then E2E_REPO_PREFIX="e2e-"; fi
E2E_REPO_PUBLIC=$(read_var E2E_REPO_PUBLIC)
E2E_REPO_PRIVATE=$(read_var E2E_REPO_PRIVATE)
if [[ -z "$E2E_REPO_PUBLIC" ]]; then E2E_REPO_PUBLIC="${E2E_REPO_PREFIX}pub"; fi
if [[ -z "$E2E_REPO_PRIVATE" ]]; then E2E_REPO_PRIVATE="${E2E_REPO_PREFIX}priv"; fi

# Default team names for public repos (fallback to TEAM_1/TEAM_2/OWNER)
DEF_PUB_PULL=$(read_var DEFAULT_PUBLIC_PULL_TEAM)
DEF_PUB_PUSH=$(read_var DEFAULT_PUBLIC_PUSH_TEAM)
DEF_PUB_ADMIN=$(read_var DEFAULT_PUBLIC_ADMIN_TEAM)
if [[ -z "$DEF_PUB_PULL" ]]; then DEF_PUB_PULL="$TEAM_1"; fi
if [[ -z "$DEF_PUB_PUSH" ]]; then DEF_PUB_PUSH="$TEAM_2"; fi
if [[ -z "$DEF_PUB_ADMIN" ]]; then DEF_PUB_ADMIN="$OWNER_TEAM"; fi

# Default team names for private repos (fallback to TEAM_1/TEAM_2/OWNER)
DEF_PRIV_PULL=$(read_var DEFAULT_PRIVATE_PULL_TEAM)
DEF_PRIV_PUSH=$(read_var DEFAULT_PRIVATE_PUSH_TEAM)
DEF_PRIV_ADMIN=$(read_var DEFAULT_PRIVATE_ADMIN_TEAM)
if [[ -z "$DEF_PRIV_PULL" ]]; then DEF_PRIV_PULL="$TEAM_1"; fi
if [[ -z "$DEF_PRIV_PUSH" ]]; then DEF_PRIV_PUSH="$TEAM_2"; fi
if [[ -z "$DEF_PRIV_ADMIN" ]]; then DEF_PRIV_ADMIN="$OWNER_TEAM"; fi

# Optional custom assignment for private repo
CUSTOM_PRIV_TEAM=$(read_var CUSTOM_PRIVATE_REPO_TEAM)
CUSTOM_PRIV_PERMISSION=$(read_var CUSTOM_PRIVATE_REPO_PERMISSION)
if [[ -z "$CUSTOM_PRIV_PERMISSION" ]]; then CUSTOM_PRIV_PERMISSION="push"; fi

cat <<EOF
manager:
  enabled: true

monitoring:
  enabled: true
  podMonitor:
    enabled: true

ldap:
  name: ${LDAP_NAME}
  host: ${LDAP_HOST}
  baseDN: ${LDAP_BASE_DN}
  bindDN: ${LDAP_BIND_DN}
  bindPW: ${LDAP_BIND_PW}

staticMemberProviders:
  - name: ${STATIC_NAME}
    groups:
      - group: ${STATIC_TEAM_NAME}
        members:
          - $(read_var EMP_STATIC_USER_INTERNAL_USERNAME)

EOF

# Enable Generic External Member Provider (HTTP) based on test.env
if [[ -n "${EMP_HTTP_ENDPOINT}" ]]; then
  cat <<EOF
genericExternalMemberProviders:
  - name: ${EMP_HTTP_NAME}
    endpoint: ${EMP_HTTP_ENDPOINT}
    username: ${EMP_HTTP_USERNAME}
    password: ${EMP_HTTP_PASSWORD}
    idField: id
    resultsField: results
    paginated: true
    pageParam: page
    totalPagesField: total_pages
    testConnectionURL: ${EMP_HTTP_TEST_CONNECTION_URL}
EOF
else
  cat <<EOF
genericExternalMemberProviders: []
EOF
fi

cat <<EOF

githubs:
  com:
    webURL: ${GITHUB_WEB_URL}
    v3APIURL: ${GITHUB_V3_API_URL}
    integrationID: ${GITHUB_INTEGRATION_ID}
    clientID: ${GITHUB_CLIENT_ID}
    clientSecret: ${GITHUB_CLIENT_SECRET}
    privateKey: |-
$(printf "%s\n" "${GITHUB_PRIVATE_KEY}" | sed 's/^/      /')

    organizations:
      - organization: ${ORGANIZATION}
        installationID: ${GITHUB_INSTALLATION_ID}
        requireVerifiedDomainEmailForMembers: false
        ttl:
          completed: 5s
          skipped: 5s
        disableInternalUsernames: false
        organizationOwnerTeams:
          - ${OWNER_TEAM}

        teams:
          - name: ${TEAM_1}
            greenhouseTeam: ${TEAM_1}
          - name: ${TEAM_2}
            greenhouseTeam: ${TEAM_2}
          - name: ${OWNER_TEAM}
            greenhouseTeam: ${OWNER_TEAM}
          - name: ${LDAP_TEAM_NAME}
            disableInternalUsernames: true
            ldap:
              provider: ${LDAP_NAME}
              group: ${LDAP_GROUP_NAME}
          - name: ${EMP_HTTP_TEAM_NAME}
            genericHTTP:
              provider: ${EMP_HTTP_NAME}
              group: ${EMP_HTTP_GROUP_ID}
          - name: ${STATIC_TEAM_NAME}
            static:
              provider: ${STATIC_NAME}
              group: ${STATIC_TEAM_NAME}

        defaultPublicRepositoryTeams:
          - team: ${DEF_PUB_PULL}
            permission: pull
          - team: ${DEF_PUB_PUSH}
            permission: push
          - team: ${DEF_PUB_ADMIN}
            permission: admin
        defaultPrivateRepositoryTeams:
          - team: ${DEF_PRIV_PULL}
            permission: pull
          - team: ${DEF_PRIV_PUSH}
            permission: push
          - team: ${DEF_PRIV_ADMIN}
            permission: admin

$( if [[ -n "${CUSTOM_PRIV_TEAM}" ]]; then \
    printf "%s\n" \
"        teamRepositoryAssignments:" \
"          - team: ${CUSTOM_PRIV_TEAM}" \
"            repositories:" \
"              - ${E2E_REPO_PRIVATE}" \
"            permission: ${CUSTOM_PRIV_PERMISSION}"; \
  fi )
    
    githubAccountLinks:
      - userID: $(read_var USER_0_GREENHOUSE_ID)
        githubID: $(read_var USER_0_GITHUB_USERID)
      - userID: $(read_var USER_1_GREENHOUSE_ID)
        githubID: $(read_var USER_1_GITHUB_USERID)
EOF
