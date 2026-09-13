metabol
=======

[![Test Status](https://github.com/Songmu/metabol/actions/workflows/test.yaml/badge.svg?branch=main)][actions]
[![Coverage Status](https://codecov.io/gh/Songmu/metabol/branch/main/graph/badge.svg)][codecov]
[![MIT License](https://img.shields.io/github/license/Songmu/metabol)][license]
[![PkgGoDev](https://pkg.go.dev/badge/github.com/Songmu/metabol)][PkgGoDev]

[actions]: https://github.com/Songmu/metabol/actions?workflow=test
[codecov]: https://codecov.io/gh/Songmu/metabol
[license]: https://github.com/Songmu/metabol/blob/main/LICENSE
[PkgGoDev]: https://pkg.go.dev/github.com/Songmu/metabol

`metabol` is a feed aggregator that archives articles locally as Markdown.

It is a stateless, configuration-driven orchestrator that collects feed items
from a deterministic time window and asks
[`mdhq`](https://github.com/Songmu/mdhq) to save the linked articles. It
separates the scheduler's execution time from the logical window being
processed, making scheduled runs repeatable and safe to retry.

## Installation

```console
# Install the latest version. (Install it into ./bin/ by default).
$ curl -sfL https://raw.githubusercontent.com/Songmu/metabol/main/install.sh | sh -s

# Specify the installation directory and version.
$ curl -sfL https://raw.githubusercontent.com/Songmu/metabol/main/install.sh |
    sh -s -- -b "$(go env GOPATH)/bin" vX.Y.Z

# Or install with Go.
$ go install github.com/Songmu/metabol/cmd/metabol@latest
```

**Prerequisite:** `metabol` invokes `mdhq` as an external command, so install it
separately and ensure it is on `PATH`. For normal local use, install it globally:

```console
$ npm install --global @songmu/mdhq
```

To use the `mdhq` version pinned and tested for this repository checkout,
install the dependencies from the repository root and add the local binary
directory to `PATH`:

```console
$ npm ci
$ export PATH="$PWD/node_modules/.bin:$PATH"
```

The locked dependency graph requires Node.js 22.19.0 or later.

## Agent Skill

The metabol binary includes an English [Agent Skill](https://agentskills.io/)
that teaches compatible coding agents how to configure metabol, select
deterministic processing windows, run backfills, and consume its JSON Lines
output. The skill is managed through the bundled
[skillsmith](https://github.com/Songmu/skillsmith) subcommand and is never
installed automatically.

```console
# Inspect the skill bundled with this metabol release.
$ metabol skills list

# Install it for the current user under ~/.agents/skills.
$ metabol skills install

# Install it under the current repository's .agents/skills directory.
$ metabol skills install --scope repo

# Preview a change, check status, and apply an updated bundled version.
$ metabol skills update --dry-run
$ metabol skills status
$ metabol skills update
```

Use `--prefix /path/to/skills` to choose a custom installation directory.
`reinstall` replaces managed skills even when the recorded version matches,
while `uninstall` removes managed copies. New skill content ships with new
metabol releases; installing a newer binary does not modify the user's skill
directory until `metabol skills update` or `reinstall` is run.

## Configuration

Create a starter configuration in the current directory:

```console
$ metabol init
```

If the directory is not empty, `metabol` asks for confirmation and defaults to
not creating the file. It never overwrites an existing `metabol.yaml`.

By default, `metabol` reads `metabol.yaml` from the current directory. The
generated configuration is:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/Songmu/metabol/main/schema.yaml

# timezone: Asia/Tokyo # Optional, but recommended.

window:
  daily: "07:00"

sources:
  - https://example.com/feed.xml
```

A source may be a URL scalar or an object. These forms are equivalent:

```yaml
sources:
  - https://example.com/feed
```

```yaml
sources:
  - url: https://example.com/feed
```

The object form also accepts an optional `name` reserved for future source
metadata. It currently does not affect processing or output.

Feed fetching is provided by [`rssnip`](https://github.com/Songmu/rssnip) and
supports RSS 2.0, Atom 1.0, RDF/RSS 1.0, and JSON Feed 1.0 and 1.1. Blog or site
URLs are also accepted for feed discovery, but direct feed URLs are preferred.

`window.daily` defines a local-time boundary, not a schedule. Every processing
window is a half-open interval, `[start, end)`, calculated in `timezone`.
Use `metabol` to run with the default config, or `metabol --config path/to/file`
to read only the explicitly selected file.

Values are resolved in this order:

```text
CLI flag > METABOL_* environment variable > configuration file > default value
```

The available settings are:

| Configuration key | CLI flag | Environment variable | Type | Default | Meaning |
| --- | --- | --- | --- | --- | --- |
| `window.daily` | - | - | `HH:MM` string | Required | Local-time boundary for each daily processing window |
| `window.count` | `--window-count` | `METABOL_WINDOW_COUNT` | Integer (`1`-`366`) | `1` | Number of consecutive processing windows |
| `sources` | - | - | Non-empty list of URLs or source objects | Required | Feed sources to process |
| `root` | `--root` | `METABOL_ROOT` | Path string | Configuration file directory | Markdown output directory |
| `assets` | `--assets` | `METABOL_ASSETS` | Boolean | `false` | Download article assets |
| `update` | `--update` | `METABOL_UPDATE` | Boolean | `false` | Re-evaluate existing Markdown |
| `timezone` | `--timezone` | `METABOL_TIMEZONE` | IANA timezone string | Local timezone | Timezone used to calculate processing windows |
| - | `--config` | `METABOL_CONFIG` | Path string | `metabol.yaml` | Configuration file to read |
| - | `--at` | `METABOL_AT` | RFC 3339 timestamp or `YYYY-MM-DD` | Not set | Select the window containing the specified date or time; when omitted, the most recently completed window is used |

Relative `root` values in the configuration file are resolved from that file's
directory. Relative values passed with `--root` or `METABOL_ROOT` are resolved
from the command's working directory. After resolving `assets`, `metabol`
explicitly passes either `--assets` or `--no-assets` to `mdhq`, so the resolved
`metabol` setting overrides any value in mdhq configuration.

## Time windows

Window boundaries are based on local calendar time in the configured timezone.
A window that crosses a daylight-saving transition may therefore span 23 or 25
hours, while remaining contiguous and non-overlapping.

By default, `metabol` selects the most recently completed window unless `--at`
is specified. For example, with a daily boundary of `07:00` in `Asia/Tokyo`, a
run at `2026-09-11 19:00 JST` processes:

```text
[2026-09-10 07:00, 2026-09-11 07:00)
```

Runs at `08:00`, `14:30`, or `23:00` on September 11 therefore select the same
logical window.

`--at` selects the `[start, end)` window containing the supplied value rather
than the last complete window:

```console
$ metabol --at 2026-09-11
$ metabol --at 2026-09-11T10:30:00+09:00
```

RFC 3339 timestamps and `YYYY-MM-DD` dates are accepted. A date without a time
means `00:00` in the configured timezone. A value exactly on a boundary belongs
to the window beginning at that boundary. Backfills are best effort because a
feed may no longer contain old items.

`window.count`, `--window-count`, or `METABOL_WINDOW_COUNT` selects multiple
consecutive windows. The default is `1`. The selected window is the newest;
earlier windows are added by walking backward and all windows are processed
from oldest to newest:

```console
$ metabol --at 2026-09-11 --window-count 3
```

This processes the window containing September 11 and the two windows
immediately before it. The count must be between `1` and `366`.

## Output and errors

### Standard output format

`metabol` writes its results to stdout as JSON Lines (JSONL): one compact JSON
object followed by a newline for each successfully processed article. The
output uses the same camelCase field names and value semantics as the
corresponding `mdhq` result fields, while omitting other `mdhq` fields.

```jsonl
{"requestedUrl":"https://example.com/a","sourceUrl":"https://example.com/a","path":"example.com/a.md","status":"saved"}
{"requestedUrl":"https://example.com/b","sourceUrl":"https://example.com/b","path":"example.com/b.md","status":"skipped"}
```

Every record contains these required, nonempty string fields:

| Field | Description |
| --- | --- |
| `requestedUrl` | URL passed to `mdhq` for the article |
| `sourceUrl` | Source URL reported by `mdhq`, which may differ after redirects or source resolution |
| `path` | Path of the Markdown file relative to `root` |
| `status` | Processing result: `saved`, `updated`, `unchanged`, or `skipped` |

No record is emitted for a failed article. Logs, warnings, and diagnostics go
to stderr, keeping stdout machine-readable.

If an individual feed or article fails, `metabol` continues processing the
remaining work, preserves successful records on stdout, and exits nonzero after
all possible work is complete. Configuration, validation, and window
calculation errors fail before article processing.

## GitHub Action

The repository includes a composite action that installs `metabol` and an
isolated, lockfile-pinned `@songmu/mdhq`, provisions Node.js 24.21.0, and then
captures the JSONL written to stdout in a manifest file:

```yaml
jobs:
  collect:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          persist-credentials: false
      - id: metabol
        uses: Songmu/metabol@v0
        with:
          config: metabol.yaml
          root: articles
          timezone: Asia/Tokyo
          at: "2026-09-11"
      - uses: actions/upload-artifact@v7
        if: ${{ !cancelled() && steps.metabol.outputs.count > 0 }}
        with:
          name: metabol-manifest
          path: ${{ steps.metabol.outputs.manifest }}
```

Inputs are `config`, `root`, `assets`, `update`, `timezone`, `at`, and
`window-count`. The action installs the latest `metabol` release and the
`@songmu/mdhq` version locked in its bundled `package-lock.json`.
Optional CLI inputs are omitted when empty, so configuration and
environment-variable precedence remains intact. Explicit `false` values for
`assets` and `update` are forwarded to the CLI.

The action exposes:

| Output | Description |
| --- | --- |
| `manifest` | Absolute path to the captured JSONL manifest |
| `count` | Number of nonempty lines in the manifest |

The file referenced by `manifest` is a capture of the CLI stdout documented
above. The `manifest` output value is the file path, not the JSONL content
itself. `count` therefore normally equals the number of successfully emitted
article records.

The outputs are initialized before installation and updated after `metabol`
runs. They therefore remain available with an empty manifest and count `0` if
setup fails, or with a partial manifest and its nonempty-line count if `metabol`
exits nonzero. Runtime failures preserve the original `metabol` or `tee` status.
The example uploads the manifest only when at least one article record was
emitted. The `!cancelled()` status check allows this step to run when `metabol`
exits nonzero after partially succeeding, while still skipping it when the
workflow is canceled.

## Guarantees and non-goals

Given the same configuration and reference time, `metabol` selects the same
logical window without an execution-history database. Re-running a window is
designed to be idempotent through `mdhq`, but the contents of remote feeds and
articles are outside `metabol`'s reproducibility guarantee.

`metabol` is not a scheduler, state database, fetched-URL database, raw feed
archive, or replacement for `mdhq`'s storage layout.

## Author

[Songmu](https://github.com/Songmu)
