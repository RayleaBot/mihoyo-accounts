package mihoyo

import (
	"context"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const overseasSalt = "6s25p5ox5y14umn1p61aqyyvbvvl3lrt"
const overseasAppSalt = "IZPgfb0dRPtBeLuFkdDznSZ6f4wWt6y2"
const overseasAuthHost = "https://api-account-os.hoyoverse.com"
const overseasRefreshHost = "https://sg-public-api.hoyoverse.com"

var overseasRegions = map[string][]string{"genshin": {"os_usa", "os_euro", "os_asia", "os_cht"}, "starrail": {"prod_official_usa", "prod_official_eur", "prod_official_asia", "prod_official_cht"}, "zzz": {"prod_gf_us", "prod_gf_eu", "prod_gf_jp", "prod_gf_sg"}}

func overseasRole(role accounts.Role) bool {
	return slices.Contains(overseasRegions[role.Game], role.Region)
}
func overseasBiz(game string) string {
	return map[string]string{"genshin": "hk4e_global", "starrail": "hkrpg_global", "zzz": "nap_global"}[game]
}
func overseasRecord(spec operation) (string, string, bool) {
	switch {
	case strings.HasPrefix(spec.path, "/game_record/app/genshin/api/"):
		return "https://sg-public-api.hoyolab.com", strings.Replace(spec.path, "/game_record/app/genshin/api/", "/event/game_record/genshin/api/", 1), true
	case strings.HasPrefix(spec.path, "/game_record/app/hkrpg/api/"):
		return "https://bbs-api-os.hoyolab.com", strings.Replace(spec.path, "/game_record/app/hkrpg/api/", "/game_record/hkrpg/api/", 1), true
	case strings.HasPrefix(spec.path, "/event/game_record_zzz/api/zzz/"):
		return "https://sg-act-public-api.hoyolab.com", spec.path, true
	case spec.path == "/event/ys_ledger/monthInfo":
		return "https://sg-hk4e-api.hoyolab.com", "/event/ysledgeros/month_info", true
	case spec.path == "/event/srledger/month_info" || spec.path == "/event/nap_ledger/month_info":
		return "https://sg-public-api.hoyolab.com", spec.path, true
	}
	return "", "", false
}
func overseasGacha(game string) gachaProfile {
	p := gachaProfiles[game]
	p.regions = overseasRegions[game]
	p.biz = overseasBiz(game)
	p.host = map[string]string{"genshin": "https://public-operation-hk4e-sg.hoyoverse.com", "starrail": "https://public-operation-hkrpg-sg.hoyoverse.com", "zzz": "https://public-operation-nap-sg.hoyoverse.com"}[game]
	return p
}

var overseasSigns = map[string][3]string{"genshin": {"https://sg-hk4e-api.hoyolab.com", "/event/sol/", "e202102251931481"}, "starrail": {"https://sg-public-api.hoyolab.com", "/event/luna/hkrpg/os/", "e202303301540311"}, "zzz": {"https://sg-public-api.hoyolab.com", "/event/luna/zzz/os/", "e202406031448091"}}

func allowedOverseasEndpoint(host, path, profile string) bool {
	if profile == "os.legacy" {
		return host == "https://api-account-os.hoyolab.com" && slices.Contains([]string{"/account/auth/api/getLTokenBySToken", "/account/auth/api/getCookieAccountInfoBySToken"}, path)
	}
	if profile == "os.web" {
		return host == "https://api-os-takumi.mihoyo.com" && path == "/binding/api/getUserGameRolesByCookie"
	}
	if profile == "os.refresh" {
		return host == overseasRefreshHost && path == "/account/ma-passport/token/getBySToken"
	}
	if profile == "os.authkey" {
		return host == overseasAuthHost && path == "/binding/api/genAuthKeyByCookieToken"
	}
	if profile == "fingerprint" && host == "https://sg-public-data-api.hoyoverse.com" && path == "/device-fp/api/getFp" {
		return true
	}
	if profile == "record.os" {
		for _, spec := range operations {
			h, p, ok := overseasRecord(spec)
			if ok && host == h && path == p {
				return true
			}
		}
		return host == "https://sg-public-api.hoyolab.com" && slices.Contains([]string{"/event/e20200928calculate/v1/sync/avatar/detail", "/event/e20200928calculate/v3/batch_compute", "/event/rpgcalc/avatar/detail", "/event/rpgcalc/compute", "/event/calculateos/furniture/blueprint", "/event/calculateos/furniture/compute"}, path)
	}
	if strings.HasPrefix(profile, "sign.os.") {
		p := overseasSigns[strings.TrimPrefix(profile, "sign.os.")]
		return host == p[0] && slices.Contains([]string{p[1] + "info", p[1] + "home", p[1] + "sign"}, path)
	}
	if profile == "billing" && host == "https://hk4e-api-os.hoyoverse.com" {
		return slices.Contains([]string{"/common/hk4e_self_help_query/User/GetUserInfo", "/common/hk4e_self_help_query/User/GetCrystalLog", "/common/hk4e_self_help_query/User/GetPrimogemLog"}, path)
	}
	if profile == "gacha" {
		for g := range overseasRegions {
			p := overseasGacha(g)
			if host == p.host && (path == p.path || g == "starrail" && path == "/common/gacha_record/api/getLdGachaLog") {
				return true
			}
		}
	}
	return false
}
func overseasCookie(tokens accounts.Tokens, session bool) string {
	pairs := []string{}
	add := func(k, v string) {
		if v != "" && !strings.ContainsAny(v, "\r\n;") {
			pairs = append(pairs, k+"="+v)
		}
	}
	if session {
		key, id := "stoken", "stuid"
		if tokens.STokenV2 {
			key, id = "stoken", "ltuid_v2"
		}
		add(key, tokens.SToken)
		add(id, tokens.AccountID)
		add("mid", tokens.MID)
	} else {
		key, id := "ltoken", "ltuid"
		if tokens.LTokenV2 {
			key, id = "ltoken_v2", "ltuid_v2"
		}
		add(key, tokens.LToken)
		add(id, tokens.AccountID)
		key, id = "cookie_token", "account_id"
		if tokens.CookieTokenV2 {
			key, id = "cookie_token_v2", "account_id_v2"
		}
		add(key, tokens.CookieToken)
		add(id, tokens.AccountID)
		if tokens.MID != "" {
			add("ltmid_v2", tokens.MID)
			add("account_mid_v2", tokens.MID)
		}
	}
	add("mi18nLang", "zh-cn")
	return strings.Join(pairs, "; ")
}

var overseasAccountID = regexp.MustCompile(`^[0-9]{1,20}$`)

func ParseOverseasCookie(raw string) (accounts.Tokens, error) {
	bad := func() (accounts.Tokens, error) { return accounts.Tokens{}, invalidInput("overseas.import") }
	if len(raw) < 1 || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n") {
		return bad()
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "cookie:") {
		raw = strings.TrimSpace(strings.TrimSpace(raw)[7:])
	}
	values := map[string]string{}
	allowed := words("ltuid ltuid_v2 account_id account_id_v2 stuid stuid_v2 ltoken ltoken_v2 cookie_token cookie_token_v2 stoken stoken_v2 mid ltmid_v2 account_mid_v2")
	for _, pair := range strings.Split(raw, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok || !slices.Contains(allowed, key) {
			continue
		}
		if value == "" || len(value) > 4096 {
			return bad()
		}
		for _, ch := range []byte(value) {
			if ch < 33 || ch > 126 {
				return bad()
			}
		}
		if old, exists := values[key]; exists && old != value {
			return bad()
		}
		values[key] = value
	}
	tokens := accounts.Tokens{Realm: "os", DeviceID: RandomDeviceID()}
	for _, key := range words("ltuid ltuid_v2 account_id account_id_v2 stuid stuid_v2") {
		if id := values[key]; id != "" {
			if !overseasAccountID.MatchString(id) || tokens.AccountID != "" && tokens.AccountID != id {
				return bad()
			}
			tokens.AccountID = id
		}
	}
	for _, key := range words("mid ltmid_v2 account_mid_v2") {
		if value := values[key]; value != "" {
			if tokens.MID != "" && tokens.MID != value {
				return bad()
			}
			tokens.MID = value
		}
	}
	pick := func(key string) (string, bool) {
		if value := values[key+"_v2"]; value != "" {
			return value, true
		}
		return values[key], false
	}
	tokens.LToken, tokens.LTokenV2 = pick("ltoken")
	tokens.CookieToken, tokens.CookieTokenV2 = pick("cookie_token")
	tokens.SToken, tokens.STokenV2 = pick("stoken")
	tokens.STokenV2 = tokens.STokenV2 || strings.HasPrefix(tokens.SToken, "v2_")
	if tokens.AccountID == "" || tokens.LToken == "" && (!tokens.STokenV2 || tokens.SToken == "" || tokens.MID == "") {
		return bad()
	}
	return tokens, nil
}
func (c *Client) refreshOverseas(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	if tokens.SToken != "" && !tokens.STokenV2 {
		return c.refreshOverseasLegacy(ctx, tokens)
	}
	if !tokens.STokenV2 || tokens.SToken == "" || tokens.MID == "" {
		return accounts.Tokens{}, &Error{Code: "plugin.account_refresh_unavailable", Operation: "accounts.refresh"}
	}
	data, err := c.request(ctx, "accounts.refresh", overseasRefreshHost, "/account/ma-passport/token/getBySToken", "os.refresh", nil, map[string]any{"dst_token_types": []int{2, 4}}, tokens)
	if err != nil {
		return accounts.Tokens{}, err
	}
	next := tokens
	if id := text(object(data["user_info"])["aid"]); id != "" && id != tokens.AccountID {
		return accounts.Tokens{}, invalid("accounts.refresh")
	}
	haveL, haveC := false, false
	for _, v := range asArray(data["tokens"]) {
		item := object(v)
		value := text(item["token"])
		if value == "" || len(value) > 8192 || strings.ContainsAny(value, "\r\n;") {
			return accounts.Tokens{}, invalid("accounts.refresh")
		}
		switch integer(item["token_type"]) {
		case 2:
			next.LToken = value
			next.LTokenV2 = true
			haveL = true
		case 4:
			next.CookieToken = value
			next.CookieTokenV2 = true
			haveC = true
		}
	}
	if !haveL || !haveC {
		return accounts.Tokens{}, invalid("accounts.refresh")
	}
	return next, nil
}

func (c *Client) refreshOverseasLegacy(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	next := tokens
	for _, part := range []struct{ path, key string }{{"getLTokenBySToken", "ltoken"}, {"getCookieAccountInfoBySToken", "cookie_token"}} {
		data, err := c.request(ctx, "accounts.refresh", "https://api-account-os.hoyolab.com", "/account/auth/api/"+part.path, "os.legacy", nil, map[string]any{"uid": tokens.AccountID, "stoken": tokens.SToken}, tokens)
		if err != nil {
			return accounts.Tokens{}, err
		}
		if id := text(data["uid"]); id != "" && id != tokens.AccountID {
			return accounts.Tokens{}, invalid("accounts.refresh")
		}
		value := text(data[part.key+"_v2"])
		v2 := value != ""
		if !v2 {
			value = text(data[part.key])
		}
		if value == "" || len(value) > 8192 || strings.ContainsAny(value, "\r\n;") {
			return accounts.Tokens{}, invalid("accounts.refresh")
		}
		if part.key == "ltoken" {
			next.LToken, next.LTokenV2 = value, v2
		} else {
			next.CookieToken, next.CookieTokenV2 = value, v2
		}
	}
	return next, nil
}
func asArray(v any) []any { items, _ := v.([]any); return items }
func overseasTimezone(role accounts.Role) int {
	if strings.HasSuffix(role.Region, "usa") || role.Region == "prod_gf_us" {
		return -5
	}
	if strings.HasSuffix(role.Region, "euro") || strings.HasSuffix(role.Region, "_eur") || role.Region == "prod_gf_eu" {
		return 1
	}
	return 8
}
func decodeAuthKey(value string) string {
	if strings.Contains(value, "%") {
		if decoded, err := url.PathUnescape(value); err == nil {
			return decoded
		}
	}
	return value
}
