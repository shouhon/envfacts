#Requires -Version 7.0

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not $IsWindows) { throw '配布物の作成は Windows AMD64 で実行してください。' }

$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
$previousToolchain = $env:GOTOOLCHAIN
try {
    $env:GOTOOLCHAIN = 'local'
    $target = @(& go env GOOS GOARCH)
    if ($LASTEXITCODE -ne 0 -or ($target -join '/') -cne 'windows/amd64') {
        throw '配布対象は Windows AMD64 のみです。GOOS・GOARCH の設定を確認してください。'
    }
    $versionMatch = [regex]::Match((Get-Content 'internal/cli/version.go' -Raw), 'const Version = "([0-9]+\.[0-9]+\.[0-9]+)"')
    if (-not $versionMatch.Success) { throw '正本から版番号を取得できませんでした。' }
    $version = $versionMatch.Groups[1].Value
    $notesMatch = [regex]::Match((Get-Content 'CHANGELOG.md' -Raw), '(?ms)^## ' + [regex]::Escape($version) + '（[^\r\n]+）\r?\n(.*?)(?=^## |\z)')
    if (-not $notesMatch.Success) { throw '現在の版の変更履歴がありません。' }

    $output = Join-Path $projectRoot "bin/releases/$version"
    if (Test-Path -LiteralPath $output) { throw "配布物の保存先が既に存在します。上書きしません: $output" }

    & (Join-Path $PSScriptRoot 'check.ps1')
    New-Item -ItemType Directory -Path $output | Out-Null

    $archiveName = "envfacts-$version-windows-amd64.zip"
    $archivePath = Join-Path $output $archiveName
    $archive = [IO.Compression.ZipFile]::Open($archivePath, [IO.Compression.ZipArchiveMode]::Create)
    try {
        $files = [ordered]@{ 'envfacts.exe' = 'bin/envfacts.exe'; 'LICENSE' = 'LICENSE'; 'README.md' = 'README.md' }
        foreach ($name in $files.Keys) {
            $entry = $archive.CreateEntry($name, [IO.Compression.CompressionLevel]::Optimal)
            # 入力ファイルの更新日時を配布物へ持ち込まない。
            $entry.LastWriteTime = [DateTimeOffset]::new(1980, 1, 1, 0, 0, 0, [TimeSpan]::Zero)
            $inputStream = [IO.File]::OpenRead((Join-Path $projectRoot $files[$name]))
            try {
                $outputStream = $entry.Open()
                try { $inputStream.CopyTo($outputStream) }
                finally { $outputStream.Dispose() }
            }
            finally { $inputStream.Dispose() }
        }
    }
    finally { $archive.Dispose() }

    $archiveHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    $manifest = [ordered]@{
        version = $version
        description = 'ローカル環境のCLIの導入情報とPATHの名前解決結果を報告'
        homepage = 'https://github.com/shouhon/envfacts'
        license = 'MIT'
        architecture = [ordered]@{
            '64bit' = [ordered]@{
                url = "https://github.com/shouhon/envfacts/releases/download/v$version/$archiveName"
                hash = $archiveHash
            }
        }
        bin = 'envfacts.exe'
    }
    $manifestPath = Join-Path $output 'envfacts.json'
    $manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestPath -Encoding utf8
    $manifestHash = (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
    @("$archiveHash  $archiveName", "$manifestHash  envfacts.json") | Set-Content -LiteralPath (Join-Path $output 'checksums.txt') -Encoding ascii
    $notesMatch.Groups[1].Value.Trim() | Set-Content -LiteralPath (Join-Path $output 'release-notes.md') -Encoding utf8

    Write-Output "配布物を作成しました: $output"
    Get-ChildItem -LiteralPath $output -File | Select-Object Name, Length
    Write-Output '公開操作は行っていません。自動公開と手動公開の手順は docs/releasing.md を参照してください。'
}
finally {
    $env:GOTOOLCHAIN = $previousToolchain
    Pop-Location
}
