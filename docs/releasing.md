# 配布・公開手順

## 配布先と形式

- ソースコード: [shouhon/envfacts](https://github.com/shouhon/envfacts)
- 実行ファイル: [GitHub Releases](https://github.com/shouhon/envfacts/releases)
- 対象: Windows 11、AMD64（x64）。実行ファイルは未署名です。
- ライセンス: [MIT](../LICENSE)
- 導入方法: Scoop、ReleaseのZIP展開、Goによる `go install`

Scoopの配布定義は同じリポジトリの `bucket/envfacts.json` に置きます。Scoop公式の配布元への登録は行いません。この構成は公式の [bucketの説明](https://github.com/ScoopInstaller/Scoop/wiki/Buckets) と [配布定義の仕様](https://github.com/ScoopInstaller/Scoop/wiki/App-Manifests) に従います。

## mainへのpushによる自動公開

[検査とRelease公開](../.github/workflows/ci-release.yml) は、mainへのpushとmainを対象とするプルリクエストで動きます。版番号の正本は `internal/cli/version.go` の `Version` だけです。

| 変更 | 実行する処理 |
| --- | --- |
| mainへのpushで版番号が変わらない | 必須検査だけを実行 |
| mainへのpushで版番号が上がる | 必須検査、配布物の作成、Release公開、Scoop配布定義の同期 |
| プルリクエスト | 必須検査。版番号が上がる場合は配布物も作成するが、公開・同期はしない |
| 版番号が下がる、有効な番号が読めない、比較元の履歴が取れない | 失敗として停止 |

版番号はpush前後のコミット間で比較します。複数コミットをまとめてpushした場合も、末尾の1コミットだけでは判定しません。版番号を含むファイルのコメントだけを変えても公開しません。新しいmainの初回pushでは比較元がないため、現在の版を公開対象にします。

公開する変更では、[AGENTS.md](../AGENTS.md) に従って正本の版番号を上げ、[変更履歴](../CHANGELOG.md) に `## <版番号>（YYYY-MM-DD）` の見出しと変更内容を追記してpushします。事前に `bucket/envfacts.json` を更新する必要はありません。検査と配布物作成を担当するジョブは読み取り権限で動き、成功した同じ実行の配布物を、書き込み権限を持つ公開ジョブへ渡します。

公開ジョブは次の順で処理します。

1. 配布物の内容・SHA-256・版番号・URLを照合し、下書きのReleaseを検証したコミットを対象として作成します。
2. ZIP、`checksums.txt`、`envfacts.json` を添付し、GitHubが報告するSHA-256との一致を確認して公開します。タグは `v<版番号>` です。
3. 公開した `envfacts.json` を取得し直してハッシュを検証し、mainの `bucket/envfacts.json` だけを更新します。コミットの説明は `chore(scoop): envfacts <版番号> の配布定義を更新` です。

公開済みの同じ版は、タグの対象コミットを確認して公開処理を省略します。タグが別コミットを指す場合や、下書きに異なる内容の添付ファイルがある場合は上書きせず停止します。アップロード中に失敗した場合は下書きのまま残し、再実行時にハッシュが一致する添付ファイルを保持して不足分だけを送ります。

公開ジョブは同時に1件とし、待機中の処理を取り消さないよう `queue: max` を指定しています。待機上限は100件で、実行順はジョブが待機に入った順です。古い処理の完了が遅れた場合に備え、最新版の選択には `make_latest: legacy` を使い、mainが次の版へ進んでいれば以前の版のScoop同期を省略します。[GitHubの同時実行制御](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)、[Release API](https://docs.github.com/en/rest/releases/releases) に従います。

### GitHub側の設定

GitHub Actionsを有効にし、公式の `actions/checkout`、`actions/setup-go`、`actions/upload-artifact`、`actions/download-artifact` と、Windows AMD64の `windows-2025` 実行環境を利用できるようにします。各Actionはコミット識別子で固定しています。Goは `go.mod` に指定した版を用意し、Go自身による追加取得は無効にします。

公開とScoop同期には自動発行される `GITHUB_TOKEN` の `contents: write` を使い、個人アクセストークンの追加は不要です。権限は公開ジョブに限定し、Gitの認証情報を作業ツリーへ保存しません。[権限の仕様](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#permissions) を参照してください。

Scoop同期はmainへの直接のコミットを作るため、ブランチ保護やリポジトリの規則で拒否される場合は同期が失敗します。保護の無効化や回避は行いません。その場合は公開した配布定義を承認済みの手順で反映する運用が必要です。`GITHUB_TOKEN` によるコミットでは新しいpushワークフローは起動しません。

### 失敗した場合

Actionsの失敗箇所とReleaseの状態を確認し、同じ実行を再実行します。公開だけが成功した場合も、再実行では配布物を変更せずScoop同期を再開します。比較元コミットは全履歴の取得を前提とし、強制pushなどで取得できない場合は停止します。

下書きの対象コミットや添付ファイルのハッシュが異なる場合は、自動で削除・差し替えをしません。状態を確認し、必要なら新しい版番号で修正版を公開します。手元で生成したZIPを公開済みの同じ版へ差し替えてはいけません。

## 配布物の手元検証

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

手元で版番号を正本から取り出す場合は次を使います。手動公開を行う場合だけ、生成した配布定義を反映します。自動公開では、公開に成功した実際の添付ファイルからCIが反映します。

```powershell
$version = [regex]::Match((Get-Content ./internal/cli/version.go -Raw), 'const Version = "([0-9]+\.[0-9]+\.[0-9]+)"').Groups[1].Value
$releaseDirectory = Join-Path $PWD "bin/releases/$version"
New-Item -ItemType Directory -Path ./bucket -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $releaseDirectory 'envfacts.json') -Destination ./bucket/envfacts.json
```

ZIPを一時ディレクトリへ展開し、ファイル一覧、`--version`、`--help` と検証済み実行ファイルとのハッシュ一致を確認します。配布定義の版・対象CPU・URL・ハッシュ、`checksums.txt` との一致も確認してください。同じソース・Go・ビルド設定で別の場所へビルドしてハッシュを比較すると、実行ファイルの再現性を確認できます。未確認の場合は再現可能と表明しません。

公開前にREADME・LICENSE・配布手順・変更履歴と配布定義を含めて差分を確認し、秘密情報と生成物がGitの対象に入っていないことを確認します。必須検証を通した変更をコミットし、設定済みの追跡先へプッシュします。生成物はコミットしません。

## 自動公開を使わない場合の手動公開

公開の明示認可があり、GitHub Actionsの自動公開が動かない場合だけ実行します。通常のmainへのpushでは自動公開が動くため、以下のコマンドを重ねて実行しません。`gh` の認証とGitの接続が利用できることを確認してください。認証情報をコマンドやログへ貼り付けず、必要なら利用者のPowerShellで `gh auth login --hostname github.com --web` を実行します。

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

導入を取り消す場合は `scoop uninstall envfacts`、ZIP導入の場合は利用者が展開したフォルダーと追加したPATHを取り消します。
