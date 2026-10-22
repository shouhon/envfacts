#Requires -Version 7.0

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not $IsWindows) {
    throw 'この検証手順は Windows 用です。'
}

$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
try {
    $packageDirectories = @(& go list -f '{{.Dir}}' ./...)
    if ($LASTEXITCODE -ne 0) {
        throw 'Go パッケージの一覧を取得できませんでした。'
    }

    $goFiles = @($packageDirectories | ForEach-Object {
        Get-ChildItem -LiteralPath $_ -Filter '*.go' -File | ForEach-Object { $_.FullName }
    })
    if ($goFiles.Count -eq 0) {
        throw '検証する Go ファイルがありません。'
    }
    $unformattedFiles = @(& gofmt -l @goFiles)
    if ($LASTEXITCODE -ne 0) {
        throw 'gofmt の実行に失敗しました。'
    }
    if ($unformattedFiles.Count -gt 0) {
        $unformattedFiles | Write-Output
        throw '未整形のファイルがあります。表示されたファイルに gofmt -w を実行してください。'
    }

    & go vet ./...
    if ($LASTEXITCODE -ne 0) {
        throw 'go vet に失敗しました。'
    }

    & go test ./...
    if ($LASTEXITCODE -ne 0) {
        throw 'go test に失敗しました。'
    }

    New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
    & go build -trimpath -o bin/envfacts.exe ./cmd/envfacts
    if ($LASTEXITCODE -ne 0) {
        throw 'ビルドに失敗しました。'
    }

    & .\bin\envfacts.exe --version
    if ($LASTEXITCODE -ne 0) {
        throw 'ビルドした実行ファイルの起動に失敗しました。'
    }

    Write-Output '整形・静的検査・テスト・ビルド・起動確認が完了しました。'
}
finally {
    Pop-Location
}
