#!/bin/bash
# Generate the Linux update manifest: BLAKE2b-256 hashes of the deb, rpm and
# Arch packages, with where to download each, in build/linux/filelist.txt.
# Sign it with: ./sign-manifest.sh <secret-key-file> build/linux
# and upload build/linux/latest.sig to /linux-client/latest.sig on the update
# server (see updater/constants_linux.go).
#
# Usage: ./generate-linux-manifest.sh [package files...]
#   With no files, uses build/pangolin-client-*-*.deb, .rpm and .pkg.tar.zst.
#
# Optional environment variables:
#   DOWNLOAD_LOCATION_TEMPLATE: as in generate-manifest.sh, with %s for the
#     version and %s for the file name, or just %s for the file name.
#     Default: https://github.com/fosrl/windows-linux/releases/download/%s/%s

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${SCRIPT_DIR}/../build"
OUT_DIR="${BUILD_DIR}/linux"
MANIFEST_FILE="${OUT_DIR}/filelist.txt"
TEMPLATE="${DOWNLOAD_LOCATION_TEMPLATE:-https://github.com/fosrl/windows-linux/releases/download/%s/%s}"

command -v b2sum >/dev/null || { echo "Error: b2sum is not installed." >&2; exit 1; }

if [ $# -eq 0 ]; then
    shopt -s nullglob
    set -- "${BUILD_DIR}"/pangolin-client-*-*.deb "${BUILD_DIR}"/pangolin-client-*-*.rpm \
        "${BUILD_DIR}"/pangolin-client-*-*.pkg.tar.zst
    [ $# -gt 0 ] || { echo "Error: no packages in ${BUILD_DIR}; run make package-linux" >&2; exit 1; }
fi

mkdir -p "${OUT_DIR}"
: > "${MANIFEST_FILE}"
placeholders=$(printf '%s' "${TEMPLATE}" | grep -o '%s' | wc -l)

for PKG in "$@"; do
    NAME=$(basename "${PKG}")
    # pangolin-client-<arch>-<version>.(deb|rpm|pkg.tar.zst)
    VERSION=$(echo "${NAME}" | sed -nE 's/^pangolin-client-[^-]+-(.+)\.(deb|rpm|pkg\.tar\.zst)$/\1/p')
    [ -n "${VERSION}" ] || { echo "Error: unexpected file name ${NAME}" >&2; exit 1; }
    HASH=$(b2sum -l 256 "${PKG}" | awk '{print $1}')
    case "${placeholders}" in
        2) LOCATION=$(printf "${TEMPLATE}" "${VERSION}" "${NAME}") ;;
        1) LOCATION=$(printf "${TEMPLATE}" "${NAME}") ;;
        *) LOCATION="${TEMPLATE}" ;;
    esac
    echo "  ${NAME} -> ${LOCATION}"
    echo "${HASH}  ${NAME}  ${LOCATION}" >> "${MANIFEST_FILE}"
done

echo "Manifest: ${MANIFEST_FILE}"
cat "${MANIFEST_FILE}"
