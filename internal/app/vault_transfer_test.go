package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func encryptedTestPassword(t *testing.T, a *App, password string) string {
	t.Helper()
	jwk := a.Vault.PublicKey()
	n, _ := base64.RawURLEncoding.DecodeString(jwk["n"].(string))
	e, _ := base64.RawURLEncoding.DecodeString(jwk["e"].(string))
	pub := rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	sealed, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &pub, []byte(password), []byte("raylea.mihoyo-accounts/key-input-v1"))
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sealed)
}
func TestEncryptedAccountMigrationAndRotation(t *testing.T) {
	source, store, record := setup(t, &fakeAPI{})
	ctx := t.Context()
	cipher, _ := source.Vault.Cipher()
	record.Delegations = []accounts.Delegation{{Ref: "synthetic-grant", Caller: "raylea.genshin", TaskID: "synthetic-task", RoleRef: record.Roles[0].Ref, Operation: "genshin.note", ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli()}}
	if err := source.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Repo.SetPublic(ctx, store, cipher, record.Ref, record.Owner, true, 10, []string{"genshin"}); err != nil {
		t.Fatal(err)
	}
	_, err := source.Repo.Select(ctx, store, cipher, record.Owner, record.Ref, record.Roles[0].Ref)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := source.Manage(ctx, store, policy(), "vault.export.start", map[string]any{"password_ciphertext": encryptedTestPassword(t, source, "synthetic-backup-password")})
	if err != nil {
		t.Fatal(err)
	}
	ref := exported["transfer"].(map[string]any)["ref"].(string)
	offset := 0
	data := ""
	for {
		part, err := source.Manage(ctx, store, policy(), "vault.export.read", map[string]any{"ref": ref, "offset": offset})
		if err != nil {
			t.Fatal(err)
		}
		data += part["chunk"].(string)
		offset = part["next_offset"].(int)
		if !part["more"].(bool) {
			break
		}
	}
	for _, marker := range []string{"synthetic-stoken", "synthetic-cookie", "synthetic-ltoken", "synthetic-backup-password"} {
		if strings.Contains(data, marker) {
			t.Fatal("plaintext escaped encrypted backup")
		}
	}
	target, err := New(t.TempDir(), &fakeAPI{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	if err = target.Vault.Initialize("synthetic-target-password", false); err != nil {
		t.Fatal(err)
	}
	targetStore := &memoryStore{values: map[string]string{}}
	upload := func() error {
		start, err := target.Manage(ctx, targetStore, policy(), "vault.import.start", nil)
		if err != nil {
			return err
		}
		ref := start["transfer"].(map[string]any)["ref"].(string)
		payload := map[string]any{"ref": ref, "offset": 0, "chunk": data}
		if _, err = target.Manage(ctx, targetStore, policy(), "vault.import.append", payload); err != nil {
			return err
		}
		if _, err = target.Manage(ctx, targetStore, policy(), "vault.import.append", payload); err != nil {
			return err
		}
		_, err = target.Manage(ctx, targetStore, policy(), "vault.import.finish", map[string]any{"ref": ref, "password_ciphertext": encryptedTestPassword(t, target, "synthetic-backup-password")})
		return err
	}
	targetStore.fail = true
	if err = upload(); err == nil || len(targetStore.values) != 0 {
		t.Fatal("failed restore partially committed")
	}
	targetStore.fail = false
	if err = upload(); err != nil {
		t.Fatal(err)
	}
	reader, _ := target.Vault.Cipher()
	restored, err := target.Repo.Get(ctx, targetStore, reader, record.Ref)
	if err != nil || restored.Tokens != record.Tokens || restored.Owner != record.Owner {
		t.Fatal("account migration lost identity or tokens")
	}
	if len(restored.Delegations) != 0 {
		t.Fatal("migration reactivated source scheduler grants")
	}
	if restored.PublicQuery != nil {
		t.Fatal("migration reactivated public sharing")
	}
	defaults, err := target.Repo.Defaults(ctx, targetStore, reader, record.Owner)
	if err != nil || defaults[record.Roles[0].Game].AccountRef != record.Ref {
		t.Fatal("default role lost")
	}
	assertCode(t, upload(), "plugin.vault_restore_nonempty")
	originalKey := reader.KeyID()
	_, err = target.Manage(ctx, targetStore, policy(), "vault.rotate", map[string]any{"password_ciphertext": encryptedTestPassword(t, target, "synthetic-wrong-password")})
	assertCode(t, err, "plugin.vault_rotation_auth_failed")
	if target.Vault.State() != "unlocked" {
		t.Fatal("failed rotation changed vault availability")
	}
	_, err = target.Manage(ctx, targetStore, policy(), "vault.rotate", map[string]any{"password_ciphertext": encryptedTestPassword(t, target, "synthetic-target-password")})
	if err != nil {
		t.Fatal(err)
	}
	reader, _ = target.Vault.Cipher()
	if reader.KeyID() == originalKey {
		t.Fatal("rotation did not advance")
	}
	for page := 0; ; page++ {
		result, err := target.Manage(ctx, targetStore, policy(), "vault.reencrypt", map[string]any{"page": page})
		if err != nil {
			t.Fatal(err)
		}
		if result["complete"].(bool) {
			break
		}
	}
	for _, value := range targetStore.values {
		var envelope map[string]any
		_ = json.Unmarshal([]byte(value), &envelope)
		if envelope["key_id"] != reader.KeyID() {
			t.Fatal("active row retained old write key")
		}
		if strings.Contains(value, "synthetic-stoken") {
			t.Fatal("plaintext database value")
		}
	}
}
func TestVaultTransfersBoundedAndCanceledOnLock(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	ctx := t.Context()
	first := ""
	for i := 0; i < 4; i++ {
		out, err := a.Manage(ctx, store, policy(), "vault.import.start", nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = out["transfer"].(map[string]any)["ref"].(string)
		}
	}
	_, err := a.Manage(ctx, store, policy(), "vault.import.start", nil)
	assertCode(t, err, "plugin.vault_transfer_limit")
	_, err = a.Manage(ctx, store, policy(), "vault.import.append", map[string]any{"ref": first, "offset": 1, "chunk": "ciphertext"})
	assertCode(t, err, "plugin.vault_transfer_conflict")
	a.transfers.mu.Lock()
	a.transfers.items[first].Touched = time.Now().Add(-11 * time.Minute)
	a.transfers.mu.Unlock()
	_, err = a.Manage(ctx, store, policy(), "vault.import.append", map[string]any{"ref": first, "offset": 0, "chunk": "ciphertext"})
	assertCode(t, err, "plugin.vault_transfer_missing")
	if _, err = a.Manage(ctx, store, policy(), "vault.lock", nil); err != nil {
		t.Fatal(err)
	}
	a.transfers.mu.Lock()
	count := len(a.transfers.items)
	a.transfers.mu.Unlock()
	if count != 0 {
		t.Fatal("lock retained transfer buffers")
	}
}
