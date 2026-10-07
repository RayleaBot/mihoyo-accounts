package app

import (
	"context"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
	"slices"
	"strings"
)

func (a *App) manageCloudCredential(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, record accounts.Record, action string, input map[string]any) (map[string]any, error) {
	var q struct {
		Revision uint64         `json:"revision"`
		Game     string         `json:"game"`
		Box      vault.InputBox `json:"box"`
	}
	if decode(input, &q) != nil || !slices.Contains([]string{"genshin", "zzz"}, q.Game) || q.Revision != record.Revision {
		return nil, failure("plugin.account_changed")
	}
	for _, key := range []string{"token", "combo_token", "cookie"} {
		if _, exists := input[key]; exists {
			return nil, failure("plugin.account_input_invalid")
		}
	}
	value := ""
	if action == "accounts.cloud.save" {
		plain, err := a.Vault.DecodeInput(q.Box, "account.cloud.save")
		if err != nil {
			return nil, err
		}
		defer clear(plain)
		if len(plain) < 1 || len(plain) > 8192 {
			return nil, failure("plugin.account_input_invalid")
		}
		for _, ch := range plain {
			if ch < 32 || ch > 126 {
				return nil, failure("plugin.account_input_invalid")
			}
		}
		value = strings.TrimSpace(string(plain))
		for _, key := range []string{"stoken=", "ltoken=", "cookie_token="} {
			if strings.Contains(strings.ToLower(value), key) {
				return nil, failure("plugin.account_input_invalid")
			}
		}
		if value == "" {
			return nil, failure("plugin.account_input_invalid")
		}
	} else if action != "accounts.cloud.clear" {
		return nil, failure("plugin.account_operation_denied")
	}
	if q.Game == "genshin" {
		record.Tokens.CloudGenshin = value
	} else {
		record.Tokens.CloudZZZ = value
	}
	delete(record.CloudStates, q.Game)
	if err := a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
		return nil, err
	}
	record.Revision++
	return map[string]any{"account": record.Summary()}, nil
}
