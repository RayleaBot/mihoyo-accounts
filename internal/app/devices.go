package app

import (
	"context"
	"github.com/RayleaBot/game-plugin-kit/vault"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
	"regexp"
)

var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{16,64}$`)

func (a *App) manageDevice(ctx context.Context, store accounts.SecretStore, action string, input map[string]any) (map[string]any, error) {
	if _, ok := input["device_id"]; ok {
		return nil, failure("plugin.account_input_invalid")
	}
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	ref := stringValue(input["account_ref"])
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer release()
	record, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return nil, err
	}
	if action == "accounts.device.reset" {
		var q struct {
			Revision uint64          `json:"revision"`
			Confirm  bool            `json:"confirm"`
			Box      *vault.InputBox `json:"box"`
		}
		if decode(input, &q) != nil || !q.Confirm {
			return nil, failure("plugin.account_write_confirmation_required")
		}
		if q.Revision != record.Revision {
			return nil, accounts.ErrConflict
		}
		id := mihoyo.RandomDeviceID()
		if q.Box != nil {
			plain, err := a.Vault.DecodeInput(*q.Box, "account.device.reset")
			if err != nil {
				return nil, err
			}
			id = string(plain)
			clear(plain)
			if !deviceIDPattern.MatchString(id) {
				return nil, failure("plugin.account_input_invalid")
			}
		}
		record.Tokens.DeviceID = id
		record.Tokens.DeviceFP = ""
		record.Tokens.DeviceFPExpiresAtMS = 0
		if err = a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
			return nil, err
		}
		record.Revision++
	} else if action != "accounts.device.status" {
		return nil, failure("plugin.account_input_invalid")
	}
	label := record.Tokens.DeviceID
	if len(label) > 8 {
		label = label[:4] + "…" + label[len(label)-4:]
	} else if label != "" {
		label = "已设置"
	}
	return map[string]any{"revision": record.Revision, "device_label": label, "has_fingerprint": record.Tokens.DeviceFP != "", "fingerprint_expires_ms": record.Tokens.DeviceFPExpiresAtMS}, nil
}
