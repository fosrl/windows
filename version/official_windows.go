package version

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	officialCommonName = "Fossorial"
)

// IsRunningOfficialVersion checks if the current executable is signed with the official certificate.
// This is an easily by-passable check, which does not serve security purposes.
// DO NOT PLACE SECURITY-SENSITIVE FUNCTIONS IN THIS FILE
func IsRunningOfficialVersion() bool {
	path, err := os.Executable()
	if err != nil {
		return false
	}

	names, err := extractCertificateNames(path)
	if err != nil {
		return false
	}
	for _, name := range names {
		if name == officialCommonName {
			return true
		}
	}
	return false
}

func extractCertificateNames(path string) ([]string, error) {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var certStore windows.Handle
	err = windows.CryptQueryObject(windows.CERT_QUERY_OBJECT_FILE, unsafe.Pointer(path16), windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED, windows.CERT_QUERY_FORMAT_FLAG_ALL, 0, nil, nil, nil, &certStore, nil, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CertCloseStore(certStore, 0)
	var cert *windows.CertContext
	var names []string
	for {
		cert, err = windows.CertEnumCertificatesInStore(certStore, cert)
		if err != nil {
			if errors.Is(err, windows.Errno(windows.CRYPT_E_NOT_FOUND)) {
				break
			}
			return nil, err
		}
		if cert == nil {
			break
		}
		nameLen := windows.CertGetNameString(cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, nil, 0)
		if nameLen == 0 {
			continue
		}
		name16 := make([]uint16, nameLen)
		if windows.CertGetNameString(cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, &name16[0], nameLen) != nameLen {
			continue
		}
		if name16[0] == 0 {
			continue
		}
		names = append(names, windows.UTF16ToString(name16))
	}
	if names == nil {
		return nil, windows.Errno(windows.CRYPT_E_NOT_FOUND)
	}
	return names, nil
}

