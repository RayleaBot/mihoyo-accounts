package mihoyo

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestRecordProfilesSignExactWireQueryAndBody(t *testing.T) {
	for _, tc := range []struct{ game, operation, version, clientType, referer string }{
		{"genshin", "genshin.note", "2.40.1", "5", "https://webstatic.mihoyo.com/"},
		{"starrail", "starrail.note", "2.40.1", "5", "https://webstatic.mihoyo.com/"},
		{"zzz", "zzz.note", "2.73.1", "2", "https://act.mihoyo.com/"},
		{"genshin", "genshin.characters", "2.40.1", "5", "https://webstatic.mihoyo.com/"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				ds := strings.Split(req.Header.Get("DS"), ",")
				if len(ds) != 3 {
					t.Fatal("missing DS")
				}
				nonce, _ := strconv.Atoi(ds[1])
				if nonce < 100000 || nonce > 999999 {
					t.Fatal("invalid record nonce")
				}
				// Mainland record salt from both Yunzai-genshin and ZZZ-Plugin.
				digest := md5.Sum([]byte("salt=xV8v4Qu54lUKrEYFZkJhB8cuOh9Asafs&t=" + ds[0] + "&r=" + ds[1] + "&b=" + string(body) + "&q=" + req.URL.RawQuery))
				if ds[2] != hex.EncodeToString(digest[:]) {
					t.Fatal("DS did not sign wire query/body with record salt")
				}
				if req.Header.Get("x-rpc-app_version") != tc.version || req.Header.Get("x-rpc-client_type") != tc.clientType || req.Header.Get("Referer") != tc.referer || !strings.Contains(req.Header.Get("User-Agent"), "miHoYoBBS/"+tc.version) {
					t.Fatal("record profile mismatch")
				}
				if req.Header.Get("x-rpc-device_fp") != "syntheticfingerprint" || strings.Contains(req.Header.Get("Cookie"), "stoken") {
					t.Fatal("wrong record authentication")
				}
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"current_resin": 1, "current_stamina": 2, "energy": map[string]any{}, "list": []any{}}}), nil
			}), nil)
			_, err := client.Execute(t.Context(), accounts.Tokens{DeviceID: "test-device", DeviceFP: "syntheticfingerprint", CookieToken: "synthetic-cookie", SToken: "synthetic-stoken"}, accounts.Role{Game: tc.game, UID: "100000001", Region: "cn"}, tc.operation, nil)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// ZZZ-Plugin's tool.js writes each mainland record's query in its own order
// and getUrl appends data.query after the DS has signed it.
func TestZZZRecordsSendZZZPluginQueries(t *testing.T) {
	const base = "role_id=10000001&server=prod_gf_cn"
	const byUID = "uid=10000001&region=prod_gf_cn"
	for _, tc := range []struct {
		name         string
		input        map[string]any
		signed, sent string
		host, path   string
	}{
		{"zzz.profile", nil, "lang=zh-cn&" + base, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/index"},
		{"zzz.note", nil, base, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/note"},
		{"zzz.characters", nil, "lang=zh-cn&" + base, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/avatar/basic"},
		{"zzz.character", map[string]any{"id_list": []any{"1041"}}, "lang=zh-cn&" + base + "&need_wiki=false", "&id_list[]=1041", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/avatar/info"},
		{"zzz.buddies", nil, "lang=zh-cn&" + base, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/buddy/info"},
		{"zzz.challenge", map[string]any{"schedule_type": 2}, "lang=zh-cn&" + base + "&schedule_type=2", "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/hadal_info_v2"},
		{"zzz.deadly", nil, "lang=zh-cn&" + byUID + "&schedule_type=1", "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/hadal_mem_detail_v2"},
		{"zzz.holo_boss", map[string]any{"schedule_type": 2}, "lang=zh-cn&" + byUID + "&schedule_type=2", "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/holo_boss_detail"},
		{"zzz.tower", nil, "lang=zh-cn&" + byUID, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/climbing_tower_detail"},
		{"zzz.void_front", nil, "lang=zh-cn&" + byUID + "&schedule_type=1", "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/void_front_battle_period_detail"},
		{"zzz.hollow_zero", nil, "lang=zh-cn&" + base, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/abyss_abstract"},
		{"zzz.hollow_zero_detail", nil, "lang=zh-cn&" + base + "&type=3", "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/abyss_detail"},
		{"zzz.lost_void", nil, "lang=zh-cn&" + byUID, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/abysss2_abstract"},
		{"zzz.zenkov", nil, "lang=zh-cn&" + byUID, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/zenkov_abstract_info"},
		{"zzz.zenkov_detail", nil, "lang=zh-cn&" + byUID, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/zenkov_detail"},
		{"zzz.exploration", nil, byUID, "", "api-takumi-record.mihoyo.com", "/event/game_record_zzz/api/zzz/exploration_detail"},
		{"zzz.monthly", nil, byUID, "&month=", "api-takumi.mihoyo.com", "/event/nap_ledger/month_info"},
		{"zzz.monthly", map[string]any{"month": "202503"}, byUID, "&month=202503", "api-takumi.mihoyo.com", "/event/nap_ledger/month_info"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != tc.host || req.URL.Path != tc.path {
					t.Fatalf("endpoint = %s%s", req.URL.Host, req.URL.Path)
				}
				if req.URL.RawQuery != tc.signed+tc.sent || req.URL.RequestURI() != tc.path+"?"+tc.signed+tc.sent {
					t.Fatalf("query = %q, request URI %q", req.URL.RawQuery, req.URL.RequestURI())
				}
				ds := strings.Split(req.Header.Get("DS"), ",")
				if len(ds) != 3 {
					t.Fatal("missing DS")
				}
				// The DS covers only the fixed query and the empty GET body.
				digest := md5.Sum([]byte("salt=xV8v4Qu54lUKrEYFZkJhB8cuOh9Asafs&t=" + ds[0] + "&r=" + ds[1] + "&b=&q=" + tc.signed))
				if ds[2] != hex.EncodeToString(digest[:]) {
					t.Fatal("DS did not sign the fixed query")
				}
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"avatar_list": []any{}}}), nil
			}), nil)
			role := accounts.Role{Game: "zzz", UID: "10000001", Region: "prod_gf_cn"}
			if _, err := client.Execute(t.Context(), accounts.Tokens{DeviceID: "test-device", DeviceFP: "syntheticfingerprint"}, role, tc.name, tc.input); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Every mainland ZZZ record the provider sends has upstream's order.
	for _, name := range OperationNames() {
		spec := operations[name]
		if spec.game == "zzz" && spec.path != "" && len(spec.order) == 0 {
			t.Errorf("%s has no upstream query order", name)
		}
	}
}

func TestZZZCharacterTakesOneCharacterARequest(t *testing.T) {
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("a refused character list reached upstream")
		return nil, nil
	}), nil)
	role := accounts.Role{Game: "zzz", UID: "10000001", Region: "prod_gf_cn"}
	for _, input := range []map[string]any{nil, {"id_list": []any{}}, {"id_list": []any{"1041", "1191"}}, {"id_list": "1041"}, {"id_list": []any{"0"}}} {
		_, err := client.Execute(t.Context(), accounts.Tokens{}, role, "zzz.character", input)
		if failure, ok := err.(*Error); !ok || failure.Code != "plugin.upstream_request_invalid" {
			t.Fatalf("%v: %v", input, err)
		}
	}
}
