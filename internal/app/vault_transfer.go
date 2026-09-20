package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/game-plugin-kit/vault"
)

const transferChunk = 64 * 1024

type vaultTransfer struct {
	Kind    string
	Data    []byte
	Touched time.Time
}
type vaultTransfers struct {
	mu    sync.Mutex
	items map[string]*vaultTransfer
}

func (v *vaultTransfers) clear() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for ref, item := range v.items {
		clear(item.Data)
		delete(v.items, ref)
	}
}
func (v *vaultTransfers) cleanup() {
	for ref, item := range v.items {
		if time.Since(item.Touched) > 10*time.Minute {
			clear(item.Data)
			delete(v.items, ref)
		}
	}
}
func (v *vaultTransfers) start(kind string, data []byte) (map[string]any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cleanup()
	if len(v.items) >= 4 {
		return nil, failure("plugin.vault_transfer_limit")
	}
	if v.items == nil {
		v.items = map[string]*vaultTransfer{}
	}
	ref := rand.Text()
	v.items[ref] = &vaultTransfer{Kind: kind, Data: data, Touched: time.Now()}
	return map[string]any{"ref": ref, "size": len(data), "chunk_size": transferChunk}, nil
}
func (v *vaultTransfers) access(action, ref string, offset int, chunk string) (map[string]any, []byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cleanup()
	item, ok := v.items[ref]
	if !ok {
		return nil, nil, failure("plugin.vault_transfer_missing")
	}
	item.Touched = time.Now()
	switch action {
	case "vault.transfer.cancel":
		clear(item.Data)
		delete(v.items, ref)
		return map[string]any{"canceled": true}, nil, nil
	case "vault.export.read":
		if item.Kind != "export" || offset < 0 || offset > len(item.Data) {
			break
		}
		end := min(offset+transferChunk, len(item.Data))
		return map[string]any{"chunk": string(item.Data[offset:end]), "next_offset": end, "more": end < len(item.Data)}, nil, nil
	case "vault.import.append":
		if item.Kind != "import" || offset < 0 || len(chunk) == 0 || len(chunk) > transferChunk || offset > len(item.Data) || offset+len(chunk) > vault.MaxBackupBytes {
			break
		}
		if offset < len(item.Data) {
			if offset+len(chunk) > len(item.Data) || string(item.Data[offset:offset+len(chunk)]) != chunk {
				break
			}
		} else {
			item.Data = append(item.Data, []byte(chunk)...)
		}
		return map[string]any{"offset": len(item.Data)}, nil, nil
	case "vault.import.finish":
		if item.Kind != "import" || len(item.Data) == 0 {
			break
		}
		copy := append([]byte{}, item.Data...)
		clear(item.Data)
		delete(v.items, ref)
		return nil, copy, nil
	}
	return nil, nil, failure("plugin.vault_transfer_conflict")
}
func (a *App) manageVault(ctx context.Context, store accounts.SecretStore, action string, input map[string]any) (map[string]any, error) {
	a.vaultGate.Lock()
	defer a.vaultGate.Unlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	password := func() (string, error) {
		value := stringValue(input["password_ciphertext"])
		if value == "" {
			return "", failure("plugin.account_input_invalid")
		}
		return a.Vault.DecodePassword(value)
	}
	switch action {
	case "vault.rotate":
		secret := ""
		if a.Vault.Mode() == "password" {
			secret, err = password()
			if err != nil {
				return nil, err
			}
		}
		count, err := a.Vault.Rotate(secret)
		if err != nil {
			if errors.Is(err, vault.ErrLocked) {
				return nil, failure("plugin.vault_rotation_auth_failed")
			}
			if errors.Is(err, vault.ErrKeyGenerations) {
				return nil, failure("plugin.vault_rotation_limit")
			}
			return nil, err
		}
		a.transfers.clear()
		return map[string]any{"rotated": true, "key_generations": count, "next_page": 0}, nil
	case "vault.reencrypt":
		page := 0
		if raw, ok := input["page"]; ok {
			if raw == nil || decode(raw, &page) != nil || page < 0 {
				return nil, failure("plugin.account_input_invalid")
			}
		}
		next, err := a.Repo.ReencryptPage(ctx, store, cipher, page)
		return map[string]any{"next_page": next, "complete": next == nil}, err
	case "vault.export.start":
		secret, err := password()
		if err != nil {
			return nil, err
		}
		snapshot, err := a.Repo.Snapshot(ctx, store, cipher)
		if err != nil {
			return nil, err
		}
		plain, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		defer clear(plain)
		sealed, err := vault.SealBackup(plain, secret)
		if err != nil {
			return nil, err
		}
		transfer, err := a.transfers.start("export", sealed)
		if err != nil {
			clear(sealed)
		}
		return map[string]any{"transfer": transfer}, err
	case "vault.import.start":
		transfer, err := a.transfers.start("import", nil)
		return map[string]any{"transfer": transfer}, err
	}
	if !strings.HasPrefix(action, "vault.export.") && !strings.HasPrefix(action, "vault.import.") && action != "vault.transfer.cancel" {
		return nil, failure("plugin.account_input_invalid")
	}
	offset := 0
	if value, exists := input["offset"]; exists {
		if value == nil || decode(value, &offset) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
	}
	if action == "vault.import.finish" {
		secret, err := password()
		if err != nil {
			return nil, err
		}
		_, raw, err := a.transfers.access(action, stringValue(input["ref"]), 0, "")
		if err != nil {
			return nil, err
		}
		defer clear(raw)
		plain, err := vault.OpenBackup(raw, secret)
		if err != nil {
			return nil, err
		}
		defer clear(plain)
		var snapshot accounts.Snapshot
		if json.Unmarshal(plain, &snapshot) != nil {
			return nil, accounts.ErrBackupInvalid
		}
		if err = a.Repo.Restore(ctx, store, cipher, snapshot); err != nil {
			return nil, err
		}
		return map[string]any{"restored": len(snapshot.Accounts)}, nil
	}
	result, _, err := a.transfers.access(action, stringValue(input["ref"]), offset, stringValue(input["chunk"]))
	return result, err
}
func vaultPublicFailure(err error) error {
	if errors.Is(err, accounts.ErrBackupInvalid) {
		return failure("plugin.vault_backup_invalid")
	}
	if errors.Is(err, accounts.ErrRestoreNonempty) {
		return failure("plugin.vault_restore_nonempty")
	}
	return err
}
