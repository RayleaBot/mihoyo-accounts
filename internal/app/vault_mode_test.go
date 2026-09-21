package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
)

func sealPassword(t *testing.T, a *App, value string) string {
	t.Helper()
	jwk := a.Vault.PublicKey()
	n, _ := base64.RawURLEncoding.DecodeString(jwk["n"].(string))
	e, _ := base64.RawURLEncoding.DecodeString(jwk["e"].(string))
	public := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	sealed, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, public, []byte(value), []byte("raylea.mihoyo-accounts/key-input-v1"))
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sealed)
}

func TestManagementSwitchesKeyProtectionWithoutReencrypting(t *testing.T) {
	directory := t.TempDir()
	a, err := New(directory, &fakeAPI{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	first, second := "synthetic-passphrase-only", "synthetic-second-passphrase"
	if err = a.Vault.Initialize(first, false); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{values: map[string]string{}}
	cipher, _ := a.Vault.Cipher()
	record, err := a.Repo.Bind(t.Context(), store, cipher, subject(), syntheticTokens(), nil, policy().IDs())
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for key, value := range store.values {
		before[key] = value
	}

	_, err = a.Manage(t.Context(), store, policy(), "vault.mode", map[string]any{"mode": "password", "password_ciphertext": sealPassword(t, a, "incorrect-passphrase"), "new_password_ciphertext": sealPassword(t, a, second)})
	assertCode(t, err, "plugin.vault_locked")
	_, err = a.Manage(t.Context(), store, policy(), "vault.mode", map[string]any{"mode": "password", "password_ciphertext": sealPassword(t, a, first), "new_password_ciphertext": sealPassword(t, a, "short")})
	assertCode(t, err, "plugin.vault_password_invalid")
	result, err := a.Manage(t.Context(), store, policy(), "vault.mode", map[string]any{"mode": "password", "password_ciphertext": sealPassword(t, a, first), "new_password_ciphertext": sealPassword(t, a, second)})
	if err != nil || result["key_protection"] != "password" {
		t.Fatalf("switch = %v, %v", result, err)
	}
	for key, value := range before {
		if store.values[key] != value {
			t.Fatalf("switching protection rewrote stored record %s", key)
		}
	}

	restarted, err := vault.NewManager(directory)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Unlock(first) == nil {
		t.Fatal("the replaced passphrase still unlocks")
	}
	if err = restarted.Unlock(second); err != nil {
		t.Fatal(err)
	}
	reader, _ := restarted.Cipher()
	if _, err = a.Repo.Get(t.Context(), store, reader, record.Ref); err != nil {
		t.Fatal("account unreadable after switching protection", err)
	}
}
