thresh
=======

[![Test Status](https://github.com/Songmu/thresh/actions/workflows/test.yaml/badge.svg?branch=main)][actions]
[![Coverage Status](https://codecov.io/gh/Songmu/thresh/branch/main/graph/badge.svg)][codecov]
[![MIT License](https://img.shields.io/github/license/Songmu/thresh)][license]
[![PkgGoDev](https://pkg.go.dev/badge/github.com/Songmu/thresh)][PkgGoDev]

[actions]: https://github.com/Songmu/thresh/actions?workflow=test
[codecov]: https://codecov.io/gh/Songmu/thresh
[license]: https://github.com/Songmu/thresh/blob/main/LICENSE
[PkgGoDev]: https://pkg.go.dev/github.com/Songmu/thresh

`thresh` is a stateless, configuration-driven orchestrator that collects feed
items from a deterministic time window and asks
[`mdhq`](https://github.com/Songmu/mdhq) to save them as Markdown. It separates
the scheduler's execution time from the logical window being processed, making
scheduled runs repeatable and safe to retry.

## Installation

```console
# Install the latest version. (Install it into ./bin/ by default).
$ curl -sfL https://raw.githubusercontent.com/Songmu/thresh/main/install.sh | sh -s

# Specify the installation directory and version.
$ curl -sfL https://raw.githubusercontent.com/Songmu/thresh/main/install.sh |
    sh -s -- -b "$(go env GOPATH)/bin" vX.Y.Z

# Or install with Go.
$ go install github.com/Songmu/thresh/cmd/thresh@latest
```

`thresh` invokes `mdhq` as an external command, so install it separately and
ensure it is on `PATH`:

```console
$ npm install --global @songmu/mdhq
```

## Configuration

By default, `thresh` reads `thresh.yaml` from the current directory:

```yaml
root: path/to/articles
assets: false
update: false
timezone: Asia/Tokyo

window:
  daily: "07:00"

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
Use `thresh` to run with the default config, or `thresh --config path/to/file`
to read only the explicitly selected file.

Values are resolved in this order:

```text
CLI flag
  > THRESH_* environment variable
  > thresh.yaml
  > downstream-specific fallback
  > default
```

The supported flags and corresponding environment variables are:

| Flag | Environment variable | Meaning |
| --- | --- | --- |
| `--config` | `THRESH_CONFIG` | Configuration file path |
| `--root` | `THRESH_ROOT` | Markdown output directory |
| `--assets` | `THRESH_ASSETS` | Download article assets |
| `--update` | `THRESH_UPDATE` | Re-evaluate existing Markdown |
| `--timezone` | `THRESH_TIMEZONE` | IANA timezone for window calculation |
| `--at` | `THRESH_AT` | Select the logical window containing a timestamp |
| `--version` | - | Print the installed `thresh` version |

`root` additionally falls back to `MDHQ_ROOT` and is required after resolution.
`assets` and `update` default to `false`; `timezone` defaults to the local
timezone.

## Time windows

Without `--at`, `thresh` selects the last window that has completely ended. For
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
$ thresh --at 2026-09-11
$ thresh --at 2026-09-11T10:30:00+09:00
```

RFC 3339 timestamps and `YYYY-MM-DD` dates are accepted. A date without a time
means `00:00` in the configured timezone. A value exactly on a boundary belongs
to the window beginning at that boundary. Backfills are best effort because a
feed may no longer contain old items.

## Output and errors

`thresh` writes one compact JSON object per successfully processed article to
stdout:

```json
{"requestedUrl":"https://example.com/a","sourceUrl":"https://example.com/a","path":"/data/mdhq/example.com/a.md","status":"saved"}
{"requestedUrl":"https://example.com/b","sourceUrl":"https://example.com/b","path":"/data/mdhq/example.com/b.md","status":"skipped"}
```

Each JSONL record contains `requestedUrl`, `sourceUrl`, `path`, and `status`.
Supported statuses are `saved`, `updated`, `unchanged`, and `skipped`. Logs and
diagnostics go to stderr, keeping stdout machine-readable.

If an individual feed or article fails, `thresh` continues processing the
remaining work, preserves successful records on stdout, and exits nonzero after
all possible work is complete. Configuration, validation, and window
calculation errors fail before article processing.

## GitHub Action

The repository includes a composite action that installs `thresh` and an
isolated, pinned `@songmu/mdhq`, then captures stdout as a JSONL manifest:

```yaml
jobs:
  collect:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - id: thresh
        uses: Songmu/thresh@v0
        with:
          config: thresh.yaml
          root: articles
          timezone: Asia/Tokyo
          at: "2026-09-11"
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: thresh-manifest
          path: ${{ steps.thresh.outputs.manifest }}
```

Inputs are `version`, `mdhq-version`, `config`, `root`, `assets`, `update`,
`timezone`, and `at`. `version` defaults to an exact semantic-version action
ref (including prerelease or build metadata), or to the latest release for a
branch, commit SHA, or moving-major ref; `mdhq-version` defaults to `0.0.4`.
Optional CLI inputs are omitted when empty, so configuration and
environment-variable precedence remains intact. Explicit `false` values for
`assets` and `update` are forwarded to the CLI.

The action exposes:

| Output | Description |
| --- | --- |
| `manifest` | Absolute path to the captured JSONL manifest |
| `count` | Number of nonempty lines in the manifest |

The outputs are initialized before installation and updated after `thresh`
runs. They therefore remain available with an empty manifest and count `0` if
setup fails, or with a partial manifest and its nonempty-line count if `thresh`
exits nonzero. Runtime failures preserve the original `thresh` or `tee` status.
Use `if: always()` on later steps that must consume a failed run's manifest.

## Guarantees and non-goals

Given the same configuration and reference time, `thresh` selects the same
logical window without an execution-history database. Re-running a window is
designed to be idempotent through `mdhq`, but the contents of remote feeds and
articles are outside `thresh`'s reproducibility guarantee.

`thresh` is not a scheduler, state database, fetched-URL database, feed archive,
or replacement for `mdhq`'s storage layout.

## Author

[Songmu](https://github.com/Songmu)
