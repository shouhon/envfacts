# envfacts

ローカル環境に導入されたCLIと、PATHから見つかる実行ファイルを整理して報告する、Windows向けの非対話型コマンドです。製品仕様の正本は [要件定義書](docs/requirements.md) です。

`scan` でScoopの保存済み導入情報とPATHの候補を調べ、日本語表示またはJSON記録を出力できます。`diff` は保存済みの記録だけを比較します。対応するローカル情報の形式と未対応範囲は [収集方式と記録形式](docs/scan-format.md)、比較の取り決めは [記録比較](docs/diff-format.md)、実装順序と完了条件は [開発計画](docs/development-plan.md) を参照してください。

## 開発環境

- Windows 11、PowerShell 7以降
- Go 1.27.3以降。必要な最低版は `go.mod` の `go` 指定で管理します。
- 外部のGoモジュール、追加の検査ツールは使用しません。標準ライブラリだけでビルド・テストできます。

公開先が未定のため、モジュール名はローカル用の `envfacts` としています。公開先を決めて `go install` による導入を提供するときに、`go.mod` と内部のインポートを実際のリポジトリのパスへ変更します。

## 実行

プロジェクトのルートで実行します。

```powershell
go run ./cmd/envfacts --help
go run ./cmd/envfacts --version
```

実行ファイルだけを作る場合は次を使います。

```powershell
go build -trimpath -o bin/envfacts.exe ./cmd/envfacts
.\bin\envfacts.exe --help
.\bin\envfacts.exe --version
.\bin\envfacts.exe scan
.\bin\envfacts.exe scan --command git --command python --json
.\bin\envfacts.exe scan --json --output .\environment.json
.\bin\envfacts.exe diff --before .\before.json --after .\after.json
.\bin\envfacts.exe diff --before .\before.json --after .\after.json --json
```

引数なしでは使い方を表示します。`scan` の名前を省略すると、Scoopの導入済みマニフェストの `bin` に公開された名前を探索します。`--command` を指定すると、その名前だけを探索します。Scoop以外のCLIも指定できますが、導入版は推測しません。対象CLIやScoopは実行せず、起動・認証・ネットワーク接続は常に未確認です。

`--json` は標準出力にJSONオブジェクト1件を表示し、診断は標準エラーへ出します。`--output` は同じJSON形式を新規ファイルへ保存します。既存ファイルの上書きや親ディレクトリの作成は行いません。保存先を指定しなければファイルを作成しません。実環境の記録にはローカルのパスが含まれるため、Gitへ追加しないでください。

`diff` は記録日時や配列の並び順を除き、追加・削除・導入版・実体・優先順などの変更を報告します。調査範囲が異なる場合は共通範囲だけを比較し、版不明や未読範囲があれば不完全とします。再調査や入力ファイルの変更は行いません。

終了コードは、調査完了・比較完了で差分なしが `0`、明示した名前がPATHから見つからない場合・差分ありが `1`、調査・保存・比較・出力が不完全な場合が `2`、引数の誤りが `64` です。不足・差分と不完全が同時に起きた場合は `2` を優先します。引数が誤っていても `--json` を認識できればJSONで説明します。

Scoopのスクリプト型shim、`env_add_path` からのコマンド列挙、`no_junction` 設定などは未対応です。未対応の情報がある場合は、読めた事実と `issues` を返し、調査成功として扱いません。対応済み形式の初版受入確認はWindows AMD64の模擬環境で完了しました。実環境全体・Windows ARM64・配布経路は未検証です。

終了コードを確認する場合はビルドした実行ファイルを使ってください。`go run` は起動したプログラムの終了コードをそのまま返すとは限りません。

## 検証

```powershell
pwsh -NoProfile -File ./scripts/check.ps1
```

検証手順の正本は [scripts/check.ps1](scripts/check.ps1) です。`gofmt -l` による整形確認、`go vet`、`go test ./...`、上記のビルド、生成した実行ファイルの `--version` と正本の一致確認、[受入手順](scripts/acceptance.ps1) を順に実行し、失敗した時点で停止します。整形と静的検査には `testdata/execution-probe/` の受入確認専用コードも含めます。

受入手順は `bin/envfacts.exe` を模擬の導入先・junction・PATHで起動し、収集から保存・比較までを確認します。各操作の前後でファイル・更新日時・属性・リンク先を照合し、対象CLIの非実行、余分な模擬値の非出力、標準出力・標準エラー・終了コードも検査します。起動されたら印を残す模擬実行ファイルをGoで作り、検出できることを先に確認してからshim等に配置します。模擬環境と記録は `.cache/acceptance-<一意な識別子>/` に作成し、その実行で作ったディレクトリだけを終了時に除去します。実際のScoop導入先や環境設定は変更しません。ビルド済みの実行ファイルの受入確認だけを繰り返す場合は `pwsh -NoProfile -File ./scripts/acceptance.ps1` を使えます。この手順単独でも模擬実行ファイルのビルドにGoを使用します。

整形が必要なファイルには `gofmt -w <ファイル>` を実行します。Goのビルド・テストでは通常の開発用キャッシュと一時ファイルを使用します。これらは、製品の調査処理が環境を書き換えてよいことを意味しません。

## 構成

```text
cmd/envfacts/          実行ファイルの入口と終了コードの反映
internal/cli/         引数、出力先、版番号とそのテスト
internal/scan/        Scoopのローカル情報、PATH探索、JSON記録の型とテスト
internal/diff/        保存済み記録の検査、共通範囲と変更の比較、そのテスト
docs/requirements.md  移動前の内容を保持した要件定義書
docs/development-plan.md  実装順序、受入確認、未決定事項
docs/scan-format.md   対応する収集方式、JSONの識別・優先順・未確認の扱い
docs/diff-format.md   記録比較の入力検査、共通範囲、変更と不完全の扱い
scripts/check.ps1     開発時の検証手順
scripts/acceptance.ps1  実行ファイルで要件7章を確認する模擬環境の受入手順
testdata/execution-probe/  誤起動の検出に使う模擬CLIのソース。製品には含めない
AGENTS.md             変更時の制約と版番号の更新規則
go.mod                モジュール名とGoの最低版
bin/                  ローカルの実行ファイル。Git管理対象外
```

テストは対象のGoファイルと同じ場所に `_test.go` として置きます。模擬の導入情報は `internal/scan/testdata/` に置き、一時ディレクトリに導入先・shim・PATHを組み立てます。テストから実環境のScoop導入先を走査・変更しません。

## 版番号と公開

配布単位は `envfacts` の実行ファイルです。版番号の正本は [internal/cli/version.go](internal/cli/version.go) の `Version` で、`--version` が直接参照します。更新条件と確認方法は [AGENTS.md](AGENTS.md) を参照してください。

正式な配布経路とライセンスは未決定です。設定済みのGit追跡先と製品の配布方法は区別して扱います。権利者を推測した設定・表記は追加していません。

## 参照した公式資料

- [Goのモジュール構成](https://go.dev/doc/modules/layout): `cmd/` と `internal/` の配置
- [Goのモジュール仕様](https://go.dev/ref/mod): ローカル用のモジュール名と `go` 指定
- [Goのリリース履歴](https://go.dev/doc/devel/release): 開発環境にあるGoの版の確認
- [Scoopのコマンド](https://github.com/ScoopInstaller/Scoop/wiki/Commands): 要件で参照されている公開資料。読み取る情報源と対応形式の根拠は [収集方式と記録形式](docs/scan-format.md) に記載しています。
