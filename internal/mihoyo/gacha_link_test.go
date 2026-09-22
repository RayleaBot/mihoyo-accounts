package mihoyo

import (
	"net/http"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestGachaLinkReadsStarRailPagesInStarRailPluginsShape(t *testing.T) {
	if _, err := ParseGachaLink("https://example.com/?authkey=synthetic-link-key"); err == nil {
		t.Fatal("a link outside miHoYo was accepted")
	}
	key, err := ParseGachaLink("https://webstatic.mihoyo.com/hkrpg/event/e20211215gacha-v2/index.html?authkey_ver=1&sign_type=2&auth_appid=webview_gacha&authkey=synthetic%2Blink%2Fkey&game_biz=hkrpg_cn")
	if err != nil || key != "synthetic+link/key" {
		t.Fatal(key, err)
	}
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if req.URL.Host != "api-takumi.mihoyo.com" || q.Get("authkey") != key || q.Has("auth_appid") || q.Has("sign_type") || q.Get("default_gacha_type") != "11" || req.Header.Get("Cookie") != "" {
			t.Fatalf("request = %s", req.URL)
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{}}}), nil
	}), nil)
	tokens := accounts.Tokens{AccountID: "1", SToken: "synthetic-stoken", GachaLinks: map[string]accounts.GachaLink{"100000001": {AuthKey: key, ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli()}}}
	role := accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}
	if _, err := client.gachaPage(t.Context(), tokens, role, map[string]any{"gacha_type": "11"}); err != nil {
		t.Fatal(err)
	}
}
