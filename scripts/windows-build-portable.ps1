param(
    [Parameter(Mandatory)][string]$Resources,
    [Parameter(Mandatory)][string]$Output,
    [string]$GlyphModel,
    [string]$ExpectedGlyphSHA256
)
$ErrorActionPreference = 'Stop'
$source = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$resourceRoot = (Resolve-Path -LiteralPath $Resources).Path
$release = (New-Item -ItemType Directory -Path $Output).FullName
$resourceFiles = @(
    'models/PP-OCRv6_rec_small.onnx',
    'onnxruntime/onnxruntime.dll',
    'onnxruntime/onnxruntime_providers_shared.dll',
    'onnxruntime/LICENSE',
    'onnxruntime/ThirdPartyNotices.txt',
    'THIRD-PARTY-NOTICES.md',
    'licenses'
)
foreach ($path in $resourceFiles) {
    if (!(Test-Path -LiteralPath (Join-Path $resourceRoot $path))) {
        throw "Required resource is missing: $path"
    }
}
foreach ($path in 'models', 'onnxruntime', 'THIRD-PARTY-NOTICES.md', 'licenses') {
    Copy-Item -LiteralPath (Join-Path $resourceRoot $path) -Destination $release -Recurse
}
$name = 'Youkai Storage Wrap Scroll QA'
$exe = Join-Path $release "$name.exe"
$overlay = ''
$buildArgs = @('build', '-buildvcs=false', '-mod=readonly', '-trimpath', '-tags', 'mygo_noinspector,mygo_nodownload,page_unknown_trace')
if ($GlyphModel) {
    $model = (Resolve-Path -LiteralPath $GlyphModel).Path
    if ($ExpectedGlyphSHA256 -notmatch '^[0-9a-fA-F]{64}$' -or (Get-FileHash -LiteralPath $model).Hash -ine $ExpectedGlyphSHA256) {
        throw 'Local glyph model SHA-256 is missing or differs'
    }
    $overlay = Join-Path ([IO.Path]::GetTempPath()) ('youkai-overlay-' + [Guid]::NewGuid().ToString('N') + '.json')
    $target = Join-Path $source 'internal/game/zzz/collect/storage_card_glyph_model.go'
    @{ Replace = @{ $target = $model } } | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $overlay -Encoding utf8NoBOM
    $buildArgs += @('-overlay', $overlay)
}
$ld = "-H=windowsgui -X github.com/egoist/mygo.production=1 -X 'github.com/egoist/mygo.packageName=$name' -X github.com/egoist/mygo.packageIdentifier=com.lotwt.youkai.storageqa.wrapscroll -X github.com/egoist/mygo.packageVersion=0.0.1"
$buildArgs += @('-ldflags', $ld, '-o', $exe, './cmd/youkai')
Push-Location $source
try {
    & go @buildArgs
    if ($LASTEXITCODE) { throw "Go build failed: $LASTEXITCODE" }
} finally {
    Pop-Location
    if ($overlay) { Remove-Item -LiteralPath $overlay -ErrorAction SilentlyContinue }
}
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'windows-start-storage.cmd') -Destination (Join-Path $release 'Start-Youkai.cmd')
Get-FileHash -LiteralPath $exe
