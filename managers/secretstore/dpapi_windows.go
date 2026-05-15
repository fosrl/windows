//go:build windows

package secretstore

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func dpapiDescription(windowsSID, userID string) string {
	return fmt.Sprintf("Pangolin:%s:%s", windowsSID, userID)
}

func encrypt(plaintext []byte, windowsSID, userID string) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("cannot encrypt empty data")
	}
	desc := dpapiDescription(windowsSID, userID)
	descPtr, err := windows.UTF16PtrFromString(desc)
	if err != nil {
		return nil, err
	}

	in := windows.DataBlob{Size: uint32(len(plaintext)), Data: &plaintext[0]}
	var out windows.DataBlob
	err = windows.CryptProtectData(&in, descPtr, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	ciphertext := make([]byte, out.Size)
	copy(ciphertext, unsafe.Slice(out.Data, out.Size))
	return ciphertext, nil
}

func decrypt(ciphertext []byte, windowsSID, userID string) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("cannot decrypt empty data")
	}
	expectedDesc := dpapiDescription(windowsSID, userID)

	in := windows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	var out windows.DataBlob
	var descPtr *uint16
	err := windows.CryptUnprotectData(&in, &descPtr, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	if descPtr != nil {
		defer windows.LocalFree(windows.Handle(unsafe.Pointer(descPtr)))
		actualDesc := windows.UTF16PtrToString(descPtr)
		if actualDesc != expectedDesc {
			return nil, fmt.Errorf("dpapi description mismatch")
		}
	}

	plaintext := make([]byte, out.Size)
	copy(plaintext, unsafe.Slice(out.Data, out.Size))
	return plaintext, nil
}
