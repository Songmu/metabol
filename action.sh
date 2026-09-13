#!/usr/bin/env bash
set -euo pipefail

cd "$GITHUB_WORKSPACE"
thresh_bin="$(mktemp -d)"
mdhq_prefix="$(mktemp -d "${RUNNER_TEMP%/}/mdhq.XXXXXX")"
trap 'rm -rf "$thresh_bin" "$mdhq_prefix"' EXIT

manifest_base="$(mktemp "${RUNNER_TEMP%/}/thresh-manifest.XXXXXX")"
manifest="${manifest_base}.jsonl"
mv "$manifest_base" "$manifest"
{
  echo "manifest=$manifest"
  echo "count=0"
} >> "$GITHUB_OUTPUT"

is_exact_semver() {
  local value="$1"
  local semver_core='(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'
  local semver_identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
  local semver_prerelease="(-${semver_identifier}(\.${semver_identifier})*)?"
  local semver_build='(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?'
  [[ "$value" =~ ^${semver_core}${semver_prerelease}${semver_build}$ ]]
}

action_ref="${ACTION_REF:-main}"
thresh_version="$THRESH_VERSION_INPUT"
if [[ -z "$thresh_version" ]]; then
  if [[ "$action_ref" == v* ]] && is_exact_semver "${action_ref#v}"; then
    thresh_version="$action_ref"
  else
    thresh_version="latest"
  fi
fi
curl -sfL --retry 3 \
  "https://raw.githubusercontent.com/Songmu/thresh/${action_ref}/install.sh" |
  sh -s -- -b "$thresh_bin" "$thresh_version" 2>&1

if [[ -z "$mdhq_prefix" || "$mdhq_prefix" != "${RUNNER_TEMP%/}/"* ]]; then
  echo "::error::failed to create a safe temporary directory for mdhq"
  exit 1
fi
rm -rf "$mdhq_prefix/package.json" "$mdhq_prefix/package-lock.json" \
  "$mdhq_prefix/node_modules"
if [[ -n "$MDHQ_VERSION_INPUT" ]]; then
  if ! is_exact_semver "$MDHQ_VERSION_INPUT"; then
    echo "::error::mdhq-version must be an exact semantic version"
    exit 1
  fi
  printf '{"private":true,"dependencies":{"@songmu/mdhq":"%s"}}\n' \
    "$MDHQ_VERSION_INPUT" > "$mdhq_prefix/package.json"
  npm install --prefix "$mdhq_prefix" --package-lock-only --ignore-scripts
else
  mdhq_package_json="$GITHUB_ACTION_PATH/package.json"
  mdhq_package_lock="$GITHUB_ACTION_PATH/package-lock.json"
  if [[ ! -f "$mdhq_package_json" || ! -f "$mdhq_package_lock" ]]; then
    echo "::error::package.json and package-lock.json are required to install mdhq; verify the action version or ref"
    exit 1
  fi
  cp "$mdhq_package_json" "$mdhq_package_lock" "$mdhq_prefix/"
fi
npm ci --prefix "$mdhq_prefix" --omit=dev
export PATH="$thresh_bin:$mdhq_prefix/node_modules/.bin:$PATH"

args=()
if [[ -n "${CONFIG_INPUT:-}" ]]; then
  args+=(--config "$CONFIG_INPUT")
fi
if [[ -n "${ROOT_INPUT:-}" ]]; then
  args+=(--root "$ROOT_INPUT")
fi
if [[ -n "${ASSETS_INPUT:-}" ]]; then
  args+=(--assets="$ASSETS_INPUT")
fi
if [[ -n "${UPDATE_INPUT:-}" ]]; then
  args+=(--update="$UPDATE_INPUT")
fi
if [[ -n "${TIMEZONE_INPUT:-}" ]]; then
  args+=(--timezone "$TIMEZONE_INPUT")
fi
if [[ -n "${AT_INPUT:-}" ]]; then
  args+=(--at "$AT_INPUT")
fi
if [[ -n "${WINDOW_COUNT_INPUT:-}" ]]; then
  args+=(--window-count "$WINDOW_COUNT_INPUT")
fi

set +e
if ((${#args[@]})); then
  thresh "${args[@]}" | tee "$manifest"
  statuses=("${PIPESTATUS[@]}")
else
  thresh | tee "$manifest"
  statuses=("${PIPESTATUS[@]}")
fi
set -e

status="${statuses[0]}"
if [[ "$status" -eq 0 && "${statuses[1]}" -ne 0 ]]; then
  status="${statuses[1]}"
fi
count="$(awk 'NF { count++ } END { print count + 0 }' "$manifest")"

{
  echo "manifest=$manifest"
  echo "count=$count"
} >> "$GITHUB_OUTPUT"

exit "$status"
