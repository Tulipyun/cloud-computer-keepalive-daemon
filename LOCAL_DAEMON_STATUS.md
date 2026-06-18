# Local daemon keepalive status

Last updated: 2026-06-18

## Branches

- Upstream PR branch: `codex/subaccount-login-keepalive`
- Upstream PR commit: `5297445 Add subaccount ZTE keepalive`
- Local daemon branch: `codex/local-daemon-keepalive`
- Local daemon commit before this note: `a4275b5 Add local daemon keepalive workflow`
- The local daemon branch is intentionally not pushed.

## Upstream PR state

- PR: https://github.com/sigming/cloud-computer-keepalive/pull/3
- Title: `Add sub-account login and ZTE keepalive support`
- The PR contains the reusable sub-account login and pure Go ZTE keepalive path.
- The local daemon/no-banner unattended workflow is kept out of the PR.

## Local daemon workflow

- Entry point is simplified to `main -> cmd.RunDaemon()`.
- No banner, disclaimer prompt, subcommands, or duration flags are required.
- Runtime config is loaded from `config.json` in the current working directory.
- If `config.json` is missing or incomplete, the program prompts for:
  - sub-account name
  - sub-account password
  - cloud PC selection only when more than one cloud PC is returned
- If `config.json` is present and complete, the program starts persistent keepalive directly.
- On unexpected keepalive failure, the daemon refreshes login state when needed and retries with exponential backoff capped at 5 minutes.
- If a connection had already been stable for more than 10 minutes before failing, retry backoff resets to 5 seconds.

## Credential storage

- Plaintext password is not saved.
- `config.json` stores `sub_password_box`, an AES-GCM encrypted local secret.
- The encryption key is derived locally with scrypt from:
  - application label
  - OS
  - hostname
  - current username
  - random per-secret salt
- The encrypted password is intended for unattended restart on the same OS user and machine.
- Moving `config.json` to another machine or OS user may require entering the password again.
- Old local config fields `password` and `sub_password`, if found, are migrated to the encrypted box and cleared on save.

## Keepalive/protocol state

- Sub-account login uses SOHO home sub-account password login.
- Sub-account `getFirmAuth` can return ZTE VMC/CAG fields instead of the original SCG auth code path.
- The local keepalive path uses pure Go:
  - SOHO login and cloud PC discovery
  - firm auth
  - ZTE VMC sysConfig/getToken/getDesktopList/startDesktop
  - ZTE CAG TCP/TLS first, UDP/KCP fallback
  - CAG mux proxy link setup
  - raw SPICE main-channel handshake
  - ZTE subchannel REDQ/auth probe
  - SOHO heartbeat plus raw SPICE auto-replies
- The long-lived raw SPICE session requires replying to server message `0x74` with client message `0x79` and a 1-byte zero body.
- Message `0x75` was observed around disconnect handling and should not be used as the `0x74` reply.

## Verification

- `go test ./...` passed after daemon changes.
- Cross-compile targets built successfully with `CGO_ENABLED=0`.
- Current local binaries:
  - `dist/cck-daemon-windows-amd64.exe`
    - SHA256: `061A0B245592F602B30D9C6C4222443ED2A2B34F26BF13BBA83F97CF2EB763E2`
  - `dist/cck-daemon-windows-arm64.exe`
    - SHA256: `557F9C89E3E1F9035365C350271E6D43086A154CAF20628D6CCAA87A8DACD13C`
  - `dist/cck-daemon-linux-amd64`
    - SHA256: `C4581F0BB7C54169B3D90AC5700C62064F25921FE9B4971460A93BDFA95423C8`

## Local files intentionally ignored

- `dist/`
- `config.json`
- `cloud_pc.json`
- `native_probe/`
- local build/cache/tool artifacts

## Future upgrade notes

- If the vendor protocol changes, start by checking:
  - `cmd/keepalive.go`
  - `internal/zte/*`
  - `internal/spice/raw.go`
  - ignored local probe material under `native_probe/`
- For unattended production use, consider adding:
  - log file rotation
  - Windows service/systemd unit wrapper
  - config migration versioning
  - optional passphrase-based secret mode for portable configs
