package app

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
	"strings"
	"testing"
)

func TestPublicQueryRequiresOptInAndNeverRevealsLendingAccount(t *testing.T) {
	api := &fakeAPI{execute: func(_ context.Context, _ accounts.Tokens, role accounts.Role, name string, _ map[string]any) (map[string]any, error) {
		if role.UID != "100000002" || name != "genshin.profile" {
			t.Fatal("not a fixed public profile query")
		}
		return map[string]any{"stats": map[string]any{"active_day_number": 10}}, nil
	}}
	a, store, record := setup(t, api)
	ctx := t.Context()
	cipher, _ := a.Vault.Cipher()
	req := request("genshin", record.Ref)
	req.Method = "public.execute"
	req.Origin.Actor.ID = "another-user"
	req.Params = map[string]any{"game": "genshin", "uid": "100000002", "region": "cn_gf01"}
	_, err := a.Service(ctx, store, policy(), req)
	assertCode(t, err, "plugin.account_public_unavailable")
	if api.executeCalls.Load() != 0 {
		t.Fatal("private account was shared by default")
	}
	if _, err = a.Repo.SetPublic(ctx, store, cipher, record.Ref, record.Owner, true, 1, []string{"genshin"}); err != nil {
		t.Fatal(err)
	}
	store.fail = true
	_, err = a.Service(ctx, store, policy(), req)
	if err == nil || api.executeCalls.Load() != 0 {
		t.Fatal("uncommitted public budget reached upstream")
	}
	store.fail = false
	result, err := a.Service(ctx, store, policy(), req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	for _, marker := range []string{record.Ref, record.Owner.ActorID, "synthetic-stoken", "synthetic-cookie", "123456789"} {
		if strings.Contains(string(raw), marker) {
			t.Fatal("lender identity or credential exposed")
		}
	}
	_, err = a.Service(ctx, store, policy(), req)
	assertCode(t, err, "plugin.account_public_unavailable")
	if _, err = a.Repo.SetPublic(ctx, store, cipher, record.Ref, record.Owner, false, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Repo.SetPublic(ctx, store, cipher, record.Ref, record.Owner, true, 1, []string{"genshin"}); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(ctx, store, policy(), req)
	assertCode(t, err, "plugin.account_public_unavailable")
	if api.executeCalls.Load() != 1 {
		t.Fatal("toggle refilled quota")
	}
	saved, _ := a.Repo.Get(ctx, store, cipher, record.Ref)
	saved.PublicQuery.Day = "2000-01-01"
	saved.PublicQuery.NextUseMS = 0
	if err = a.Repo.Update(ctx, store, cipher, saved, saved.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Service(ctx, store, policy(), req); err != nil {
		t.Fatal("new day failed", err)
	}
	if err = a.Repo.Remove(ctx, store, cipher, record.Ref); err != nil {
		t.Fatal(err)
	}
	refs, err := a.Repo.PublicRefs(ctx, store, cipher)
	if err != nil || len(refs) != 0 {
		t.Fatal("deleted account remained indexed")
	}
}

// The pool reads Genshin's character list right after the index on the same
// account, as miao reads both with one public cookie: each read is charged,
// only the next call waits out the account's spacing, a failed index ends the
// call, and a list that cannot be read leaves the index answered with the
// list's failure, without the lending account.
func TestPublicCharacterListFollowsIndexOnOneAccount(t *testing.T) {
	asked, failing := []string{}, ""
	api := &fakeAPI{execute: func(_ context.Context, _ accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
		if role.UID != "100000002" || len(input) != 0 {
			t.Fatal("not a fixed public query")
		}
		asked = append(asked, name)
		if name == failing {
			return nil, &mihoyo.Error{Code: "plugin.upstream_challenge_required", Operation: name}
		}
		return map[string]any{"list": []any{map[string]any{"id": 10000046}}}, nil
	}}
	a, store, record := setup(t, api)
	ctx := t.Context()
	cipher, _ := a.Vault.Cipher()
	if _, err := a.Repo.SetPublic(ctx, store, cipher, record.Ref, record.Owner, true, 6, []string{"genshin", "starrail"}); err != nil {
		t.Fatal(err)
	}
	query := func(game, operation string) (map[string]any, error) {
		req := request(game, record.Ref)
		req.Method = "public.execute"
		req.Origin.Actor.ID = "another-user"
		req.Params = map[string]any{"game": game, "uid": "100000002", "region": map[string]string{"genshin": "cn_gf01", "starrail": "prod_gf_cn"}[game], "operation": operation}
		return a.Service(ctx, store, policy(), req)
	}
	rested := func() {
		saved, err := a.Repo.Get(ctx, store, cipher, record.Ref)
		if err != nil {
			t.Fatal(err)
		}
		saved.PublicQuery.NextUseMS = 0
		if err = a.Repo.Update(ctx, store, cipher, saved, saved.Revision); err != nil {
			t.Fatal(err)
		}
	}
	listFailure := func(result map[string]any) string {
		failed, _ := result["failure"].(map[string]any)
		if _, listed := result["data"]; listed || result["index"] == nil {
			t.Errorf("result = %v", result)
		}
		code, _ := failed["code"].(string)
		return code
	}
	_, err := query("genshin", "note")
	assertCode(t, err, "plugin.account_operation_denied")
	_, err = query("starrail", "characters")
	assertCode(t, err, "plugin.account_operation_denied")
	result, err := query("genshin", "characters")
	if err != nil || result["operation"] != "genshin.characters" || result["index"] == nil || result["data"] == nil || result["failure"] != nil {
		t.Fatalf("result = %v, %v", result, err)
	}
	raw, _ := json.Marshal(result)
	for _, marker := range []string{record.Ref, record.Owner.ActorID, "synthetic-stoken", "synthetic-cookie", "123456789"} {
		if strings.Contains(string(raw), marker) {
			t.Fatal("lender identity or credential exposed")
		}
	}
	// The next call waits out the account's spacing.
	_, err = query("genshin", "profile")
	assertCode(t, err, "plugin.account_public_unavailable")
	rested()
	failing = "genshin.profile"
	_, err = query("genshin", "characters")
	assertCode(t, err, "plugin.upstream_challenge_required")
	rested()
	failing = "genshin.characters"
	result, err = query("genshin", "characters")
	if code := listFailure(result); err != nil || code != "plugin.upstream_challenge_required" {
		t.Fatalf("result = %v, %v", result, err)
	}
	// One request left covers the index alone.
	rested()
	failing = ""
	result, err = query("genshin", "characters")
	if code := listFailure(result); err != nil || code != "plugin.account_public_unavailable" {
		t.Fatalf("result = %v, %v", result, err)
	}
	rested()
	_, err = query("genshin", "profile")
	assertCode(t, err, "plugin.account_public_unavailable")
	if strings.Join(asked, ",") != "genshin.profile,genshin.characters,genshin.profile,genshin.profile,genshin.characters,genshin.profile" {
		t.Errorf("asked = %v", asked)
	}
}
