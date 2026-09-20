package mihoyo

import (
	"encoding/json"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/http"
	"testing"
)

func TestGrowthUsesFixedCalculatorEndpointsAndStructuredInputs(t *testing.T) {
	for _, game := range []string{"genshin", "starrail"} {
		t.Run(game, func(t *testing.T) {
			calls := 0
			tokens := accounts.Tokens{CookieToken: "synthetic-growth-cookie"}
			role := accounts.Role{Game: game, UID: "100000001", Region: "cn"}
			client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Host != "api-takumi.mihoyo.com" || req.URL.Scheme != "https" || req.Header.Get("DS") == "" {
					t.Fatal("calculator host/profile changed")
				}
				if req.Method == "GET" {
					if req.URL.Query().Get("uid") != role.UID || req.URL.Query().Get("region") != role.Region {
						t.Fatal("calculator identity changed")
					}
					return reply(map[string]any{"retcode": 0, "data": map[string]any{"skills": []any{}, "cookie_token": tokens.CookieToken}}), nil
				}
				var body map[string]any
				if json.NewDecoder(req.Body).Decode(&body) != nil {
					t.Fatal("invalid calculator body")
				}
				if game == "genshin" {
					item := object(body["items"].([]any)[0])
					if item["avatar_level_target"] != float64(80) || len(item["skill_list"].([]any)) != 1 {
						t.Fatal("genshin parameters dropped")
					}
				} else {
					if body["game"] != "hkrpg" || object(body["avatar"])["target_level"] != float64(80) {
						t.Fatal("starrail parameters dropped")
					}
				}
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"overall_material_consume": map[string]any{}, "avatar_consume": []any{}}}), nil
			}), nil)
			if _, err := client.Execute(t.Context(), tokens, role, game+".growth_detail", map[string]any{"character_id": "1001"}); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"character_id": "1001", "current_level": 1, "target_level": 80, "skills": []any{map[string]any{"id": 100101, "current_level": 1, "target_level": 6}}}
			if _, err := client.Execute(t.Context(), tokens, role, game+".growth_compute", input); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []map[string]any{{"character_id": 1001, "current_level": 1, "target_level": 1.5}, {"character_id": 1001, "current_level": 81, "target_level": 80}, {"character_id": 1001, "current_level": 1, "target_level": 80, "body": map[string]any{}}, {"character_id": 1001, "current_level": 1, "target_level": 80, "weapon_id": 1002}} {
				if _, err := client.Execute(t.Context(), tokens, role, game+".growth_compute", bad); err == nil {
					t.Fatal("invalid compute accepted")
				}
			}
			if calls != 2 {
				t.Fatal("invalid input reached upstream")
			}
		})
	}
}
