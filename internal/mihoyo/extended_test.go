package mihoyo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestExtendedOperationsPinIdentityAndValidateMonth(t *testing.T) {
	for _, tc := range []struct {
		name, game, uidKey, regionKey, path string
		input                               map[string]any
	}{
		{"genshin.monthly", "genshin", "bind_uid", "bind_region", "/event/ys_ledger/monthInfo", map[string]any{"month": 9}},
		{"starrail.monthly", "starrail", "uid", "region", "/event/srledger/month_info", map[string]any{"month": "202609"}},
		{"zzz.deadly", "zzz", "uid", "region", "/event/game_record_zzz/api/zzz/hadal_mem_detail_v2", map[string]any{"schedule_type": 2}},
		{"starrail.rogue", "starrail", "role_id", "server", "/game_record/app/hkrpg/api/rogue", nil},
		{"zzz.void_front_summary", "zzz", "uid", "region", "/event/game_record_zzz/api/zzz/void_front_battle_period_detail", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Path != tc.path || req.URL.Query().Get(tc.uidKey) != "100000001" || req.URL.Query().Get(tc.regionKey) != "test-region" || req.URL.Scheme != "https" || req.Header.Get("DS") == "" {
					t.Fatal("wrong endpoint identity or profile")
				}
				if tc.name == "starrail.rogue" && req.URL.Query().Get("schedule_type") != "3" {
					t.Fatal("wrong default rogue period")
				}
				if tc.name == "zzz.void_front_summary" && req.URL.Query().Get("schedule_type") != "1" {
					t.Fatal("summary did not use supported period endpoint")
				}
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"month_data": map[string]any{}, "list": []any{}, "basic_info": map[string]any{}, "void_front_battle_detail": map[string]any{}}}), nil
			}), nil)
			role := accounts.Role{Game: tc.game, UID: "100000001", Region: "test-region"}
			if _, err := client.Execute(t.Context(), accounts.Tokens{}, role, tc.name, tc.input); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Execute(t.Context(), accounts.Tokens{}, role, tc.name, map[string]any{"uid": "another"}); err == nil || calls != 1 {
				t.Fatal("identity override accepted")
			}
		})
	}
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid month reached upstream")
		return nil, nil
	}), nil)
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		if _, err := client.Execute(t.Context(), accounts.Tokens{}, accounts.Role{Game: game, UID: "100000001", Region: "cn"}, game+".monthly", map[string]any{"month": "202613"}); err == nil {
			t.Fatal("invalid month accepted")
		}
	}
}

func TestStarRailSummaryKeepsActivityCookieInsideProvider(t *testing.T) {
	tokens := accounts.Tokens{AccountID: "synthetic-account", CookieToken: "synthetic-cookie"}
	role := accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/common/badge/v1/login/account" {
			response := reply(map[string]any{"retcode": 0, "data": map[string]any{}})
			response.Header.Add("Set-Cookie", "e_hkrpg_token=synthetic-activity-secret; Secure; HttpOnly")
			return response, nil
		}
		if req.Header.Get("DS") != "" || !strings.Contains(req.Header.Get("Cookie"), "e_hkrpg_token=synthetic-activity-secret") || req.URL.Query().Get("badge_uid") != role.UID {
			t.Fatal("activity session missing or identity wrong")
		}
		if strings.HasSuffix(req.URL.Path, "pool_stat") {
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"cards": []any{}, "token": "must-drop"}}), nil
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{}, "has_more": false}}), nil
	}), nil)
	result, err := client.Execute(context.Background(), tokens, role, "starrail.gacha_summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(result)
	if strings.Contains(string(wire), "secret") || strings.Contains(string(wire), "token") || result["coverage"] != "five_star_and_pool_summary" {
		t.Fatal("summary leaked session or lost coverage boundary")
	}
}
