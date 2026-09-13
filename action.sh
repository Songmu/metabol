#!/usr/bin/env bash
set -euo pipefail

cd "$GITHUB_WORKSPACE"
thresh_bin="$(mktemp -d)"
mdhq_prefix="$(mktemp -d)"
trap 'rm -rf "$thresh_bin" "$mdhq_prefix"' EXIT

manifest_base="$(mktemp "${RUNNER_TEMP%/}/thresh-manifest.XXXXXX")"
manifest="${manifest_base}.jsonl"
mv "$manifest_base" "$manifest"
{
  echo "manifest=$manifest"
  echo "count=0"
} >> "$GITHUB_OUTPUT"

action_ref="${ACTION_REF:-main}"
thresh_version="$THRESH_VERSION_INPUT"
if [[ -z "$thresh_version" ]]; then
  semver_core='(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'
  semver_identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
  semver_prerelease="(-${semver_identifier}(\.${semver_identifier})*)?"
  semver_build='(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?'
  if [[ "$action_ref" =~ ^v${semver_core}${semver_prerelease}${semver_build}$ ]]; then
    thresh_version="$action_ref"
  else
    thresh_version="latest"
  fi
fi
curl -sfL --retry 3 \
  "https://raw.githubusercontent.com/Songmu/thresh/${action_ref}/install.sh" |
  sh -s -- -b "$thresh_bin" "$thresh_version" 2>&1

npm install --global --prefix "$mdhq_prefix" \
  "@songmu/mdhq@${MDHQ_VERSION_INPUT}"
export PATH="$thresh_bin:$mdhq_prefix/bin:$mdhq_prefix:$PATH"

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
