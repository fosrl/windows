#!/bin/bash
# Generate a file list with BLAKE2b-256 hashes for MSI files
# Outputs to build/ directory for production use
# Usage: ./generate-manifest.sh [msi-file1] [msi-file2] ...
#   If no files specified, uses all .msi files in build/ directory
#
# Optional environment variables:
#   DOWNLOAD_LOCATION_TEMPLATE: Template for download location (overrides default)
#     - Use %s as placeholder for filename
#     - Example: "https://github.com/owner/repo/releases/download/v%s/%s" (version + filename)
#     - Example: "https://example.com/downloads/%s" (filename only)
#     - Example: "/windows-client/%s" (relative path on update server)
#     - If unset, uses default: https://github.com/miloschwartz/sparkleupdatetest/releases/download/%s/%s

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${SCRIPT_DIR}/.."
BUILD_DIR="${PROJECT_ROOT}/build"
MANIFEST_FILE="${BUILD_DIR}/filelist.txt"

# Check if b2sum is available
if ! command -v b2sum &> /dev/null; then
    echo "Error: 'b2sum' is not installed."
    echo ""
    echo "Installation options:"
    echo "  macOS:   brew install coreutils (provides b2sum)"
    echo "  Linux:   Usually pre-installed"
    echo "  Windows: Use WSL or download from a coreutils package"
    exit 1
fi

# Create build directory if it doesn't exist
mkdir -p "${BUILD_DIR}"

# Default download location template (can be overridden by environment variable)
DEFAULT_DOWNLOAD_LOCATION_TEMPLATE="https://github.com/miloschwartz/sparkleupdatetest/releases/download/%s/%s"

# Use environment variable if set, otherwise use default
DOWNLOAD_LOCATION_TEMPLATE="${DOWNLOAD_LOCATION_TEMPLATE:-${DEFAULT_DOWNLOAD_LOCATION_TEMPLATE}}"

echo "Generating file list with BLAKE2b-256 hashes..."
echo "Output directory: ${BUILD_DIR}"
echo "Download location template: ${DOWNLOAD_LOCATION_TEMPLATE}"

# Find all MSI files in build directory if no files specified
if [ $# -eq 0 ]; then
    echo "Looking for MSI files in: ${BUILD_DIR}"
    MSI_FILES=$(find "${BUILD_DIR}" -name "*.msi" -type f 2>/dev/null || true)
    
    if [ -z "${MSI_FILES}" ]; then
        echo "Error: No MSI files found in ${BUILD_DIR}"
        echo ""
        echo "You can specify MSI files manually:"
        echo "  $0 /path/to/file1.msi /path/to/file2.msi"
        exit 1
    fi
else
    MSI_FILES="$@"
fi

# Generate hashes
echo "Processing MSI files..."
> "${MANIFEST_FILE}"  # Clear/create file

for MSI_FILE in ${MSI_FILES}; do
    if [ ! -f "${MSI_FILE}" ]; then
        echo "Warning: File not found: ${MSI_FILE}"
        continue
    fi
    
    echo "  Hashing: $(basename "${MSI_FILE}")"
    
    # Generate BLAKE2b-256 hash (256 bits = 32 bytes = 64 hex characters)
    HASH=$(b2sum -l 256 "${MSI_FILE}" | awk '{print $1}')
    FILENAME=$(basename "${MSI_FILE}")
    
    # Build download location from template
    # Extract version from filename (e.g., "pangolin-amd64-1.0.31.msi" -> "1.0.31")
    # This assumes the format: pangolin-<arch>-<version>.msi
    VERSION=$(echo "${FILENAME}" | sed -E 's/^pangolin-[^-]+-(.+)\.msi$/\1/')
    
    # Verify version extraction worked
    if [ "${VERSION}" = "${FILENAME}" ]; then
        echo "Error: Could not extract version from filename: ${FILENAME}"
        echo "  Expected format: pangolin-<arch>-<version>.msi"
        exit 1
    fi
    
    # Count placeholders in template by counting occurrences of '%s'
    # Replace all '%s' with a marker, then count the markers
    PLACEHOLDER_COUNT=$(echo "${DOWNLOAD_LOCATION_TEMPLATE}" | sed 's/%s/X/g' | tr -cd 'X' | wc -c | tr -d ' ')
    
    # Substitute placeholders using printf (which handles %s correctly)
    if [ "${PLACEHOLDER_COUNT}" -eq 2 ]; then
        # Template has two placeholders: version and filename
        # Use printf with proper escaping - printf will substitute %s with arguments
        DOWNLOAD_LOCATION=$(printf "${DOWNLOAD_LOCATION_TEMPLATE}" "${VERSION}" "${FILENAME}")
    elif [ "${PLACEHOLDER_COUNT}" -eq 1 ]; then
        # Template has one placeholder: filename
        DOWNLOAD_LOCATION=$(printf "${DOWNLOAD_LOCATION_TEMPLATE}" "${FILENAME}")
    else
        # Template has no placeholders, use as-is
        DOWNLOAD_LOCATION="${DOWNLOAD_LOCATION_TEMPLATE}"
    fi
    
    # Verify the download location doesn't contain unsubstituted placeholders
    if echo "${DOWNLOAD_LOCATION}" | grep -q '%s'; then
        echo "Error: Download location still contains unsubstituted placeholders: ${DOWNLOAD_LOCATION}"
        echo "  Template: ${DOWNLOAD_LOCATION_TEMPLATE}"
        echo "  Version: ${VERSION}"
        echo "  Filename: ${FILENAME}"
        echo "  Placeholder count: ${PLACEHOLDER_COUNT}"
        exit 1
    fi
    
    echo "    Download location: ${DOWNLOAD_LOCATION}"
    
    # Append to manifest: "hash  filename  download_location"
    echo "${HASH}  ${FILENAME}  ${DOWNLOAD_LOCATION}" >> "${MANIFEST_FILE}"
done

if [ ! -s "${MANIFEST_FILE}" ]; then
    echo "Error: No files were processed. Manifest file is empty."
    exit 1
fi

echo ""
echo "✓ File list generated: ${MANIFEST_FILE}"
echo ""
echo "Next step: Sign the manifest with:"
echo "  ./sign-manifest.sh <secret-key-file>"
echo ""
echo "Contents:"
cat "${MANIFEST_FILE}"

