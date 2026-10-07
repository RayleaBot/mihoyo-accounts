package app

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func TestUnavailableNativeKeyExplainsManagementAndGroupFailures(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "master-key.json")
	keyFile := []byte(`{"version":1,"mode":"dpapi","wrapped":"AQIDBA=="}`)
	if err := os.WriteFile(path, keyFile, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(directory, &fakeAPI{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	store := &memoryStore{values: map[string]string{}}
	check := func(err error) {
		t.Helper()
		assertCode(t, err, "plugin.vault_locked")
		failure := publicError(err)
		if failure.Details["reason"] != "native_key_unavailable" {
			t.Fatalf("native recovery reason was lost: %v", failure)
		}
		if failure.Message == message("plugin.vault_locked") {
			t.Fatal("native key rejection was presented as an ordinary lock")
		}
	}
	status, err := a.Manage(t.Context(), store, policy(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status["vault_state"] != "locked" || status["key_protection"] != "dpapi" {
		t.Fatalf("incorrect vault status: %v", status)
	}
	diagnostic, ok := status["vault_error"].(map[string]any)
	if !ok {
		t.Fatal("management status omitted the recovery diagnostic")
	}
	check(&rayleabot.ActionError{Code: stringValue(diagnostic["code"]), Message: stringValue(diagnostic["message"]), Details: object(diagnostic["details"])})
	_, err = a.Manage(t.Context(), store, policy(), "vault.unlock", nil)
	check(err)
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		groupRequest := request(game, "fixture-account")
		groupRequest.Origin.EventType = "message.group"
		_, err = a.Service(t.Context(), store, policy(), groupRequest)
		check(err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, keyFile) || len(store.values) != 0 {
		t.Fatal("failed requests modified the key or account store", err)
	}
	recoveryDirectory := t.TempDir()
	password := "synthetic-recovery-password"
	if _, err = vault.CreateKeyFile(recoveryDirectory, password); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(recoveryDirectory, "master-key.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, restored, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Manage(t.Context(), store, policy(), "vault.unlock", map[string]any{"password_ciphertext": sealPassword(t, a, password)}); err != nil {
		t.Fatal("restored password key could not be unlocked", err)
	}
	status, err = a.Manage(t.Context(), store, policy(), "status", nil)
	if err != nil || status["vault_state"] != "unlocked" || status["vault_error"] != nil {
		t.Fatal("successful recovery kept the old failure", status, err)
	}
}

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
