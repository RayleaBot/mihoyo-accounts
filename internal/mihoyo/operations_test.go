package mihoyo

import (
	"net/http"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestOperationsPinRoleAndRejectCredentialResponses(t *testing.T) {
	tokens := accounts.Tokens{CookieToken: "synthetic-cookie-secret", LToken: "synthetic-ltoken-secret", AccountID: "123456789", DeviceID: "synthetic-device"}
	role := accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}
	calls := 0
	response := map[string]any{"current_resin": 10, "max_resin": 200, "cookie_token": tokens.CookieToken}
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Scheme != "https" || req.URL.Host != "api-takumi-record.mihoyo.com" || req.URL.Query().Get("role_id") != role.UID || req.URL.Query().Get("server") != role.Region {
			t.Fatal("caller overrode fixed endpoint or role")
		}
		if !strings.Contains(req.Header.Get("Cookie"), tokens.CookieToken) {
			t.Fatal("upstream did not receive authentication")
		}
		return reply(map[string]any{"retcode": 0, "data": response}), nil
	}), nil)
	result, err := client.Execute(t.Context(), tokens, role, "genshin.note", nil)
	if err != nil || result["cookie_token"] != nil {
		t.Fatalf("unexpected response: %v", err)
	}
	for _, input := range []map[string]any{{"url": "https://example.com"}, {"role_id": "other"}, {"Cookie": "override"}} {
		if _, err := client.Execute(t.Context(), tokens, role, "genshin.note", input); err == nil {
			t.Fatal("arbitrary input accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid operation reached upstream")
	}
	response = map[string]any{"current_resin": tokens.CookieToken}
	if _, err := client.Execute(t.Context(), tokens, role, "genshin.note", nil); err == nil {
		t.Fatal("credential value crossed service boundary")
	}
	response = map[string]any{"daily_task": map[string]any{"authkey": "synthetic-authkey"}}
	if _, err := client.Execute(t.Context(), tokens, role, "genshin.note", nil); err == nil {
		t.Fatal("nested auth material crossed service boundary")
	}
}
