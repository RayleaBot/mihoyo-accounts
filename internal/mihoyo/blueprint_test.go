package mihoyo

import (
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/http"
	"testing"
)

func TestBlueprintFixedRegionReadAndBoundedCompute(t *testing.T) {
	calls := 0
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api-takumi.mihoyo.com" {
			t.Fatal("wrong host")
		}
		if r.Method == "GET" {
			if r.URL.Query().Get("share_code") != "12345678901" || r.URL.Query().Get("region") != "cn_gf01" {
				t.Fatal("wrong blueprint identity")
			}
		} else {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if len(body["list"].([]any)) != 1 {
				t.Fatal("wrong material list")
			}
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{map[string]any{"id": 123, "name": "合成摆设", "num": 2, "cookie": "discard"}}}}), nil
	}), nil)
	role := accounts.Role{Game: "genshin", UID: "100000001", Region: "cn_gf01"}
	out, err := c.Execute(t.Context(), accounts.Tokens{}, role, "genshin.blueprint_read", map[string]any{"share_code": "12345678901"})
	if err != nil || out["list"].([]any)[0].(map[string]any)["cookie"] != nil {
		t.Fatal(out, err)
	}
	if _, err = c.Execute(t.Context(), accounts.Tokens{}, role, "genshin.blueprint_compute", map[string]any{"list": []any{map[string]any{"id": 123, "cnt": 2}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Execute(t.Context(), accounts.Tokens{}, role, "genshin.blueprint_compute", map[string]any{"list": []any{map[string]any{"id": 123, "cnt": -1}}}); err == nil || calls != 2 {
		t.Fatal("invalid quantities sent")
	}
}
