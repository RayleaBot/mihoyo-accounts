package mihoyo

import (
	"encoding/json"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/http"
	"strings"
	"testing"
)

func TestBillingUsesCSCAuthorizationAndChecksUID(t *testing.T) {
	tokens := accounts.Tokens{SToken: "synthetic-token", CookieToken: "synthetic-cookie"}
	role := accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}
	wrong := false
	keys := 0
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/binding/api/genAuthKey":
			keys++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["auth_appid"] != "csc" {
				t.Fatal("wrong auth scope")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-csc-key"}}), nil
		case "/common/hk4e_self_help_query/User/GetUserInfo":
			uid := role.UID
			if wrong {
				uid = "100000002"
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"uid": uid}}), nil
		case "/common/hk4e_self_help_query/User/GetCrystalLog":
			if r.Header.Get("Cookie") != "" || r.URL.Query().Get("authkey") != "synthetic-csc-key" {
				t.Fatal("wrong billing authorization")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{map[string]any{"id": "1844674407370955101", "add_num": 60, "authkey": "synthetic-csc-key"}}}}), nil
		}
		t.Fatal(r.URL.Path)
		return nil, nil
	}), nil)
	for range 2 {
		data, err := c.Execute(t.Context(), tokens, role, "genshin.billing", map[string]any{"category": "crystal"})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(data)
		if strings.Contains(string(raw), "synthetic") || !strings.Contains(string(raw), "1844674407370955101") {
			t.Fatal("credential leak or id rounding")
		}
	}
	if keys != 1 {
		t.Fatal("csc cache not reused")
	}
	wrong = true
	if _, err := c.Execute(t.Context(), tokens, role, "genshin.billing", map[string]any{"category": "crystal"}); err == nil {
		t.Fatal("wrong role accepted")
	}
}
func TestRedeemExactRegionsAndAlreadyClaimed(t *testing.T) {
	calls := 0
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "sg-hkrpg-api.hoyolab.com" || r.URL.Query().Get("region") != "prod_official_eur" {
			t.Fatal("wrong redemption endpoint")
		}
		return reply(map[string]any{"retcode": -2017, "data": nil}), nil
	}), nil)
	role := accounts.Role{Game: "starrail", UID: "700000001", Region: "prod_official_eur"}
	out, err := c.Execute(t.Context(), accounts.Tokens{Realm: "os", CookieToken: "synthetic-cookie"}, role, "starrail.redeem", map[string]any{"code": "TEST1234"})
	if err != nil || out["outcome"] != "already_redeemed" {
		t.Fatal(out, err)
	}
	role.Region = "prod_gf_cn"
	if _, err = c.Execute(t.Context(), accounts.Tokens{}, role, "starrail.redeem", map[string]any{"code": "TEST1234"}); err == nil || calls != 1 {
		t.Fatal("CN unexpectedly redeemed")
	}
}
