# envfacts

ローカル環境に導入されたCLIと、PATHから見つかる実行ファイルを整理して報告する、Windows向けの非対話型コマンドです。製品仕様の正本は [要件定義書](docs/requirements.md) です。

現在は開発環境のセットアップが完了した段階です。日本語の `--help` と `--version` を実行できます。情報収集の `scan` と記録比較の `diff` は未実装です。実装順序と完了条件は [開発計画](docs/development-plan.md) を参照してください。

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
```

この段階では、引数なしでも使い方を表示します。未対応の引数は標準エラーに説明を出し、終了コード `64` を返します。標準出力への書き込みに失敗した場合は `2` を返します。`scan`・`diff` とJSON出力の契約は、後続の実装で追加します。

終了コードを確認する場合はビルドした実行ファイルを使ってください。`go run` は起動したプログラムの終了コードをそのまま返すとは限りません。

## 検証

```powershell
pwsh -NoProfile -File ./scripts/check.ps1
```

検証手順の正本は [scripts/check.ps1](scripts/check.ps1) です。`gofmt -l` による整形確認、`go vet ./...`、`go test ./...`、上記のビルド、生成した実行ファイルの `--version` を順に実行し、失敗した時点で停止します。起動確認は `--help` でも行ってください。

整形が必要なファイルには `gofmt -w <ファイル>` を実行します。Goのビルド・テストでは通常の開発用キャッシュと一時ファイルを使用します。これらは、製品の調査処理が環境を書き換えてよいことを意味しません。

## 構成

```text
cmd/envfacts/          実行ファイルの入口と終了コードの反映
internal/cli/         引数、出力先、版番号とそのテスト
docs/requirements.md  移動前の内容を保持した要件定義書
docs/development-plan.md  実装順序、受入確認、未決定事項
scripts/check.ps1     開発時の検証手順
AGENTS.md             変更時の制約と版番号の更新規則
go.mod                モジュール名とGoの最低版
bin/                  ローカルの実行ファイル。Git管理対象外
```

テストは対象のGoファイルと同じ場所に `_test.go` として置きます。今後の模擬の導入情報やshimは、利用するパッケージの `testdata/` に追加します。実環境のScoop導入先を検証用に変更しません。

## 版番号と公開

配布単位は `envfacts` の実行ファイルです。版番号の正本は [internal/cli/version.go](internal/cli/version.go) の `Version` で、`--version` が直接参照します。更新条件と確認方法は [AGENTS.md](AGENTS.md) を参照してください。

配布経路、公開リポジトリ、ライセンスは未決定です。公開先や権利者を推測した設定・表記は追加していません。

## 参照した公式資料

- [Goのモジュール構成](https://go.dev/doc/modules/layout): `cmd/` と `internal/` の配置
- [Goのモジュール仕様](https://go.dev/ref/mod): ローカル用のモジュール名と `go` 指定
- [Goのリリース履歴](https://go.dev/doc/devel/release): 開発環境にあるGoの版の確認
- [Scoopのコマンド](https://github.com/ScoopInstaller/Scoop/wiki/Commands): 要件で参照されている公開資料。導入情報を読む方式の確定は後続の実装で行います。
