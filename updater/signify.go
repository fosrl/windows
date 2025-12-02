//go:build windows

package updater

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/fosrl/newt/logger"
	"golang.org/x/crypto/blake2b"
)

/*
 * Generate with:
 *   $ b2sum -l 256 *.msi > list
 *   $ signify -S -e -s release.sec -m list
 *   $ upload ./list.sec
 */

type fileEntry struct {
	hash             [blake2b.Size256]byte
	downloadLocation string // Optional: can be a relative path or full URL
}

type fileList map[string]fileEntry

func readFileList(input []byte) (fileList, error) {
	logger.Info("Updater: Parsing signed file list (input size: %d bytes)", len(input))

	logger.Info("Updater: Decoding public key from base64")
	publicKeyBytes, err := base64.StdEncoding.DecodeString(releasePublicKeyBase64)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize+10 || publicKeyBytes[0] != 'E' || publicKeyBytes[1] != 'd' {
		logger.Error("Updater: Invalid public key - decode error: %v, length: %d", err, len(publicKeyBytes))
		return nil, errors.New("Invalid public key")
	}
	logger.Info("Updater: Public key decoded successfully (length: %d)", len(publicKeyBytes))

	lines := bytes.SplitN(input, []byte{'\n'}, 3)
	if len(lines) != 3 {
		logger.Error("Updater: Manifest has wrong number of lines: %d (expected 3)", len(lines))
		return nil, errors.New("Signature input has too few lines")
	}
	logger.Info("Updater: Manifest has 3 lines as expected")

	if !bytes.HasPrefix(lines[0], []byte("untrusted comment: ")) {
		logger.Error("Updater: Missing untrusted comment prefix")
		return nil, errors.New("Signature input is missing untrusted comment")
	}
	logger.Info("Updater: Untrusted comment found: %s", string(lines[0]))

	logger.Info("Updater: Decoding signature from base64")
	signatureBytes, err := base64.StdEncoding.DecodeString(string(lines[1]))
	if err != nil {
		logger.Error("Updater: Failed to decode signature: %v", err)
		return nil, errors.New("Signature input is not valid base64")
	}
	logger.Info("Updater: Signature decoded (length: %d)", len(signatureBytes))

	if len(signatureBytes) != ed25519.SignatureSize+10 || !bytes.Equal(signatureBytes[:10], publicKeyBytes[:10]) {
		logger.Error("Updater: Signature length/keyid mismatch - sigLen: %d, expected: %d", len(signatureBytes), ed25519.SignatureSize+10)
		return nil, errors.New("Signature input bytes are incorrect length, type, or keyid")
	}
	logger.Info("Updater: Signature format valid, verifying...")

	if !ed25519.Verify(publicKeyBytes[10:], lines[2], signatureBytes[10:]) {
		logger.Error("Updater: Ed25519 signature verification failed!")
		return nil, errors.New("Signature is invalid")
	}
	logger.Info("Updater: ✓ Ed25519 signature verified successfully")

	fileLines := strings.Split(string(lines[2]), "\n")
	logger.Info("Updater: Parsing %d file hash lines", len(fileLines))
	fileHashes := make(map[string]fileEntry, len(fileLines))
	for index, line := range fileLines {
		if len(line) == 0 && index == len(fileLines)-1 {
			logger.Info("Updater: Skipping empty last line")
			break
		}
		// Parse line format: "hash  filename" or "hash  filename  download_location"
		// The separator is "  " (two spaces) between fields
		parts := strings.SplitN(line, "  ", 3)
		if len(parts) < 2 {
			logger.Error("Updater: Invalid file hash line format at index %d: %s", index, line)
			return nil, errors.New("File hash line has too few components")
		}

		hashStr := parts[0]
		filename := parts[1]
		downloadLocation := ""
		if len(parts) >= 3 {
			downloadLocation = parts[2]
		}

		maybeHash, err := hex.DecodeString(hashStr)
		if err != nil || len(maybeHash) != blake2b.Size256 {
			logger.Error("Updater: Invalid hash at line %d - decode error: %v, length: %d", index, err, len(maybeHash))
			return nil, errors.New("File hash is invalid hex or incorrect number of bytes")
		}
		var hash [blake2b.Size256]byte
		copy(hash[:], maybeHash)

		entry := fileEntry{
			hash:             hash,
			downloadLocation: downloadLocation,
		}
		fileHashes[filename] = entry

		if downloadLocation != "" {
			logger.Info("Updater: Parsed file entry: %s (hash: %x, location: %s)", filename, hash, downloadLocation)
		} else {
			logger.Info("Updater: Parsed file entry: %s (hash: %x, location: <default>)", filename, hash)
		}
	}
	if len(fileHashes) == 0 {
		logger.Error("Updater: No file hashes found in manifest")
		return nil, errors.New("No file hashes found in signed input")
	}
	logger.Info("Updater: Successfully parsed %d file entries from manifest", len(fileHashes))
	return fileHashes, nil
}
