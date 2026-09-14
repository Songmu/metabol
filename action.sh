#!/usr/bin/env bash
set -euo pipefail

cd "$GITHUB_WORKSPACE"
metabol_version="v0.0.2"
metabol_bin="$(mktemp -d)"
mdhq_prefix="$(mktemp -d "${RUNNER_TEMP%/}/mdhq.XXXXXX")"
trap 'rm -rf "$metabol_bin" "$mdhq_prefix"' EXIT

results_base="$(mktemp "${RUNNER_TEMP%/}/metabol-results.XXXXXX")"
results="${results_base}.jsonl"
mv "$results_base" "$results"
{
  echo "results=$results"
  echo "count=0"
} >> "$GITHUB_OUTPUT"

sh "$GITHUB_ACTION_PATH/install.sh" -b "$metabol_bin" "$metabol_version"
cp "$GITHUB_ACTION_PATH/package.json" "$GITHUB_ACTION_PATH/package-lock.json" \
  "$mdhq_prefix/"
npm ci --prefix "$mdhq_prefix" --omit=dev
export PATH="$metabol_bin:$mdhq_prefix/node_modules/.bin:$PATH"

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
if [[ -n "${CATCHUP_INPUT:-}" ]]; then
  args+=(--catchup="$CATCHUP_INPUT")
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
  metabol "${args[@]}" | tee "$results"
  statuses=("${PIPESTATUS[@]}")
else
  metabol | tee "$results"
  statuses=("${PIPESTATUS[@]}")
fi
set -e

status="${statuses[0]}"
if [[ "$status" -eq 0 && "${statuses[1]}" -ne 0 ]]; then
  status="${statuses[1]}"
fi
count="$(awk 'NF { count++ } END { print count + 0 }' "$results")"

{
  echo "results=$results"
  echo "count=$count"
} >> "$GITHUB_OUTPUT"

exit "$status"
