# 配布・公開手順

## 配布先と形式

- ソースコード: [shouhon/envfacts](https://github.com/shouhon/envfacts)
- 実行ファイル: [GitHub Releases](https://github.com/shouhon/envfacts/releases)
- 対象: Windows 11、AMD64（x64）。実行ファイルは未署名です。
- ライセンス: [MIT](../LICENSE)
- 導入方法: Scoop、ReleaseのZIP展開、Goによる `go install`

Scoopの配布定義は同じリポジトリの `bucket/envfacts.json` に置きます。Scoop公式の配布元への登録は行いません。この構成は公式の [bucketの説明](https://github.com/ScoopInstaller/Scoop/wiki/Buckets) と [配布定義の仕様](https://github.com/ScoopInstaller/Scoop/wiki/App-Manifests) に従います。

## 手元での準備

版番号の正本は `internal/cli/version.go` の `Version` だけです。[AGENTS.md](../AGENTS.md) の規則に従って更新し、[変更履歴](../CHANGELOG.md) に同じ版の変更を記載します。JSONの `schema_version` は別に管理します。

Windows AMD64、PowerShell 7以降、`go.mod` の最低版を満たすGoで実行します。

```powershell
pwsh -NoProfile -File ./scripts/package-release.ps1
```

この手順はGoの自動取得を無効にして `scripts/check.ps1` を実行し、すべて成功した後、`bin/releases/<版番号>/` に次を作ります。既存の保存先は上書きしません。

| ファイル | 内容 |
| --- | --- |
| `envfacts-<版番号>-windows-amd64.zip` | 検証済みの `envfacts.exe`、`LICENSE`、`README.md` |
| `envfacts.json` | ZIPのURLとSHA-256を持つScoop用配布定義 |
| `checksums.txt` | ZIPと配布定義のSHA-256 |
| `release-notes.md` | 変更履歴の該当版から取り出した公開用の説明 |

ZIPの内容は上記3ファイルに限定し、実環境の記録・キャッシュ・模擬CLIを含めません。ビルドには `-trimpath -buildvcs=false` を使い、開発元の絶対パスや作業ツリーの状態を除きます。対象コミットはReleaseのタグで識別します。

版番号は正本から取り出し、生成した配布定義を反映します。

```powershell
$version = [regex]::Match((Get-Content ./internal/cli/version.go -Raw), 'const Version = "([0-9]+\.[0-9]+\.[0-9]+)"').Groups[1].Value
$releaseDirectory = Join-Path $PWD "bin/releases/$version"
New-Item -ItemType Directory -Path ./bucket -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $releaseDirectory 'envfacts.json') -Destination ./bucket/envfacts.json
```

ZIPを一時ディレクトリへ展開し、ファイル一覧、`--version`、`--help` と検証済み実行ファイルとのハッシュ一致を確認します。配布定義の版・対象CPU・URL・ハッシュ、`checksums.txt` との一致も確認してください。同じソース・Go・ビルド設定で別の場所へビルドしてハッシュを比較すると、実行ファイルの再現性を確認できます。未確認の場合は再現可能と表明しません。

公開前にREADME・LICENSE・配布手順・変更履歴と配布定義を含めて差分を確認し、秘密情報と生成物がGitの対象に入っていないことを確認します。必須検証を通した変更をコミットし、設定済みの追跡先へプッシュします。生成物はコミットしません。

## GitHub Releaseの公開

公開の明示認可がある場合だけ実行します。`gh` の認証とGitの接続が利用できることを確認してください。認証情報をコマンドやログへ貼り付けず、必要なら利用者のPowerShellで `gh auth login --hostname github.com --web` を実行します。

```powershell
git push
if ($LASTEXITCODE -ne 0) { throw 'プッシュに失敗しました。公開を停止します。' }
$commit = git rev-parse HEAD
gh release create "v$version" `
  (Join-Path $releaseDirectory "envfacts-$version-windows-amd64.zip") `
  (Join-Path $releaseDirectory 'checksums.txt') `
  (Join-Path $releaseDirectory 'envfacts.json') `
  --repo shouhon/envfacts --target $commit --title "envfacts v$version" `
  --notes-file (Join-Path $releaseDirectory 'release-notes.md') --latest
```

`gh release create` はタグがない場合に `--target` のコミットからタグを作ります。[GitHub CLI公式手順](https://cli.github.com/manual/gh_release_create) を参照してください。公開済みの同じ版のタグや配布物を差し替えず、修正が必要なら新しい版を作ります。途中で失敗した場合は `gh release view "v$version" --repo shouhon/envfacts` でタグ・下書き・添付ファイルの状態を確認してから不足分だけを処理します。添付ファイルの強制上書きはしません。

公開後は次を確認します。

1. Releaseが下書きではなく公開済みで、タグの対象が検証したコミットと一致する。
2. 添付されたZIPと配布定義を新しい一時ディレクトリへダウンロードし、手元のSHA-256と一致する。
3. READMEのReleaseリンクと、配布定義にあるZIPのURLへアクセスできる。
4. 利用者の許可した検証用Scoop環境で導入・`--version`・更新・削除を確認する。既存のScoop環境を無断で書き換えない。

Scoopを使った実際の導入と、配布定義・ZIPを手元で確認したことは区別して報告します。

## 旧版へ戻す方法

以前の版はGitHub Releasesの各タグから取得できます。Scoopでは導入済みの旧版が残っていれば `scoop reset envfacts@<以前の版番号>` で切り替え、`envfacts --version` を確認します。旧版が残っていない場合は以前のReleaseのZIPを取得し、同じReleaseのSHA-256を照合して別フォルダーに展開し、その実行ファイルを直接使います。環境の記録を削除する必要はありません。

今回が初回のReleaseであるため、公開済みの旧版はありません。初回導入を取り消す場合は `scoop uninstall envfacts`、ZIP導入の場合は利用者が展開したフォルダーと追加したPATHを取り消します。
