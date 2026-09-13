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

`metabol` is a stateless, configuration-driven orchestrator that collects feed
items from a deterministic time window and asks
[`mdhq`](https://github.com/Songmu/mdhq) to save them as Markdown. It separates
the scheduler's execution time from the logical window being processed, making
scheduled runs repeatable and safe to retry.

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

**Prerequisite:** The supported `@songmu/mdhq` version is managed in
`package.json` and `package-lock.json`; the locked dependency graph requires
Node.js 22.19.0 or later. `metabol` invokes `mdhq` as an external command, so
install it separately and ensure it is on `PATH`. To use the
repository-managed version during development:

```console
$ npm ci
$ export PATH="$PWD/node_modules/.bin:$PATH"
```

## Configuration

By default, `metabol` reads `metabol.yaml` from the current directory:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/Songmu/metabol/main/schema.yaml
root: path/to/articles
assets: false
update: false
timezone: Asia/Tokyo

window:
  daily: "07:00"
  count: 1

sources:
  - https://hnrss.org/newest
  - url: https://dropbox.tech/feed
    name: dropbox-tech
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

The object form currently also accepts `name` as reserved source metadata.
`window.daily` defines a local-time boundary, not a schedule. Every processing
window is a half-open interval, `[start, end)`, calculated in `timezone`.
Use `metabol` to run with the default config, or `metabol --config path/to/file`
to read only the explicitly selected file.

Values are resolved in this order:

```text
CLI flag
  > METABOL_* environment variable
  > metabol.yaml
  > directory containing the configuration file (root only)
```

The supported flags and corresponding environment variables are:

| Flag | Environment variable | Meaning |
| --- | --- | --- |
| `--config` | `METABOL_CONFIG` | Configuration file path |
| `--root` | `METABOL_ROOT` | Markdown output directory |
| `--assets` | `METABOL_ASSETS` | Download article assets |
| `--update` | `METABOL_UPDATE` | Re-evaluate existing Markdown |
| `--timezone` | `METABOL_TIMEZONE` | IANA timezone for window calculation |
| `--at` | `METABOL_AT` | Select the logical window containing a timestamp |
| `--window-count` | `METABOL_WINDOW_COUNT` | Process consecutive logical windows ending with the selected window |
| `--version` | - | Print the installed `metabol` version |

When `root` is not set by the CLI, `METABOL_ROOT`, or the configuration file,
metabol uses the directory containing the selected configuration file.
Relative `root` values in the configuration file are resolved from that file's
directory. Relative values passed with `--root` or `METABOL_ROOT` are resolved
from the command's working directory.
`assets` and `update` default to `false`; `timezone` defaults to the local
timezone. After resolving `assets`, `metabol` explicitly passes either
`--assets` or `--no-assets` to `mdhq`, so the resolved `metabol` setting
overrides any value in mdhq configuration.

## Time windows

Without `--at`, `metabol` selects the last window that has completely ended. For
example, with a daily boundary of `07:00` in `Asia/Tokyo`, a run at
`2026-09-11 19:00 JST` processes:

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

### Manifest format

`metabol` writes its manifest to stdout as JSON Lines (JSONL): one compact JSON
object followed by a newline for each successfully processed article. The
manifest uses the same camelCase field names and value semantics as the
corresponding `mdhq` result fields, while omitting other `mdhq` fields.

```jsonl
{"requestedUrl":"https://example.com/a","sourceUrl":"https://example.com/a","path":"/data/mdhq/example.com/a.md","status":"saved"}
{"requestedUrl":"https://example.com/b","sourceUrl":"https://example.com/b","path":"/data/mdhq/example.com/b.md","status":"skipped"}
```

Every record contains these required, nonempty string fields:

| Field | Description |
| --- | --- |
| `requestedUrl` | URL passed to `mdhq` for the article |
| `sourceUrl` | Source URL reported by `mdhq`, which may differ after redirects or source resolution |
| `path` | Path of the Markdown file managed by `mdhq` |
| `status` | Processing result: `saved`, `updated`, `unchanged`, or `skipped` |

No record is emitted for a failed article. Logs, warnings, and diagnostics go
to stderr and are not part of the manifest, keeping stdout machine-readable.

If an individual feed or article fails, `metabol` continues processing the
remaining work, preserves successful records on stdout, and exits nonzero after
all possible work is complete. Configuration, validation, and window
calculation errors fail before article processing.

## GitHub Action

**Prerequisite:** The runner must provide Node.js 22.19.0 or later because the
locked `@songmu/mdhq` dependency graph currently requires it.

The repository includes a composite action that installs `metabol` and an
isolated, lockfile-pinned `@songmu/mdhq`, then captures stdout as a JSONL
manifest:

```yaml
jobs:
  collect:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - id: metabol
        uses: Songmu/metabol@v0
        with:
          config: metabol.yaml
          root: articles
          timezone: Asia/Tokyo
          at: "2026-09-11"
      - uses: actions/upload-artifact@v4
        if: always()
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

The file referenced by `manifest` contains the CLI stdout format documented
above; the output value is a path, not the JSONL content itself. `count`
therefore normally equals the number of successfully emitted article records.

The outputs are initialized before installation and updated after `metabol`
runs. They therefore remain available with an empty manifest and count `0` if
setup fails, or with a partial manifest and its nonempty-line count if `metabol`
exits nonzero. Runtime failures preserve the original `metabol` or `tee` status.
Use `if: always()` on later steps that must consume a failed run's manifest.

## Guarantees and non-goals

Given the same configuration and reference time, `metabol` selects the same
logical window without an execution-history database. Re-running a window is
designed to be idempotent through `mdhq`, but the contents of remote feeds and
articles are outside `metabol`'s reproducibility guarantee.

`metabol` is not a scheduler, state database, fetched-URL database, feed archive,
or replacement for `mdhq`'s storage layout.

## Author

[Songmu](https://github.com/Songmu)
