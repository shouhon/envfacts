# 開発・検証手順

## 開発環境

- Windows 11、PowerShell 7以降
- Goの最低版は [go.mod](../go.mod) を正本とします。
- 標準ライブラリだけでビルド・テストします。外部のGoモジュールは使用しません。

## 検証

リポジトリのルートで実行します。

```powershell
pwsh -NoProfile -File ./scripts/check.ps1
```

検証手順の正本は [scripts/check.ps1](../scripts/check.ps1) です。`gofmt -l`、`go vet`、`go test ./...`、ビルド、生成した実行ファイルの `--version` と正本の一致確認、[受入手順](../scripts/acceptance.ps1) を順に実行し、失敗した時点で停止します。整形と静的検査には `testdata/execution-probe/` の受入確認専用コードも含めます。

受入手順は `bin/envfacts.exe` を模擬の導入先・junction・PATHで起動し、収集から保存・比較までを確認します。各操作の前後でファイル・更新日時・属性・リンク先を照合し、対象CLIの非実行、余分な模擬値の非出力、標準出力・標準エラー・終了コードを検査します。実際のScoop導入先や環境設定は変更しません。

起動されたら印を残す模擬実行ファイルをGoで作り、検出できることを先に確認してからshim等に配置します。模擬環境と記録は `.cache/acceptance-<一意な識別子>/` に作成し、その実行で作ったディレクトリだけを終了時に除去します。

受入確認だけを再実行する場合は次を使えます。この手順単独でも模擬実行ファイルのビルドにGoを使用します。

```powershell
pwsh -NoProfile -File ./scripts/acceptance.ps1
```

整形が必要なファイルには `gofmt -w <ファイル>` を実行します。Goのビルド・テストが作るキャッシュや一時ファイルは、製品の調査処理が環境を書き換えることとは区別します。

## 実行ファイルの生成

```powershell
go build -trimpath -buildvcs=false -o bin/envfacts.exe ./cmd/envfacts
.\bin\envfacts.exe --help
.\bin\envfacts.exe --version
```

`-trimpath` でビルド元の絶対パスを除き、`-buildvcs=false` で作業ツリーの状態による差を除きます。配布対象のコミットはGitHub Releaseのタグで識別します。製品の終了コードを確認する場合は実行ファイルを使い、`go run` の終了コードを証拠にしないでください。

## 構成

| 場所 | 役割 |
| --- | --- |
| `cmd/envfacts/` | OSの入出力と終了コードの接続 |
| `internal/cli/` | 引数、出力先、版番号 |
| `internal/scan/` | Scoopのローカル情報、PATH探索、JSONの型 |
| `internal/diff/` | 記録の検査、共通範囲と変更の比較 |
| `internal/scan/testdata/` | 機密情報を含まない模擬の導入情報 |
| `testdata/execution-probe/` | 誤起動を検出する模擬CLI。製品には含めない |
| `scripts/` | 検証、受入確認、配布物の作成 |
| `bucket/envfacts.json` | Scoop用の配布定義。配布物の作成手順から生成 |
| `bin/`、`.cache/` | 生成物と一時ファイル。Git管理対象外 |

テストは対象のGoファイルと同じ場所の `_test.go` に置きます。模擬の導入先・shim・PATHは一時ディレクトリへ組み立て、実環境のScoop導入先を走査・変更しません。

版番号の正本は [internal/cli/version.go](../internal/cli/version.go) の `Version` です。更新条件は [AGENTS.md](../AGENTS.md)、配布手順は [配布・公開手順](releasing.md) を参照してください。
