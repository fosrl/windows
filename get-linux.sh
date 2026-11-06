#!/bin/sh

# Get Pangolin - Linux desktop client installation script
# Usage: curl -fsSL https://static.pangolin.net/get-linux.sh | sh
#
# Detects the distribution and CPU architecture, downloads the matching
# package (deb, rpm or Arch) from the latest Linux release on GitHub and
# installs it with the system package manager.
#
# Options:
#   --format deb|rpm|arch   Skip distribution detection.
#   --version X.Y.Z         Install this version instead of the latest.

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# GitHub repository info. Windows and Linux are released from the same repo,
# with tags prefixed by platform (linux-0.1.0, windows-0.16.0).
REPO="fosrl/windows-linux"
TAG_PREFIX="linux-"
VERSIONS_API_URL="https://api.fossorial.io/api/v1/versions"

# Output helpers
print_status() {
    printf '%b[INFO]%b %s\n' "${GREEN}" "${NC}" "$1"
}

print_warning() {
    printf '%b[WARN]%b %s\n' "${YELLOW}" "${NC}" "$1" >&2
}

print_error() {
    printf '%b[ERROR]%b %s\n' "${RED}" "${NC}" "$1" >&2
}

# Fetch a URL to stdout
fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$1"
    else
        print_error "Neither curl nor wget is available."
        exit 1
    fi
}

# Download a URL to a file
download() {
    if command -v curl >/dev/null 2>&1; then
        curl -fSL --progress-bar "$1" -o "$2"
    else
        wget -q --show-progress "$1" -O "$2"
    fi
}

# Latest Linux client version from the Fossorial versions API
# (data.linux.latestVersion), without any "linux-" or "v" prefix.
get_latest_version() {
    latest_info=$(fetch "$VERSIONS_API_URL" 2>/dev/null || true)
    if [ -z "$latest_info" ]; then
        print_error "Failed to fetch latest version info"
        exit 1
    fi

    # Extract data.linux.latestVersion without requiring jq.
    version=$(printf '%s' "$latest_info" | tr -d '\n\r' | sed -n 's/.*"linux"[[:space:]]*:[[:space:]]*{[^}]*"latestVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    if [ -z "$version" ]; then
        print_error "Could not parse linux.latestVersion from versions API response"
        print_error "Re-run with --version X.Y.Z, or download a package from https://github.com/${REPO}/releases"
        exit 1
    fi

    printf '%s' "$version" | sed "s/^${TAG_PREFIX}//; s/^v//"
}

# CPU architecture, as used in the package names
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)
            print_error "Unsupported architecture: $(uname -m). Packages are built for x86_64 and ARM64."
            exit 1
            ;;
    esac
}

# Package format for this distribution: deb, rpm or arch. Reads
# /etc/os-release, then falls back to whichever package manager is present.
detect_format() {
    ids=""
    if [ -r /etc/os-release ]; then
        # shellcheck disable=SC1091
        ids=$(. /etc/os-release && printf '%s %s' "${ID:-}" "${ID_LIKE:-}")
    fi

    for id in $ids; do
        case "$id" in
            debian|ubuntu) echo "deb"; return ;;
            fedora|rhel|centos|rocky|almalinux|suse|opensuse*|sles) echo "rpm"; return ;;
            arch|archarm|manjaro|endeavouros|cachyos) echo "arch"; return ;;
        esac
    done

    if command -v apt-get >/dev/null 2>&1; then echo "deb"; return; fi
    if command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1 || command -v zypper >/dev/null 2>&1; then
        echo "rpm"; return
    fi
    if command -v pacman >/dev/null 2>&1; then echo "arch"; return; fi

    print_error "Could not detect your distribution's package format."
    print_error "Re-run with --format deb, --format rpm or --format arch."
    exit 1
}

# File extension for a package format
format_extension() {
    case "$1" in
        deb) echo "deb" ;;
        rpm) echo "rpm" ;;
        arch) echo "pkg.tar.zst" ;;
        *) print_error "Unknown package format: $1 (expected deb, rpm or arch)"; exit 1 ;;
    esac
}

# Command prefix to run as root: empty when already root
get_sudo_cmd() {
    if [ "$(id -u)" -eq 0 ]; then
        echo ""
    elif command -v sudo >/dev/null 2>&1; then
        echo "sudo"
    else
        print_error "Installing a package needs root, and sudo is not available."
        print_error "Run this script as root or install sudo."
        exit 1
    fi
}

# Install a downloaded package file with the system package manager
install_package() {
    format="$1"
    file="$2"
    sudo_cmd="$3"

    case "$format" in
        deb)
            # apt-get treats the argument as a file because it is an
            # absolute path. It installs the dependencies too.
            $sudo_cmd apt-get install -y "$file"
            ;;
        rpm)
            if command -v dnf >/dev/null 2>&1; then
                $sudo_cmd dnf install -y "$file"
            elif command -v zypper >/dev/null 2>&1; then
                # The package isn't signed with a key zypper knows.
                $sudo_cmd zypper --non-interactive install --allow-unsigned-rpm "$file"
            elif command -v yum >/dev/null 2>&1; then
                $sudo_cmd yum install -y "$file"
            else
                print_error "No dnf, zypper or yum found to install the rpm."
                exit 1
            fi
            ;;
        arch)
            $sudo_cmd pacman -U --noconfirm --needed "$file"
            ;;
    esac
}

# Parse arguments
parse_args() {
    FORMAT=""
    VERSION=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --format) FORMAT="${2:-}"; shift ;;
            --format=*) FORMAT="${1#--format=}" ;;
            --version) VERSION="${2:-}"; shift ;;
            --version=*) VERSION="${1#--version=}" ;;
            -h|--help)
                echo "Usage: get-linux.sh [--format deb|rpm|arch] [--version X.Y.Z]"
                exit 0
                ;;
            *) print_warning "Ignoring unknown argument: $1" ;;
        esac
        shift
    done
}

main() {
    parse_args "$@"

    if [ "$(uname -s)" != "Linux" ]; then
        print_error "This script installs the Linux client. For other platforms see https://pangolin.net/downloads"
        exit 1
    fi

    print_status "Installing the Pangolin client for Linux..."

    ARCH=$(detect_arch)
    if [ -z "$FORMAT" ]; then
        FORMAT=$(detect_format)
    fi
    EXTENSION=$(format_extension "$FORMAT")
    print_status "Detected platform: ${FORMAT} package for ${ARCH}"

    if [ -z "$VERSION" ]; then
        print_status "Fetching latest version..."
        VERSION=$(get_latest_version)
    fi
    VERSION=$(printf '%s' "$VERSION" | sed "s/^${TAG_PREFIX}//; s/^v//")
    print_status "Version: ${VERSION}"

    SUDO_CMD=$(get_sudo_cmd)

    asset_name="pangolin-client-${ARCH}-${VERSION}.${EXTENSION}"
    download_url="https://github.com/${REPO}/releases/download/${TAG_PREFIX}${VERSION}/${asset_name}"

    # Readable by everyone so apt's unprivileged _apt user can open the file
    # instead of warning that it fell back to root.
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT
    chmod 755 "$TMP_DIR"
    package_file="${TMP_DIR}/${asset_name}"

    print_status "Downloading ${download_url}"
    if ! download "$download_url" "$package_file"; then
        print_error "Download failed. Check that version ${VERSION} has a ${FORMAT} package for ${ARCH}:"
        print_error "https://github.com/${REPO}/releases/tag/${TAG_PREFIX}${VERSION}"
        exit 1
    fi
    chmod 644 "$package_file"

    if [ -n "$SUDO_CMD" ]; then
        print_status "Root privileges are needed to install the package"
    fi
    install_package "$FORMAT" "$package_file" "$SUDO_CMD"

    if command -v pangolin-client >/dev/null 2>&1 || [ -x /usr/bin/pangolin-client ]; then
        print_status "Pangolin ${VERSION} installed successfully!"
        print_status "Open Pangolin from your applications menu, then log in from the tray icon."
    else
        print_error "Installation failed: pangolin-client was not found after installing."
        exit 1
    fi
}

main "$@"
