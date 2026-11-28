param(
    [Parameter(Mandatory)][string]$Executable,
    [Parameter(Mandatory)][string]$Installer,
    [Parameter(Mandatory)][ValidateSet('enabled', 'disabled')][string]$Updates,
    [string]$UpdateArchive,
    [string]$ProfilePath
)

$ErrorActionPreference = 'Stop'

function Assert-PackagedExecutable {
    param([string]$Directory, [string]$ExpectedHash, [string]$Artifact)

    $files = @(Get-ChildItem -LiteralPath $Directory -Recurse -File -Filter 'Youkai.exe')
    if ($files.Count -ne 1 -or (Get-FileHash -LiteralPath $files[0].FullName).Hash -ne $ExpectedHash) {
        throw "$Artifact must contain exactly one Youkai.exe identical to the self-tested executable"
    }
}

$exe = (Resolve-Path -LiteralPath $Executable).Path
$setup = (Resolve-Path -LiteralPath $Installer).Path
$work = [IO.Directory]::CreateTempSubdirectory('youkai-package-check-').FullName
$stderr = Join-Path $work 'stderr.txt'
$stdout = Join-Path $work 'stdout.txt'
$original = @(Get-ChildItem Env: | Where-Object { $_.Name -like 'YOUKAI_*' })
try {
    foreach ($entry in $original) {
        [Environment]::SetEnvironmentVariable($entry.Name, $null, 'Process')
    }
    $env:YOUKAI_SELFTEST = '1'
    $process = Start-Process -FilePath $exe -NoNewWindow -Wait -PassThru `
        -RedirectStandardOutput $stdout -RedirectStandardError $stderr
    $lines = @(Get-Content -LiteralPath $stderr)
    $lines | ForEach-Object { Write-Host $_ }
    if ($process.ExitCode -ne 0) {
        Get-Content -LiteralPath $stdout | ForEach-Object { Write-Host $_ }
        throw "Windows package self-test failed: exit code $($process.ExitCode)"
    }
    if (-not ($lines -match "youkai: self-test: updates=$Updates$") -or
        -not ($lines -match 'youkai: self-test passed:')) {
        throw 'The executable did not report the expected update state and successful OCR self-test'
    }
    $prefix = 'youkai: self-test: windows storage: '
    $profiles = @($lines | Where-Object { $_ -match [regex]::Escape($prefix) })
    if ($profiles.Count -ne 1) {
        throw 'The executable did not report exactly one verified Windows drive storage profile'
    }
    $profileJson = $profiles[0].Substring($profiles[0].IndexOf($prefix) + $prefix.Length)
    $profile = ConvertFrom-Json -AsHashtable -InputObject $profileJson
    if ($profile.source -cne 'drive-storage' -or $profile.model.version -cnotmatch '^[0-9a-f]{64}$' -or
        $profile.model.sha256 -cnotmatch '^[0-9a-f]{64}$' -or $profile.pageUnknownTrace -isnot [bool] -or
        $profile.pageUnknownTrace -or $profile.config.fastBadges -isnot [bool] -or $profile.config.fastBadges) {
        throw 'The executable is missing the verified badge model or uses an unsupported production configuration'
    }
    foreach ($setting in 'reuseFrameMatch', 'fastBeforeClick', 'pacedDetail', 'batchEvidence', 'savedSnapshot', 'deferWrapRead', 'reuseScrollPages') {
        if ($profile.config[$setting] -isnot [bool] -or -not $profile.config[$setting]) {
            throw "The executable did not enable the verified Windows drive storage option: $setting"
        }
    }

    $hash = (Get-FileHash -LiteralPath $exe).Hash
    $installed = (New-Item -Path (Join-Path $work 'installer') -ItemType Directory).FullName
    & 7z x $setup '-ir!Youkai.exe' "-o$installed" -y
    if ($LASTEXITCODE -ne 0) { throw 'Could not extract Youkai.exe from the NSIS installer' }
    Assert-PackagedExecutable $installed $hash 'NSIS installer'

    if ($UpdateArchive) {
        $archive = (Resolve-Path -LiteralPath $UpdateArchive).Path
        $entries = @(& tar -tf $archive)
        if ($LASTEXITCODE -ne 0) { throw 'Could not list the update archive' }
        $executables = @($entries | Where-Object { $_ -cmatch '(^|/)Youkai\.exe$' })
        if ($executables.Count -ne 1) { throw 'The update archive must contain exactly one Youkai.exe' }
        $updated = (New-Item -Path (Join-Path $work 'update') -ItemType Directory).FullName
        & tar -xf $archive -C $updated -- $executables[0]
        if ($LASTEXITCODE -ne 0) { throw 'Could not extract Youkai.exe from the update archive' }
        Assert-PackagedExecutable $updated $hash 'Update archive'
    }
    if ($ProfilePath) {
        Set-Content -LiteralPath $ProfilePath -Value $profileJson -Encoding utf8NoBOM
    }
    Write-Host "Packaged executable verified: SHA-256 $hash"
} finally {
    Remove-Item Env:YOUKAI_SELFTEST -ErrorAction SilentlyContinue
    foreach ($entry in $original) {
        [Environment]::SetEnvironmentVariable($entry.Name, $entry.Value, 'Process')
    }
    Remove-Item -LiteralPath $work -Recurse -Force
}
