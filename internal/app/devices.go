package app

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
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
	if action == "accounts.device.import" {
		var q struct {
			Revision uint64         `json:"revision"`
			Box      vault.InputBox `json:"box"`
		}
		if decode(input, &q) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		if q.Revision != record.Revision {
			return nil, accounts.ErrConflict
		}
		plain, err := a.Vault.DecodeInput(q.Box, "account.device.import")
		if err != nil {
			return nil, err
		}
		tokens, err := importDevice(plain, record.Tokens)
		clear(plain)
		if err != nil {
			return nil, err
		}
		if tokens.Device != nil {
			// As ZZZ-Plugin does, device information is registered for a
			// fingerprint at once.
			if tokens, err = a.API.Fingerprint(ctx, tokens); err != nil {
				return nil, err
			}
		}
		record.Tokens = tokens
		if err = a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
			return nil, err
		}
		record.Revision++
	} else if action == "accounts.device.reset" {
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
		resetTokens(&record.Tokens, id)
		if err = a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
			return nil, err
		}
		record.Revision++
	} else if action != "accounts.device.status" {
		return nil, failure("plugin.account_input_invalid")
	}
	// The management page shows the whole device ID: only CK is masked.
	result := map[string]any{"revision": record.Revision, "device_label": record.Tokens.DeviceID, "has_fingerprint": record.Tokens.DeviceFP != "", "fingerprint_expires_ms": record.Tokens.DeviceFPExpiresAtMS, "fingerprint_imported": record.Tokens.DeviceFPImported}
	if device := record.Tokens.Device; device != nil {
		result["device_model"] = device.Brand() + " " + device.Model
	}
	return result, nil
}

var devicePattern = regexp.MustCompile(`^[\x21-\x7e ]{1,256}$`)

// importDevice reads ZZZ-Plugin's two 绑定设备 forms: a device ID with its
// fingerprint, taken from the official app's requests, or the device
// information its Android helper copies.
func importDevice(raw []byte, tokens accounts.Tokens) (accounts.Tokens, error) {
	var input struct {
		DeviceID string `json:"device_id"`
		DeviceFP string `json:"device_fp"`
		accounts.Device
	}
	if len(raw) > 8192 || json.Unmarshal(raw, &input) != nil {
		return tokens, failure("plugin.account_input_invalid")
	}
	if input.DeviceID != "" || input.DeviceFP != "" {
		if !deviceIDPattern.MatchString(input.DeviceID) || !regexp.MustCompile(`^[A-Za-z0-9]{8,128}$`).MatchString(input.DeviceFP) {
			return tokens, failure("plugin.account_input_invalid")
		}
		tokens.DeviceID, tokens.DeviceFP, tokens.DeviceFPExpiresAtMS = input.DeviceID, input.DeviceFP, 0
		tokens.Device, tokens.DeviceFPImported = nil, true
		return tokens, nil
	}
	device := input.Device
	for _, value := range []string{device.Name, device.Board, device.Model, device.OAID, device.AndroidVersion, device.Fingerprint, device.Product} {
		if !devicePattern.MatchString(value) {
			return tokens, failure("plugin.account_input_invalid")
		}
	}
	tokens.Device, tokens.DeviceFP, tokens.DeviceFPExpiresAtMS, tokens.DeviceFPImported = &device, "", 0, false
	return tokens, nil
}
