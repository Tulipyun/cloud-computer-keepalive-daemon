param(
    [ValidateSet('standard', 'longtest')]
    [string]$Variant = 'standard',
    [string]$Version = 'v0.2.0',
    # Restrict the build to a subset of targets, for example
    #   -Targets windows-amd64,windows-arm64
    # CCK_BUILD_TARGETS is accepted as an environment fallback.
    [string[]]$Targets
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$output = Join-Path $repo ("dist\release-{0}-{1}" -f $Version, $Variant)
New-Item -ItemType Directory -Force -Path $output | Out-Null

$prefix = if ($Variant -eq 'longtest') { 'cck-longtest' } else { 'cck-daemon' }
$knownTargets = 'windows-amd64', 'windows-arm64', 'linux-amd64'

# Collect the requested target names from the parameter and the environment
# fallback, splitting any comma-separated form and dropping empty pieces.
$requested = @()
foreach ($entry in @($Targets) + @($env:CCK_BUILD_TARGETS)) {
    foreach ($piece in ("$entry" -split ',')) {
        $trimmed = $piece.Trim()
        if ($trimmed) { $requested += $trimmed }
    }
}
if ($requested.Count -eq 0) {
    $requested = $knownTargets
}

$selected = @()
foreach ($name in $requested) {
    if ($knownTargets -notcontains $name) {
        throw "Unknown target '$name'. Available: $($knownTargets -join ', ')"
    }
    if ($selected -notcontains $name) { $selected += $name }
}

Write-Output ("Building {0} {1}: {2}" -f $Variant, $Version, ($selected -join ', '))

# The standard variant has no diagnostics package, so only the longtest variant
# receives an embedded build label.
$ldflags = '-s -w'
if ($Variant -eq 'longtest') {
    $ldflags += " -X cloud-computer-keepalive/internal/diagnostics.BuildLabel=$Version-$Variant"
}

$previous = @{
    CGO_ENABLED = $env:CGO_ENABLED
    GOOS        = $env:GOOS
    GOARCH      = $env:GOARCH
}

try {
    $env:CGO_ENABLED = '0'
    foreach ($name in $selected) {
        $parts = $name -split '-'
        $env:GOOS = $parts[0]
        $env:GOARCH = $parts[1]
        $outputName = "{0}-{1}-{2}" -f $prefix, $Version, $name
        if ($env:GOOS -eq 'windows') { $outputName += '.exe' }
        $path = Join-Path $output $outputName
        & go build -buildvcs=false -trimpath -ldflags $ldflags -o $path $repo
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $name"
        }
    }
} finally {
    $env:CGO_ENABLED = $previous.CGO_ENABLED
    $env:GOOS = $previous.GOOS
    $env:GOARCH = $previous.GOARCH
}

$hashLines = Get-ChildItem -LiteralPath $output -File |
    Where-Object Name -NotIn @('SHA256SUMS.txt', 'BUILD_INFO.txt') |
    Sort-Object Name |
    ForEach-Object {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
        "$hash  $($_.Name)"
    }
$hashLines | Set-Content -LiteralPath (Join-Path $output 'SHA256SUMS.txt') -Encoding ascii

$branch = (& git -C $repo branch --show-current).Trim()
$commit = (& git -C $repo rev-parse HEAD).Trim()
$buildInfo = @(
    "version=$Version"
    "variant=$Variant"
    "branch=$branch"
    "commit=$commit"
    "targets=$($selected -join ',')"
    "ldflags=$ldflags"
    "go=$(& go version)"
    "cgo=0"
    "builtAt=$([DateTimeOffset]::Now.ToString('o'))"
)
$buildInfo | Set-Content -LiteralPath (Join-Path $output 'BUILD_INFO.txt') -Encoding ascii

Get-ChildItem -LiteralPath $output -File | Sort-Object Name | ForEach-Object { "$($_.Length)`t$($_.Name)" }
