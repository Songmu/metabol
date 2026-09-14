---
name: metabol
description: Use the metabol CLI to collect articles from RSS, Atom, RDF, or JSON feeds into a local Markdown archive through mdhq. Use this skill whenever a task involves configuring metabol, choosing deterministic daily collection windows, running feed backfills, automating metabol with cron or GitHub Actions, or interpreting its JSON Lines output and partial-failure behavior.
license: MIT
---

# metabol

Use `metabol` to collect feed entries from deterministic time windows and save
their linked articles as Markdown through the external `mdhq` command.

## Prepare the environment

Install `metabol`, then install `mdhq` separately and ensure both commands are
on `PATH`:

```bash
go install github.com/Songmu/metabol/cmd/metabol@latest
npm install --global @songmu/mdhq
```

Create `metabol.yaml` in the working directory, then edit the generated source
list for the feeds to collect:

```bash
metabol init
```

An example configuration is:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/Songmu/metabol/main/schema.yaml
timezone: Asia/Tokyo

window:
  daily: "07:00"

sources:
  - https://example.com/feed.xml
```

Set `root` when the Markdown archive should not be written relative to the
configuration file:

```yaml
root: ./articles
assets: false
update: false
```

## Run a collection

Run `metabol` with no positional arguments:

```bash
metabol
metabol --config path/to/metabol.yaml
```

Values resolve in this order:

```text
CLI flag > METABOL_* environment variable > configuration file > default
```

Useful overrides include `--root`, `--assets`, `--update`, `--timezone`,
`--catchup`, `--at`, and `--window-count`.

## Choose time windows deliberately

`window.daily` is a local-time boundary, not a schedule. By default, `metabol`
processes the most recently completed half-open window `[start, end)`.

Use `--at` for reproducible backfills. A date is interpreted as midnight in the
configured timezone, while a timestamp may include an explicit UTC offset:

```bash
metabol --at 2026-09-11
metabol --at 2026-09-11T10:30:00+09:00
```

The selected window contains the specified instant. An instant exactly on a
boundary belongs to the window that starts at that boundary.

Use `--catchup` for a one-shot fetch that extends the newest selected window
through the latest items currently available from each feed:

```bash
metabol --catchup
```

Catchup is not a watch or streaming mode. If `--at` is also specified, `--at`
takes precedence and metabol warns that catchup was ignored.

Use `--window-count` to include earlier consecutive windows. Processing always
runs from oldest to newest:

```bash
metabol --at 2026-09-11 --window-count 3
```

For scheduled operation, let cron, launchd, systemd, or GitHub Actions decide
when to invoke `metabol`; keep the logical collection boundary in
`metabol.yaml`.

## Consume output safely

Standard output is JSON Lines with one successful article per line:

```json
{"requestedUrl":"https://example.com/a","sourceUrl":"https://example.com/a","path":"example.com/a.md","status":"saved"}
```

Diagnostics go to standard error. A feed or article failure does not discard
successful output: `metabol` continues where possible and exits nonzero after
processing. Preserve that exit status in shell pipelines instead of treating
partial output as complete success.

`status` is one of `saved`, `updated`, `unchanged`, or `skipped`. Use
`--update` to re-evaluate existing Markdown and `--assets` to download article
assets.

## Manage this skill

The metabol binary ships this skill through skillsmith. Users explicitly
manage the installed copy:

```bash
metabol skills list
metabol skills install
metabol skills status
metabol skills update
```

The default destination is `~/.agents/skills`. Use `--scope repo` for
`<repo-root>/.agents/skills`, `--prefix` for a custom directory, and
`--dry-run` to preview changes.
