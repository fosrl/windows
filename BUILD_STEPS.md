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

- **Node.js 20+ and npm**: For building the webview UI in `ui/frontend`
  - The UI is a Vite + React + TypeScript app rendered with [Wails v3](https://v3.wails.io/). `make build` builds it and embeds it into the exe.

- **wails3 CLI** (only to regenerate the TypeScript bindings after changing a bound Go service in `ui/`)
  - `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0`, then `make bindings`. On Linux, install it with `-tags gtk3` unless you have WebKitGTK 6.0.

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

Windows and Linux are released separately, each from its own tag and with its
own GitHub release.

CI (`.github/workflows/windows.yml`) does all of this when a `windows-` tag is
pushed: it builds `Pangolin.exe`, signs it with the DigiCert certificate
(jsign), builds and signs `pangolin-amd64-<version>.msi`, makes and signs the
update manifest, and attaches both to a draft GitHub release. Push a tag named
`windows-` and the version (e.g. `windows-1.0.3` or `windows-1.0.3-rc.1`).
Nothing in the repo holds the version: CI strips the prefix, stamps the rest
into the binary with `-ldflags` (`make build VERSION=1.0.3`) and passes it to
WiX (`-d Version=...`, without any `-rc.N`). A build without `VERSION` reports
itself as `dev`.

After publishing the release, upload `windows-latest.sig` to the update server
as `/windows-client/latest.sig`.

The steps below are the manual equivalent.

### 1. Build the Application

```bash
make build VERSION=1.0.3
```

This builds the frontend (`ui/frontend/dist`) and creates `build/Pangolin.exe`
with the version set in `version.Number`.

To preview the UI in a desktop browser without Windows, run `npm run mock` in `ui/frontend` and open `http://localhost:5173/#/tray`, `#/preferences`, `#/login` or `#/progress?kind=update`. Mock mode swaps the Go services for fake data.

### 2. Build MSI Installers

```bash
# Windows
scripts\build-msi.bat 1.0.3
```

This creates `build\pangolin-amd64-1.0.3.msi`.

### 3. Generate Manifest

```bash
# Auto-detect all MSI files in build/
./scripts/generate-manifest.sh

# Or specify files manually
./scripts/generate-manifest.sh build/pangolin-amd64-1.0.1.msi
```

This creates `build/filelist.txt` with BLAKE2b-256 hashes of all MSI files.

### 4. Sign Manifest

```bash
./scripts/sign-manifest.sh signing-keys/release.sec
```

This creates `build/latest.sig` (the signed manifest).

### 5. Upload to Update Server

Upload the following files to your update server:

1. **`build/latest.sig`** → Upload to path specified in `updater/constants.go` (`latestVersionPath`)
   - Example: `/windows-client/latest.sig`

2. **MSI files** → Upload to path specified in `updater/constants.go` (`msiPath`)
   - Example: `/windows-client/pangolin-amd64-1.0.1.msi`

## Linux Release Workflow

Linux packages are built by CI (`.github/workflows/linux.yml`) when a
`linux-` tag is pushed. They can also be built by hand on a machine with Go, Node.js,
`libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (Arch: `gtk3`, `webkit2gtk-4.1`),
plus [nfpm](https://nfpm.goreleaser.com).

### 1. Tag the Release

Push a tag named `linux-` and the version (e.g. `linux-1.0.3` or
`linux-1.0.3-rc.1`); CI strips the prefix and passes the rest to the build as
`VERSION`. After publishing the release, upload `linux-latest.sig` to the
update server as `/linux-client/latest.sig`, then run the AUR workflow with the
tag.

### 2. Packages (CI or by hand)

```bash
make package-linux VERSION=1.0.3                     # this machine's architecture
make package-linux VERSION=1.0.3 LINUX_GOARCH=arm64  # on an arm64 machine
```

This makes `build/pangolin-client-<arch>-<version>.deb`, `.rpm` and
`.pkg.tar.zst`. Packages built for release should come from Ubuntu 22.04 (as CI
does), so they run on Ubuntu 22.04+, Debian 12+, Fedora and Arch. RHEL 9 and its
rebuilds don't ship WebKitGTK 4.1, so they can't install the package.

Each package installs the binary, the `pangolin-client` systemd service (enabled
and started on install), the app menu and autostart entries, and the GNOME Shell
extension. Arch users install the `.pkg.tar.zst` from the release with
`sudo pacman -U <file>`.

### 3. Manifest

CI makes and signs the Linux manifest when the `UPDATE_SIGNING_KEY` secret holds
`release.sec`, and attaches it to the draft release as `linux-latest.sig`. By hand:

```bash
./scripts/generate-linux-manifest.sh
./scripts/sign-manifest.sh signing-keys/release.sec build/linux
```

The manifest points at the packages on the GitHub release by default (set
`DOWNLOAD_LOCATION_TEMPLATE` as for Windows to change that).

### 4. Publish

1. Publish the draft GitHub release (CI attaches the signed Windows MSI too).
2. Upload `linux-latest.sig` to the update server as `/linux-client/latest.sig`
   (`latestVersionPath` in `updater/constants_linux.go`).
3. Publishing the release runs `.github/workflows/aur.yml`, which updates the
   `pangolin-client-bin` AUR package when the `AUR_SSH_PRIVATE_KEY` secret is set
   (not set up yet: there is no AUR account).

### How Linux Updates Install

The daemon checks the Linux manifest like the Windows manager does. On deb, rpm
and Arch installs, an administrator (root or a member of `sudo`, `wheel` or `admin`) can
install an update from the app: the daemon downloads the package, checks its
hash against the signed manifest, and installs it with `apt-get`, `dnf`,
`zypper`, `rpm` or `pacman -U` in a transient systemd unit (`pangolin-client-update-*`), so the
install survives the package restarting the daemon. The new daemon reopens the
UIs that were open. The AUR package (which marks itself `aur` in
`/usr/share/pangolin-client/package-format`, as its AUR helper updates it) and
hand-installed builds show the command or download link instead
(`updater.ManualUpdate`).
