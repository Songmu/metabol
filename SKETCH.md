# thresh

`thresh` は、複数の情報源から一定期間の記事を収集し、`mdhq` を利用して Markdown として保存するための、設定駆動・stateless なオーケストレーターである。

`rssnip` と `mdhq` を組み合わせ、定期的な情報収集を、処理対象期間を再現可能な形で実行することを目的とする。

## コンセプト

基本的なデータフローは以下。

```text
scheduler
    │
    ▼
  thresh
    │
    ├── config
    │
    ▼
 rssnip
    │
    │ article URLs
    ▼
  mdhq
    │
    ▼
Markdown directory
```

`thresh` 自身は crawler、feed parser、HTML-to-Markdown converter、scheduler などを再実装しない。

既存の小さなパーツを組み合わせ、その実行を宣言的な設定からオーケストレーションする。

## 名前

`thresh` は「脱穀する」という意味。農作業では、収穫した穀物から利用可能な grain を取り出す工程を指す。これを、

```text
Web / feeds
    ↓
  thresh
    ↓
usable Markdown
```

になぞらえている。

単に情報を取得するだけではなく、外部の情報源から素材を集め、自分が扱いやすい Markdown という形にして手元へ残す、というニュアンスを持たせる。

## 技術選定

GoのCLIとして実装する。Songmu/tagpr 同様に、カスタムGitHub Actionsも提供する。カスタムGitHub Actionsは内部的には `thresh` バイナリを実行する、composite アクションとする。

- rssnipはライブラリとして利用する
- mdhqは外部コマンド実行する
	- カスタムGitHub Actions提供するうえではsetup時にmdhqをインストールする必要がある
	- mdhqのバージョン管理をどうするかは課題

### 参考
- [GitHub - Songmu/rssnip](https://github.com/Songmu/rssnip)
- [GitHub - Songmu/mdhq](https://github.com/Songmu/mdhq)

## 設計原則

### Small tools, loosely coupled

`thresh` は既存 CLI やソフトウェアの責務を奪わない。

主な責務分担は以下。

* `rssnip`
    * RSS / Atom 等の取得
    * Feed item の解析
    * 時間範囲による絞り込み
    * 記事 URL の抽出
* `mdhq`
    * Web content の取得
    * 本文抽出
    * Markdown 化
    * metadata の付与
    * Markdown の保存
    * 保存先や更新判定の管理
* `thresh`
    * 設定の読み込み
    * 対象 time window の決定
    * source の列挙
    * `rssnip` → `mdhq` の orchestration

保存レイアウトなど、`mdhq` がすでに持っている概念を `thresh` 側で再定義しない。

### Stateless
`thresh` は原則として永続的な状態を持たない。例えば以下のようなモノ。

* `last_run`
* 最終取得日時
* 取得済み URL の DB
* 独自キャッシュ
* 独自の Markdown 管理 DB

代わりに、対象期間を毎回決定論的に計算して再実行する。

同じ logical window を何度でも安全に処理できる、再計算可能・冪等なシステムを目指す。

```text
same config
+ same target window
────────────────────
= same logical job
```

決定論的に再現されるのは、処理対象の window と logical job の定義である。feed の保持期間や記事本文などの外部データは変化し得るため、取得結果や生成される Markdown が同一になることは保証しない。

重複や既存コンテンツの更新判定は、可能な限り `mdhq` の責務とする。

### Scheduler independent

`thresh` 自身は scheduler を持たず、定期実行は外部に任せる。例えば、

* cron
* launchd
* systemd timer
* GitHub Actions
* その他の job scheduler

など。

```text
scheduler
    │
    │ "when to run"
    ▼
  thresh
    │
    │ "what time range to process"
    ▼
  window
```

「いつプロセスを起動するか」と「どの期間の記事を処理するか」を分離することで、 scheduler の遅延や実行時刻のずれが、取得対象期間に影響しないようにする。

## 設定項目とファイル

設定ファイルはYAMLとする。YAMLライブラリは `github.com/goccy/go-yaml` を使う。

設定項目は一部、コマンドラインflagや環境変数で指定できるようにする。主に、GitHub Actionsなどで動的に設定できるようにするため。 `--flagname` を `THRESH_FLAGNAME` 環境変数で指定できるようにする。

設定値は、以下の優先順位で解決する。

```text
CLI flag
    > THRESH_* environment variable
    > thresh.yaml
    > downstream tool specific fallback
    > default value
```

基本設定は以下。

```yaml
root: path/to/dir
assets: false
update: false
timezone: Asia/Tokyo

window:
  daily: "07:00"

sources:
  - https://hnrss.org/newest
  - https://dropbox.tech/feed
```

設定ファイル名は実行ディレクトリ直下の `thresh.yaml` を基本とする。 設定ファイルが見つからない場合はエラーとする。`--config` で変更可能とする。configが明示的に指定されているときには、 `thresh.yaml` を探しには行かない。

### `root`

成果物のMarkdownを配置するディレクトリ。以下の優先順位で解決する。

```text
--root
    > THRESH_ROOT
    > thresh.yaml の root
    > 設定ファイルの配置ディレクトリ
```

解決した値は `mdhq` の `--root` に明示的に渡す。

### `assets`

Markdown配置時に、画像などのダウンロードも行なうかどうかのフラグ。これは、 `mdhq` の `--no-assets` に反転する形で対応する。デフォルトでは `false` とする。つまり、画像のダウンロードを行わない。

これも `--assets` flagで設定可能とする。

### `update`

既存の Markdown がある場合に、記事を再取得して更新判定を行なうかどうかのフラグ。デフォルトでは `false` とし、既存の記事は `mdhq` の通常動作に従って `skipped` とする。

`true` の場合は `mdhq` の `--update` を指定する。これも `--update` flagで設定可能とする。

### `timezone`

window の計算に使用する timezone。

```yaml
timezone: Asia/Tokyo
```

window は単純な UTC duration ではなく、指定 timezone 上の calendar time を基準に計算する。

`--timezone` flagで設定可能とし、指定がない場合はローカルタイムゾーンとする。

### `window`

収集対象となる時間範囲の定義。

初期実装 では以下を基本形とする。

```yaml
window:
  daily: "07:00"
```

これは、 **毎日 07:00 を境界として時間軸を window に分割する** という意味で、 毎日 07:00 に thresh を実行するという意味ではない。

例えば、

```text
09/10 07:00          09/11 07:00          09/12 07:00
     │                     │                     │
─────┼─────────────────────┼─────────────────────┼─────
     [       window        )[       window       )
```

各 window は常に半開区間、

```text
[start, end)
```

として扱う。

境界上の記事が隣接する二つの window に重複して含まれないようにする。

### Timezone transition

daily boundary は、指定 timezone 上の local time から実時間上の instant へ変換し、時系列順の境界として扱う。

DST などの timezone transition によって local time が一意に定まらない場合は、以下の規則で解決する。

* local time が一度だけ存在する場合は、その instant を使用する
* local time が二度存在する場合は、早い方の instant を使用する
* local time が存在しない場合は、timezone transition の差分だけ後ろへずらす
* 複数の boundary が同じ instant に解決された場合は、一つの boundary にまとめる

解決後の boundary を実時間上で単調増加する列として扱い、隣接する boundary から常に `[start, end)` の window を作る。DST の移行を含む window は 23時間や25時間などになり得るが、時間軸上に隙間や重複は作らない。

この解決規則は `thresh` 側で明示的に実装し、Go の `time.Date` が曖昧な local time に対して選択する timezone に依存しないようにする。

### Last complete window

通常実行では、最後に完全に終了した window を対象とする。

`last-complete` を暗黙のデフォルトとし、通常は設定ファイルに記述しない。

例えば現在時刻が、

```text
2026-09-11 19:00 JST
```

で、

```yaml
window:
  daily: "07:00"
```

なら対象は、

```text
[2026-09-10 07:00, 2026-09-11 07:00)
```

となる。

したがって scheduler が同じ日に、

```text
08:00
14:30
23:00
```

のどの時刻に起動しても、同じ logical window を処理する。

これが実行時刻依存を排除するための中心的な仕組みである。

## Window の内部モデル

内部的には、時間軸を boundary によって partition し、その中から processing window を選択すると考える。

```text
boundary generator
        │
        ▼
────┬────────┬────────┬────────┬────
    │        │        │        │
 partition partition partition
                      ▲
                      │
               last complete
                      │
                      ▼
              processing window
```

公開インターフェースでは `partition` ではなく `window` と呼ぶ。

`partition` は時間軸全体を重複なく分割する内部モデルとしては正確だが、利用者が意識するのは、

> 今回どの時間範囲を処理するか

なので `window` の方が自然である。

## Window の将来拡張

初期実装 では `daily` のみでもよい。

```yaml
window:
  daily: "07:00"
```

内部では boundary generator として実装し、将来的に他の粒度を追加できるようにする。

例えば hourly。

```yaml
window:
  hourly: 15
```

これは、

```text
10:15 ───── 11:15 ───── 12:15 ───── 13:15
```

のような window を生成するイメージ。

さらに将来的には、

```yaml
window:
  weekly: "monday 07:00"
```

や、

```yaml
window:
  monthly: "1 07:00"
```

なども考えられる。

より一般的な escape hatch として、

```yaml
window:
  cron: "0 7 * * *"
```

も考えられる。

この cron は scheduler ではなく、 cron expression が生成する tick を window boundary として利用する。

```text
cron ticks

      ↓                     ↓                     ↓
09/10 07:00            09/11 07:00            09/12 07:00
      ├─────────────────────┤
              window
```

ただし、これらを 初期実装 ですべて仕様化する必要はない。

単純なユースケースを単純に記述できることを優先する。

## Sources

初期実装 では URL の文字列を直接列挙できる。

```yaml
sources:
  - https://hnrss.org/newest
  - https://dropbox.tech/feed
```

初期実装では、日時による絞り込みと item の順序に `rssnip` のデフォルト動作を利用する。日時は `date_published` を優先し、利用できない場合は `date_modified` を使う。item の順序は feed の順序を維持する。これらを変更する設定項目は、必要になるまで追加しない。

記事 URL には JSON Feed item の `url` を使用し、`url` がない item は対象外とする。source の設定順、各 feed の item 順に URL を処理し、同じ文字列の URL が実行中に再度現れた場合は `mdhq` に渡さずスキップする。最初に現れた URL を優先し、重複分の結果は stdout の JSONL に出力しない。

redirect や canonical URL によって同一記事へ到達する場合など、取得前には判定できない重複は `mdhq` の URL identity と保存処理に任せる。

ただし将来の拡張を考慮し、object 形式も許容できる設計にする。

```yaml
sources:
  - https://hnrss.org/newest
  - url: https://dropbox.tech/feed
    name: dropbox-tech
```

つまり内部的には、

```yaml
- https://example.com/feed
```

を、

```yaml
- url: https://example.com/feed
```

の shorthand として扱える。

将来的には必要に応じて、

```yaml
sources:
  - url: https://example.com/feed
    type: rss
    name: example
```

のような source 固有設定を追加できる余地を残すが、現在必要のない設定項目を先回りして増やさない。

## CLI

基本的な実行形は、

```console
$ thresh
```

を基本形とする。将来的にサブコマンドを追加する可能性はある。

`--at` または `THRESH_AT` で基準時刻を明示し、設定された window definition に基づいて、その時刻を含む logical window を選択できる。

```console
$ thresh --at 2026-09-11
$ THRESH_AT=2026-09-11T10:30:00+09:00 thresh
```

RFC 3339 timestamp と `YYYY-MM-DD` を受け付ける。日付だけの場合は設定 timezone の `00:00` として扱い、boundary と一致する時刻はその boundary から始まる window に含める。これにより、任意の過去 window を DB や実行履歴なしに再要求できる。

ただし、過去の記事が feed に残っているかどうかは情報源に依存するため、過去 window の取得は best effort とする。

## stdout

stdout については、後続タスクが扱いやすい機械可読形式とする。

取得した記事情報を JSON Lines (`JSONL`) で出力する。

`thresh` は `mdhq` の JSON 出力を解析し、後続処理に必要な項目を抽出して、`thresh` 自身の出力を組み立てる。`mdhq` の出力をそのまま pass-through はしないが、項目名と値の意味は可能な限り `mdhq` のものを踏襲する。

```json
{"requestedUrl":"https://example.com/a","sourceUrl":"https://example.com/a","path":"/data/mdhq/example.com/a.md","status":"saved"}
{"requestedUrl":"https://example.com/b","sourceUrl":"https://example.com/b","path":"/data/mdhq/example.com/b.md","status":"skipped"}
```

初期実装では `requestedUrl`, `sourceUrl`, `path`, `status` を出力する。追加項目が必要になった場合も、`mdhq` の項目名を可能な限り踏襲する。

カスタム GitHub Action はこの JSONL を利用して、後続の step が扱いやすい GitHub Actions output を生成する。具体的な output の構成はカスタム GitHub Action の設計で定める。

取得本文そのものは stdout に流さず、`mdhq` が管理する Markdown file を artifact とする。

つまり、

```text
stdout
  ↓
article metadata / manifest

filesystem
  ↓
Markdown contents
```

という分離を基本とする。

ログは stdout を汚染しないよう stderr に出す。

```text
stdout → machine-readable data
stderr → human-readable logs
```

## エラー処理

複数の source や記事の一部で取得・保存に失敗しても、処理可能な残りの対象は続行する。

`mdhq` の処理が正常に完了した記事は、`saved`, `updated`, `unchanged`, `skipped` のいずれの場合も stdout に JSONL で出力する。feed の取得失敗や記事の保存失敗は、対象の source や URL とともに stderr に出力し、stdout の JSONL には含めない。

すべての対象を処理した後、一件でもエラーがあった場合は non-zero で終了する。設定の読み込みや validation、window の計算など、記事の処理を開始できないエラーについては即時終了する。

## 想定実行フロー

例えば、

```yaml
root: path/to/dir
update: false
timezone: Asia/Tokyo
window:
  daily: "07:00"
sources:
  - https://hnrss.org/newest
  - https://dropbox.tech/feed
```

に対して thresh を実行すると、

```text
1. thresh.yaml を読み込む
2. timezone を決定する
3. `--at` / `THRESH_AT` があればその基準時刻を含む window、なければ現在時刻から last complete window を求める
   [2026-09-10 07:00, 2026-09-11 07:00)
4. 各 source に対して rssnip を実行する
5. window に含まれる記事 URL を得る
6. URL を mdhq に渡す
7. mdhq が Markdown 化・保存・更新判定し root に保存する
8. 記事情報を stdout に JSONL で出力する
```

という流れになる。

## やらないこと

以下を `thresh` の責務にしない。

* RSS parser の再実装
* HTML 本文抽出の再実装
* Markdown converter の再実装
* Markdown 保存レイアウトの独自定義

以下も初期では責務としない。

* scheduler
* crawler state DB
* `last_run` の保存
* 取得済み URL DB
* queue / worker system
* distributed crawling
* Web UI

必要になった場合も、まず既存ツールとの composition で解決できないかを検討する。

## 設計方針

`thresh` の設計では以下を優先する。

1. **Stateless**
	* 実行履歴ではなく logical window から対象を決定する。
2. **Deterministic**
     * 同じ設定と同じ基準時刻から、同じ processing window と logical job を決定できる。
3. **Idempotent**
     * 同じ window を安全に再実行できる。
4. **Scheduler independent**
     * 実行時刻と処理対象期間を分離する。
5. **Composable**
     * `rssnip`, `mdhq`, `jq`, `xargs` など既存 CLI と自然につなげられる。
6. **Machine-readable**
     * stdout は後続処理が利用できる structured data とする。
7. **Human-friendly configuration**
     * 一般的な用途は簡潔に書ける。

   ```yaml
   window:
     daily: "07:00"
   ```

   のような設定を優先し、最初から cron DSL などを要求しない。

8. **Minimal abstraction**
	* `rssnip` や `mdhq` がすでに持つ概念を `thresh` 側に重複して持たない。
9. **Extensible, not speculative**
	* `sources` の object 化や window boundary generator のように拡張可能な内部構造は持つが、まだ必要のない機能を公開仕様として先行実装しない。

## 初期実装 のスコープ

```yaml
root: path/to/dir
update: false
timezone: Asia/Tokyo

window:
  daily: "07:00"

sources:
  - https://hnrss.org/newest
  - https://dropbox.tech/feed
```

を読み込み、

```console
$ thresh
$ thresh --at 2026-09-11
```

により、

```text
last complete daily window
or explicitly selected daily window
        ↓
configured feeds
        ↓
rssnip
        ↓
article URLs
        ↓
mdhq
        ↓
Markdown files
```

を生成できること。

この小さな核をまず成立させ、その後必要に応じて、

* source object
* hourly / weekly / monthly window
* cron boundary

などを追加していく。

## 要約

`thresh` は、

> **scheduler から独立した固定 time window を設定から決定し、デフォルトの last-complete window または `--at` / `THRESH_AT` で明示した window について複数 source から記事を収集し、`rssnip` と `mdhq` を組み合わせて Markdown として蓄積する stateless なオーケストレーター**

である。

時間モデルの核は、

```text
window.daily = 07:00
+ last-complete by default
+ explicit selection by --at / THRESH_AT
+ [start, end)
```

の4点。

システム設計の核は、

```text
stateless
+ deterministic
+ idempotent
+ composable
```

の4点とする。
