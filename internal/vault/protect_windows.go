//go:build windows

package vault

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func NativeAvailable() bool { return true }

func protectNative(value []byte) ([]byte, error)   { return cryptNative(value, true) }
func unprotectNative(value []byte) ([]byte, error) { return cryptNative(value, false) }

// unsafe is confined to the Windows DATA_BLOB boundary; the returned OS buffer
// is copied, cleared and released before this function returns.
func cryptNative(value []byte, protect bool) ([]byte, error) {
	if len(value) == 0 || len(value) > 64*1024 {
		return nil, ErrLocked
	}
	input := windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
	entropyBytes := []byte(namespace + "/master-key-v1")
	entropy := windows.DataBlob{Size: uint32(len(entropyBytes)), Data: &entropyBytes[0]}
	var output windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptUnprotectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	if err != nil {
		return nil, errors.New("Windows key protection failed")
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	if output.Data == nil || output.Size == 0 || output.Size > 64*1024 {
		return nil, ErrLocked
	}
	protected := unsafe.Slice(output.Data, int(output.Size))
	result := append([]byte(nil), protected...)
	clear(protected)
	return result, nil
}
