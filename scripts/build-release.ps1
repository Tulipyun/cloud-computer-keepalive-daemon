param(
    [ValidateSet('standard', 'longtest')]
    [string]$Variant = 'standard',
    [string]$Version = 'v0.2.0',
    # Restrict the build to a subset of targets, for example @('windows-amd64').
    [string[]]$Targets
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$output = Join-Path $repo ("dist\release-{0}-{1}" -f $Version, $Variant)
New-Item -ItemType Directory -Force -Path $output | Out-Null

$prefix = if ($Variant -eq 'longtest') { 'cck-longtest' } else { 'cck-daemon' }
$allTargets = @(
    @{ Name = 'windows-amd64'; GOOS = 'windows'; GOARCH = 'amd64'; Suffix = 'windows-amd64.exe' },
    @{ Name = 'windows-arm64'; GOOS = 'windows'; GOARCH = 'arm64'; Suffix = 'windows-arm64.exe' },
    @{ Name = 'linux-amd64';   GOOS = 'linux';   GOARCH = 'amd64'; Suffix = 'linux-amd64' }
)
$targets = if ($Targets) {
    $allTargets | Where-Object { $Targets -contains $_.Name }
} else {
    $allTargets
}
if (-not $targets) {
    throw "No build targets matched: $($Targets -join ', ')"
}

# The standard variant has no diagnostics package, so only the longtest variant
# receives an embedded build label.
$ldflags = '-s -w'
if ($Variant -eq 'longtest') {
    $ldflags += " -X cloud-computer-keepalive/internal/diagnostics.BuildLabel=$Version-$Variant"
}

$previous = @{
    CGO_ENABLED = $env:CGO_ENABLED
    GOOS = $env:GOOS
    GOARCH = $env:GOARCH
}

try {
    $env:CGO_ENABLED = '0'
    foreach ($target in $targets) {
        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH
        $name = "{0}-{1}-{2}" -f $prefix, $Version, $target.Suffix
        $path = Join-Path $output $name
        & go build -buildvcs=false -trimpath -ldflags $ldflags -o $path $repo
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $($target.GOOS)/$($target.GOARCH)"
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
    "targets=$(($targets | ForEach-Object { $_.Name }) -join ',')"
    "ldflags=$ldflags"
    "go=$(& go version)"
    "cgo=0"
    "builtAt=$([DateTimeOffset]::Now.ToString('o'))"
)
$buildInfo | Set-Content -LiteralPath (Join-Path $output 'BUILD_INFO.txt') -Encoding ascii

Get-ChildItem -LiteralPath $output -File | Sort-Object Name | Select-Object Name, Length
