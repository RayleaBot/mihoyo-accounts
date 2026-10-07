package app

import (
	"context"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

// A read the official API refuses as unauthenticated is repeated once with
// web tokens re-issued from the SToken, as another client of the same
// account may have replaced them; the account keeps the new tokens and stays
// valid.
func TestRefusedWebTokensAreReissuedFromTheSTokenAndTheReadRepeated(t *testing.T) {
	api := &fakeAPI{}
	api.execute = func(_ context.Context, tokens accounts.Tokens, _ accounts.Role, _ string, _ map[string]any) (map[string]any, error) {
		if tokens.CookieToken != "synthetic-refreshed-cookie" {
			return nil, &mihoyo.Error{Code: "plugin.upstream_auth_invalid", RetCode: -100, Reason: "请先登录"}
		}
		return map[string]any{"month": 1}, nil
	}
	a, store, record := setup(t, api)
	result, err := a.Service(t.Context(), store, policy(), request("starrail", record.Ref))
	if err != nil || api.refreshCalls.Load() != 1 || api.executeCalls.Load() != 2 || result["data"].(map[string]any)["month"] != 1 {
		t.Fatalf("result %v, %v after %d refreshes and %d reads", result, err, api.refreshCalls.Load(), api.executeCalls.Load())
	}
	cipher, _ := a.Vault.Cipher()
	current, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || current.Status != "valid" || current.Tokens.CookieToken != "synthetic-refreshed-cookie" || current.Tokens.SToken != record.Tokens.SToken || current.LastCheck == nil || current.LastCheck.Kind != "refresh" || current.LastCheck.State != "available" {
		t.Fatalf("saved %+v, %v", current.Summary(), err)
	}
}

// An account marked invalid reads again once its tokens are re-issued; when
// the SToken is refused as well, nothing is read and it stays invalid.
func TestInvalidAccountRecoversOnlyWhileTheSTokenWorks(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	markInvalid := func() {
		t.Helper()
		current, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
		if err != nil {
			t.Fatal(err)
		}
		current.Status = "invalid"
		if err := a.Repo.Update(t.Context(), store, cipher, current, current.Revision); err != nil {
			t.Fatal(err)
		}
	}
	markInvalid()
	if _, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref)); err != nil || api.refreshCalls.Load() != 1 || api.executeCalls.Load() != 1 {
		t.Fatalf("recovery: %v after %d refreshes and %d reads", err, api.refreshCalls.Load(), api.executeCalls.Load())
	}
	if current, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref); current.Status != "valid" {
		t.Fatalf("status %q after recovery", current.Status)
	}
	api.refresh = func(context.Context, accounts.Tokens) (accounts.Tokens, error) {
		return accounts.Tokens{}, &mihoyo.Error{Code: "plugin.upstream_auth_invalid", RetCode: -100}
	}
	markInvalid()
	_, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
	assertCode(t, err, "plugin.upstream_auth_invalid")
	if current, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref); current.Status != "invalid" || api.executeCalls.Load() != 1 || current.LastCheck == nil || current.LastCheck.State != "auth_invalid" {
		t.Fatalf("status %q, %d reads, check %+v", current.Status, api.executeCalls.Load(), current.LastCheck)
	}
}
