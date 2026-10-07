package mihoyo

import (
	"encoding/json"
	"errors"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/http"
	"testing"
)

func TestGameSignKeepsIdentityAndHandlesAlreadySignedAndChallenge(t *testing.T) {
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		for _, outcome := range []string{"signed", "already", "race", "challenge", "first", "auth"} {
			t.Run(game+"/"+outcome, func(t *testing.T) {
				calls, posts := 0, 0
				role := accounts.Role{Game: game, UID: "100000001", Region: map[string]string{"genshin": "cn_gf01", "starrail": "prod_gf_cn", "zzz": "prod_gf_cn"}[game]}
				client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					expected := "api-takumi.mihoyo.com"
					if game == "zzz" {
						expected = "act-nap-api.mihoyo.com"
					}
					if req.URL.Scheme != "https" || req.URL.Host != expected || req.Header.Get("x-rpc-app_version") != signVersion || req.Header.Get("DS") == "" {
						t.Fatal("wrong sign endpoint or profile")
					}
					if req.Method == "GET" {
						if req.URL.Query().Get("uid") != role.UID || req.URL.Query().Get("region") != role.Region || req.URL.Query().Get("act_id") != signActivities[game] {
							t.Fatal("sign identity changed")
						}
						if outcome == "auth" {
							return reply(map[string]any{"retcode": -100, "data": map[string]any{}}), nil
						}
						return reply(map[string]any{"retcode": 0, "data": map[string]any{"is_sign": outcome == "already", "first_bind": outcome == "first", "total_sign_day": 2, "today": "2026-09-19"}}), nil
					}
					posts++
					var body map[string]any
					if json.NewDecoder(req.Body).Decode(&body) != nil || body["uid"] != role.UID || body["region"] != role.Region || body["act_id"] != signActivities[game] {
						t.Fatal("invalid sign body")
					}
					if outcome == "race" {
						return reply(map[string]any{"retcode": -5003, "data": nil}), nil
					}
					if outcome == "challenge" {
						return reply(map[string]any{"retcode": 0, "data": map[string]any{"success": 1, "gt": "synthetic-gt", "challenge": "synthetic-challenge"}}), nil
					}
					return reply(map[string]any{"retcode": 0, "data": map[string]any{"success": 0}}), nil
				}), nil)
				result, err := client.Execute(t.Context(), accounts.Tokens{}, role, game+".sign", nil)
				if outcome == "auth" || outcome == "challenge" || outcome == "first" {
					want := map[string]string{"auth": "plugin.upstream_sign_unavailable", "challenge": "plugin.upstream_challenge_required", "first": "plugin.upstream_first_sign_required"}[outcome]
					var actual *Error
					if !errors.As(err, &actual) || actual.Code != want {
						t.Fatal(err)
					}
					if result != nil {
						t.Fatal("challenge material escaped")
					}
				} else if err != nil || result["signed"] != true {
					t.Fatal(result, err)
				}
				if outcome == "already" || outcome == "auth" || outcome == "first" {
					if calls != 1 || posts != 0 {
						t.Fatal("unnecessary sign write")
					}
				} else if calls != 2 || posts != 1 {
					t.Fatal("sign was retried")
				}
			})
		}
	}
}
func TestSignRewardsAreSanitized(t *testing.T) {
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"month": 9, "awards": []any{map[string]any{"name": "摩拉", "cnt": 5000, "icon": "https://example.invalid/image"}}, "cookie_token": "synthetic-cookie"}}), nil
	}), nil)
	out, err := client.Execute(t.Context(), accounts.Tokens{}, accounts.Role{Game: "genshin", UID: "1", Region: "cn_gf01"}, "genshin.sign_rewards", nil)
	if err != nil || len(out["awards"].([]any)) != 1 || out["cookie_token"] != nil {
		t.Fatal(out, err)
	}
}
