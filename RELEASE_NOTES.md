# v0.1.0 - Private daemon baseline

Date: 2026-07-14

This release establishes the first private baseline for future development.

## Included

- Pure Go sub-account password login.
- Pure Go ZTE VMC/CAG connection and desktop startup.
- CAG TCP/TLS with UDP/KCP fallback.
- Raw SPICE main and subchannel setup.
- SOHO heartbeat and SPICE protocol auto-replies.
- Persistent no-argument daemon entry point.
- Machine-bound encrypted password storage in `config.json`.
- Automatic login refresh and reconnect backoff.

## Binaries

- Windows x64: `cck-daemon-windows-amd64.exe`
- Windows ARM64: `cck-daemon-windows-arm64.exe`
- Linux x64: `cck-daemon-linux-amd64`

## Verification

- `go test ./...`
- Cross-compilation with `CGO_ENABLED=0`
- Previously verified live sub-account ZTE/SPICE session beyond 150 seconds

Runtime configuration, credentials, tokens, captures, and local probe data are not included.

