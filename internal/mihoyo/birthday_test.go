package mihoyo

import (
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/http"
	"strings"
	"testing"
)

func TestBirthdayKeepsSessionPrivateAndClaimsOnlyListedRole(t *testing.T) {
	posts := 0
	tokens := accounts.Tokens{AccountID: "1234", LToken: "synthetic-ltoken", CookieToken: "synthetic-cookie"}
	role := accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/common/badge/v1/login/account" {
			res := reply(map[string]any{"retcode": 0, "data": map[string]any{}})
			res.Header = http.Header{"Set-Cookie": []string{"e_hk4e_token=synthetic-birthday-session; Path=/; Secure"}}
			return res, nil
		}
		if r.URL.Host != "hk4e-api.mihoyo.com" || !strings.Contains(r.Header.Get("Cookie"), "e_hk4e_token=synthetic-birthday-session") {
			t.Fatal("wrong activity authentication")
		}
		if strings.HasSuffix(r.URL.Path, "post_my_draw") {
			posts++
			return reply(map[string]any{"retcode": 0, "data": nil}), nil
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"role": []any{map[string]any{"role_id": "10000046", "name": "胡桃", "take_picture": "https://webstatic.mihoyo.com/test.png", "token": "ignored"}}}}), nil
	}), nil)
	out, err := c.Execute(t.Context(), tokens, role, "genshin.birthday_list", nil)
	if err != nil || len(out["items"].([]any)) != 1 {
		t.Fatal(out, err)
	}
	if _, err = c.Execute(t.Context(), tokens, role, "genshin.birthday_claim", map[string]any{"role_id": "10000001"}); err == nil || posts != 0 {
		t.Fatal("unlisted role claimed")
	}
	if out, err = c.Execute(t.Context(), tokens, role, "genshin.birthday_claim", map[string]any{"role_id": "10000046"}); err != nil || out["claimed"] != true || posts != 1 {
		t.Fatal(out, err)
	}
}
