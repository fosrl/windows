# Production Build Scripts

These scripts help you build and prepare update manifests for production releases.

## Prerequisites

Install the required tools:

- **signify**: For Ed25519 key generation and signing
  - macOS: `brew install signify-osx`
  - Linux: Install from your distribution's package manager
  - Windows: Use WSL or download from OpenBSD

- **b2sum**: For BLAKE2b hash generation
  - macOS: `brew install coreutils` (provides `b2sum`)
  - Linux: Usually pre-installed
  - Windows: Use WSL or download from a coreutils package

- **WiX Toolset**: For building MSI installers (Windows only)
  - Download from: https://github.com/wixtoolset/wix/releases/
  - The MSI build uses `WixToolset.Util.wixext` for upgrade migration custom actions (included with WiX v4)

## Initial Setup

### 1. Generate Signing Keys (One-time)

Important: Skip this step if you already have signing keys.

```bash
./scripts/generate-keys.sh signing-keys
```

This creates:

- `signing-keys/release.pub` - Public key
- `signing-keys/release.sec` - Secret key (keep this secure!)

### 2. Extract Public Key for constants.go

Important: Skip this step if you've already set the public key in `constants.go`.

```bash
./scripts/extract-public-key.sh signing-keys/release.pub
```

Copy the output and update `updater/constants.go`:

```go
const (
    releasePublicKeyBase64 = "<paste-key-here>"
    // ... other constants
)
```

## Production Release Workflow

### 1. Set Version Number

```bash
./scripts/set-version.sh 1.0.3
```

This updates both `version/version.go` and `pangolin.wxs` with the new version.

### 2. Build the Application

```bash
make build
```

This creates `build/Pangolin.exe`.

### 3. Build MSI Installers

```bash
# Windows
scripts\build-msi.bat
```

### 4. Add Version to File Name

Rename the generated MSI files to include the version number:

```
build\pangolin-amd64-<version>.msi
```

### 4. Generate Manifest

```bash
# Auto-detect all MSI files in build/
./scripts/generate-manifest.sh

# Or specify files manually
./scripts/generate-manifest.sh build/pangolin-amd64-1.0.1.msi
```

This creates `build/filelist.txt` with BLAKE2b-256 hashes of all MSI files.

### 5. Sign Manifest

```bash
./scripts/sign-manifest.sh signing-keys/release.sec
```

This creates `build/latest.sig` (the signed manifest).

### 6. Upload to Update Server

Upload the following files to your update server:

1. **`build/latest.sig`** → Upload to path specified in `updater/constants.go` (`latestVersionPath`)
   - Example: `/windows-client/latest.sig`

2. **MSI files** → Upload to path specified in `updater/constants.go` (`msiPath`)
   - Example: `/windows-client/pangolin-amd64-1.0.1.msi`
