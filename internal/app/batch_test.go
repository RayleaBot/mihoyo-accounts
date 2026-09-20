package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

func startBatch(t *testing.T, a *App, s *memoryStore, mode string, items ...accounts.Record) batchView {
	t.Helper()
	targets := []batchItem{}
	for _, r := range items {
		targets = append(targets, batchItem{r.Ref, r.Revision})
	}
	out, err := a.Manage(t.Context(), s, policy(), "accounts.batch.start", map[string]any{"mode": mode, "items": targets, "confirm": true})
	if err != nil {
		t.Fatal(err)
	}
	return out["batch"].(batchView)
}
func stepBatch(t *testing.T, a *App, s *memoryStore, v batchView) batchView {
	t.Helper()
	out, err := a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": v.Sequence})
	if err != nil {
		t.Fatal(err)
	}
	return out["batch"].(batchView)
}
func TestAccountCheckDistinguishesAuthenticationFromOtherFailures(t *testing.T) {
	for _, code := range []string{"", "plugin.upstream_auth_invalid", "plugin.upstream_device_required", "plugin.upstream_challenge_required", "plugin.upstream_rate_limited", "plugin.upstream_unavailable"} {
		t.Run(code, func(t *testing.T) {
			api := &fakeAPI{roles: func(_ context.Context, _ accounts.Tokens, game string) ([]accounts.Role, error) {
				if game != "genshin" {
					t.Fatal("unexpected endpoint")
				}
				if code != "" {
					return nil, &mihoyo.Error{Code: code}
				}
				return []accounts.Role{}, nil
			}}
			a, store, record := setup(t, api)
			_, err := a.Manage(t.Context(), store, policy(), "accounts.check", map[string]any{"account_ref": record.Ref})
			if (err != nil) != (code != "") {
				t.Fatal(err)
			}
			cipher, _ := a.Vault.Cipher()
			saved, e := a.Repo.Get(t.Context(), store, cipher, record.Ref)
			if e != nil || saved.Tokens != record.Tokens || len(saved.Roles) != 3 || saved.LastCheck == nil || !saved.LastCheck.Valid() || api.refreshCalls.Load() != 0 {
				t.Fatal("check changed credentials or roles", e)
			}
			if (saved.Status == "invalid") != (code == "plugin.upstream_auth_invalid") {
				t.Fatal("temporary failure marked invalid", saved.Status)
			}
			view, _ := json.Marshal(saved.Summary())
			for _, token := range []string{record.Tokens.SToken, record.Tokens.CookieToken, record.Tokens.LToken} {
				if strings.Contains(string(view), token) {
					t.Fatal("check exposed credential")
				}
				for _, sealed := range store.values {
					if strings.Contains(sealed, token) {
						t.Fatal("check wrote plaintext")
					}
				}
			}
		})
	}
}
func TestAccountBatchReplayCancellationAndVaultLock(t *testing.T) {
	api := &fakeAPI{}
	a, s, record := setup(t, api)
	v := startBatch(t, a, s, "refresh", record)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": 0}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	done := stepBatch(t, a, s, v)
	if done.State != "completed" || done.Processed != 1 || api.refreshCalls.Load() != 1 {
		t.Fatal(done)
	}
	_, err := a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": 2})
	assertCode(t, err, "plugin.account_batch_sequence")
	v = startBatch(t, a, s, "check", record)
	_, err = a.Manage(t.Context(), s, policy(), "vault.lock", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": 0})
	assertCode(t, err, "plugin.vault_locked")
	if err = a.Vault.Unlock("synthetic-passphrase-only"); err != nil {
		t.Fatal(err)
	}
	v = stepBatch(t, a, s, v)
	if v.Sequence != 1 {
		t.Fatal(v)
	}
	v = startBatch(t, a, s, "refresh", record)
	_, err = a.Manage(t.Context(), s, policy(), "accounts.batch.cancel", map[string]any{"ref": v.Ref})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": 0})
	assertCode(t, err, "plugin.account_batch_sequence")
	if api.refreshCalls.Load() != 1 {
		t.Fatal("canceled batch refreshed")
	}
	// Explicit cancellation frees capacity for later batches.
	for range 10 {
		v = startBatch(t, a, s, "check", record)
		_, err = a.Manage(t.Context(), s, policy(), "accounts.batch.cancel", map[string]any{"ref": v.Ref})
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestAccountBatchCleanupSkipsChangedOrRecoveredAccounts(t *testing.T) {
	a, s, r := setup(t, &fakeAPI{})
	cipher, _ := a.Vault.Cipher()
	r.Status = "invalid"
	if err := a.Repo.Update(t.Context(), s, cipher, r, r.Revision); err != nil {
		t.Fatal(err)
	}
	r, _ = a.Repo.Get(t.Context(), s, cipher, r.Ref)
	v := startBatch(t, a, s, "remove_invalid", r)
	if _, err := a.Repo.Bind(t.Context(), s, cipher, r.Owner, r.Tokens, r.Roles, r.AllowedCallers); err != nil {
		t.Fatal(err)
	}
	v = stepBatch(t, a, s, v)
	if v.Results[0].State != "skipped" {
		t.Fatal(v)
	}
	r, err := a.Repo.Get(t.Context(), s, cipher, r.Ref)
	if err != nil || r.Status != "valid" {
		t.Fatal("fresh login removed", err)
	}
	r.Status = "invalid"
	if err = a.Repo.Update(t.Context(), s, cipher, r, r.Revision); err != nil {
		t.Fatal(err)
	}
	r, _ = a.Repo.Get(t.Context(), s, cipher, r.Ref)
	v = startBatch(t, a, s, "remove_invalid", r)
	v = stepBatch(t, a, s, v)
	if v.Results[0].State != "success" {
		t.Fatal(v)
	}
	if _, err = a.Repo.Get(t.Context(), s, cipher, r.Ref); err != accounts.ErrNotFound {
		t.Fatal("invalid account not removed", err)
	}
}
func TestAccountStatisticsPageReplayAndSubjectIdentity(t *testing.T) {
	api := &fakeAPI{roles: func(context.Context, accounts.Tokens, string) ([]accounts.Role, error) {
		t.Fatal("statistics queried upstream")
		return nil, nil
	}}
	a, s, _ := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	for i := 0; i < 129; i++ {
		owner := subject()
		owner.BotID = fmt.Sprintf("bot-%d", i/16)
		tokens := syntheticTokens()
		tokens.AccountID = fmt.Sprintf("%d", 10000+i)
		if _, err := a.Repo.Bind(t.Context(), s, cipher, owner, tokens, []accounts.Role{role("genshin")}, nil); err != nil {
			t.Fatal(err)
		}
	}
	v := startBatch(t, a, s, "statistics")
	first := stepBatch(t, a, s, v)
	replay := stepBatch(t, a, s, v)
	if first.Statistics.Accounts != 128 || replay.Statistics.Accounts != 128 || first.State != "running" {
		t.Fatal(first)
	}
	done := stepBatch(t, a, s, first)
	if done.State != "completed" || done.Statistics.Accounts != 130 || done.Statistics.Subjects != 9 || done.Statistics.GameRoles["genshin"] != 130 || done.Statistics.Pages != 2 {
		t.Fatal(done.Statistics)
	}
	if api.refreshCalls.Load() != 0 || api.executeCalls.Load() != 0 {
		t.Fatal("stats used upstream")
	}
}
func TestAccountBatchValidationExpiryAndStorageFailure(t *testing.T) {
	a, s, r := setup(t, &fakeAPI{})
	for _, input := range []map[string]any{{"mode": "refresh", "items": []batchItem{{r.Ref, r.Revision}}}, {"mode": "check", "items": []batchItem{{r.Ref, r.Revision}, {r.Ref, r.Revision}}}, {"mode": "statistics", "items": []batchItem{{r.Ref, r.Revision}}}, {"mode": "check", "items": []batchItem{{r.Ref, 0}}}} {
		_, err := a.Manage(t.Context(), s, policy(), "accounts.batch.start", input)
		assertCode(t, err, "plugin.account_input_invalid")
	}
	v := startBatch(t, a, s, "check", r)
	s.fail = true
	_, err := a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": 0})
	assertCode(t, err, "plugin.account_operation_failed")
	s.fail = false
	v = stepBatch(t, a, s, v)
	if v.Processed != 1 {
		t.Fatal(v)
	}
	job, _ := a.batches.get(v.Ref)
	job.mu.Lock()
	job.expires = time.Now().Add(-time.Minute)
	job.mu.Unlock()
	_, err = a.Manage(t.Context(), s, policy(), "accounts.batch.step", map[string]any{"ref": v.Ref, "sequence": 0})
	assertCode(t, err, "plugin.account_batch_missing")
}

func TestFingerprintOrMetadataUpdateDoesNotPretendCredentialsWereRefreshed(t *testing.T) {
	api := &fakeAPI{}
	a, s, r := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	release, err := a.acquire(t.Context(), r.Ref)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.Manage(t.Context(), s, policy(), "accounts.refresh", map[string]any{"account_ref": r.Ref})
		done <- err
	}()
	ready := false
	for range 1000 {
		a.locksMu.Lock()
		users := a.locks[r.Ref].users
		a.locksMu.Unlock()
		if users == 2 {
			ready = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !ready {
		release()
		t.Fatal("refresh did not wait for current account operation")
	}
	r.Tokens.DeviceFP = "updated-synthetic-fingerprint"
	r.Tokens.DeviceFPExpiresAtMS = time.Now().Add(time.Hour).UnixMilli()
	if err = a.Repo.Update(t.Context(), s, cipher, r, r.Revision); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if api.refreshCalls.Load() != 1 {
		t.Fatal("fingerprint change skipped credential exchange")
	}
}

func TestBatchMissingRefreshMaterialPreservesUsableCookie(t *testing.T) {
	api := &fakeAPI{}
	a, s, r := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	r.Tokens.SToken = ""
	if err := a.Repo.Update(t.Context(), s, cipher, r, r.Revision); err != nil {
		t.Fatal(err)
	}
	r, _ = a.Repo.Get(t.Context(), s, cipher, r.Ref)
	v := startBatch(t, a, s, "refresh", r)
	v = stepBatch(t, a, s, v)
	if v.Results[0].State != "skipped" || v.Results[0].Code != "plugin.account_refresh_unavailable" || api.refreshCalls.Load() != 0 {
		t.Fatal(v)
	}
	saved, err := a.Repo.Get(t.Context(), s, cipher, r.Ref)
	if err != nil || saved.Status != "valid" || saved.Revision != r.Revision || saved.Tokens != r.Tokens {
		t.Fatal("refresh material missing invalidated account", err)
	}
	_, err = a.Manage(t.Context(), s, policy(), "accounts.batch.start", map[string]any{"mode": "statistics", "password": "synthetic-plaintext"})
	assertCode(t, err, "plugin.account_input_invalid")
}
