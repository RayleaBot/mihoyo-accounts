package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

func TestGameQueriesShareEncryptedFingerprintAndRetryOnlyOnce(t *testing.T) {
	registrations := 0
	api := &fakeAPI{fingerprint: func(_ context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
		registrations++
		tokens.DeviceFP, tokens.DeviceFPExpiresAtMS = "syntheticfingerprint", time.Now().Add(time.Hour).UnixMilli()
		return tokens, nil
	}}
	a, store, record := setup(t, api)
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		if _, err := a.Service(t.Context(), store, policy(), request(game, record.Ref)); err != nil {
			t.Fatal(err)
		}
	}
	if registrations != 1 {
		t.Fatal("three games did not share persisted fingerprint")
	}
	for _, encrypted := range store.values {
		if strings.Contains(encrypted, "syntheticfingerprint") {
			t.Fatal("fingerprint stored in plaintext")
		}
	}
	api.execute = func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error) {
		return nil, &mihoyo.Error{Code: "plugin.upstream_device_required", RetCode: 10041, Operation: "genshin.note"}
	}
	before := api.executeCalls.Load()
	_, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
	assertCode(t, err, "plugin.upstream_device_required")
	if registrations != 2 || api.executeCalls.Load()-before != 2 {
		t.Fatal("device rejection must renew once and retry once")
	}
	cipher, _ := a.Vault.Cipher()
	current, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || current.Status != "valid" || current.Tokens.SToken == "" {
		t.Fatal("device rejection invalidated credentials")
	}
	// A subsequent authentication failure must save against the new revision.
	api.execute = func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error) {
		return nil, &mihoyo.Error{Code: "plugin.upstream_auth_invalid", RetCode: 10001}
	}
	_, err = a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
	assertCode(t, err, "plugin.upstream_auth_invalid")
	current, err = a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || current.Status != "invalid" {
		t.Fatal("authentication failure did not persist")
	}
}

func TestFingerprintFailurePreservesAccountAndStopsBusinessRequest(t *testing.T) {
	api := &fakeAPI{fingerprint: func(context.Context, accounts.Tokens) (accounts.Tokens, error) {
		return accounts.Tokens{}, &mihoyo.Error{Code: "plugin.upstream_unavailable", Operation: "device.fingerprint"}
	}}
	a, store, record := setup(t, api)
	_, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
	assertCode(t, err, "plugin.upstream_unavailable")
	cipher, _ := a.Vault.Cipher()
	current, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || current.Revision != record.Revision || current.Status != "valid" || api.executeCalls.Load() != 0 {
		t.Fatal("fingerprint failure mutated account or sent an incomplete request")
	}
}
