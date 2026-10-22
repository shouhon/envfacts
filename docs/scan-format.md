# 収集方式と記録形式

要件の正本は [要件定義書](requirements.md) です。この文書は `scan` の対応形式と `schema_version: 1` の取り決めを記載します。保存済みの記録を比較する `diff` の対応範囲は [記録比較](diff-format.md) を参照してください。

## 読み取るローカル情報

2026年10月22日、Scoopの [core.ps1](https://github.com/ScoopInstaller/Scoop/blob/master/lib/core.ps1)、[manifest.ps1](https://github.com/ScoopInstaller/Scoop/blob/master/lib/manifest.ps1)、[install.ps1](https://github.com/ScoopInstaller/Scoop/blob/master/lib/install.ps1) を確認しました。以下の読み取りだけを行い、スクリプトや対象CLIを起動しません。

- 利用者単位の導入先は `SCOOP`、設定の `root_path`、`USERPROFILE\scoop` の順に選びます。共通の導入先は `SCOOP_GLOBAL`、設定の `global_path`、`ProgramData\scoop` の順に選びます。
- 設定は `XDG_CONFIG_HOME\scoop\config.json`、未指定なら `USERPROFILE\.config\scoop\config.json` を読みます。分かっている利用者単位の導入先に `config.json` があれば、その設定を優先します。Scoop本体の配置から未知の導入先を逆算する探索はしません。
- `<導入先>\apps\<パッケージ>\current` の保存済み `scoop-manifest.json` を読み、存在しない場合だけ旧名の `manifest.json` を読みます。破損や権限エラーの場合に旧名へ逃げません。bucketは読みません。
- 通常の導入版は保存済みマニフェストの `version`、`nightly` は `current` の物理的な参照先の `nightly-…` ディレクトリを根拠にします。`current` がない場合に版ディレクトリをソートして使用中の版を推測しません。
- `bin` の文字列、文字列の配列、配列内の `[対象, 別名, 引数（省略可）]` に対応します。引数は記録しません。アーキテクチャ別の定義は、保存済み `scoop-install.json`（存在しない場合は旧名の `install.json`）の `architecture` で選びます。
- `bin` のあるパッケージは `kind: cli`、`bin` がなくショートカットがあるものは `gui`、どちらもないものは `no_commands`、判断できないものは `unknown` です。これは保存済み定義の分類であり、実行ファイルすべての用途判定ではありません。

## PATHとshim

[PowerShellのコマンド優先順位](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_command_precedence?view=powershell-7.5) と公式実装の [CommandDiscovery.cs](https://github.com/PowerShell/PowerShell/blob/master/src/System.Management.Automation/engine/CommandDiscovery.cs)、[CommandPathSearch.cs](https://github.com/PowerShell/PowerShell/blob/master/src/System.Management.Automation/engine/CommandPathSearch.cs) に基づき、外部ファイルの候補を次の順に調べます。

1. プロセスのPATHをセミコロンで区切り、空要素を除きます。相対パスは作業ディレクトリからの絶対パスにし、`~`・`~\` は利用者フォルダーに解決します。同じパスは大文字小文字を区別せず1回だけ調べます。現在のディレクトリはPATHで明示された場合だけ対象です。
2. 各ディレクトリ内で `.ps1` を先に、続いて現在の `PATHEXT` の順に候補を列挙します。拡張子を明示した名前はその拡張子だけを調べます。名前の大文字小文字を区別しません。
3. 候補ごとに物理的な参照先を確認します。Scoopの `shims` 内の `.exe` は、対応する `.shim` の `path = "絶対パス"` を読みます。`args` は出力せず、対象を起動しません。
4. 候補の実体と、そのコマンドを公開する保存済み `bin` の対象が一致する場合だけ関連付けます。優先する候補に関連するパッケージが1件に確定した場合だけ、その導入版と根拠を反映します。後順位の候補の版を流用しません。

スクリプト型shim（`.cmd`・`.ps1`など）、相対参照など未対応の `.shim`、`env_add_path` による公開コマンドの列挙、`no_junction` による使用中の版の選択には未対応です。理由と情報源を `issues` に記録して `incomplete` にします。`env_add_path` のパッケージでも、名前を指定したPATH探索の結果は返します。Scoop以外のスクリプトは存在と物理パスだけを確認し、本文を解釈しません。

## JSONの識別と順序

| 項目 | 取り決め |
| --- | --- |
| `schema_version` / `tool` / `command` | `1` / `envfacts` / `scan`。引数エラーでは認識できた `scan`・`diff` を保持し、コマンド自体が不明なら空文字 |
| `captured_at` | UTCのRFC 3339形式。比較時の変更判定から除外する対象 |
| `coverage.scoop_roots` | `scope`（`user` / `global`）、導入先、選択根拠、`complete` / `absent` / `incomplete` |
| `coverage.command_selection` / `requested_commands` | `scoop` / `explicit` と、明示した名前の集合。小文字化・重複除去・ソート |
| `coverage.search_paths` / `extensions` | 実際の探索先と状態、探索する拡張子。ディレクトリの `priority` は元のPATHの位置で、1から始まる |
| `coverage.excluded` | 実行・認証・通信、PowerShellの上書き、WSL等、他の導入台帳の対象外範囲 |
| `packages[].id` | `scoop:<scope>:<小文字化した絶対パッケージディレクトリ>`。同名でも導入先や利用者単位・共通が異なれば別の識別子 |
| `packages[].install_path` / `sources` | `current` の物理的な参照先と、実際に読み取ったマニフェスト等の場所 |
| `packages[].version` / `version_source` / `version_reason` | 保存済みの導入版と根拠。不明なら `null` / `null` / 日本語の理由 |
| `commands[].name` | 小文字化した探索名。拡張子を指定した場合はその拡張子も含む |
| `commands[].status` | 読めた範囲に候補があれば `resolved`、なければ `not_found`。未読範囲は全体の状態・coverage・issuesで併せて判断する |
| `preferred_path` / `actual_path` | 最優先の候補と確認できた実体。候補がなければ両方 `null`。shimが未対応なら候補を残し実体を `null` にする |
| `commands[].package_ids` | 探索名を公開する導入パッケージ。PATHから未検出でも保持する |
| `candidates[].package_ids` | 実体を照合できた導入パッケージだけ。未確認なら空配列 |
| `candidates[].priority` / `path_priority` / `extension_priority` | 全候補内・PATH内・拡張子内の優先順位を数値で明示。配列の並びだけを意味の根拠にしない |
| `commands[].version` / `version_source` / `version_reason` | 最優先候補に確かな導入情報がなければ `null` / `null` / 日本語の理由。版不明同士を同じ版と判断しない |
| `invocation_status` | 記録全体と各コマンドで常に `not_tested` |
| `issues` | `code`、日本語の `message`、情報源の `source`。生の例外文・ファイル本文・URL・引数・無関係な設定は含めない |

パッケージは識別子順、コマンドと関連パッケージの集合は名前・識別子順、候補は明示した優先順位順に出力します。空の一覧は `[]` とし、日時以外は同じ情報から安定した記録を作ります。M3では識別子と優先順位に基づき比較し、JSON配列の並べ替え自体は差分にしません。

## 不完全な調査と保存

存在しない導入先・PATHのディレクトリは `absent` とし、読取不能と区別します。読取不能・破損・未対応形式は `issues` に残し、読める範囲の収集を続けます。明示した名前の不足だけなら `missing`、不足と不完全が併存すれば `incomplete` を優先します。外部CLIの導入版が分からないことだけでは収集失敗にしません。

`--output` は排他的な新規作成で、既存の記録を上書きしません。保存に失敗しても収集した事実を標準出力に残し、`save_failed` と終了コード `2` を返します。JSONの表示・保存に対象ファイルの本文や余分な設定は含めません。
