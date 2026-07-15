# Build Instructions — chiller

## Prerequisites
- **Build Tool**: Go toolchain **1.23+** (verified on go1.26.4). Module: `github.com/enki-offen/chiller`.
- **Dependencies** (fetched by `go`): `gopkg.in/yaml.v3` (config), `github.com/playwright-community/playwright-go v0.5200.1` (financial auth-proxy browser session; runtime Chromium is Build/Infra, not needed to compile or to run the CI test suite — tests use `FakeSession`).
- **Environment Variables**: none required to build. Standard `GOFLAGS`/`GOCACHE` honored.
- **System Requirements**: any Linux/macOS/Windows Go supports; ~200MB for module cache + build cache.

## Build Steps

### 1. Install Dependencies
```bash
go mod download        # or: go mod tidy   (network required once)
```

### 2. Configure Environment
```bash
# No env vars required for build. Optional:
export CGO_ENABLED=0   # pure-Go static binary (chiller uses no cgo)
```

### 3. Build All Units
```bash
go build ./...                      # compile every package (domain, findings, scan, platform, cmd)
go build -o bin/chiller ./cmd/chiller   # produce the single deployable binary
```

### 4. Verify Build Success
- **Expected Output**: no output from `go build ./...` (success is silent); `bin/chiller` produced.
- **Build Artifacts**: `bin/chiller` — single binary, all 4 roles (`--role=all|manager|worker|proxy`).
- **Architecture guard** (NFR-02, run every build):
  ```bash
  make arch-check    # asserts data plane never imports platform, kernel imports no unit
  ```
- **Common Warnings**: none expected. `go vet ./...` must be clean.

## Troubleshooting

### Build fails with dependency errors
- **Cause**: offline module cache missing playwright-go (or its transitive deps).
- **Solution**: `GOFLAGS=-mod=mod go get github.com/playwright-community/playwright-go@v0.5200.1 && go mod tidy`. Pin **v0.5200.1** — v0.6100.0 declares a mismatched module path and will not resolve.

### Build fails with compilation errors after editing a seam
- **Cause**: a seam interface (`internal/platform/seams.go`) changed but an impl (`taskstore`/`workqueue`/`progress`) or the orchestrator wasn't updated.
- **Solution**: the `var _ platform.X = (*Mem)(nil)` compile-time assertions in each impl pinpoint the drift; satisfy the interface, rebuild.

### `make arch-check` fails
- **Cause**: a data-plane package (`internal/scan/...` or `internal/findings/...`) imported `internal/platform/...`.
- **Solution**: invert the dependency — the control plane depends on the data plane, never the reverse. Move the shared type into `internal/domain` if genuinely kernel-level.
