# Build and release procedure

## Source variants

Standard source (`main`):

```powershell
git switch main
git describe --tags --always
```

Diagnostic source (`codex/v0.2-longtest-diagnostics`):

```powershell
git switch codex/v0.2-longtest-diagnostics
git describe --tags --always
```

Do not build a standard-named binary from the diagnostic branch or a longtest-named binary from the standard branch. The build script names the current checkout but does not alter its behavior. Each release must keep the fix commits present on **both** branches; the diagnostic branch is `main` plus the instrumentation, so synchronize changes with `git cherry-pick`.

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

Standard (clean, no log collection):

```powershell
.\scripts\build-release.ps1 -Variant standard -Version v0.2.1
```

Diagnostic (`cck-longtest-*`, writes logs, packet journals and incidents):

```powershell
git switch codex/v0.2-longtest-diagnostics
.\scripts\build-release.ps1 -Variant longtest -Version v0.2.1
```

Pass `-Targets` to limit the build to a subset of platforms:

```powershell
.\scripts\build-release.ps1 -Variant standard -Version v0.2.1 -Targets windows-amd64, windows-arm64
```

Each command produces:

- Windows amd64
- Windows arm64
- Linux amd64
- `SHA256SUMS.txt`
- `BUILD_INFO.txt`

All targets use `CGO_ENABLED=0`, `-buildvcs=false`, `-trimpath` and linker flags `-s -w`. The exact source commit remains recorded in `BUILD_INFO.txt` without making documentation-only commits change the executable bytes. The diagnostic variant additionally receives

```text
-X cloud-computer-keepalive/internal/diagnostics.BuildLabel=<version>-longtest
```

so the label recorded in `session.json` matches the published asset name.

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

Private GitHub Release `v0.2.1` contains two Windows binaries per variant:

| Asset | Variant | Architecture |
| --- | --- | --- |
| `cck-daemon-v0.2.1-windows-amd64.exe` | standard | x64 |
| `cck-daemon-v0.2.1-windows-arm64.exe` | standard | ARM64 |
| `cck-longtest-v0.2.1-windows-amd64.exe` | longtest | x64 |
| `cck-longtest-v0.2.1-windows-arm64.exe` | longtest | ARM64 |

plus one `SHA256SUMS.txt` and one `BUILD_INFO.txt` per variant.

The standard variant is built from `main`; the longtest variant is built from
`codex/v0.2-longtest-diagnostics`. Both branches carry the subchannel session-ID fix, so
the two variants differ only in diagnostic capture. The release notes should link to
`PROJECT_STATE.md` and `docs/PROTOCOL_IMPLEMENTATION.md`.
