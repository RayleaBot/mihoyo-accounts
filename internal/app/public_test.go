package app

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
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
