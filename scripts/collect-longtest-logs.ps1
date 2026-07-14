param(
    [string]$Session = ""
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$logs = Join-Path $root "logs"

if (-not (Test-Path -LiteralPath $logs)) {
    throw "No logs directory found: $logs"
}

if ($Session) {
    $source = Get-Item -LiteralPath $Session
} else {
    $source = Get-ChildItem -LiteralPath $logs -Directory |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
}

if (-not $source) {
    throw "No diagnostic session directory found"
}

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$destination = Join-Path $root ("longtest-logs-{0}-{1}.zip" -f $source.Name, $stamp)
Compress-Archive -LiteralPath $source.FullName -DestinationPath $destination -CompressionLevel Optimal
Write-Output $destination
