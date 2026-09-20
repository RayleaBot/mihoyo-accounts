package mihoyo

import (
	"net/http"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestTokenEndpointsUseTheirOwnAuthenticationProfiles(t *testing.T) {
	tokens := accounts.Tokens{AccountID: "123456789", SToken: "synthetic-stoken", MID: "synthetic-mid", DeviceID: "synthetic-device"}
	requests := 0
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Scheme != "https" {
			t.Fatal("credential request did not use HTTPS")
		}
		switch req.URL.Path {
		case "/account/auth/api/getLTokenBySToken":
			if req.Header.Get("DS") != "" || req.URL.Query().Get("stuid") != tokens.AccountID || req.URL.Query().Get("uid") != "" {
				t.Fatal("LToken exchange used the passport/record signature or wrong identity parameter")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"ltoken": "synthetic-ltoken"}}), nil
		case "/account/auth/api/getCookieAccountInfoBySToken":
			if req.Header.Get("DS") != "" || req.URL.Query().Get("uid") != tokens.AccountID || req.Header.Get("Cookie") == "" {
				t.Fatal("cookie exchange did not use the SToken authentication profile")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"cookie_token": "synthetic-cookie"}}), nil
		default:
			t.Fatal("unexpected token endpoint")
			return nil, nil
		}
	}), nil)
	updated, err := client.Refresh(t.Context(), tokens)
	if err != nil || requests != 2 || updated.LToken == "" || updated.CookieToken == "" {
		t.Fatal("token exchange failed")
	}
}
