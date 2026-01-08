#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="config/crd/bases"
CHART_DIR="charts/repo-guard/crds"

# Helper: map Kind to chart filename (lowercased Kind + "-crd.yaml")
kind_to_filename() {
  # Input: Kind (e.g., GithubOrganization)
  # Output: filename (e.g., githuborganization-crd.yaml)
  local kind="$1"
  # Portable lowercase conversion (works on macOS bash 3.x)
  local lc
  lc=$(printf "%s" "$kind" | tr '[:upper:]' '[:lower:]')
  printf "%s-crd.yaml" "$lc"
}

# Detect if installed yq is mikefarah v4
has_mikefarah_yq_v4() {
  local yq_bin
  yq_bin=${YQ:-yq}
  if ! command -v "$yq_bin" >/dev/null 2>&1; then
    return 1
  fi
  # Sample outputs:
  #  - "yq (https://github.com/mikefarah/yq/) version 4.44.1"
  #  - "yq version 4.34.1"
  #  - other (python yq / kislyuk) will not contain mikefarah nor version 4 prefix
  local ver
  ver=$("$yq_bin" --version 2>/dev/null || true)
  if printf "%s" "$ver" | grep -qi 'mikefarah'; then
    :
  else
    return 1
  fi
  if printf "%s" "$ver" | grep -Eq '\bversion[[:space:]]+4\.'; then
    return 0
  fi
  if printf "%s" "$ver" | grep -Eq '\byq[[:space:]]+v4\.'; then
    return 0
  fi
  return 1
}

merge_additional_printer_columns() {
  # Merge additionalPrinterColumns from generated (src) and existing (dst) CRDs by 'name'
  # Requires yq (mikefarah v4). If not present, just copy src over dst.
  local src="$1"  # generated from BASE_DIR
  local dst="$2"  # existing in CHART_DIR

  if ! has_mikefarah_yq_v4; then
    # No yq v4 available; perform header-preserving copy
    write_with_preserved_header "$src" "$dst"
    return 0
  fi

  # Create a temp file to hold the merged document
  local tmp
  tmp="$(mktemp)"

  # Strategy:
  # - Take the generated CRD ($src) as the base document
  # - For versions named "v1", union the additionalPrinterColumns arrays by unique 'name'
  # - Everything else comes from $src
  ${YQ:-yq} ea -n '
    def cols(d): (d.spec.versions // [])
      | map(select(.name == "v1").additionalPrinterColumns // [])
      | (.[0] // []);
    def setcols(d; cols): d | (.spec.versions |= (map(
      if .name == "v1" then .additionalPrinterColumns = cols else . end)));

    (load("'"$src"'")) as $src |
    (load("'"$dst"'")) as $dst |
    ($src) as $base |
    ((cols($src) + cols($dst)) | unique_by(.name)) as $mergedCols |
    setcols($base; $mergedCols)
  ' > "$tmp"

  # Write merged content back to destination, preserving SPDX or any leading header from dst
  write_with_preserved_header "$tmp" "$dst"
}

# Extract .spec.names.kind from a CRD YAML using awk (no external deps)
extract_spec_names_kind() {
  local file="$1"
  awk '
    BEGIN { in_names=0 }
    /^[[:space:]]*names:[[:space:]]*$/ { in_names=1; next }
    in_names==1 {
      # if we hit a new top-level key, stop
      if ($0 ~ /^[^[:space:]]/) { in_names=0; next }
      # look for a line starting with kind:
      if ($0 ~ /^[[:space:]]*kind:[[:space:]]*/) {
        line=$0
        sub(/^[[:space:]]*kind:[[:space:]]*/, "", line)
        sub(/[[:space:]]*#.*/, "", line)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
        print line
        exit 0
      }
    }
  ' "$file"
}

# Find matching chart CRD file by Kind
find_chart_file_by_kind() {
  local kind="$1"
  local lc_kind
  lc_kind=$(printf "%s" "$kind" | tr '[:upper:]' '[:lower:]')

  # First try by conventional filename
  local by_name="$CHART_DIR/$(kind_to_filename "$kind")"
  if [[ -f "$by_name" ]]; then
    printf "%s" "$by_name"
    return 0
  fi
  # Fallback: scan chart directory and match by spec.names.kind
  local f ck
  for f in "$CHART_DIR"/*.yaml; do
    [[ -e "$f" ]] || continue
    ck=$(extract_spec_names_kind "$f" || true)
    if [[ -n "$ck" ]] && [[ "$(printf "%s" "$ck" | tr '[:upper:]' '[:lower:]')" == "$lc_kind" ]]; then
      printf "%s" "$f"
      return 0
    fi
  done
  return 1
}

# Read and preserve any leading comment header (e.g., SPDX) from destination file, then
# write the body from source file without its own leading comments/---.
# Usage: write_with_preserved_header <src> <dst>
write_with_preserved_header() {
  local src="$1"
  local dst="$2"

  local header=""
  if [[ -f "$dst" ]]; then
    # Collect contiguous leading comment or blank lines until first non-comment or '---'
    while IFS= read -r line; do
      if [[ "$line" =~ ^[[:space:]]*# ]]; then
        header+="$line"$'\n'
        continue
      fi
      if [[ "$line" =~ ^[[:space:]]*$ ]]; then
        header+="$line"$'\n'
        continue
      fi
      # Stop on YAML doc start or first non-comment content
      break
    done < "$dst"
  fi

  # Prepare body by stripping leading comments, blank lines, and YAML doc start from src
  local body_tmp
  body_tmp="$(mktemp)"
  awk '
    BEGIN { drop=1 }
    {
      if (drop) {
        if ($0 ~ /^[[:space:]]*#/ || $0 ~ /^[[:space:]]*$/ || $0 ~ /^---[[:space:]]*$/) { next }
        drop=0
      }
      print
    }
  ' "$src" > "$body_tmp"

  # Assemble final file
  local out_tmp
  out_tmp="$(mktemp)"
  if [[ -n "$header" ]]; then
    printf "%s" "$header" > "$out_tmp"
  else
    : > "$out_tmp"
  fi
  # Append the body directly without adding a YAML document start (---) at the beginning
  cat "$body_tmp" >> "$out_tmp"

  mv "$out_tmp" "$dst"
  rm -f "$body_tmp"
}

# Iterate over generated CRDs and sync to chart if a counterpart exists
for f in "$BASE_DIR"/*.yaml; do
  # Extract Kind from generated CRD without yq dependency
  kind=$(extract_spec_names_kind "$f" || true)
  if [[ -z "$kind" ]]; then
    echo "Skipping (no Kind): $f" >&2
    continue
  fi

  if target=$(find_chart_file_by_kind "$kind" 2>/dev/null); then
    # Merge additionalPrinterColumns where possible; otherwise replace
    merge_additional_printer_columns "$f" "$target"
    echo "Synced $f -> $target"
  else
    # Only copy if you also want to add new CRDs into the chart automatically
    # Uncomment the next 2 lines if desired:
    # cp "$f" "$target"
    # echo "Added $f -> $target"
    :
  fi

done
