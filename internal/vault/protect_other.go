//go:build !windows

package vault

// NativeAvailable reports whether the key can be protected by the operating
// system account. Only Windows DPAPI is implemented.
func NativeAvailable() bool { return false }

func protectNative([]byte) ([]byte, error)   { return nil, ErrNativeUnavailable }
func unprotectNative([]byte) ([]byte, error) { return nil, ErrLocked }
