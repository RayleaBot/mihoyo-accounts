package app

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
	"strings"
	"testing"
)

func TestCookieImportEncryptsStagesAndSeparatesMatchingChinaID(t *testing.T) {
	calls := 0
	api := &fakeAPI{roles: func(_ context.Context, tokens accounts.Tokens, game string) ([]accounts.Role, error) {
		calls++
		if tokens.Realm != "os" || tokens.LToken != "synthetic-overseas-ltoken" {
			t.Fatal("wrong import credentials")
		}
		return []accounts.Role{{Ref: game + ":os:600000001", Game: game, GameBiz: game + "_global", UID: "600000001", Region: map[string]string{"genshin": "os_usa", "starrail": "prod_official_usa", "zzz": "prod_gf_us"}[game]}}, nil
	}}
	a, store, china := setup(t, api)
	raw := "ltuid_v2=" + china.Tokens.AccountID + "; ltoken_v2=synthetic-overseas-ltoken; cookie_token_v2=synthetic-overseas-cookie; ltmid_v2=synthetic-overseas-mid"
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.import.start", map[string]any{"cookie": raw, "subject": china.Owner, "realm": "os"}); err == nil {
		t.Fatal("plaintext accepted")
	}
	start, err := a.Manage(t.Context(), store, policy(), "accounts.import.start", map[string]any{"box": cloudBox(t, a, raw, "account.cookie.import"), "subject": china.Owner, "realm": "os"})
	if err != nil {
		t.Fatal(err)
	}
	job := start["import"].(cookieImport)
	cipher, _ := a.Vault.Cipher()
	for i := 0; i < 3; i++ {
		out, err := a.Manage(t.Context(), store, policy(), "accounts.import.step", map[string]any{"ref": job.Ref, "sequence": i})
		if err != nil {
			t.Fatal(err)
		}
		job = out["import"].(cookieImport)
		replayed, err := a.Manage(t.Context(), store, policy(), "accounts.import.step", map[string]any{"ref": job.Ref, "sequence": i})
		if err != nil || replayed["import"].(cookieImport).Sequence != job.Sequence || calls != i+1 {
			t.Fatal("step replay reissued request", err, calls)
		}
		listed, _ := a.Repo.List(t.Context(), store, cipher, china.Owner)
		if i < 2 && len(listed) != 1 {
			t.Fatal("partial import saved")
		}
	}
	if job.State != "bound" || job.Account.Realm != "os" || job.Account.Ref == china.Ref {
		t.Fatal("realm identity merged")
	}
	listed, err := a.Repo.List(t.Context(), store, cipher, china.Owner)
	if err != nil || len(listed) != 2 {
		t.Fatal(listed, err)
	}
	wire, _ := json.Marshal(job)
	for _, marker := range []string{"synthetic-overseas-ltoken", "synthetic-overseas-cookie", "synthetic-overseas-mid"} {
		if strings.Contains(string(wire), marker) {
			t.Fatal("import view leaked credential")
		}
		for _, sealed := range store.values {
			if strings.Contains(sealed, marker) {
				t.Fatal("import stored plaintext")
			}
		}
	}
}
func TestCookieImportAuthenticationFailureAndCancelKeepOnlyOldAccount(t *testing.T) {
	api := &fakeAPI{roles: func(context.Context, accounts.Tokens, string) ([]accounts.Role, error) {
		return nil, &mihoyo.Error{Code: "plugin.upstream_auth_invalid"}
	}}
	a, store, old := setup(t, api)
	start, err := a.Manage(t.Context(), store, policy(), "accounts.import.start", map[string]any{"box": cloudBox(t, a, "ltuid_v2=123; ltoken_v2=synthetic-invalid-overseas", "account.cookie.import"), "subject": old.Owner, "realm": "os"})
	if err != nil {
		t.Fatal(err)
	}
	job := start["import"].(cookieImport)
	_, err = a.Manage(t.Context(), store, policy(), "accounts.import.step", map[string]any{"ref": job.Ref, "sequence": 0})
	assertCode(t, err, "plugin.upstream_auth_invalid")
	if a.imports.items[job.Ref].tokens.LToken != "" {
		t.Fatal("failed import retained credential")
	}
	cipher, _ := a.Vault.Cipher()
	list, _ := a.Repo.List(t.Context(), store, cipher, old.Owner)
	if len(list) != 1 || list[0].Ref != old.Ref {
		t.Fatal("failed import modified old account")
	}
	if _, err = a.Manage(t.Context(), store, policy(), "accounts.import.cancel", map[string]any{"ref": job.Ref}); err != nil {
		t.Fatal(err)
	}
}

func TestChinaCookieImportExchangesSTokenAndUpdatesTheQRAccount(t *testing.T) {
	api := &fakeAPI{roles: func(_ context.Context, tokens accounts.Tokens, game string) ([]accounts.Role, error) {
		if tokens.RealmName() != "cn" || tokens.CookieToken != "synthetic-refreshed-cookie" {
			t.Fatal("China import did not exchange its SToken")
		}
		return []accounts.Role{{Ref: game + ":cn:100000001", Game: game, GameBiz: game + "_cn", UID: "100000001", Region: "cn_gf01"}}, nil
	}}
	a, store, china := setup(t, api)
	raw := "stuid=" + china.Tokens.AccountID + "; stoken=v2_synthetic-china-stoken; mid=synthetic-china-mid"
	start, err := a.Manage(t.Context(), store, policy(), "accounts.import.start", map[string]any{"box": cloudBox(t, a, raw, "account.cookie.import"), "subject": china.Owner, "realm": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	job := start["import"].(cookieImport)
	for i := 0; i < 4 && job.State == "discovering"; i++ {
		out, err := a.Manage(t.Context(), store, policy(), "accounts.import.step", map[string]any{"ref": job.Ref, "sequence": job.Sequence})
		if err != nil {
			t.Fatal(err)
		}
		job = out["import"].(cookieImport)
	}
	cipher, _ := a.Vault.Cipher()
	listed, err := a.Repo.List(t.Context(), store, cipher, china.Owner)
	if err != nil || job.State != "bound" || len(listed) != 1 || listed[0].Ref != china.Ref {
		t.Fatal("China import did not update the QR account", job.State, len(listed), err)
	}
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.import.start", map[string]any{"box": cloudBox(t, a, "ltuid=1; ltoken=only", "account.cookie.import"), "subject": china.Owner, "realm": "cn"}); err == nil {
		t.Fatal("China cookie without SToken or CookieToken accepted")
	}
}
