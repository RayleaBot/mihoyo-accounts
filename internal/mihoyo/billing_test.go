package mihoyo

import (
	"encoding/json"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestBillingUsesCSCAuthorizationAndChecksUID(t *testing.T) {
	tokens := accounts.Tokens{SToken: "synthetic-token", CookieToken: "synthetic-cookie"}
	role := accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}
	wrong := false
	keys := 0
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/binding/api/genAuthKey":
			keys++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["auth_appid"] != "csc" {
				t.Fatal("wrong auth scope")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-csc-key"}}), nil
		case "/common/hk4e_self_help_query/User/GetUserInfo":
			uid := role.UID
			if wrong {
				uid = "100000002"
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"uid": uid}}), nil
		case "/common/hk4e_self_help_query/User/GetCrystalLog":
			if r.Header.Get("Cookie") != "" || r.URL.Query().Get("authkey") != "synthetic-csc-key" {
				t.Fatal("wrong billing authorization")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{map[string]any{"id": "1844674407370955101", "add_num": 60, "authkey": "synthetic-csc-key"}}}}), nil
		}
		t.Fatal(r.URL.Path)
		return nil, nil
	}), nil)
	for range 2 {
		data, err := c.Execute(t.Context(), tokens, role, "genshin.billing", map[string]any{"category": "crystal"})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(data)
		if strings.Contains(string(raw), "synthetic") || !strings.Contains(string(raw), "1844674407370955101") {
			t.Fatal("credential leak or id rounding")
		}
	}
	if keys != 1 {
		t.Fatal("csc cache not reused")
	}
	wrong = true
	if _, err := c.Execute(t.Context(), tokens, role, "genshin.billing", map[string]any{"category": "crystal"}); err == nil {
		t.Fatal("wrong role accepted")
	}
}
func TestRedeemExactRegionsAndAlreadyClaimed(t *testing.T) {
	calls := 0
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "sg-hkrpg-api.hoyolab.com" || r.URL.Query().Get("region") != "prod_official_eur" {
			t.Fatal("wrong redemption endpoint")
		}
		return reply(map[string]any{"retcode": -2017, "data": nil}), nil
	}), nil)
	role := accounts.Role{Game: "starrail", UID: "700000001", Region: "prod_official_eur"}
	out, err := c.Execute(t.Context(), accounts.Tokens{Realm: "os", CookieToken: "synthetic-cookie"}, role, "starrail.redeem", map[string]any{"code": "TEST1234"})
	if err != nil || out["outcome"] != "already_redeemed" {
		t.Fatal(out, err)
	}
	role.Region = "prod_gf_cn"
	if _, err = c.Execute(t.Context(), accounts.Tokens{}, role, "starrail.redeem", map[string]any{"code": "TEST1234"}); err == nil || calls != 1 {
		t.Fatal("CN unexpectedly redeemed")
	}
}

func TestStarRailBillingPageSize(t *testing.T) {
	tokens := accounts.Tokens{SToken: "synthetic-token"}
	role := accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}
	sizes := []string{}
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/binding/api/genAuthKey" {
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-csc-key"}}), nil
		}
		sizes = append(sizes, r.URL.Query().Get("page_size"))
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{}}}), nil
	}), nil)
	for _, input := range []map[string]any{{"category": "power"}, {"category": "power", "page_size": 50}} {
		if _, err := c.Execute(t.Context(), tokens, role, "starrail.billing", input); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(sizes, ",") != "10,50" {
		t.Fatal(sizes)
	}
	for _, size := range []any{9, 51, "x"} {
		if _, err := c.Execute(t.Context(), tokens, role, "starrail.billing", map[string]any{"category": "power", "page_size": size}); err == nil {
			t.Fatal("page size accepted:", size)
		}
	}
	genshin := accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}
	if _, err := c.Execute(t.Context(), tokens, genshin, "genshin.billing", map[string]any{"category": "crystal", "page_size": 20}); err == nil {
		t.Fatal("genshin page size accepted")
	}
}

func TestStarRailBillingPagesByCursor(t *testing.T) {
	tokens := accounts.Tokens{SToken: "synthetic-token"}
	role := accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}
	// 47 records, newest first with IDs 147..101, in pages of 20 after end_id.
	records := []any{}
	for id := 147; id > 100; id-- {
		records = append(records, map[string]any{"id": strconv.Itoa(id), "time": "2026-09-28 12:00:00", "action": "随时间回复开拓力", "add_num": "1", "relic_name": "过客的逢春木簪", "uid": "100000001"})
	}
	cursors := []string{}
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/binding/api/genAuthKey" {
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic-csc-key"}}), nil
		}
		end := r.URL.Query().Get("end_id")
		cursors = append(cursors, end)
		start := 0
		for start < len(records) && end != "0" && text(object(records[start])["id"]) != end {
			start++
		}
		if end != "0" {
			start++
		}
		page := records[min(start, len(records)):min(start+20, len(records))]
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": page}}), nil
	}), nil)
	read := func(input map[string]any) ([]any, map[string]any) {
		out, err := c.Execute(t.Context(), tokens, role, "starrail.billing", input)
		if err != nil {
			t.Fatal(input, err)
		}
		items, _ := out["items"].([]any)
		return items, out
	}
	items, out := read(map[string]any{"category": "power", "page_size": 50})
	if len(items) != 20 || out["has_more"] != true || out["next_end_id"] != "128" {
		t.Fatal(len(items), out["has_more"], out["next_end_id"])
	}
	if first := object(items[0]); first["item_name"] != "过客的逢春木簪" || first["uid"] != nil {
		t.Fatal("relic name not kept or unselected field copied:", first)
	}
	items, out = read(map[string]any{"category": "power", "end_id": "108"})
	if len(items) != 7 || out["has_more"] != false || text(object(items[0])["id"]) != "107" {
		t.Fatal(len(items), out["has_more"])
	}
	cursors = nil
	items, out = read(map[string]any{"category": "stellar", "page": 3})
	if len(items) != 7 || out["has_more"] != false || strings.Join(cursors, ",") != "0,128,108" {
		t.Fatal(len(items), out["has_more"], cursors)
	}
	if items, _ = read(map[string]any{"category": "stellar", "page": 4}); len(items) != 0 {
		t.Fatal("a page past the end returned records")
	}
	if _, err := c.Execute(t.Context(), tokens, role, "starrail.billing", map[string]any{"category": "stellar", "page": 51}); err == nil {
		t.Fatal("an unbounded page walk was accepted")
	}
}
