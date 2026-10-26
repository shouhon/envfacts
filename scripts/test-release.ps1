#Requires -Version 7.0

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'release-ci.ps1')

$cacheRoot = [IO.Path]::GetFullPath((Join-Path (Split-Path -Parent $PSScriptRoot) '.cache'))
$work = Join-Path $cacheRoot ('release-test-' + [guid]::NewGuid().ToString('N'))
$version = '0.3.2'
$before = 'a' * 40
$commit = 'b' * 40
$script:caseCount = 0
$script:requests = [Collections.Generic.List[string]]::new()

function Assert-ReleaseTest([bool] $Condition, [string] $Message) {
    if (-not $Condition) { throw $Message }
}

function Assert-ReleaseThrows([scriptblock] $Action, [string] $Message) {
    $caught = $false
    try { & $Action }
    catch {
        $caught = $true
        if (-not $_.Exception.Message.Contains($Message)) { throw }
    }
    Assert-ReleaseTest $caught "想定した失敗が起きませんでした: $Message"
}

function Reset-ReleaseMock {
    $script:requests.Clear()
    $script:release = $null
    $script:tagCommit = ''
    $script:mainVersion = $version
    $script:bucketContent = '{"version":"0.3.1"}'
    $script:denyApi = $false
    $script:failUploadName = ''
    $script:sources = @{
        "${before}:internal/cli/version.go" = 'const Version = "0.3.1"'
        "${commit}:internal/cli/version.go" = "const Version = `"$version`""
    }
}

function Test-ReleaseCase([string] $Name, [scriptblock] $Action) {
    Reset-ReleaseMock
    & $Action
    $script:caseCount++
    Write-Host "確認済み: $Name"
}

# 外部のGit・GitHub CLIを呼ばず、読み書きの要求と部分失敗を観測する。
function git {
    $global:LASTEXITCODE = 0
    if ($args.Count -ne 2 -or $args[0] -cne 'show') { throw '想定外のGit操作です。' }
    if (-not $script:sources.ContainsKey($args[1])) { $global:LASTEXITCODE = 1; return }
    return $script:sources[$args[1]]
}

function gh {
    $global:LASTEXITCODE = 0
    if ($args[0] -ceq 'api') {
        $path = $args[1]
        $method = 'GET'
        $methodIndex = [array]::IndexOf($args, '--method')
        if ($methodIndex -ge 0) { $method = $args[$methodIndex + 1] }
        $script:requests.Add("$method $path")
        if ($script:denyApi) { $global:LASTEXITCODE = 1; return }
        $result = $null
        switch -Regex ($path) {
            '/releases\?per_page=100$' {
                $items = @()
                if ($null -ne $script:release) { $items = @($script:release) }
                $result = ,$items
            }
            '/git/matching-refs/tags/' {
                $result = @()
                if ($script:tagCommit) {
                    $result = @(@{ ref = "refs/tags/v$version"; object = @{ type = 'commit'; sha = $script:tagCommit } })
                }
            }
            '/releases$' {
                Assert-ReleaseTest ($method -ceq 'POST' -and $args -contains 'draft=true') '下書きとして作成していません。'
                Assert-ReleaseTest ($args -contains "target_commitish=$commit") '検証対象のコミットを指定していません。'
                $script:release = @{ id = 42; tag_name = "v$version"; draft = $true; target_commitish = $commit; assets = @() }
                $result = $script:release
            }
            '/releases/42$' {
                if ($method -ceq 'PATCH') {
                    Assert-ReleaseTest ($args -contains 'draft=false' -and $args -contains 'make_latest=legacy') '公開時の版番号による最新版判定がありません。'
                    $script:release.draft = $false
                    $script:tagCommit = $script:release.target_commitish
                }
                $result = $script:release
            }
            '/releases/tags/' { $result = $script:release }
            '/contents/internal/cli/version.go\?ref=main$' {
                $text = "const Version = `"$script:mainVersion`""
                $result = @{ content = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($text)) }
            }
            '/contents/bucket/envfacts.json' {
                if ($method -ceq 'PUT') {
                    Assert-ReleaseTest ($args -contains 'branch=main' -and $args -contains 'sha=fixture-sha') '同期先と既存ファイルの識別子がありません。'
                    $field = @($args | Where-Object { $_ -clike 'content=*' })[0]
                    $script:bucketContent = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($field.Substring(8)))
                }
                $result = @{ sha = 'fixture-sha'; content = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($script:bucketContent)) }
            }
            default { throw "想定外のGitHub API呼び出しです: $method $path" }
        }
        return ConvertTo-Json -InputObject $result -Depth 20 -Compress
    }
    if ($args[0] -ceq 'release' -and $args[1] -ceq 'upload') {
        $file = $args[3]
        $name = Split-Path -Leaf $file
        $script:requests.Add("upload $name")
        Assert-ReleaseTest (-not ($args -contains '--clobber')) '添付ファイルの強制上書きが要求されました。'
        if ($script:failUploadName -ceq $name) { $global:LASTEXITCODE = 1; return }
        $script:release.assets += @{ name = $name; state = 'uploaded'; digest = 'sha256:' + (Get-FileHash -LiteralPath $file).Hash.ToLowerInvariant() }
        return
    }
    if ($args[0] -ceq 'release' -and $args[1] -ceq 'download') {
        $script:requests.Add('download envfacts.json')
        $outputIndex = [array]::IndexOf($args, '--output')
        Copy-Item -LiteralPath (Join-Path $work 'envfacts.json') -Destination $args[$outputIndex + 1]
        return
    }
    throw '想定外のGitHub CLI操作です。'
}

try {
    New-Item -ItemType Directory -Path $work | Out-Null
    $zip = Join-Path $work "envfacts-$version-windows-amd64.zip"
    $archive = [IO.Compression.ZipFile]::Open($zip, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($name in @('envfacts.exe', 'LICENSE', 'README.md')) {
            $entry = $archive.CreateEntry($name)
            $writer = [IO.StreamWriter]::new($entry.Open())
            try { $writer.Write('機密情報を含まない模擬の配布物') }
            finally { $writer.Dispose() }
        }
    }
    finally { $archive.Dispose() }
    $archiveHash = (Get-FileHash -LiteralPath $zip).Hash.ToLowerInvariant()
    $manifest = @{
        version = $version; bin = 'envfacts.exe'
        architecture = @{ '64bit' = @{
            url = "https://github.com/shouhon/envfacts/releases/download/v$version/envfacts-$version-windows-amd64.zip"
            hash = $archiveHash
        } }
    }
    $manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $work 'envfacts.json') -Encoding utf8
    $manifestHash = (Get-FileHash -LiteralPath (Join-Path $work 'envfacts.json')).Hash.ToLowerInvariant()
    @("$archiveHash  envfacts-$version-windows-amd64.zip", "$manifestHash  envfacts.json") |
        Set-Content -LiteralPath (Join-Path $work 'checksums.txt') -Encoding ascii
    Set-Content -LiteralPath (Join-Path $work 'release-notes.md') -Value '模擬の公開説明' -Encoding utf8

    Test-ReleaseCase '版番号が変わらないpushでは公開しない' {
        $plan = Get-EnvfactsReleasePlan -Before $commit -Commit $commit
        Assert-ReleaseTest (-not $plan.Changed -and $plan.Version -ceq $version) '変更なしの判定が不正です。'
    }
    Test-ReleaseCase 'push全体の前後を比較して版番号の更新を検出する' {
        $plan = Get-EnvfactsReleasePlan -Before $before -Commit $commit
        Assert-ReleaseTest ($plan.Changed -and $plan.PreviousVersion -ceq '0.3.1') '版番号の更新を検出できません。'
    }
    Test-ReleaseCase '初回pushを扱い、不明な履歴と版番号の後退は停止する' {
        Assert-ReleaseTest (Get-EnvfactsReleasePlan -Before ('0' * 40) -Commit $commit).Changed '初回pushの判定が不正です。'
        Assert-ReleaseThrows { Get-EnvfactsReleasePlan -Before ('c' * 40) -Commit $commit } '変更前の版番号'
        Assert-ReleaseThrows { Get-EnvfactsReleasePlan -Before $commit -Commit $before } '版番号を下げる'
        Assert-ReleaseThrows { Get-EnvfactsReleasePlan -Before 'main' -Commit $commit } '完全なコミット識別子'
        Assert-ReleaseThrows { Get-EnvfactsSourceVersion 'const Version = "0.03.2"' } '有効な版番号'
    }
    Test-ReleaseCase '配布物の破損を公開前に検出する' {
        $path = Join-Path $work 'checksums.txt'
        $original = [IO.File]::ReadAllBytes($path)
        try {
            Set-Content -LiteralPath $path -Value @('不正なハッシュ', '不正なハッシュ')
            Assert-ReleaseThrows { Publish-EnvfactsRelease $version $commit $work } 'SHA-256'
            Assert-ReleaseTest ($script:requests.Count -eq 0) '破損した配布物で外部操作を要求しました。'
        }
        finally { [IO.File]::WriteAllBytes($path, $original) }
    }
    Test-ReleaseCase '検証済みの3ファイルを下書きへ添付してから公開する' {
        Publish-EnvfactsRelease $version $commit $work
        Assert-ReleaseTest (-not $script:release.draft -and $script:release.assets.Count -eq 3) '公開状態が不正です。'
        Assert-ReleaseTest ($script:tagCommit -ceq $commit) 'タグの対象コミットが不正です。'
    }
    Test-ReleaseCase '同じ処理の再実行で公開済みの版を変更しない' {
        Publish-EnvfactsRelease $version $commit $work
        $script:requests.Clear()
        Publish-EnvfactsRelease $version $commit $work
        Assert-ReleaseTest (@($script:requests | Where-Object { $_ -notlike 'GET *' }).Count -eq 0) '公開済みの版を変更しました。'
    }
    Test-ReleaseCase '別コミットの既存タグとAPIの認証失敗では公開しない' {
        $script:tagCommit = 'c' * 40
        Assert-ReleaseThrows { Publish-EnvfactsRelease $version $commit $work } '別のコミット'
        $script:denyApi = $true
        Assert-ReleaseThrows { Publish-EnvfactsRelease $version $commit $work } 'GitHub API'
        Assert-ReleaseTest (@($script:requests | Where-Object { $_ -like 'POST *' }).Count -eq 0) '不正な状態で公開を要求しました。'
    }
    Test-ReleaseCase 'アップロードの部分失敗は下書きから不足分だけ再開する' {
        $script:failUploadName = 'checksums.txt'
        Assert-ReleaseThrows { Publish-EnvfactsRelease $version $commit $work } '下書きを残して停止'
        Assert-ReleaseTest ($script:release.draft -and $script:release.assets.Count -eq 1) '不完全なReleaseを公開しました。'
        $script:failUploadName = ''
        $script:requests.Clear()
        Publish-EnvfactsRelease $version $commit $work
        Assert-ReleaseTest (-not $script:release.draft -and @($script:requests | Where-Object { $_ -like 'upload *' }).Count -eq 2) '不足分だけの再開になっていません。'
    }
    Test-ReleaseCase '下書きに異なる添付ファイルがある場合は上書きしない' {
        $script:failUploadName = 'checksums.txt'
        Assert-ReleaseThrows { Publish-EnvfactsRelease $version $commit $work } '下書きを残して停止'
        $script:release.assets[0].digest = 'sha256:' + ('0' * 64)
        Assert-ReleaseThrows { Publish-EnvfactsRelease $version $commit $work } '上書きしません'
        Assert-ReleaseTest $script:release.draft '不一致の添付ファイルを公開しました。'
    }
    Test-ReleaseCase '公開した配布定義を取得しmainのScoop定義だけを同期する' {
        Publish-EnvfactsRelease $version $commit $work
        Sync-EnvfactsReleaseBucket $version $work
        Assert-ReleaseTest (($script:bucketContent | ConvertFrom-Json).version -ceq $version) 'Scoopの版番号が更新されません。'
        $script:requests.Clear()
        Sync-EnvfactsReleaseBucket $version $work
        Assert-ReleaseTest (@($script:requests | Where-Object { $_ -like 'PUT *' }).Count -eq 0) '同期済みの定義を再更新しました。'
    }
    Test-ReleaseCase 'mainが次の版へ進んだ場合はScoop定義を後退させない' {
        $script:mainVersion = '0.3.3'
        Sync-EnvfactsReleaseBucket $version $work
        Assert-ReleaseTest ($script:requests.Count -eq 1) '過去の版の配布定義を更新しました。'
    }
    Test-ReleaseCase '公開ファイルのハッシュ不一致ではScoopへ反映しない' {
        Publish-EnvfactsRelease $version $commit $work
        $script:release.assets[2].digest = 'sha256:' + ('0' * 64)
        Assert-ReleaseThrows { Sync-EnvfactsReleaseBucket $version $work } 'SHA-256がReleaseと一致'
        Assert-ReleaseTest (@($script:requests | Where-Object { $_ -like 'PUT *' }).Count -eq 0) '不一致の配布定義を反映しました。'
    }
    Write-Host "Releaseの判定・公開・再開・Scoop同期の確認が完了しました: $script:caseCount 件。外部操作は行っていません。"
}
finally {
    if (-not $work.StartsWith($cacheRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw '模擬の配布物の削除先が検証用の範囲外です。'
    }
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
    $global:LASTEXITCODE = 0
}
