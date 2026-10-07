package app

import (
	"context"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
	"testing"
	"time"
)

func TestExplicitSignConsentAndNoAutomaticWriteReplay(t *testing.T) {
	api := &fakeAPI{execute: func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error) {
		return nil, &mihoyo.Error{Code: "plugin.upstream_device_required"}
	}}
	a, store, record := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	record.Tokens.DeviceFP = "synthetic-existing-fp"
	record.Tokens.DeviceFPExpiresAtMS = time.Now().Add(time.Hour).UnixMilli()
	if err := a.Repo.Update(t.Context(), store, cipher, record, record.Revision); err != nil {
		t.Fatal(err)
	}
	req := request("genshin", record.Ref)
	req.Params["operation"] = "genshin.sign"
	_, err := a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_write_confirmation_required")
	if api.executeCalls.Load() != 0 {
		t.Fatal("unconfirmed write reached upstream")
	}
	req.Params["write_confirmed"] = true
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.upstream_device_required")
	if api.executeCalls.Load() != 1 {
		t.Fatal("write was automatically retried")
	}
	saved, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if saved.Status == "invalid" {
		t.Fatal("device verification invalidated credentials")
	}
}

func TestSignDelegationRequiresSeparateWriteConsent(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	req := request("genshin", record.Ref)
	req.Method = "delegation.create"
	req.Params["operation"] = "genshin.sign"
	req.Params["days"] = 30
	req.Params["task_id"] = "sign-task"
	_, err := a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_write_confirmation_required")
	req.Params["write_confirmed"] = true
	if _, err = a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
}
