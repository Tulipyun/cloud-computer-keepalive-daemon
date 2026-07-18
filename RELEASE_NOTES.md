# v0.2.0 - Protocol-hardened private baseline

Date: 2026-07-18

This release replaces the initial `v0.1.0` private baseline with the protocol-hardened implementation validated by a continuous four-day ZTE/SPICE session.

## Included

- Pure Go sub-account password login and unattended configuration flow.
- ZTE VMC desktop startup and decoded `connectStr` handling.
- CAG TCP/TLS with UDP/KCP fallback.
- CAG mux main link and seven authenticated SPICE subchannels.
- SPICE SET_ACK generation/window handling, ACK_SYNC, normal ACK and PING/PONG.
- ZTE `0x74 -> 0x79` long-session reply.
- Display readiness based on MARK, SURFACE_CREATE or DRAW_COPY evidence.
- Display channel closure propagation to the owning session.
- SOHO heartbeat business-code validation and three-failure threshold.
- Classified retry policy with jitter, re-login and stable-session counter reset.
- Machine-bound encrypted password storage in `config.json`.
- Optional long-test diagnostic source branch and binaries.

## Release assets

Standard binaries:

- `cck-daemon-v0.2.0-windows-amd64.exe`
- `cck-daemon-v0.2.0-windows-arm64.exe`
- `cck-daemon-v0.2.0-linux-amd64`

Diagnostic binaries:

- `cck-longtest-v0.2.0-windows-amd64.exe`
- `cck-longtest-v0.2.0-windows-arm64.exe`
- `cck-longtest-v0.2.0-linux-amd64`

The release also contains SHA256 manifests and a source archive for the diagnostic tag.

## Verification

- `go test ./...`: passed on both source variants.
- Formal source packages: `go vet` passed.
- Cross-compilation: Windows amd64, Windows arm64 and Linux amd64 passed with `CGO_ENABLED=0`.
- Live ZTE authentication: 7/7 SPICE subchannels.
- Display readiness: MARK and SURFACE_CREATE observed on display link 7.
- Soak test: 96 hours 6 minutes in one session without reconnect.
- Packet diagnostics: sequence `1..915895` with no gaps.
- Main-channel keepalive: 34,598 `0x74 -> 0x79` exchanges.
- Runtime: stable goroutine and memory counts; no incident or protocol failure.

## Deferred

Two isolated SOHO HTTP timeouts occurred during the soak test and recovered immediately. Decoupling SOHO HTTP heartbeat from the raw SPICE main loop remains a possible future hardening change, but is intentionally not included in this frozen release.

Runtime configuration, credentials, tokens, captures, raw diagnostic logs and local probe data are not included.

