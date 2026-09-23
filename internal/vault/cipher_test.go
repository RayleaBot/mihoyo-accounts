package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordEncryptionBindsKeyAndRecord(t *testing.T) {
	key := NewKey()
	cipher, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"cookie_token":"synthetic-cookie-marker","stoken":"synthetic-stoken-marker"}`)
	first, err := cipher.Seal("account.a", plain)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := cipher.Seal("account.a", plain)
	if first == second || bytes.Contains([]byte(first), []byte("synthetic-cookie-marker")) {
		t.Fatal("ciphertext reused a nonce or exposed plaintext")
	}
	got, err := cipher.Open("account.a", first)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %v", err)
	}
	if _, err := cipher.Open("account.b", first); !errors.Is(err, ErrCiphertext) {
		t.Fatal("record swap was accepted")
	}
	other, _ := NewCipher(NewKey())
	if _, err := other.Open("account.a", first); !errors.Is(err, ErrCiphertext) {
		t.Fatal("wrong key was accepted")
	}
	var envelope map[string]any
	_ = json.Unmarshal([]byte(first), &envelope)
	envelope["sealed"] = "AAAA"
	tampered, _ := json.Marshal(envelope)
	if _, err := cipher.Open("account.a", string(tampered)); !errors.Is(err, ErrCiphertext) {
		t.Fatal("tampered data was accepted")
	}
}

func TestPasswordKeyFilePreservesExistingKey(t *testing.T) {
	directory := t.TempDir()
	password := "synthetic-key-passphrase"
	key, err := CreateKeyFile(directory, password)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(directory, keyFileName))
	if bytes.Contains(data, key.Value) || bytes.Contains(data, []byte(password)) {
		t.Fatal("key file contains plaintext material")
	}
	loaded, err := LoadKeyRing(directory, password)
	if err != nil || !bytes.Equal(loaded[0].Value, key.Value) || loaded[0].ID != key.ID {
		t.Fatalf("load key: %v", err)
	}
	if _, err := LoadKeyRing(directory, "wrong-passphrase"); !errors.Is(err, ErrLocked) {
		t.Fatal("wrong passphrase accepted")
	}
	if _, err := CreateKeyFile(directory, password); err == nil {
		t.Fatal("existing key was overwritten")
	}
	after, _ := os.ReadFile(filepath.Join(directory, keyFileName))
	if !bytes.Equal(data, after) {
		t.Fatal("existing key changed")
	}
	if _, err := LoadKeyRing(t.TempDir(), password); !errors.Is(err, ErrMissingKey) {
		t.Fatal("missing key was not distinguished")
	}
}
