//go:build windows

package updater

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/version"
)

func versionNewerThanUs(candidate string) (bool, error) {
	logger.Info("Updater: Comparing versions - candidate: %s, current: %s", candidate, version.Number)
	candidateParts := strings.Split(candidate, ".")
	ourParts := strings.Split(version.Number, ".")
	logger.Info("Updater: Candidate parts: %v, Current parts: %v", candidateParts, ourParts)

	if len(candidateParts) == 0 || len(ourParts) == 0 {
		return false, errors.New("Empty version")
	}
	l := len(candidateParts)
	if len(ourParts) > l {
		l = len(ourParts)
	}
	logger.Info("Updater: Comparing %d version components", l)

	for i := 0; i < l; i++ {
		var err error
		cP, oP := uint64(0), uint64(0)
		if i < len(candidateParts) {
			if len(candidateParts[i]) == 0 {
				return false, errors.New("Empty version part")
			}
			cP, err = strconv.ParseUint(candidateParts[i], 10, 16)
			if err != nil {
				return false, errors.New("Invalid version integer part")
			}
		}
		if i < len(ourParts) {
			if len(ourParts[i]) == 0 {
				return false, errors.New("Empty version part")
			}
			oP, err = strconv.ParseUint(ourParts[i], 10, 16)
			if err != nil {
				return false, errors.New("Invalid version integer part")
			}
		}
		logger.Info("Updater: Component %d - candidate: %d, current: %d", i, cP, oP)
		if cP == oP {
			logger.Info("Updater: Component %d matches, continuing", i)
			continue
		}
		newer := cP > oP
		logger.Info("Updater: Component %d differs - candidate is newer: %v", i, newer)
		return newer, nil
	}
	logger.Info("Updater: All components match - candidate is not newer")
	return false, nil
}

func findCandidate(candidates fileList) (*UpdateFound, error) {
	prefix := fmt.Sprintf(msiArchPrefix, version.Arch())
	suffix := msiSuffix
	currentVersion := version.Number
	logger.Info("Updater: findCandidate() - Current version: %s, Architecture: %s", currentVersion, version.Arch())
	logger.Info("Updater: Looking for files matching prefix: %s, suffix: %s", prefix, suffix)
	logger.Info("Updater: Total files in manifest: %d", len(candidates))

	fileNames := make([]string, 0, len(candidates))
	for name := range candidates {
		fileNames = append(fileNames, name)
		logger.Info("Updater: Manifest contains file: %s", name)
	}

	for name, entry := range candidates {
		logger.Info("Updater: Checking file: %s", name)
		hasPrefix := strings.HasPrefix(name, prefix)
		hasSuffix := strings.HasSuffix(name, suffix)
		logger.Info("Updater: File %s - hasPrefix(%s): %v, hasSuffix(%s): %v", name, prefix, hasPrefix, suffix, hasSuffix)

		if hasPrefix && hasSuffix {
			candidateVersion := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
			logger.Info("Updater: File matches pattern! Extracted version: %s", candidateVersion)

			if len(candidateVersion) > 128 {
				logger.Error("Updater: Version string too long: %d characters", len(candidateVersion))
				return nil, errors.New("Version length is too long")
			}

			logger.Info("Updater: Comparing candidate version %s with current version %s", candidateVersion, currentVersion)
			newer, err := versionNewerThanUs(candidateVersion)
			if err != nil {
				logger.Error("Updater: Version comparison error: %v", err)
				return nil, fmt.Errorf("error comparing version %s: %w", candidateVersion, err)
			}
			logger.Info("Updater: Version comparison result - %s is newer than %s: %v", candidateVersion, currentVersion, newer)

			if newer {
				logger.Info("Updater: ✓ Update candidate found: %s (hash: %x, location: %s)", name, entry.hash, entry.downloadLocation)
				return &UpdateFound{
					name:             name,
					hash:             entry.hash,
					downloadLocation: entry.downloadLocation,
				}, nil
			} else {
				logger.Info("Updater: Candidate version %s is not newer, skipping", candidateVersion)
			}
		} else {
			logger.Info("Updater: File %s does not match pattern (needs prefix '%s' and suffix '%s')", name, prefix, suffix)
		}
	}
	logger.Info("Updater: No update candidate found after checking all %d files", len(candidates))
	return nil, nil
}
