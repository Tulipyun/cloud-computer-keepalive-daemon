# Project state and handoff

Last updated: 2026-07-18

## Stable baseline

- Private repository: `Tulipyun/cloud-computer-keepalive-daemon`
- Standard branch: `main`
- Standard tag: `v0.2.0`
- Diagnostic branch: `codex/v0.2-longtest-diagnostics`
- Diagnostic tag: `v0.2.0-longtest-1`
- Frozen fallback tag: `v0.1.0`

The standard and diagnostic variants share the same protocol implementation through the v0.2 hardening commit. The diagnostic branch adds only local runtime, event, packet and incident recording.

## Verified state

- Sub-account login does not require a mobile phone number.
- SOHO cloud list and firm-auth discovery work for the tested account type.
- ZTE VMC startup, CAG TCP/TLS, mux and raw SPICE operate without the official client or SDK.
- CAG UDP/KCP remains available as fallback.
- Seven ZTE subchannels authenticate successfully.
- Display readiness is established through link 7 MARK and SURFACE_CREATE.
- One raw SPICE session remained connected for 96 hours 6 minutes.
- No reconnect, display closure, protocol incident, panic, memory leak or goroutine leak occurred.

## Known deferred item

The SOHO HTTP request is currently executed in the same loop that reads the raw SPICE main channel. Two isolated 30-second HTTP timeouts during the soak test delayed main-channel processing to 36 and 39 seconds, but the server kept the session alive and the next heartbeat succeeded immediately.

This is documented but intentionally frozen because the event rate was extremely low and the current version is usable. If future logs show related disconnects, move SOHO heartbeat to an independent goroutine or reduce its HTTP timeout, then add a test proving that delayed SOHO responses cannot delay `0x74 -> 0x79`.

## Do not change without new capture evidence

- Do not replace `0x79` with `0x75` for server message `0x74`.
- Do not periodically resend DISPLAY_INIT or INPUT_INIT on the main channel.
- Do not enable synthetic 21 Hz display type-3 traffic from third-party implementations without confirming direction, cadence and counters from a fresh official-client capture.
- Do not add connection rotation merely because it appears in another implementation; the verified session did not require rotation.

## Local-only evidence

The workspace may contain a private soak-test archive and historical reverse-engineering material outside the Git repository. These files may contain private endpoints, VM identifiers, auth material or packet payloads. Keep them local and use the redacted summary in `docs/SOAK_TEST_20260714.md` for GitHub.

Never commit:

- `config.json` or `cloud_pc.json`
- `logs/`, incident files or packet journals
- `native_probe/` and SDK extraction material
- raw captures, account information or tokens

## Starting a future task

1. Open this repository and read `PROJECT_STATE.md`.
2. Fetch the private remote and verify tags and branches.
3. Start ordinary work from `main` or create `codex/<new-version>` from `v0.2.0`.
4. Use the diagnostic branch only when packet-level evidence is needed.
5. Run `go test ./...` before and after changes.
6. Perform a live session test, then rebuild all three target architectures.
7. Scan tracked files and release assets for credentials before pushing.

## Primary code entry points

- `cmd/daemon.go`: unattended setup, login refresh and retry scheduling.
- `cmd/keepalive.go`: SOHO/SCG/ZTE route selection and long-running session loops.
- `internal/soho/`: SOHO request signing, headers and sub-account login.
- `internal/zte/`: VMC APIs, security wrapper, CAG auth, mux and proxy links.
- `internal/spice/raw.go`: raw SPICE handshake, message parser and auto-replies.
- `internal/config/`: runtime config and machine-bound encrypted secret.
- `internal/diagnostics/`: diagnostic branch packet and incident recorder.
