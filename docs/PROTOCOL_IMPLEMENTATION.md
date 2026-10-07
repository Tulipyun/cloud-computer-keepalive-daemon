# Protocol implementation reference

This document records the verified technical path without account-specific values. It is intended as the starting point when vendor behavior changes.

## End-to-end path

```text
local config
  -> SOHO public key and sub-account password login
  -> SOHO cloud PC list
  -> SOHO getFirmAuth
  -> ZTE sysConfig/getToken/getDesktopList/startDesktop
  -> decode connectStr
  -> CAG TCP/TLS, or UDP/KCP fallback
  -> CAG mux add-link
  -> raw SPICE main REDQ and authentication
  -> seven SPICE subchannel REDQs and authentication
  -> display initialization and readiness evidence
  -> SOHO heartbeat plus continuous SPICE automatic replies
```

## SOHO layer

Base request behavior is implemented in `internal/soho/soho.go`.

Sub-account login sequence:

1. `/login/publicKey/v1` obtains the password-encryption key material.
2. The password is encrypted according to the SOHO login protocol.
3. The login JSON uses field `subAccount` and is RSA chunk-encrypted.
4. `/login/home/namePwdLogin/v1` returns the SOHO token and user ID.

Cloud discovery:

- `/cc/cloudPc/list/v6` uses the authenticated SOHO headers.
- The selected `userServiceId` is persisted and reused on automatic re-login.
- `/cc/getFirmAuth/v1` is called with `{"userServiceId":"..."}` after RSA encryption.

Long-running heartbeat:

- Endpoint: `/cc/cloudPc/heartbeat/v2`
- Body before encryption: `{"userServiceId":"..."}`
- Accepted business codes: `2000` and `4041`
- Interval: approximately 25 seconds
- Failure threshold: three consecutive failures

The tested sub-account route returns ZTE fields rather than `scAuthCode`:

- `vmUserName`, `vmPassword`, `vmId`
- `vmcIp`, `vmcPort`
- `cagIp`, `cagPort`

If `scAuthCode` exists, the legacy SCG/CEM route remains available.

## ZTE VMC/CAG HTTP layer

Implementation: `internal/zte/client.go` and `internal/zte/security.go`.

Compatibility constants currently mirror the observed official client profile:

- client version: `V7.24.11`
- `requestFrom=2`
- language: `zh`
- response security: `RspSecurity=1`
- HTTP/1.1 is forced and the observed private certificate chain is accepted

API order:

1. `/cs/cs_sysConfig.action`
2. `/cs/cs_getToken.action`
3. `/cs/cs_getDesktopList.action`
4. `/cs/cs_startDesktop.action`
5. `/cs/cs_startDesktop_async_query.action` only when the initial response has no `connectStr`

Important get-token parameters include `encrypt=4`, `newVersionCtrl=1`, `netflags=1`, `unityType=1` and `isvm=0`. The JSON body identifies a Windows-style client profile with `clienttype=0`, `hardware=4`, `nettype=2` and `ostype=1`.

`startDesktop` carries the access token, selected VM ID, desktop relation fields, terminal serial, network profile, protocol version and the compatibility flags observed from the official client. Consult `startDesktopBody` before changing these fields; seemingly cosmetic values may participate in server-side policy checks.

## Decoded connect parameters

The encrypted `connectStr` is decoded and parsed by `internal/zte/connect_params.go`.

Required or used arguments:

| Argument | Meaning |
| --- | --- |
| `-h` | target SPICE host, usually a private VM address |
| `-p` | target SPICE port |
| `-k` | SPICE/CAG connection key |
| `--vmid` | VM identifier |
| `--accessToken` | ZTE access token |
| `--proxy-sport` | CAG proxy service port used in authentication |
| `--vmip` | VM address metadata when present |

The original decoded command line is sensitive and must never be logged or committed in full.

## CAG transport and authentication

Implementations:

- TCP/TLS: `internal/zte/cag_tcp.go`
- UDP/KCP fallback: `internal/zte/cag.go`
- multiplexing: `internal/zte/cag_mux.go`
- add-link framing: `internal/zte/cag_proxy.go`

TCP/TLS is attempted first because it was the path validated by the four-day test. UDP/KCP remains a fallback for environments where TCP CAG access fails.

Observed outer authentication packet types:

- `0x06`: client authentication head
- `0x07`: server head acknowledgement
- `0x08`: client authentication payload containing the required VM/proxy fields
- `0x09`: server authentication result

After authentication, the same secure connection carries CAG mux frames. Add-link command `0x1a` creates a logical channel. Close-link command `0x2a` terminates it. Each link has a link UUID plus trace/span identifiers; subchannels reuse the main connection trace relationship.

## SPICE main and subchannels

Implementation: `internal/spice/raw.go` and the ZTE section of `cmd/keepalive.go`.

Main flow:

1. Open CAG mux link 1.
2. Send the ZTE raw main `REDQ` containing key, VM ID, link UUID and trace fields.
3. Validate the REDQ reply and complete SPICE ticket authentication.
4. Read MAIN_INIT and obtain the SPICE connection/session ID.
5. Send client information and ATTACH_CHANNELS.

Seven subchannels are opened and authenticated:

| CAG link | SPICE channel type | Channel ID |
| --- | --- | --- |
| 3 | 4 | 1 |
| 2 | 6 | 0 |
| 4 | 5 | 0 |
| 6 | 3 | 0 |
| 7 | 2 | 0 |
| 8 | 4 | 0 |
| 5 | 2 | 1 |

Links 5 and 7 are tracked as display channels. DISPLAY_INIT is sent to the authenticated display targets. The session is considered ready only after MARK (`0x66`), SURFACE_CREATE (`0x13a`) or DRAW_COPY (`0x130`) is observed. A 30-second readiness timeout prevents MAIN_INIT-only false positives.

## Long-session message handling

Each raw channel owns independent ACK state:

- server PING `0x04` -> client PONG `0x03` with the same payload
- server SET_ACK `0x03` -> client ACK_SYNC `0x01` containing the generation
- after the negotiated ACK window -> client ACK `0x02`
- server ZTE heartbeat `0x74` -> client `0x79` with one-byte body `0x00`

Message `0x75` was observed near disconnect handling and is not the reply to `0x74`.

All authenticated subchannels are continuously drained. A write failure, malformed frame, EOF or closure of all display channels ends the current session and enters classified retry handling.

## Subchannel connection identifier

Each subchannel REDQ carries a `connectionID` at payload offset `16..20`. It must be
byte-identical to the identifier the server assigns to the main channel.

The assignment is visible in the main-channel `MAIN_INIT` (`0x67`) payload as a
little-endian `uint32` at offset `5..9`. `RawMainHandshake` reports it as
`SpiceSessionID`, and `BuildZTERawChannelREDQ` writes it back into every subchannel
REDQ unchanged.

Do not compute this value from the RSA-key reply and do not re-encode it with a byte
swap. The earlier implementation searched `MAIN_INIT` for a `02 00 00 00 01` anchor
and read the four bytes preceding it; the 2026-10 server profile does not emit that
anchor, and the fallback `payload[3:7]` produced a two-byte-shifted value that the
server rejected. See `DIAGNOSIS_20261007.md` at the workspace root and the regression
test in `internal/spice/raw_maininit_test.go`.

## Subchannel acceptance on the current server profile

| CAG link | SPICE channel type | Channel ID | Current server |
| --- | --- | --- | --- |
| 3 | 4 | 1 | closed with `0x2a` |
| 2 | 6 | 0 | authenticated |
| 4 | 5 | 0 | authenticated |
| 6 | 3 | 0 | authenticated |
| 7 | 2 | 0 | authenticated (display, MARK + SURFACE_CREATE) |
| 8 | 4 | 0 | authenticated |
| 5 | 2 | 1 | closed with `0x2a` |

Links 3 and 5 carry `channelID=1`. The server closes them with a close-link frame
before the client ticket is written. The remaining five links authenticate, display
readiness is reached through link 7, and the session stays healthy. The same close
frames are present in the 2026-07 recording, where the older client mis-counted them
as successes.

## Retry model

Implementation: `cmd/failure.go` and `cmd/daemon.go`.

| Class | Typical cause | Action |
| --- | --- | --- |
| authentication | expired token, invalid session, login rejection, SOHO code `4015` | refresh login, short backoff |
| maintenance | platform maintenance, HTTP 502/503/504 | slower backoff, cap 5 minutes |
| network | timeout, reset, EOF, dial failure | exponential backoff, cap 2 minutes |
| protocol | malformed frame, readiness failure, missing required fields | separate backoff, cap 2 minutes |
| local config | missing account, unreadable config, unavailable password | stop blind retry |

Retry delays include jitter. A session stable for more than 10 minutes resets accumulated failure counts. Retry waits can be interrupted with `Ctrl+C`.

## Credential handling

Runtime configuration is `config.json` in the current working directory. Plaintext password fields are migrated into `sub_password_box` and cleared.

`sub_password_box` uses:

- scrypt: `N=32768`, `r=8`, `p=1`, 32-byte key
- key material: application label, GOOS, hostname, current username and random salt
- AES-GCM with a random nonce

The encrypted value is intended for unattended restart on the same machine and system user, not portable secret storage.

## Diagnostic variant

The diagnostic branch adds:

- rotating `runtime.log` with DEBUG detail
- `events.jsonl` for route, heartbeat, readiness, retry and health snapshots
- `packets.jsonl` with sequence, direction, layer, channel, type, size, SHA256 and bounded head/tail data
- a 512-packet rolling window with up to 8192 stored bytes per incident record
- goroutine and runtime snapshots

Logs rotate at 64 MB with five backups. They contain private protocol material and must remain local.
