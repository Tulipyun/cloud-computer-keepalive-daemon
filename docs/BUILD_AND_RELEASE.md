# Build and release procedure

## Source variants

Standard source:

```powershell
git switch main
git describe --tags --always
```

Diagnostic source:

```powershell
git switch codex/v0.2-longtest-diagnostics
git describe --tags --always
```

Do not build a standard-named binary from the diagnostic branch or a longtest-named binary from the standard branch. The build script names the current checkout but does not alter its behavior.

## Workspace-local Go environment

The existing local workspace uses:

```powershell
$Workspace = 'C:\Users\wp\Documents\yun'
$env:PATH = (Join-Path $Workspace '.tools\go\bin') + ';' + $env:PATH
$env:GOCACHE = Join-Path $Workspace '.cache\go-build'
$env:GOPATH = Join-Path $Workspace '.cache\go'
$env:GOTMPDIR = Join-Path $Workspace '.cache\tmp'
```

Any replacement toolchain or cache must remain under the workspace.

## Verification

```powershell
go test ./...
go vet ./cmd ./internal/...
```

Ignored reverse-engineering probes are not part of the formal package set and may contain unrelated warnings.

## Build commands

Standard:

```powershell
.\scripts\build-release.ps1 -Variant standard -Version v0.2.0
```

Diagnostic:

```powershell
.\scripts\build-release.ps1 -Variant longtest -Version v0.2.0
```

Each command produces:

- Windows amd64
- Windows arm64
- Linux amd64
- `SHA256SUMS.txt`
- `BUILD_INFO.txt`

All targets use `CGO_ENABLED=0`, `-trimpath` and linker flags `-s -w`.

## Source archive

The standard tag receives GitHub's automatic source archive. Attach a separate diagnostic source archive:

```powershell
git archive --format=zip --prefix=cloud-computer-keepalive-v0.2.0-longtest-1/ `
  -o dist\release-v0.2.0-longtest\cloud-computer-keepalive-v0.2.0-longtest-1-source.zip `
  v0.2.0-longtest-1
```

## Sensitive-data checks

Before staging or publishing:

```powershell
git status --short
git ls-files
rg -n -i 'password|token|authorization|sohotoken|vmPassword|sub_password' `
  --glob '!go.sum' --glob '!docs/**' --glob '!README.md' .
```

Review matches manually. Protocol field names and test placeholders are expected; real values are not.

Confirm that none of these paths are tracked:

```powershell
git ls-files config.json cloud_pc.json logs native_probe dist
```

Inspect release asset names and SHA256 hashes. Never upload raw logs, packet captures, incident files or local configuration.

## Release layout

Private GitHub Release `v0.2.0` should contain:

- three standard binaries
- three diagnostic binaries
- one checksum manifest for each variant
- one build-info file for each variant
- diagnostic source ZIP

The release notes should link to `PROJECT_STATE.md`, `docs/PROTOCOL_IMPLEMENTATION.md` and `docs/SOAK_TEST_20260714.md`.
