#Requires -Version 7.0

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not $IsWindows) {
    throw 'この受入確認は Windows 用です。'
}

$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $projectRoot 'bin/envfacts.exe'
if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
    throw '先に scripts/check.ps1 を実行して bin/envfacts.exe をビルドしてください。'
}
$cacheRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot '.cache'))
$work = Join-Path $cacheRoot ('acceptance-' + [guid]::NewGuid().ToString('N'))
$targets = Join-Path $work '模擬環境 空白'
$records = Join-Path $work '記録'
$userRoot = Join-Path $targets '利用者 Scoop'
$globalRoot = Join-Path $targets '共通 Scoop'
$external = Join-Path $targets '外部 CLI'
$marker = Join-Path $work '実行されたら失敗.txt'
$probe = Join-Path $work '模擬CLI.exe'
$lockedPath = ''
$processEnvironment = @{
    USERPROFILE = $targets
    ProgramData = (Join-Path $targets '共通設定')
    XDG_CONFIG_HOME = (Join-Path $targets '設定')
    SCOOP = $userRoot
    SCOOP_GLOBAL = $globalRoot
    PATH = ((Join-Path $userRoot 'shims') + ';' + $external + ';' + (Join-Path $globalRoot 'shims'))
    PATHEXT = '.EXE;.CMD'
    ENVFACTS_FIXTURE_TOKEN = 'fixture-secret-do-not-report'
    ENVFACTS_FIXTURE_MARKER = $marker
}

function Assert-Condition([bool] $Condition, [string] $Message) {
    if (-not $Condition) { throw $Message }
}

function Write-Fixture([string] $Path, [string] $Text) {
    New-Item -ItemType Directory -Path (Split-Path -Parent $Path) -Force | Out-Null
    [IO.File]::WriteAllText($Path, $Text, [Text.UTF8Encoding]::new($false))
}

function Save-Record([string] $Name, $Value) {
    $path = Join-Path $records $Name
    Write-Fixture $path ($Value | ConvertTo-Json -Depth 50)
    return $path
}

function Get-Snapshot([string] $ExcludedFile = '') {
    # リンクをたどらず、ディレクトリの追加、本文・更新日時・属性・参照先の変更を検出する。
    $entries = @(Get-ChildItem -LiteralPath $work -Force -Recurse | Sort-Object FullName | ForEach-Object {
        if ($_.FullName -eq $ExcludedFile) { return }
        $entry = [ordered]@{ Path = $_.FullName; Attributes = [int]$_.Attributes; Target = $_.Target }
        if (-not $_.PSIsContainer) {
            # 共有拒否で保護したファイルは更新日時・大きさを照合し、解放後に本文も確認する。
            if ($_.FullName -ne $lockedPath) { $entry.Hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash }
            $entry.Length = $_.Length
            $entry.WrittenAt = $_.LastWriteTimeUtc.Ticks
        }
        $entry
    })
    return ConvertTo-Json -InputObject $entries -Depth 5 -Compress
}

function Invoke-Product([string[]] $Arguments, [int] $Code, [string] $Status = '', [string] $NewRecord = '') {
    if ($NewRecord) {
        Assert-Condition (-not (Test-Path -LiteralPath $NewRecord)) '新規保存の確認先が既に存在します。'
    }
    $snapshot = Get-Snapshot $NewRecord
    $start = [Diagnostics.ProcessStartInfo]::new($binary)
    $start.WorkingDirectory = $work
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    $start.StandardErrorEncoding = [Text.UTF8Encoding]::new($false)
    foreach ($argument in $Arguments) { $start.ArgumentList.Add($argument) }
    foreach ($key in $processEnvironment.Keys) { $start.Environment[$key] = $processEnvironment[$key] }
    $process = [Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(10000)) {
            $process.Kill($true)
            $process.WaitForExit()
            throw '実行ファイルが制限時間内に終了しませんでした。'
        }
        $result = @{ Out = $stdout.Result; Err = $stderr.Result; Json = $null }
        Assert-Condition ($process.ExitCode -eq $Code) ('終了コードが不正です。期待: ' + $Code + '、実際: ' + $process.ExitCode)
    }
    finally { $process.Dispose() }

    Assert-Condition (-not ($result.Out + $result.Err).Contains('fixture-secret')) '余分な模擬の機密値が出力されています。'
    Assert-Condition (-not ($result.Out + $result.Err).Contains('example.invalid')) '取得元の余分な情報が出力されています。'
    Assert-Condition ((Get-Snapshot $NewRecord) -ceq $snapshot) '実行ファイルが模擬環境または入力記録を変更しました。'
    Assert-Condition (-not (Test-Path -LiteralPath $marker)) '調査対象のコマンドが実行されました。'
    Assert-Condition (($Code -in @(2, 64)) -eq ($result.Err.Length -gt 0)) '診断と標準出力の分離が不正です。'
    if ($Status) {
        # JSONオブジェクト1件の後ろに診断や別の値が続いていないことも検査する。
        $document = [System.Text.Json.JsonDocument]::Parse([string]$result.Out)
        try {
            Assert-Condition ($document.RootElement.ValueKind -eq [System.Text.Json.JsonValueKind]::Object) 'JSONの最上位がオブジェクトではありません。'
            Assert-Condition ($document.RootElement.GetProperty('captured_at').GetString().EndsWith('Z')) '記録日時がUTCではありません。'
        }
        finally { $document.Dispose() }
        $result.Json = ConvertFrom-Json -InputObject $result.Out -AsHashtable
        $report = $result.Json
        Assert-Condition ($report.status -ceq $Status -and $report.command -ceq $Arguments[0]) 'JSONの状態またはコマンド名が不正です。'
        Assert-Condition ($report.schema_version -eq 1 -and $report.tool -ceq 'envfacts' -and $report.invocation_status -ceq 'not_tested') 'JSONの記録形式または未確認の扱いが不正です。'
        foreach ($command in $report.commands) {
            Assert-Condition ($command.invocation_status -ceq 'not_tested') '対象コマンドを起動確認済みとして扱っています。'
        }
    }
    if ($NewRecord) {
        if ($Arguments -contains '--json-summary') {
            $saved = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($NewRecord)) -AsHashtable
            Assert-Condition ($saved.ContainsKey('packages') -and $saved.packages.Count -eq $result.Json.package_count) '要約モードで詳細記録が保存されていません。'
            Assert-Condition (($saved.commands | ConvertTo-Json -Depth 50 -Compress) -ceq ($result.Json.commands | ConvertTo-Json -Depth 50 -Compress)) '詳細保存と要約でコマンドの結果が異なります。'
        }
        else {
            Assert-Condition ([IO.File]::ReadAllText($NewRecord) -ceq $result.Out) '新規保存と標準出力のJSONが異なります。'
        }
    }
    return $result
}

function Assert-Issue($Result, [string] $Code) {
    Assert-Condition (@($Result.Json.issues | Where-Object code -CEQ $Code).Count -gt 0) ('必要な診断がありません: ' + $Code)
}

function Compare-Records([string] $Before, [string] $After, [int] $Code, [string] $Status) {
    return Invoke-Product @('diff', '--before', $Before, '--after', $After, '--json') $Code $Status
}

try {
    New-Item -ItemType Directory -Path $targets, $records, $external | Out-Null
    Push-Location -LiteralPath $projectRoot
    try {
        & go build -trimpath -o $probe ./testdata/execution-probe
        if ($LASTEXITCODE -ne 0) { throw '非実行を検査する模擬CLIのビルドに失敗しました。' }
    }
    finally { Pop-Location }
    # 検出用の模擬CLI自身が有効な実行形式で、印を残せることを先に確認する。
    $probeStart = [Diagnostics.ProcessStartInfo]::new($probe)
    $probeStart.UseShellExecute = $false
    $probeStart.CreateNoWindow = $true
    $probeStart.Environment['ENVFACTS_FIXTURE_MARKER'] = $marker
    $probeProcess = [Diagnostics.Process]::Start($probeStart)
    try {
        if (-not $probeProcess.WaitForExit(10000)) {
            $probeProcess.Kill($true)
            $probeProcess.WaitForExit()
            throw '模擬CLIの動作確認が制限時間内に終了しませんでした。'
        }
        Assert-Condition ($probeProcess.ExitCode -eq 9 -and (Test-Path -LiteralPath $marker -PathType Leaf)) '模擬CLIの実行を検出できません。'
    }
    finally { $probeProcess.Dispose() }
    Remove-Item -LiteralPath $marker
    foreach ($installation in @(@($userRoot, '1.2.3'), @($globalRoot, '2.0.0'))) {
        $root, $version = $installation
        $versionPath = Join-Path $root ('apps/engine-package/' + $version)
        $target = Join-Path $versionPath 'engine.cmd'
        $manifest = @{
            version = $version
            bin = @(@('engine.cmd', 'alpha', '--token fixture-secret-do-not-report'), @('engine.cmd', 'beta'))
            url = 'https://fixture:fixture-secret-do-not-report@example.invalid'
            post_install = 'throw "fixture-secret-do-not-report"'
        }
        Write-Fixture (Join-Path $versionPath 'scoop-manifest.json') ($manifest | ConvertTo-Json -Depth 10)
        Write-Fixture $target ('@echo 実行禁止 > "' + $marker + '"')
        $current = Join-Path $root 'apps/engine-package/current'
        New-Item -ItemType Junction -Path $current -Target $versionPath | Out-Null
        foreach ($name in @('alpha', 'beta')) {
            $shimPath = Join-Path $root ('shims/' + $name + '.exe')
            New-Item -ItemType Directory -Path (Split-Path -Parent $shimPath) -Force | Out-Null
            Copy-Item -LiteralPath $probe -Destination $shimPath
            Write-Fixture (Join-Path $root ('shims/' + $name + '.shim')) ('path = "' + (Join-Path $current 'engine.cmd') + '"' + "`r`nargs = --token fixture-secret-do-not-report")
        }
    }
    Write-Fixture (Join-Path $userRoot 'apps/gui/current/scoop-manifest.json') '{"version":"3.0","shortcuts":[["gui.exe","模擬GUI"]]}'
    Write-Fixture (Join-Path $userRoot 'apps/data/current/scoop-manifest.json') '{"version":"1.0"}'
    Write-Fixture (Join-Path $external 'alpha.cmd') ('@echo 実行禁止 > "' + $marker + '"')
    foreach ($name in @('outsider', 'scoop', 'npm', 'uv')) {
        Copy-Item -LiteralPath $probe -Destination (Join-Path $external ($name + '.exe'))
    }
    $binaryHash = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash
    $before = Join-Path $records '前の 記録.json'
    $initial = Invoke-Product @('scan', '--json', '--output', $before) 0 'complete' $before
    $alpha = @($initial.Json.commands | Where-Object name -CEQ 'alpha')[0]
    Assert-Condition ($initial.Json.packages.Count -eq 4 -and $initial.Json.commands.Count -eq 2) 'パッケージと公開名の区別が不正です。'
    Assert-Condition ($alpha.candidates.Count -eq 3 -and $alpha.package_ids.Count -eq 2 -and $alpha.version -ceq '1.2.3') '同名導入または隠れた候補の照合が不正です。'
    Assert-Condition ($alpha.preferred_path -eq (Join-Path $userRoot 'shims/alpha.exe') -and $alpha.actual_path -eq (Join-Path $userRoot 'apps/engine-package/1.2.3/engine.cmd')) '優先候補またはjunction経由の実体が不正です。'
    Assert-Condition ($alpha.candidates[2].actual_path -eq (Join-Path $globalRoot 'apps/engine-package/2.0.0/engine.cmd')) '後順位の共通導入の実体が不正です。'
    Assert-Condition (@($initial.Json.packages | Where-Object kind -CEQ 'gui').Count -eq 1 -and @($initial.Json.packages | Where-Object kind -CEQ 'no_commands').Count -eq 1) 'コマンドを公開しないパッケージの分類が不正です。'
    Write-Output '受入条件1〜3: 別名・複数コマンド・両導入先・junction・優先候補と後順位を確認しました。'

    $summaryRecord = Join-Path $records '要約と同時保存した詳細.json'
    $summary = Invoke-Product @('scan', '--command', 'alpha', '--json-summary', '--output', $summaryRecord) 0 'complete' $summaryRecord
    Assert-Condition ($summary.Json.output_format -ceq 'summary' -and -not $summary.Json.ContainsKey('packages')) '要約と詳細記録を区別できません。'
    Assert-Condition (($summary.Json.commands[0] | ConvertTo-Json -Depth 50 -Compress) -ceq ($alpha | ConvertTo-Json -Depth 50 -Compress)) '要約で導入版・根拠・候補の優先順位・起動未確認を失っています。'
    $null = Compare-Records $summaryRecord $summaryRecord 0 'unchanged'
    $summaryOnly = Save-Record '比較に使えない表示用要約.json' $summary.Json
    Assert-Issue (Compare-Records $summaryOnly $summaryOnly 2 'incomplete') 'invalid_record'

    $missing = Invoke-Product @('scan', '--command', 'missing', '--json') 1 'missing'
    Assert-Condition ($missing.Json.commands[0].status -ceq 'not_found' -and $missing.Json.commands[0].package_ids.Count -eq 0) '未導入の名前の扱いが不正です。'
    $summaryMissing = Invoke-Product @('scan', '--command', 'missing', '--json-summary') 1 'missing'
    Assert-Condition ($summaryMissing.Json.commands[0].status -ceq 'not_found') '要約で名前不足を失っています。'
    $unknown = Invoke-Product @('scan', '--command', 'outsider', '--json') 0 'complete'
    Assert-Condition ($unknown.Json.commands[0].status -ceq 'resolved' -and $null -eq $unknown.Json.commands[0].version -and $unknown.Json.commands[0].version_reason.Length -gt 0) '外部コマンドの版不明の扱いが不正です。'
    $summaryUnknown = Invoke-Product @('scan', '--command', 'outsider', '--json-summary') 0 'complete'
    Assert-Condition ($summaryUnknown.Json.commands[0].status -ceq 'resolved' -and $null -eq $summaryUnknown.Json.commands[0].version -and $summaryUnknown.Json.commands[0].version_reason.Length -gt 0) '要約で版不明を失っています。'
    $originalPath = $processEnvironment.PATH
    $processEnvironment.PATH = $external
    try {
        $offPath = Invoke-Product @('scan', '--command', 'beta', '--json') 1 'missing'
        Assert-Condition ($offPath.Json.commands[0].status -ceq 'not_found' -and $offPath.Json.commands[0].package_ids.Count -eq 2) 'PATH外の導入情報を失っています。'
    }
    finally { $processEnvironment.PATH = $originalPath }
    Write-Output '受入条件4: 未導入・PATH外の導入・版不明を区別しました。'

    $manifestPath = Join-Path $userRoot 'apps/engine-package/1.2.3/scoop-manifest.json'
    $originalManifest = [IO.File]::ReadAllText($manifestPath)
    $lockedFile = [IO.File]::Open($manifestPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
    $lockedPath = $manifestPath
    try {
        $locked = Invoke-Product @('scan', '--command', 'alpha', '--command', 'missing', '--json') 2 'incomplete'
        Assert-Issue $locked 'unreadable_manifest'
        Assert-Condition (@($locked.Json.commands | Where-Object status -CEQ 'resolved').Count -eq 1) '読取拒否で読めた候補を失っています。'
        $summaryLocked = Invoke-Product @('scan', '--command', 'alpha', '--command', 'missing', '--json-summary') 2 'incomplete'
        Assert-Issue $summaryLocked 'unreadable_manifest'
        Assert-Condition (($summaryLocked.Json.commands | ConvertTo-Json -Depth 50 -Compress) -ceq ($locked.Json.commands | ConvertTo-Json -Depth 50 -Compress)) '要約で読取拒否・名前不足と判明した結果を区別できません。'
    }
    finally { $lockedFile.Dispose(); $lockedPath = '' }
    Assert-Condition ([IO.File]::ReadAllText($manifestPath) -ceq $originalManifest) '読取拒否の確認でマニフェストを変更しています。'
    try {
        Write-Fixture $manifestPath '{"version":'
        $broken = Invoke-Product @('scan', '--command', 'alpha', '--json') 2 'incomplete'
        Assert-Issue $broken 'invalid_manifest'
        Assert-Condition ($broken.Json.commands[0].status -ceq 'resolved') '破損情報で候補を失っています。'
    }
    finally { Write-Fixture $manifestPath $originalManifest }
    Write-Output '受入条件6: 破損と実際の読取拒否を報告し、読めた事実を保持しました。'

    $reordered = ConvertFrom-Json -InputObject $initial.Out -AsHashtable
    $reordered.captured_at = '2025-01-01T00:00:00Z'
    $reordered.metadata = @{ token = 'fixture-secret-do-not-report' }
    foreach ($array in @($reordered.packages, $reordered.commands, $reordered.coverage.scoop_roots, $reordered.coverage.search_paths)) { [array]::Reverse($array) }
    foreach ($command in $reordered.commands) { [array]::Reverse($command.candidates); [array]::Reverse($command.package_ids) }
    foreach ($package in $reordered.packages) { [array]::Reverse($package.commands); [array]::Reverse($package.sources) }
    $reorderedPath = Save-Record '順序と日時だけの違い.json' $reordered
    $null = Compare-Records $before $reorderedPath 0 'unchanged'
    $updatedManifest = ConvertFrom-Json -InputObject $originalManifest -AsHashtable
    $updatedManifest.version = '1.2.4'
    Write-Fixture $manifestPath ($updatedManifest | ConvertTo-Json -Depth 10)
    $after = Join-Path $records '導入版変更.json'
    $null = Invoke-Product @('scan', '--json', '--output', $after) 0 'complete' $after
    $versionChange = Compare-Records $before $after 1 'changed'
    Assert-Condition (@($versionChange.Json.changes | Where-Object kind -CEQ 'version_changed').Count -gt 0) '導入版の変更を検出していません。'
    $processEnvironment.PATH = (Join-Path $globalRoot 'shims') + ';' + $external + ';' + (Join-Path $userRoot 'shims')
    $priority = Join-Path $records '優先順変更.json'
    $null = Invoke-Product @('scan', '--json', '--output', $priority) 0 'complete' $priority
    $priorityChange = Compare-Records $after $priority 1 'changed'
    Assert-Condition (@($priorityChange.Json.changes | Where-Object kind -CEQ 'priority_changed').Count -gt 0 -and @($priorityChange.Json.changes | Where-Object kind -CEQ 'actual_path_changed').Count -gt 0) '優先順または実体の変更を検出していません。'
    $processEnvironment.PATH = $originalPath
    Write-Output '受入条件7: 順序と日時の違いを無視し、導入版・実体・PATH優先順の変更を検出しました。'

    $partial = Join-Path $records '明示名だけ.json'
    $null = Invoke-Product @('scan', '--command', 'alpha', '--json', '--output', $partial) 0 'complete' $partial
    $scope = Compare-Records $before $partial 2 'incomplete'
    Assert-Issue $scope 'coverage_mismatch'
    Assert-Condition ($scope.Json.coverage.common.commands.Count -eq 1 -and $scope.Json.coverage.common.commands[0] -ceq 'alpha' -and $scope.Json.changes.Count -gt 0) '範囲違いで共通範囲または既知の変更を失っています。'
    $unsupported = ConvertFrom-Json -InputObject $initial.Out -AsHashtable
    $unsupported.schema_version = 2
    $unsupportedPath = Save-Record '未対応版.json' $unsupported
    Assert-Issue (Compare-Records $before $unsupportedPath 2 'incomplete') 'unsupported_schema_version'
    $corruptPath = Join-Path $records '破損.json'
    Write-Fixture $corruptPath '{"fixture-secret-do-not-report":'
    Assert-Issue (Compare-Records $before $corruptPath 2 'incomplete') 'invalid_record'
    $unknownPath = Save-Record '版不明.json' $unknown.Json
    Assert-Issue (Compare-Records $unknownPath $unknownPath 2 'incomplete') 'unknown_version'
    $recordHash = (Get-FileHash -LiteralPath $after -Algorithm SHA256).Hash
    $lockedFile = [IO.File]::Open($after, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
    $lockedPath = $after
    try { Assert-Issue (Compare-Records $before $after 2 'incomplete') 'unreadable_record' }
    finally { $lockedFile.Dispose(); $lockedPath = '' }
    Assert-Condition ((Get-FileHash -LiteralPath $after -Algorithm SHA256).Hash -ceq $recordHash) '読取拒否の確認で入力記録を変更しています。'
    Write-Output '受入条件8: 共通範囲・未対応版・破損JSON・読取拒否・版不明を確認しました。'

    $overwrite = Invoke-Product @('scan', '--command', 'missing', '--json', '--output', $before) 2 'incomplete'
    Assert-Issue $overwrite 'save_failed'
    Assert-Condition ($overwrite.Json.commands[0].status -ceq 'not_found') '保存失敗で不足の事実を失っています。'
    $noParent = Join-Path $records '未作成/記録.json'
    Assert-Issue (Invoke-Product @('scan', '--json', '--output', $noParent) 2 'incomplete') 'save_failed'
    foreach ($arguments in @(@('scan', '--command', '--json'), @('diff', '--before', $before, '--json'), @('scan', '--command', '--json-summary'), @('scan', '--json', '--json-summary'))) {
        $null = Invoke-Product $arguments 64 'invalid_arguments'
    }
    $invalid = Invoke-Product @('scan', '--unknown') 64
    Assert-Condition ($invalid.Out.Length -eq 0) '引数エラーで通常表示に余分な出力があります。'
    $humanScan = Invoke-Product @('scan') 0
    Assert-Condition ($humanScan.Out.Contains('調査: 完了') -and $humanScan.Out.Contains('未確認')) '調査の日本語表示が不正です。'
    $humanDiff = Invoke-Product @('diff', '--before', $before, '--after', $after) 1
    Assert-Condition ($humanDiff.Out.Contains('比較: 差分あり') -and $humanDiff.Out.Contains('導入版変更')) '比較の日本語表示が不正です。'
    $help = Invoke-Product @('--help') 0
    Assert-Condition ($help.Out.Contains('使い方:') -and $help.Out.Contains('envfacts scan') -and $help.Out.Contains('envfacts diff')) '使い方の表示が不正です。'
    $version = Invoke-Product @('--version') 0
    $versionSource = Get-Content -LiteralPath (Join-Path $projectRoot 'internal/cli/version.go') -Raw
    $versionMatch = [regex]::Match($versionSource, 'const Version = "([0-9]+\.[0-9]+\.[0-9]+)"')
    Assert-Condition ($versionMatch.Success -and $version.Out.Trim() -ceq ('envfacts ' + $versionMatch.Groups[1].Value)) '版番号が正本と一致しません。'
    # 比較時に再調査すると、読取不能な模擬Scoopにより成功できない状態にする。
    Write-Fixture $manifestPath '{"version":'
    $null = Compare-Records $before $before 0 'unchanged'

    Write-Fixture (Join-Path $userRoot 'apps/unrelated-first/current/scoop-manifest.json') '{"version":"1.0","env_add_path":"bin"}'
    $summaryBefore = Invoke-Product @('scan', '--command', 'alpha', '--json-summary') 2 'incomplete'
    foreach ($index in 1..100) {
        Write-Fixture (Join-Path $userRoot ('apps/unrelated-' + $index + '/current/scoop-manifest.json')) '{"version":"1.0","env_add_path":"bin"}'
    }
    $summaryAfter = Invoke-Product @('scan', '--command', 'alpha', '--json-summary') 2 'incomplete'
    $detailAfter = Invoke-Product @('scan', '--command', 'alpha', '--json') 2 'incomplete'
    Assert-Condition ($summaryAfter.Json.package_count -eq $summaryBefore.Json.package_count + 100 -and $summaryAfter.Json.issue_count -eq $summaryBefore.Json.issue_count + 100) '要約で全体の集計を失っています。'
    $aggregated = @($summaryAfter.Json.issues | Where-Object code -CEQ 'unsupported_env_add_path')
    Assert-Condition ($aggregated.Count -eq 1 -and $aggregated[0].count -eq 101) '同じ原因の警告を件数で集約していません。'
    Assert-Condition ($summaryAfter.Json.issue_count -eq $detailAfter.Json.issues.Count -and ($summaryAfter.Json.commands | ConvertTo-Json -Depth 50 -Compress) -ceq ($detailAfter.Json.commands | ConvertTo-Json -Depth 50 -Compress)) '要約と詳細の診断またはコマンドの結果が異なります。'
    Assert-Condition ($summaryAfter.Out.Length -le $summaryBefore.Out.Length + 128 -and $summaryAfter.Err.Length -le $summaryBefore.Err.Length + 32) '無関係なパッケージ数に比例して要約の出力が増えています。'
    Assert-Condition (-not ($summaryAfter.Out + $summaryAfter.Err).Contains('unrelated-')) '要約に無関係なパッケージの一覧があります。'
    Write-Output ('要約表示: 無関係なパッケージ100件の追加で標準出力 ' + [Text.Encoding]::UTF8.GetByteCount($summaryBefore.Out) + ' → ' + [Text.Encoding]::UTF8.GetByteCount($summaryAfter.Out) + 'バイト。全体の不完全・警告・名前解決結果を保持しました。')

    Assert-Condition ((Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash -ceq $binaryHash) '受入確認中に実行ファイルが変わりました。'
    Write-Output '受入条件5・9: 各操作前後の不変・対象の非実行・余分な値の非出力・保存・出力先・終了コード0/1/2/64を確認しました。'
    Write-Output ('受入確認完了: ' + $version.Out.Trim() + '、SHA-256: ' + $binaryHash)
}
finally {
    # この実行で作った、プロジェクトの.cache直下のディレクトリだけを除去する。
    $cleanupPath = [IO.Path]::GetFullPath($work)
    if ([IO.Path]::GetDirectoryName($cleanupPath) -cne $cacheRoot -or -not [IO.Path]::GetFileName($cleanupPath).StartsWith('acceptance-')) {
        throw '一時ディレクトリの除去先が想定範囲外です。'
    }
    if (Test-Path -LiteralPath $cleanupPath) { Remove-Item -LiteralPath $cleanupPath -Recurse -Force }
}
