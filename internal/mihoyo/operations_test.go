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
	// Fields named like credentials are dropped; the rest of the page stays.
	response = map[string]any{"daily_task": map[string]any{"authkey": "synthetic-authkey", "finished_num": 4}}
	result, err = client.Execute(t.Context(), tokens, role, "genshin.note", nil)
	if err != nil || object(result["daily_task"])["authkey"] != nil || object(result["daily_task"])["finished_num"] == nil {
		t.Fatal("nested auth material crossed service boundary or the page was dropped")
	}
}

func TestStarRailChallengeRecordsAskDetailUnlessSimple(t *testing.T) {
	role := accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}
	for _, name := range []string{"starrail.challenge", "starrail.story", "starrail.boss", "starrail.peak"} {
		asked := []string{}
		client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
			asked = append(asked, req.URL.Query().Get("need_all"))
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"star_num": 12}}), nil
		}), nil)
		// The detailed record is the default; StarRail-plugin's 简易 record
		// leaves need_all out.
		for _, input := range []map[string]any{nil, {"need_all": true}, {"need_all": false, "schedule_type": 2}} {
			if _, err := client.Execute(t.Context(), accounts.Tokens{}, role, name, input); err != nil {
				t.Fatalf("%s %v: %v", name, input, err)
			}
		}
		if _, err := client.Execute(t.Context(), accounts.Tokens{}, role, name, map[string]any{"need_all": "false"}); err == nil {
			t.Fatalf("%s accepted a need_all that is not a boolean", name)
		}
		if strings.Join(asked, ",") != "true,true," {
			t.Errorf("%s need_all = %q", name, asked)
		}
	}
}
