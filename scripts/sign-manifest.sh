#!/bin/bash
# Sign the file list manifest with Ed25519 private key
# Outputs signed manifest to build/ directory for production use
# Usage: ./sign-manifest.sh <secret-key-file> [manifest-dir]
#   secret-key-file: Path to the release.sec file
#   manifest-dir: Directory holding filelist.txt (default: build; the Linux
#     manifest is in build/linux)

set -e

if [ $# -lt 1 ]; then
    echo "Usage: $0 <secret-key-file>"
    echo "  secret-key-file: Path to the release.sec file"
    echo ""
    echo "Example:"
    echo "  $0 ../signing-keys/release.sec"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${SCRIPT_DIR}/.."
BUILD_DIR="${2:-${PROJECT_ROOT}/build}"
SECRET_KEY_FILE="$1"
MANIFEST_FILE="${BUILD_DIR}/filelist.txt"
SIGNED_MANIFEST="${BUILD_DIR}/latest.sig"

# Check if signify is available (Debian and Ubuntu call it signify-openbsd)
SIGNIFY=signify
if ! command -v signify &> /dev/null; then
    if command -v signify-openbsd &> /dev/null; then
        SIGNIFY=signify-openbsd
    else
        echo "Error: 'signify' is not installed."
        echo "Install it first (see generate-keys.sh for instructions)"
        exit 1
    fi
fi

# Check if secret key exists
if [ ! -f "${SECRET_KEY_FILE}" ]; then
    echo "Error: Secret key not found: ${SECRET_KEY_FILE}"
    exit 1
fi

# Check if manifest exists
if [ ! -f "${MANIFEST_FILE}" ]; then
    echo "Error: Manifest file not found: ${MANIFEST_FILE}"
    echo "Run ./generate-manifest.sh first"
    exit 1
fi

echo "Signing manifest file..."
echo "Secret key: ${SECRET_KEY_FILE}"
echo "Manifest: ${MANIFEST_FILE}"
echo "Output: ${SIGNED_MANIFEST}"

# Sign the manifest
# -S: sign
# -e: embed the signature in the file
# -s: secret key file
# -m: message file to sign
"${SIGNIFY}" -S -e -s "${SECRET_KEY_FILE}" -m "${MANIFEST_FILE}"

# The signed file will be created as ${MANIFEST_FILE}.sig
SIGNED_OUTPUT="${MANIFEST_FILE}.sig"
if [ ! -f "${SIGNED_OUTPUT}" ]; then
    echo "Error: Signed file was not created"
    exit 1
fi

# Copy to latest.sig (the filename expected by the updater)
cp "${SIGNED_OUTPUT}" "${SIGNED_MANIFEST}"

echo ""
echo "✓ Manifest signed successfully!"
echo ""
echo "Signed manifest: ${SIGNED_MANIFEST}"
echo ""
echo "This file should be uploaded to your update server at the path specified in"
echo "updater/constants.go (latestVersionPath)."
echo ""
echo "The MSI files should be uploaded to the path specified in updater/constants.go (msiPath)."

