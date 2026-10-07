package app

import (
	"context"
	"testing"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

// A sign-in read refused as unauthenticated is repeated with re-issued
// tokens like any other read; when it stays refused, the account keeps its
// sign-in code and is not marked invalid.
func TestSignRefusalIsRetriedAfterRefreshWithoutMarkingInvalid(t *testing.T) {
	api := &fakeAPI{}
	api.execute = func(_ context.Context, tokens accounts.Tokens, _ accounts.Role, _ string, _ map[string]any) (map[string]any, error) {
		if tokens.CookieToken != "synthetic-refreshed-cookie" {
			return nil, &mihoyo.Error{Code: "plugin.upstream_sign_unavailable", RetCode: -100}
		}
		return map[string]any{"is_sign": false}, nil
	}
	a, store, record := setup(t, api)
	req := request("genshin", record.Ref)
	req.Params["operation"] = "genshin.sign_info"
	result, err := a.Service(t.Context(), store, policy(), req)
	if err != nil || api.refreshCalls.Load() != 1 || result["data"].(map[string]any)["is_sign"] != false {
		t.Fatalf("result %v, %v after %d refreshes", result, err, api.refreshCalls.Load())
	}
	api.execute = func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error) {
		return nil, &mihoyo.Error{Code: "plugin.upstream_sign_unavailable", RetCode: -100}
	}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.upstream_sign_unavailable")
	cipher, _ := a.Vault.Cipher()
	if current, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref); current.Status != "valid" || api.refreshCalls.Load() != 2 {
		t.Fatalf("status %q after %d refreshes", current.Status, api.refreshCalls.Load())
	}
}

// After the SToken is refused, automatic refreshes leave it alone for a
// while; a later call tries again, and the management page's refresh always
// does.
func TestRefusedSTokenIsNotAskedAgainWithinTheInterval(t *testing.T) {
	api := &fakeAPI{}
	api.execute = func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error) {
		return nil, &mihoyo.Error{Code: "plugin.upstream_auth_invalid", RetCode: -100}
	}
	api.refresh = func(context.Context, accounts.Tokens) (accounts.Tokens, error) {
		return accounts.Tokens{}, &mihoyo.Error{Code: "plugin.upstream_auth_invalid", RetCode: -100}
	}
	a, store, record := setup(t, api)
	for range 2 {
		_, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
		assertCode(t, err, "plugin.upstream_auth_invalid")
	}
	if api.refreshCalls.Load() != 1 || api.executeCalls.Load() != 1 {
		t.Fatalf("%d refreshes and %d reads inside the interval", api.refreshCalls.Load(), api.executeCalls.Load())
	}
	cipher, _ := a.Vault.Cipher()
	current, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || current.Status != "invalid" {
		t.Fatalf("status %q, %v", current.Status, err)
	}
	current.LastCheck.CheckedAtMS = time.Now().Add(-refreshRetryInterval - time.Minute).UnixMilli()
	if err := a.Repo.Update(t.Context(), store, cipher, current, current.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
	assertCode(t, err, "plugin.upstream_auth_invalid")
	if api.refreshCalls.Load() != 2 {
		t.Fatalf("%d refreshes after the interval", api.refreshCalls.Load())
	}
	_, err = a.Manage(t.Context(), store, policy(), "accounts.refresh", map[string]any{"account_ref": record.Ref})
	assertCode(t, err, "plugin.upstream_auth_invalid")
	if api.refreshCalls.Load() != 3 {
		t.Fatalf("%d refreshes after the explicit refresh", api.refreshCalls.Load())
	}
}

// The check probes the Cookie Token without the LToken, re-issues it from
// the SToken when refused, and marks an account without an SToken invalid.
func TestCheckProbesTheCookieTokenAndReissuesIt(t *testing.T) {
	api := &fakeAPI{roles: func(_ context.Context, tokens accounts.Tokens, _ string) ([]accounts.Role, error) {
		if tokens.LToken == "" && tokens.CookieToken != "synthetic-refreshed-cookie" {
			return nil, &mihoyo.Error{Code: "plugin.upstream_auth_invalid", RetCode: -100}
		}
		return []accounts.Role{role("genshin")}, nil
	}}
	a, store, record := setup(t, api)
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.check", map[string]any{"account_ref": record.Ref}); err != nil || api.refreshCalls.Load() != 1 {
		t.Fatalf("check: %v after %d refreshes", err, api.refreshCalls.Load())
	}
	cipher, _ := a.Vault.Cipher()
	saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || saved.Status != "valid" || saved.Tokens.CookieToken != "synthetic-refreshed-cookie" || saved.LastCheck == nil || saved.LastCheck.Kind != "check" || saved.LastCheck.State != "available" {
		t.Fatalf("saved %+v, %v", saved.Summary(), err)
	}
	saved.Tokens.SToken, saved.Tokens.CookieToken = "", "synthetic-cookie"
	if err := a.Repo.Update(t.Context(), store, cipher, saved, saved.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = a.Manage(t.Context(), store, policy(), "accounts.check", map[string]any{"account_ref": record.Ref})
	assertCode(t, err, "plugin.upstream_auth_invalid")
	if current, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref); current.Status != "invalid" || api.refreshCalls.Load() != 1 || current.LastCheck.State != "auth_invalid" {
		t.Fatalf("status %q, check %+v after %d refreshes", current.Status, current.LastCheck, api.refreshCalls.Load())
	}
}
