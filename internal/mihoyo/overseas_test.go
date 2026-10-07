package mihoyo

import (
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/http"
	"strings"
	"testing"
)

func osTokens() accounts.Tokens {
	return accounts.Tokens{Realm: "os", AccountID: "123456789", LToken: "synthetic-os-ltoken", LTokenV2: true, CookieToken: "synthetic-os-cookie", CookieTokenV2: true, SToken: "v2_synthetic-os-stoken", STokenV2: true, MID: "synthetic-os-mid", DeviceID: "synthetic-os-device"}
}
func TestOverseasCookieVersionsAndMismatchedIDs(t *testing.T) {
	tokens, err := ParseCookie("Cookie: ltuid_v2=123456789; account_id_v2=123456789; ltoken_v2=synthetic-os-ltoken; cookie_token_v2=synthetic-os-cookie; stoken=v2_synthetic-os-stoken; ltmid_v2=synthetic-os-mid; analytics=ignored", "os")
	if err != nil || tokens.Realm != "os" || !tokens.LTokenV2 || !tokens.CookieTokenV2 || !tokens.STokenV2 || tokens.MID == "" {
		t.Fatal(err)
	}
	if cookie := cookieHeader(tokens, false); !strings.Contains(cookie, "ltoken_v2=") || strings.Contains(cookie, tokens.SToken) || strings.Contains(cookie, "analytics") {
		t.Fatal("wrong OS business cookie")
	}
	if cookie := cookieHeader(tokens, true); !strings.Contains(cookie, "stoken="+tokens.SToken) || strings.Contains(cookie, "stoken_v2=") {
		t.Fatal("wrong OS refresh cookie")
	}
	for _, raw := range []string{"ltuid=1; account_id=2; ltoken=value", "ltuid=1; ltoken=value\r\nInjected=x", "https://example.com?ltoken=bad"} {
		if _, err = ParseCookie(raw, "os"); err == nil {
			t.Fatal("invalid authorization accepted")
		}
	}
}
func TestOverseasRolesAndRecordRoutingNeverUseChinaCredentials(t *testing.T) {
	for game, region := range map[string]string{"genshin": "os_usa", "starrail": "prod_official_eur", "zzz": "prod_gf_jp"} {
		t.Run(game, func(t *testing.T) {
			calls := 0
			tokens := osTokens()
			c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/binding/api/getUserGameRolesByCookie" {
					if r.URL.Host != "api-os-takumi.mihoyo.com" || r.URL.Query().Get("game_biz") != overseasBiz(game) {
						t.Fatal("incorrect discovery destination")
					}
					return reply(map[string]any{"retcode": 0, "data": map[string]any{"list": []any{map[string]any{"game_uid": "800000001", "region": region, "nickname": "fixture", "level": 60}}}}), nil
				}
				want := map[string]string{"genshin": "sg-public-api.hoyolab.com", "starrail": "bbs-api-os.hoyolab.com", "zzz": "sg-act-public-api.hoyolab.com"}[game]
				if r.URL.Host != want || r.Header.Get("x-rpc-app_version") != "1.5.0" || !strings.Contains(r.Header.Get("Cookie"), "ltoken_v2="+tokens.LToken) || strings.Contains(r.Header.Get("Cookie"), tokens.SToken) {
					t.Fatal("incorrect record profile")
				}
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"current_resin": 10, "current_stamina": 10, "energy": map[string]any{"progress": map[string]any{"current": 10, "max": 240}}}}), nil
			}), nil)
			roles, err := c.RolesForGame(t.Context(), tokens, game)
			if err != nil || len(roles) != 1 || roles[0].GameBiz != overseasBiz(game) {
				t.Fatal(roles, err)
			}
			if _, err = c.Execute(t.Context(), tokens, roles[0], game+".note", nil); err != nil {
				t.Fatal(err)
			}
			tokens.Realm = "cn"
			if _, err = c.Execute(t.Context(), tokens, roles[0], game+".note", nil); err == nil || calls != 2 {
				t.Fatal("CN credentials crossed to overseas")
			}
		})
	}
}
func TestOverseasRefreshUsesSTokenAndReturnsBothV2Tokens(t *testing.T) {
	tokens := osTokens()
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sg-public-api.hoyoverse.com" || r.Header.Get("x-rpc-app_id") != "c9oqaq3s3gu8" || !strings.Contains(r.Header.Get("Cookie"), "stoken="+tokens.SToken) || strings.Contains(r.Header.Get("Cookie"), tokens.CookieToken) {
			t.Fatal("wrong refresh profile")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["dst_token_types"].([]any)) != 2 {
			t.Fatal("missing refresh targets")
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"tokens": []any{map[string]any{"token_type": 2, "token": "synthetic-refreshed-os-ltoken"}, map[string]any{"token_type": 4, "token": "synthetic-refreshed-os-cookie"}}}}), nil
	}), nil)
	next, err := c.Refresh(t.Context(), tokens)
	if err != nil || next.LToken == tokens.LToken || next.CookieToken == tokens.CookieToken || next.AccountID != tokens.AccountID || !next.CookieTokenV2 || !next.LTokenV2 {
		t.Fatal(err)
	}
}
func TestOverseasGachaCookieAuthorization(t *testing.T) {
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		t.Run(game, func(t *testing.T) {
			tokens := osTokens()
			p := overseasGacha(game)
			role := accounts.Role{Game: game, UID: "600000001", Region: p.regions[0]}
			c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/binding/api/genAuthKeyByCookieToken" {
					if r.URL.Host != "api-account-os.hoyoverse.com" || !strings.Contains(r.Header.Get("Cookie"), tokens.CookieToken) || strings.Contains(r.Header.Get("Cookie"), tokens.SToken) {
						t.Fatal("wrong gacha exchange")
					}
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["game_uid"] != role.UID || body["game_biz"] != p.biz {
						t.Fatal("wrong role")
					}
					return reply(map[string]any{"retcode": 0, "data": map[string]any{"authkey": "synthetic%2Bauth+key"}}), nil
				}
				if "https://"+r.URL.Host != p.host || r.Header.Get("Cookie") != "" || r.URL.Query().Get("authkey") != "synthetic+auth+key" {
					t.Fatal("wrong private authkey route")
				}
				data := map[string]any{"list": []any{}}
				if game == "zzz" {
					data["region_time_zone"] = -13
				} else if game == "starrail" {
					data["region_time_zone"] = -5
				}
				return reply(map[string]any{"retcode": 0, "data": data}), nil
			}), nil)
			out, err := c.Execute(t.Context(), tokens, role, game+".gacha", map[string]any{"gacha_type": p.pools[0]})
			if err != nil || game != "genshin" && out["region_time_zone"] == nil {
				t.Fatal(out, err)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "synthetic") {
				t.Fatal("authorization escaped proxy")
			}
		})
	}
}
func TestOverseasSignNestedChallengeIsNotSuccess(t *testing.T) {
	tokens := osTokens()
	role := accounts.Role{Game: "genshin", UID: "600000001", Region: "os_usa"}
	challenge := true
	c := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sg-hk4e-api.hoyolab.com" || r.Header.Get("x-rpc-signgame") != "hk4e" {
			t.Fatal("wrong sign endpoint")
		}
		if r.Method == "GET" {
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"is_sign": false, "total_sign_day": 0}}), nil
		}
		if r.URL.Query().Get("act_id") != "e202102251931481" {
			t.Fatal("wrong activity")
		}
		risk := map[string]any{"risk_code": 0}
		if challenge {
			risk = map[string]any{"risk_code": 1, "gt": "test", "challenge": "test"}
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"gt_result": risk}}), nil
	}), nil)
	if _, err := c.Execute(t.Context(), tokens, role, "genshin.sign", nil); err == nil {
		t.Fatal("risk challenge reported signed")
	}
	challenge = false
	if out, err := c.Execute(t.Context(), tokens, role, "genshin.sign", nil); err != nil || out["signed"] != true {
		t.Fatal(out, err)
	}
}
