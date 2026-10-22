# envfacts

Windowsに導入されたCLIと、PATHから見つかる実行ファイルを整理する非対話型コマンドです。Scoopの導入情報と名前解決の結果を、日本語またはJSONで出力します。環境の記録をLLMへ渡したり、CLIの追加・更新前後を比較したりするときに使えます。

対象CLIを起動せず、保存済みの導入情報とファイルの存在から確認できる事実を報告します。導入済み・PATHから発見済み・起動確認済みを区別し、分からない情報は未確認として扱います。

## 主な機能

- Scoopの利用者単位・全利用者共通の導入情報から、パッケージ名、導入版、公開コマンドを収集
- PATH上の同名コマンドと優先順、解釈できるshimの参照先を確認
- 日本語表示、JSON出力、既存ファイルを上書きしない記録の保存
- 保存済みの記録を比較し、追加・削除・導入版・実体・優先順の変更を報告

## 動作環境

- Windows 11、AMD64（x64）
- PowerShellでの利用を想定
- 配布済みの実行ファイルを使う場合、Goは不要

Windows ARM64、WSL、コンテナーは動作確認の対象外です。

## インストール

### Scoop

[Scoop](https://scoop.sh/)とGitを導入済みのPowerShellで実行します。このリポジトリをScoopの追加の配布元（bucket）として登録します。

```powershell
scoop bucket add envfacts https://github.com/shouhon/envfacts
scoop install envfacts/envfacts
envfacts --version
```

更新と削除は次のコマンドで行います。

```powershell
scoop update
scoop update envfacts
scoop uninstall envfacts
```

### ZIPをダウンロード

[GitHub Releases](https://github.com/shouhon/envfacts/releases/latest)から `envfacts-<版番号>-windows-amd64.zip` と `checksums.txt` をダウンロードします。

```powershell
Get-FileHash .\envfacts-<版番号>-windows-amd64.zip -Algorithm SHA256
```

結果が `checksums.txt` の値と一致することを確認し、ZIPを任意のフォルダーへ展開してください。展開先で `./envfacts.exe --help` を実行できます。どこからでも呼び出す場合は、展開先を利用者のPATHに追加します。

### Goでインストール

Goの必要な最低版は [go.mod](go.mod) を参照してください。

```powershell
go install github.com/shouhon/envfacts/cmd/envfacts@latest
```

生成先は `GOBIN`、未設定なら `GOPATH` 配下の `bin` です。生成先をPATHに追加して使います。

## 使い方

Scoopの導入情報にあるコマンドを調べます。

```powershell
envfacts scan
```

名前を明示すると、そのコマンドだけをPATHから探します。Scoop以外で導入したコマンドも指定できますが、導入版は推測しません。

```powershell
envfacts scan --command git --command python --json
```

環境の記録を保存し、変更後の記録と比較します。各保存先は、まだ存在しないファイルを指定してください。

```powershell
envfacts scan --json --output .\before.json
# CLIの追加・更新後に実行
envfacts scan --json --output .\after.json
envfacts diff --before .\before.json --after .\after.json
envfacts diff --before .\before.json --after .\after.json --json
```

```powershell
envfacts --help
envfacts --version
```

`--json` は標準出力にJSONオブジェクト1件を出し、診断は標準エラーへ出します。`--output` は同じJSON形式を新規ファイルへ保存し、既存ファイルの上書きや親ディレクトリの作成は行いません。保存先を指定しなければファイルを作成しません。

`diff` は保存済みの記録だけを読み、再調査しません。日時や配列の並び順だけの違いは除外します。調査範囲が異なる場合は共通範囲だけを比較し、版不明や未読範囲がある場合は不完全として報告します。

### 終了コード

| コード | 意味 |
| --- | --- |
| `0` | 調査完了、または比較完了で差分なし |
| `1` | 明示したコマンドが見つからない、または差分あり |
| `2` | 調査・保存・比較・出力が不完全、記録が不正、読取不能など |
| `64` | 引数の誤り |

不足・差分と不完全が同時に起きた場合は `2` を優先します。引数が誤っていても、`--json` を認識できればJSONで説明します。

## 調査範囲と制限

- 対象CLIの `--version`、Scoop・npm・uvの情報取得コマンドは実行しません。起動・認証・ネットワーク接続の可否は常に未確認です。
- Scoop以外の管理方式の導入台帳、プロジェクトの仮想環境、PowerShellの関数・エイリアス・プロファイルは自動調査しません。
- Scoopのスクリプト型shim、`env_add_path` からのコマンド列挙、`no_junction` 設定などは未対応です。読めた事実と `issues` を返し、調査成功として扱いません。
- 確認済みの範囲は、対応する形式を使ったWindows AMD64の模擬環境です。すべての実環境や導入方式での動作を保証するものではありません。

記録にはローカルのパスが含まれます。共有前に内容を確認し、実環境の記録をGitへ追加しないでください。ファイル本文、環境変数の全件一覧、shimの引数、認証情報は記録しません。

## 文書

- [要件定義書](docs/requirements.md)
- [収集方式とJSON形式](docs/scan-format.md)
- [記録比較の仕様](docs/diff-format.md)
- [開発計画と受入確認](docs/development-plan.md)
- [開発・検証手順](docs/development.md)
- [配布・公開手順](docs/releasing.md)
- [変更履歴](CHANGELOG.md)

## 開発

Windows 11、PowerShell 7以降、[go.mod](go.mod)で指定された版以降のGoを使用します。外部のGoモジュールや追加の検査ツールは不要です。

```powershell
git clone https://github.com/shouhon/envfacts.git
cd envfacts
pwsh -NoProfile -File ./scripts/check.ps1
.\bin\envfacts.exe --help
```

検証には整形確認、静的検査、テスト、ビルド、版番号の照合、模擬環境での受入確認が含まれます。詳細は [開発・検証手順](docs/development.md) を参照してください。

不具合や改善案は [Issues](https://github.com/shouhon/envfacts/issues) へお寄せください。再現手順、版番号、期待した結果と実際の結果を記載し、実環境のパスや認証情報をそのまま掲載しないでください。

## ライセンス

[MIT License](LICENSE)
