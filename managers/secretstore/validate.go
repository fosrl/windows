//go:build windows

package secretstore

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"
)

const maxUserIDLen = 128

// ValidateUserID ensures a Pangolin user id is safe to use as a file name component.
func ValidateUserID(userID string) error {
	if userID == "" {
		return errors.New("user id is empty")
	}
	if len(userID) > maxUserIDLen {
		return errors.New("user id is too long")
	}
	if userID == "." || userID == ".." {
		return errors.New("invalid user id")
	}
	for _, r := range userID {
		if r == '/' || r == '\\' || r == filepath.Separator {
			return errors.New("invalid user id")
		}
		if unicode.IsControl(r) {
			return errors.New("invalid user id")
		}
	}
	return nil
}

// ValidateWindowsSID ensures a Windows SID string is safe as a directory name.
func ValidateWindowsSID(sid string) error {
	if sid == "" {
		return errors.New("windows sid is empty")
	}
	if len(sid) > 256 {
		return errors.New("windows sid is too long")
	}
	if !strings.HasPrefix(sid, "S-") {
		return errors.New("invalid windows sid")
	}
	for _, r := range sid {
		if r == '/' || r == '\\' || r == filepath.Separator || unicode.IsControl(r) {
			return errors.New("invalid windows sid")
		}
	}
	return nil
}
