# v0.2 long-test diagnostic build

Build label: `v0.2.0-longtest-1`

This temporary build is based on the v0.2 protocol-hardening candidate. It adds extensive local diagnostics for an unattended soak test. It does not enable the unverified synthetic 21 Hz display traffic.

## Before starting

- Put the executable and `config.json` in a stable working directory.
- Start the executable from that directory.
- Do not run another client or keepalive process for the same account during the test.
- Make sure the disk has at least 2 GB free.
- Diagnostic logs contain private network endpoints and protocol material. Do not publish them or attach them to a public issue.

## Starting the test

Windows x64:

```powershell
.\cck-longtest-v0.2.0-windows-amd64.exe
```

Windows ARM64:

```powershell
.\cck-longtest-v0.2.0-windows-arm64.exe
```

Linux x64:

```bash
chmod +x ./cck-longtest-v0.2.0-linux-amd64
./cck-longtest-v0.2.0-linux-amd64
```

Build checksums:

- `cck-longtest-v0.2.0-windows-amd64.exe`
  - SHA256: `239BD956C384417DD3A03BA7DF5712902993AED36B931E58B763D32DD29F5341`
- `cck-longtest-v0.2.0-windows-arm64.exe`
  - SHA256: `849241F56D9A326356A8C5D1E6AAFE3204402713B04B6D1EC254214BD5175F50`
- `cck-longtest-v0.2.0-linux-amd64`
  - SHA256: `1C9E2F1186C55FD8797B6D64F7B4CAD535D2DEC7CD2437F5A05354F08BC0ADEE`

The program continues reconnecting according to the v0.2 classified retry policy. Leave the process running after an error so the logs include the recovery attempt and subsequent protocol traffic.

Use `Ctrl+C` for a normal stop. Avoid killing the process unless it is completely unresponsive.

## Log directory

Every start creates a new directory:

```text
logs/session-YYYYMMDD-HHMMSS.mmm/
```

Files:

- `session.json`: build, OS, architecture, process ID, and start time.
- `runtime.log`: full DEBUG runtime log; console remains at INFO level.
- `events.jsonl`: structured route, heartbeat, readiness, retry, and health events.
- `packets.jsonl`: packet direction, layer, channel, type, size, SHA256, and short head/tail samples.
- `incidents/incident-*.json`: error time, classification, goroutine dump, and the rolling packet window immediately before the error.

`runtime.log`, `events.jsonl`, and `packets.jsonl` rotate at 64 MB and retain five backups each. The maximum retained diagnostic volume is approximately 1.2 GB.

## Observation points already included

- firmAuth route selection and CAG transport choice
- TCP/TLS to UDP/KCP fallback
- CAG authentication and mux frames
- raw SPICE main and subchannel messages
- DISPLAY_INIT and display readiness evidence
- MARK, SURFACE_CREATE, and DRAW_COPY
- SET_ACK generation/window, ACK_SYNC, normal ACK, PING/PONG, and `0x74 -> 0x79`
- SOHO heartbeat result and consecutive failure count
- display channel closure and protocol read/write errors
- retry classification, re-login decisions, delay, and consecutive failure count
- one-minute heartbeat, ACK, display, memory, GC, and goroutine snapshots

## Returning logs for analysis

1. Stop the process with `Ctrl+C` after the test.
2. Run the included collector from the program directory:

```powershell
.\scripts\collect-longtest-logs.ps1
```

It creates a zip for the latest session without including `config.json`, credentials, or unrelated probe data.

If the process was started more than once, preserve every session covering the failure and recovery period. The most useful material is the complete session directory, not only `runtime.log`.

## What to report with the archive

- Approximate time when the remote desktop appeared offline or rebooted.
- Whether the keepalive process was still running.
- Whether an `incident-*.json` file appeared near that time.
- Whether the official client was opened during the test.
- Any network outage, sleep, VPN change, or system clock adjustment during the test.
