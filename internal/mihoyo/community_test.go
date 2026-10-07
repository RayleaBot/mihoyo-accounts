package mihoyo

import (
	"encoding/json"
	"errors"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/http"
	"strings"
	"testing"
)

func TestCommunityUsesAccountCredentialsAndProjectsOnlyBusinessData(t *testing.T) {
	tokens := accounts.Tokens{AccountID: "123456789", SToken: "synthetic-stoken", CookieToken: "synthetic-cookie", LToken: "synthetic-ltoken", MID: "synthetic-mid", CloudGenshin: "synthetic-cloud-token"}
	role := accounts.Role{Game: "genshin"}
	calls := 0
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Scheme != "https" || req.URL.Host != "bbs-api.miyoushe.com" {
			t.Fatal("incorrect community destination")
		}
		switch req.URL.Path {
		case "/apihub/wapi/getUserMissionsState":
			if req.URL.Query().Get("point_sn") != "myb" || !strings.Contains(req.Header.Get("Cookie"), tokens.CookieToken) || strings.Contains(req.Header.Get("Cookie"), tokens.SToken) {
				t.Fatal("wrong status authentication")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"total_points": 100, "can_get_points": 20, "already_received_points": 40, "cookie": "must-drop", "states": []any{map[string]any{"mission_id": 58, "is_get_award": true, "token": "must-drop"}}}}), nil
		case "/apihub/app/api/signIn":
			var body map[string]any
			json.NewDecoder(req.Body).Decode(&body)
			if body["gids"] != "2" || req.Header.Get("DS") == "" || !strings.Contains(req.Header.Get("Cookie"), tokens.SToken) || req.Header.Get("x-rpc-app_version") != "2.109.0" {
				t.Fatal("wrong app sign profile")
			}
			return reply(map[string]any{"retcode": 0, "data": nil}), nil
		default:
			t.Fatal("unexpected endpoint")
			return nil, nil
		}
	}), nil)
	out, err := client.Execute(t.Context(), tokens, role, "genshin.community_status", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), "must-drop") {
		t.Fatal("unknown fields returned")
	}
	out, err = client.Execute(t.Context(), tokens, role, "genshin.community_sign", nil)
	if err != nil || out["accepted"] != true {
		t.Fatal(out, err)
	}
	if _, err = client.Execute(t.Context(), tokens, role, "genshin.community_like", map[string]any{"post_id": "1", "url": "https://other"}); err == nil || calls != 2 {
		t.Fatal("extra endpoint input accepted")
	}
}
func TestCloudWalletUsesOnlyCloudTokenAndKeepsGameAuthSeparate(t *testing.T) {
	tokens := accounts.Tokens{AccountID: "123456789", CookieToken: "synthetic-cookie", SToken: "synthetic-stoken", CloudGenshin: "oi=123;ct=synthetic-cloud-token"}
	role := accounts.Role{Game: "genshin"}
	reject := false
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api-cloudgame.mihoyo.com" || req.Header.Get("Cookie") != "" || req.Header.Get("x-rpc-combo_token") != tokens.CloudGenshin || req.Header.Get("x-rpc-device_fp") != "" {
			t.Fatal("mixed cloud/game credentials")
		}
		if reject {
			return reply(map[string]any{"retcode": -100, "data": nil}), nil
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"free_time": map[string]any{"free_time": 120, "send_freetime": 15}, "coin": map[string]any{"coin_num": 0}, "play_card": map[string]any{"short_msg": "未开通"}, "token": "must-drop"}}), nil
	}), nil)
	out, err := client.Execute(t.Context(), tokens, role, "genshin.cloud_sign", nil)
	if err != nil || out["checked"] != true || text(out["send_freetime"]) != "15" {
		t.Fatal(out, err)
	}
	reject = true
	_, err = client.Execute(t.Context(), tokens, role, "genshin.cloud_sign", nil)
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Code != "plugin.upstream_cloud_unavailable" {
		t.Fatal(err)
	}
	tokens.CloudGenshin = ""
	_, err = client.Execute(t.Context(), tokens, role, "genshin.cloud_sign", nil)
	if !errors.As(err, &upstream) || upstream.Code != "plugin.account_cloud_unavailable" {
		t.Fatal(err)
	}
}
