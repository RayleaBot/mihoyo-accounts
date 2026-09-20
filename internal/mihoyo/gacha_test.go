package mihoyo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func gachaRecord(uid, pool, id string) map[string]any {
	return map[string]any{"uid": uid, "gacha_type": pool, "gacha_id": "9001", "id": id, "item_id": "1001", "time": "2026-09-01 08:00:00", "rank_type": "4", "count": "1"}
}
func TestGachaUsesFixedEndpointsAndKeepsAuthorizationInsideProvider(t *testing.T) {
	for _, tc := range []struct {
		game, region, pool, actual, path string
		zone                             int
	}{
		{"genshin", "cn_gf01", "301", "400", "/gacha_info/api/getGachaLog", 8},
		{"starrail", "prod_gf_cn", "21", "21", "/common/gacha_record/api/getLdGachaLog", 8},
		{"zzz", "prod_gf_cn", "12001", "102", "/common/gacha_record/api/getGachaLog", 0},
	} {
		t.Run(tc.game, func(t *testing.T) {
			authCalls, pages := 0, 0
			tokens := accounts.Tokens{AccountID: "123456789", SToken: "synthetic-stoken", CookieToken: "synthetic-cookie", MID: "synthetic-mid"}
			role := accounts.Role{Game: tc.game, UID: "100000001", Region: tc.region}
			client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Scheme != "https" {
					t.Fatal("unencrypted request")
				}
				if req.URL.Path == "/binding/api/genAuthKey" {
					authCalls++
					var body map[string]any
					_ = json.NewDecoder(req.Body).Decode(&body)
					if body["game_biz"] != gachaProfiles[tc.game].biz || body["region"] != tc.region || !strings.Contains(req.Header.Get("Cookie"), tokens.SToken) {
						t.Fatal("wrong authorization role/profile")
					}
					return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-private-authkey"}}), nil
				}
				pages++
				if req.URL.Path != tc.path || req.URL.Query().Get("authkey") != "synthetic-private-authkey" || req.Header.Get("Cookie") != "" || req.URL.Query().Get("size") != "20" {
					t.Fatal("wrong gacha page request")
				}
				record := gachaRecord(role.UID, tc.actual, "1844674407370955101")
				record["authkey"] = "synthetic-private-authkey"
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{record}, "region_time_zone": tc.zone}}), nil
			}), nil)
			for range 2 {
				result, err := client.Execute(t.Context(), tokens, role, tc.game+".gacha", map[string]any{"gacha_type": tc.pool})
				if err != nil {
					t.Fatal(err)
				}
				wire, _ := json.Marshal(result)
				expectedZone := tc.zone
				if tc.game == "zzz" {
					expectedZone += 8
				}
				if strings.Contains(string(wire), "synthetic-") || result["timezone"] != expectedZone || result["has_more"] != false {
					t.Fatal("gacha DTO leaked credentials or changed timezone")
				}
			}
			if authCalls != 1 || pages != 2 {
				t.Fatal("authkey cache not reused")
			}
			if _, err := client.Execute(t.Context(), tokens, role, tc.game+".gacha", map[string]any{"gacha_type": tc.pool, "url": "https://example.com"}); err == nil || pages != 2 {
				t.Fatal("arbitrary endpoint accepted")
			}
		})
	}
}

func TestGachaExpiredKeyRetriesOnceAndDoesNotInvalidateCookie(t *testing.T) {
	for _, game := range []string{"genshin", "starrail"} {
		authCalls, pages := 0, 0
		client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/binding/api/genAuthKey" {
				authCalls++
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-key"}}), nil
			}
			pages++
			return reply(map[string]any{"retcode": -100, "data": nil}), nil
		}), nil)
		spec := gachaProfiles[game]
		_, err := client.Execute(t.Context(), accounts.Tokens{AccountID: "123", SToken: "synthetic-token"}, accounts.Role{Game: game, UID: "100000001", Region: spec.regions[0]}, game+".gacha", map[string]any{"gacha_type": spec.pools[0]})
		failure, ok := err.(*Error)
		if !ok || failure.Code == "plugin.upstream_auth_invalid" || authCalls != 2 || pages != 2 {
			t.Fatal("wrong gacha expiry classification/retry")
		}
	}
}

func TestGachaRejectsWrongRoleRepeatedCursorAndSecretEcho(t *testing.T) {
	for _, mutation := range []func(map[string]any){func(r map[string]any) { r["uid"] = "other" }, func(r map[string]any) { r["id"] = "100" }, func(r map[string]any) { r["name"] = "synthetic-key" }} {
		client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/binding/api/genAuthKey" {
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-key"}}), nil
			}
			record := gachaRecord("100000001", "100", "99")
			mutation(record)
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{record}}}), nil
		}), nil)
		_, err := client.Execute(t.Context(), accounts.Tokens{SToken: "synthetic-token"}, accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}, "genshin.gacha", map[string]any{"gacha_type": "100", "end_id": "100"})
		if err == nil {
			t.Fatal("invalid gacha page accepted")
		}
	}
}
