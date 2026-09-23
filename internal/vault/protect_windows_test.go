//go:build windows

package vault

import (
	"bytes"
	"testing"
)

func TestWindowsProtectedKeyRoundTrip(t *testing.T) {
	directory := t.TempDir()
	key, err := CreateKeyFile(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKeyRing(directory, "")
	if err != nil || !bytes.Equal(key.Value, loaded[0].Value) {
		t.Fatalf("DPAPI round trip failed: %v", err)
	}
}
