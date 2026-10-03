package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnavailableNativeKeyIsPreservedUntilRestored(t *testing.T) {
	directory := t.TempDir()
	unavailable, err := json.Marshal(keyFile{Version: 1, Mode: "dpapi", Wrapped: "AQIDBA=="})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, keyFileName)
	if err = os.WriteFile(path, unavailable, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(directory)
	if err != nil {
		t.Fatal(err)
	}
	if m.State() != "locked" {
		t.Fatal("unreadable native key must keep the store locked")
	}
	if _, err = m.Cipher(); !errors.Is(err, ErrNativeKeyUnavailable) {
		t.Fatalf("startup diagnostic = %v", err)
	}
	for _, open := range []func() error{
		func() error { return m.Unlock("") },
		func() error { return m.Initialize("", true) },
	} {
		if err = open(); !errors.Is(err, ErrNativeKeyUnavailable) {
			t.Fatalf("native key rejection = %v", err)
		}
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, unavailable) {
			t.Fatal("a failed unlock replaced the original key file", err)
		}
	}

	recoveryDirectory := t.TempDir()
	password := "synthetic-recovery-password"
	key, err := CreateKeyFile(recoveryDirectory, password)
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.Seal("account.fixture", []byte("synthetic-credential"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(recoveryDirectory, keyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, restored, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = m.Unlock(password); err != nil {
		t.Fatal("restored password key did not unlock", err)
	}
	reader, err := m.Cipher()
	if err != nil {
		t.Fatal("restored key kept the startup failure", err)
	}
	if plain, err := reader.Open("account.fixture", sealed); err != nil || string(plain) != "synthetic-credential" {
		t.Fatal("restored key could not read its existing record", err)
	}
	m.Lock()
	if _, err = m.Cipher(); !errors.Is(err, ErrLocked) {
		t.Fatalf("explicit lock retained a stale native key error: %v", err)
	}
}

// Key files written by earlier releases must keep opening: a bare single key,
// and a version 2 ring whose older generations still read old records.
func TestExistingKeyFileFormatsStayReadable(t *testing.T) {
	password := "synthetic-key-password"
	t.Run("single key", func(t *testing.T) {
		directory := t.TempDir()
		key, err := CreateKeyFile(directory, password)
		if err != nil {
			t.Fatal(err)
		}
		sealer, _ := NewCipher(key)
		sealed, _ := sealer.Seal("account.fixture", []byte("synthetic-credential"))
		m, _ := NewManager(directory)
		if err = m.Unlock(password); err != nil {
			t.Fatal(err)
		}
		reader, _ := m.Cipher()
		if plain, err := reader.Open("account.fixture", sealed); err != nil || string(plain) != "synthetic-credential" {
			t.Fatal("single-key record unreadable", err)
		}
	})
	t.Run("ring with an older generation", func(t *testing.T) {
		directory := t.TempDir()
		older, newer := NewKey(), NewKey()
		oldSealer, _ := NewCipher(older)
		sealed, _ := oldSealer.Seal("account.fixture", []byte("synthetic-credential"))
		if err := rewrapKeyFile(directory, password, "password", []Key{newer, older}); err != nil {
			t.Fatal(err)
		}
		m, _ := NewManager(directory)
		if err := m.Unlock(password); err != nil {
			t.Fatal(err)
		}
		reader, _ := m.Cipher()
		if reader.KeyID() != newer.ID {
			t.Fatal("new records are not sealed with the newest key")
		}
		if plain, err := reader.Open("account.fixture", sealed); err != nil || string(plain) != "synthetic-credential" {
			t.Fatal("older-generation record unreadable", err)
		}
	})
}

func TestSwitchModeKeepsRecordsReadable(t *testing.T) {
	directory := t.TempDir()
	first, second := "synthetic-key-password", "another-synthetic-password"
	m, _ := NewManager(directory)
	if err := m.Initialize(first, false); err != nil {
		t.Fatal(err)
	}
	cipher, _ := m.Cipher()
	sealed, _ := cipher.Seal("account.fixture", []byte("synthetic-credential"))
	original, _ := os.ReadFile(filepath.Join(directory, keyFileName))

	if err := m.SwitchMode("password", "incorrect-password", second); !errors.Is(err, ErrLocked) {
		t.Fatalf("wrong current passphrase: %v", err)
	}
	if err := m.SwitchMode("password", first, "short"); !errors.Is(err, ErrPassword) {
		t.Fatalf("short new passphrase: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(directory, keyFileName)); !bytes.Equal(original, after) {
		t.Fatal("a rejected switch changed the key file")
	}
	if err := m.SwitchMode("password", first, second); err != nil {
		t.Fatal(err)
	}
	opens := func(t *testing.T, m *Manager) {
		t.Helper()
		reader, err := m.Cipher()
		if err != nil {
			t.Fatal(err)
		}
		if plain, err := reader.Open("account.fixture", sealed); err != nil || string(plain) != "synthetic-credential" {
			t.Fatal("record unreadable after switching", err)
		}
	}
	restarted, _ := NewManager(directory)
	if restarted.Unlock(first) == nil {
		t.Fatal("the old passphrase still unlocks")
	}
	if err := restarted.Unlock(second); err != nil {
		t.Fatal(err)
	}
	opens(t, restarted)

	if !NativeAvailable() {
		if err := m.SwitchMode("dpapi", second, ""); !errors.Is(err, ErrNativeUnavailable) {
			t.Fatalf("switch to unavailable native protection: %v", err)
		}
		return
	}
	if err := m.SwitchMode("dpapi", second, ""); err != nil {
		t.Fatal(err)
	}
	if KeyFileMode(directory) != "dpapi" {
		t.Fatal("key file mode unchanged")
	}
	automatic, _ := NewManager(directory)
	if automatic.State() != "unlocked" {
		t.Fatal("a Windows-protected key did not unlock at start")
	}
	opens(t, automatic)
	if err := automatic.SwitchMode("password", "", first); err != nil {
		t.Fatal(err)
	}
	if locked, _ := NewManager(directory); locked.State() != "locked" {
		t.Fatal("a passphrase key unlocked without the passphrase")
	}
	keyFile, _ := os.ReadFile(filepath.Join(directory, keyFileName))
	if bytes.Contains(keyFile, []byte(first)) || bytes.Contains(keyFile, []byte("synthetic-credential")) {
		t.Fatal("plaintext in key file")
	}
}
