#Requires -Version 7.0

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-EnvfactsSourceVersion([string] $Source) {
    $matches = [regex]::Matches($Source, '(?m)^const Version = "((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))"\r?$')
    if ($matches.Count -ne 1) { throw '正本から有効な版番号を一意に取得できませんでした。' }
    return $matches[0].Groups[1].Value
}

function Get-EnvfactsReleasePlan([string] $Before, [string] $Commit) {
    if ($Before -cnotmatch '^[0-9a-f]{40}$' -or $Commit -cnotmatch '^[0-9a-f]{40}$' -or $Commit -match '^0+$') {
        throw '比較対象にはpush前後の完全なコミット識別子を指定してください。'
    }
    $source = & git show "${Commit}:internal/cli/version.go"
    if ($LASTEXITCODE -ne 0) { throw '公開対象の版番号を読み取れませんでした。' }
    $version = Get-EnvfactsSourceVersion ($source -join "`n")
    $previous = ''
    if ($Before -notmatch '^0+$') {
        $source = & git show "${Before}:internal/cli/version.go"
        if ($LASTEXITCODE -ne 0) { throw '変更前の版番号を読み取れませんでした。履歴の取得範囲を確認してください。' }
        $previous = Get-EnvfactsSourceVersion ($source -join "`n")
        if ([version]$version -lt [version]$previous) { throw '版番号を下げる変更は自動公開できません。' }
    }
    return [pscustomobject]@{ Version = $version; PreviousVersion = $previous; Changed = $version -cne $previous }
}

function Assert-EnvfactsManifest($Manifest, [string] $Version, [string] $ArchiveHash) {
    $url = "https://github.com/shouhon/envfacts/releases/download/v$Version/envfacts-$Version-windows-amd64.zip"
    if ($Manifest.version -cne $Version -or $Manifest.architecture.'64bit'.url -cne $url -or
        $Manifest.architecture.'64bit'.hash -cne $ArchiveHash -or $Manifest.bin -cne 'envfacts.exe') {
        throw 'Scoop配布定義の版番号・URL・SHA-256・実行ファイル名が配布物と一致しません。'
    }
}

function Assert-EnvfactsReleaseFiles([string] $Version, [string] $Directory) {
    $null = Get-EnvfactsSourceVersion "const Version = `"$Version`""
    $names = @("envfacts-$Version-windows-amd64.zip", 'envfacts.json')
    $lines = @(Get-Content -LiteralPath (Join-Path $Directory 'checksums.txt'))
    if ($lines.Count -ne $names.Count) { throw 'SHA-256一覧の件数が不正です。' }
    for ($i = 0; $i -lt $names.Count; $i++) {
        $hash = (Get-FileHash -LiteralPath (Join-Path $Directory $names[$i]) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($lines[$i] -cne "$hash  $($names[$i])") { throw '配布物のSHA-256が一覧と一致しません。' }
    }
    $manifest = Get-Content -LiteralPath (Join-Path $Directory 'envfacts.json') -Raw | ConvertFrom-Json
    Assert-EnvfactsManifest $manifest $Version ($lines[0].Substring(0, 64))
    if ([string]::IsNullOrWhiteSpace((Get-Content -LiteralPath (Join-Path $Directory 'release-notes.md') -Raw))) {
        throw '公開用の説明が空です。'
    }
    $archive = [IO.Compression.ZipFile]::OpenRead((Join-Path $Directory $names[0]))
    try {
        if (($archive.Entries.FullName | Sort-Object) -join ',' -cne 'envfacts.exe,LICENSE,README.md') {
            throw 'ZIPに含まれるファイルが配布対象と一致しません。'
        }
    }
    finally { $archive.Dispose() }
}

function Invoke-EnvfactsReleaseApi([string[]] $Arguments) {
    $response = & gh api @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'GitHub APIの呼び出しに失敗しました。公開・同期処理を停止します。' }
    return ,(ConvertFrom-Json -InputObject ($response -join "`n") -AsHashtable -NoEnumerate)
}

function Get-EnvfactsReleaseTagCommit([string] $Version) {
    $refs = Invoke-EnvfactsReleaseApi @("repos/shouhon/envfacts/git/matching-refs/tags/v$Version")
    $refs = @($refs | Where-Object { $_.ref -ceq "refs/tags/v$Version" })
    if ($refs.Count -eq 0) { return '' }
    $target = $refs[0].object
    for ($i = 0; $target.type -ceq 'tag' -and $i -lt 8; $i++) {
        $tag = Invoke-EnvfactsReleaseApi @("repos/shouhon/envfacts/git/tags/$($target.sha)")
        $target = $tag.object
    }
    if ($target.type -cne 'commit') { throw '公開タグの対象コミットを確認できませんでした。' }
    return $target.sha
}

function Publish-EnvfactsRelease([string] $Version, [string] $Commit, [string] $Directory) {
    if ($Commit -cnotmatch '^[0-9a-f]{40}$') { throw '公開対象のコミット識別子が不正です。' }
    Assert-EnvfactsReleaseFiles $Version $Directory
    $endpoint = 'repos/shouhon/envfacts/releases'
    $pages = Invoke-EnvfactsReleaseApi @("${endpoint}?per_page=100", '--paginate', '--slurp')
    $releases = @(foreach ($page in $pages) { foreach ($item in $page) { if ($item.tag_name -ceq "v$Version") { $item } } })
    if ($releases.Count -gt 1) { throw '同じ版のReleaseが複数あります。' }
    $tagCommit = Get-EnvfactsReleaseTagCommit $Version
    if ($tagCommit -and $tagCommit -cne $Commit) { throw '既存タグが別のコミットを指しています。上書きしません。' }
    if ($releases.Count -eq 1 -and -not $releases[0].draft) {
        if ($tagCommit -cne $Commit) { throw '公開済みの版と対象コミットが一致しません。' }
        Write-Host "v$Version は公開済みです。配布物を変更せず、Scoopの同期へ進みます。"
        return
    }
    if ($releases.Count -eq 1) {
        $release = $releases[0]
        if ($release.target_commitish -cne $Commit) { throw '下書きの対象コミットが一致しません。' }
    }
    else {
        $release = Invoke-EnvfactsReleaseApi @($endpoint, '--method', 'POST',
            '--raw-field', "tag_name=v$Version", '--raw-field', "target_commitish=$Commit",
            '--raw-field', "name=envfacts v$Version", '--field', 'draft=true',
            '--field', "body=@$(Join-Path $Directory 'release-notes.md')", '--raw-field', 'make_latest=legacy')
    }

    $names = @("envfacts-$Version-windows-amd64.zip", 'checksums.txt', 'envfacts.json')
    foreach ($name in $names) {
        $file = Join-Path $Directory $name
        $digest = 'sha256:' + (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant()
        $assets = @($release.assets | Where-Object { $_.name -ceq $name })
        if ($assets.Count -gt 0) {
            if ($assets.Count -ne 1 -or $assets[0].state -cne 'uploaded' -or $assets[0].digest -cne $digest) {
                throw '下書きの添付ファイルが手元の配布物と一致しません。上書きしません。'
            }
            continue
        }
        & gh release upload "v$Version" $file --repo shouhon/envfacts
        if ($LASTEXITCODE -ne 0) { throw '添付ファイルのアップロードに失敗しました。下書きを残して停止します。' }
    }
    $release = Invoke-EnvfactsReleaseApi @("$endpoint/$($release.id)")
    if ($release.assets.Count -ne $names.Count) { throw 'Releaseの添付ファイルの件数が不正です。' }
    foreach ($name in $names) {
        $assets = @($release.assets | Where-Object { $_.name -ceq $name })
        $digest = 'sha256:' + (Get-FileHash -LiteralPath (Join-Path $Directory $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($assets.Count -ne 1 -or $assets[0].state -cne 'uploaded' -or $assets[0].digest -cne $digest) {
            throw 'アップロードした添付ファイルのSHA-256を確認できませんでした。公開を停止します。'
        }
    }
    $release = Invoke-EnvfactsReleaseApi @("$endpoint/$($release.id)", '--method', 'PATCH',
        '--field', 'draft=false', '--raw-field', 'make_latest=legacy')
    if ($release.draft -or (Get-EnvfactsReleaseTagCommit $Version) -cne $Commit) {
        throw 'Releaseの公開状態と対象コミットを確認できませんでした。'
    }
    Write-Host "v$Version を公開しました。"
}

function Sync-EnvfactsReleaseBucket([string] $Version, [string] $Directory) {
    $null = Get-EnvfactsSourceVersion "const Version = `"$Version`""
    $root = 'repos/shouhon/envfacts'
    $source = Invoke-EnvfactsReleaseApi @("$root/contents/internal/cli/version.go?ref=main")
    $mainVersion = Get-EnvfactsSourceVersion ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($source.content)))
    if ([version]$mainVersion -gt [version]$Version) {
        Write-Host 'mainは次の版へ進んでいるため、以前の版の配布定義を反映しません。'
        return
    }
    if ($mainVersion -cne $Version) { throw 'mainの版番号が公開した版と一致しません。' }
    $release = Invoke-EnvfactsReleaseApi @("$root/releases/tags/v$Version")
    if ($release.draft) { throw '未公開の配布定義はScoopへ反映できません。' }
    $archives = @($release.assets | Where-Object { $_.name -ceq "envfacts-$Version-windows-amd64.zip" -and $_.state -ceq 'uploaded' })
    $manifests = @($release.assets | Where-Object { $_.name -ceq 'envfacts.json' -and $_.state -ceq 'uploaded' })
    if ($archives.Count -ne 1 -or $manifests.Count -ne 1 -or $archives[0].digest -cnotmatch '^sha256:[0-9a-f]{64}$') {
        throw '公開済みのZIPと配布定義を確認できませんでした。'
    }

    $download = Join-Path $Directory ('published-manifest-' + [guid]::NewGuid().ToString('N') + '.json')
    try {
        & gh release download "v$Version" --pattern envfacts.json --output $download --repo shouhon/envfacts
        if ($LASTEXITCODE -ne 0) { throw '公開済みの配布定義を取得できませんでした。' }
        $hash = 'sha256:' + (Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($hash -cne $manifests[0].digest) { throw '取得した配布定義のSHA-256がReleaseと一致しません。' }
        $content = Get-Content -LiteralPath $download -Raw
        $manifest = $content | ConvertFrom-Json
        Assert-EnvfactsManifest $manifest $Version ($archives[0].digest.Substring(7))
        $current = Invoke-EnvfactsReleaseApi @("$root/contents/bucket/envfacts.json?ref=main")
        $oldContent = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($current.content))
        $oldManifest = $oldContent | ConvertFrom-Json
        if ($oldContent -ceq $content) { Write-Host 'Scoopの配布定義は同期済みです。'; return }
        if ([version]$oldManifest.version -gt [version]$Version) {
            Write-Host 'Scoopの配布定義は新しい版へ進んでいるため、更新しません。'
            return
        }
        if ($oldManifest.version -ceq $Version) { throw '同じ版のScoop配布定義が公開済みの内容と異なります。上書きしません。' }
        $encoded = [Convert]::ToBase64String([IO.File]::ReadAllBytes($download))
        $null = Invoke-EnvfactsReleaseApi @("$root/contents/bucket/envfacts.json", '--method', 'PUT',
            '--raw-field', 'branch=main', '--raw-field', "sha=$($current.sha)", '--raw-field', "content=$encoded",
            '--raw-field', "message=chore(scoop): envfacts $Version の配布定義を更新")
        Write-Host "Scoopの配布定義を $Version に更新しました。"
    }
    finally {
        if (Test-Path -LiteralPath $download -PathType Leaf) { Remove-Item -LiteralPath $download }
    }
}
