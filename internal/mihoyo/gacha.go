package mihoyo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

type cachedAuthKey struct {
	value   string
	expires time.Time
}
type gachaProfile struct {
	host, path, biz string
	pools, regions  []string
}

var gachaProfiles = map[string]gachaProfile{
	"genshin":  {"https://public-operation-hk4e.mihoyo.com", "/gacha_info/api/getGachaLog", "hk4e_cn", []string{"100", "200", "301", "302", "400", "500"}, []string{"cn_gf01", "cn_qd01"}},
	"starrail": {"https://public-operation-hkrpg.mihoyo.com", "/common/gacha_record/api/getGachaLog", "hkrpg_cn", []string{"1", "2", "11", "12", "21", "22"}, []string{"prod_gf_cn", "prod_qd_cn"}},
	"zzz":      {"https://public-operation-common.mihoyo.com", "/common/gacha_record/api/getGachaLog", "nap_cn", []string{"1001", "2001", "3001", "5001", "12001", "13001"}, []string{"prod_gf_cn"}},
}
var recordDigits = regexp.MustCompile(`^[0-9]{1,19}$`)
var zzzPools = map[string]string{"1001": "1", "2001": "2", "3001": "3", "5001": "5", "12001": "102", "13001": "103"}

func allowedGachaEndpoint(host, path, profile string) bool {
	if profile != "gacha" {
		return false
	}
	for game, spec := range gachaProfiles {
		if host == spec.host && (path == spec.path || game == "starrail" && path == "/common/gacha_record/api/getLdGachaLog") {
			return true
		}
	}
	return false
}

// ZZZGachaLink names the link of ZZZ-Plugin's 获取抽卡链接. It is not in the
// operation table, so execute never reaches it; only the gacha.link service
// method does.
const ZZZGachaLink = "zzz.gacha_link"

// zzzGachaLink is the exclusive channel's record page with an authkey from
// the SToken, in ZZZ-Plugin's getZZZGachaLink shape. As upstream, only
// mainland roles are supported.
func (c *Client) zzzGachaLink(ctx context.Context, tokens accounts.Tokens, role accounts.Role) (map[string]any, error) {
	spec := gachaProfiles["zzz"]
	if role.Game != "zzz" || tokens.Realm == "os" || role.UID == "" || !slices.Contains(spec.regions, role.Region) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: ZZZGachaLink}
	}
	key, err := c.gachaAuthKey(ctx, tokens, role, false)
	if err != nil {
		return nil, err
	}
	query := url.Values{"authkey_ver": {"1"}, "sign_type": {"2"}, "auth_appid": {"webview_gacha"}, "init_log_gacha_type": {"2001"}, "init_log_gacha_base_type": {"2"}, "gacha_id": {"2c1f5692fdfbb733a08733f9eb69d32aed1d37"}, "timestamp": {strconv.FormatInt(c.now().Unix(), 10)}, "lang": {"zh-cn"}, "device_type": {"mobile"}, "plat_type": {"ios"}, "region": {role.Region}, "authkey": {key}, "game_biz": {spec.biz}, "gacha_type": {"2001"}, "real_gacha_type": {"2"}, "page": {"1"}, "size": {"20"}, "end_id": {"0"}}
	return map[string]any{"url": spec.host + spec.path + "?" + query.Encode()}, nil
}

func (c *Client) gachaAuthKey(ctx context.Context, tokens accounts.Tokens, role accounts.Role, force bool) (string, error) {
	return c.businessAuthKey(ctx, tokens, role, "webview_gacha", "gacha.authorize", force)
}
func (c *Client) businessAuthKey(ctx context.Context, tokens accounts.Tokens, role accounts.Role, appID, operation string, force bool) (string, error) {
	if appID != "webview_gacha" && appID != "csc" {
		return "", invalidInput(operation)
	}
	digest := sha256.Sum256([]byte(tokens.RealmName() + "\x00" + appID + "\x00" + tokens.AccountID + "\x00" + tokens.SToken + "\x00" + tokens.CookieToken + "\x00" + role.Game + "\x00" + role.Region + "\x00" + role.UID))
	cacheID := hex.EncodeToString(digest[:])
	c.gachaMu.Lock()
	entry := c.authKeys[cacheID]
	c.gachaMu.Unlock()
	if !force && entry.value != "" && c.now().Before(entry.expires) {
		return entry.value, nil
	}
	if tokens.SToken == "" && tokens.Realm != "os" {
		return "", &Error{Code: "plugin.upstream_auth_invalid", Operation: operation}
	}
	uid, err := strconv.ParseUint(role.UID, 10, 64)
	if err != nil {
		return "", invalidInput(operation)
	}
	host, path, profile := webHost, "/binding/api/genAuthKey", "authkey"
	body := map[string]any{"auth_appid": appID, "game_biz": gachaProfiles[role.Game].biz, "game_uid": uid, "region": role.Region}
	if tokens.Realm == "os" {
		if !tokens.CookieTokenV2 || tokens.CookieToken == "" || tokens.MID == "" {
			return "", &Error{Code: "plugin.account_auth_material_missing", Operation: operation}
		}
		host, path, profile = overseasAuthHost, "/binding/api/genAuthKeyByCookieToken", "os.authkey"
		body["game_biz"] = overseasBiz(role.Game)
		body["game_uid"] = role.UID
	}
	data, err := c.request(ctx, operation, host, path, profile, nil, body, tokens)
	if err != nil {
		return "", err
	}
	key := text(data["authkey"])
	if tokens.Realm == "os" {
		key = decodeAuthKey(key)
	}
	if len(key) < 8 || len(key) > 16384 || strings.ContainsAny(key, "\r\n") {
		return "", invalid(operation)
	}
	c.gachaMu.Lock()
	defer c.gachaMu.Unlock()
	if c.authKeys == nil {
		c.authKeys = map[string]cachedAuthKey{}
	}
	for id, entry := range c.authKeys {
		if !c.now().Before(entry.expires) {
			delete(c.authKeys, id)
		}
	}
	if len(c.authKeys) >= 128 {
		for id := range c.authKeys {
			delete(c.authKeys, id)
			break
		}
	}
	c.authKeys[cacheID] = cachedAuthKey{key, c.now().Add(30 * time.Minute)}
	return key, nil
}

func (c *Client) gachaPage(ctx context.Context, tokens accounts.Tokens, role accounts.Role, input map[string]any) (map[string]any, error) {
	name := role.Game + ".gacha"
	spec := gachaProfiles[role.Game]
	if tokens.Realm == "os" {
		spec = overseasGacha(role.Game)
	}
	if !slices.Contains(spec.regions, role.Region) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	for key := range input {
		if !slices.Contains([]string{"gacha_type", "end_id", "page"}, key) {
			return nil, invalidInput(name)
		}
	}
	pool := text(input["gacha_type"])
	endID := text(input["end_id"])
	if endID == "" {
		endID = "0"
	}
	page := 1
	if raw, exists := input["page"]; exists {
		var err error
		page, err = strconv.Atoi(stringNumber(raw))
		if err != nil {
			return nil, invalidInput(name)
		}
	}
	if !slices.Contains(spec.pools, pool) || !recordDigits.MatchString(endID) || page < 1 || page > 10001 {
		return nil, invalidInput(name)
	}
	key, err := c.gachaAuthKey(ctx, tokens, role, false)
	if err != nil {
		return nil, err
	}
	query := url.Values{"authkey_ver": {"1"}, "auth_appid": {"webview_gacha"}, "authkey": {key}, "sign_type": {"2"}, "lang": {"zh-cn"}, "region": {role.Region}, "game_biz": {spec.biz}, "gacha_type": {pool}, "page": {strconv.Itoa(page)}, "size": {"20"}, "end_id": {endID}}
	path := spec.path
	if role.Game == "starrail" {
		query.Set("default_gacha_type", "11")
		query.Del("auth_appid")
		query.Del("sign_type")
	}
	if role.Game == "starrail" && (pool == "21" || pool == "22") {
		path = "/common/gacha_record/api/getLdGachaLog"
	}
	if role.Game == "zzz" {
		query.Set("init_log_gacha_type", pool)
		query.Set("init_log_gacha_base_type", zzzPools[pool])
		query.Set("real_gacha_type", zzzPools[pool])
		query.Set("gacha_id", "2c1f5692fdfbb733a08733f9eb69d32aed1d37")
		query.Set("timestamp", strconv.FormatInt(c.now().Unix(), 10))
		query.Set("device_type", "mobile")
		query.Set("plat_type", "ios")
	}
	data, err := c.request(ctx, name, spec.host, path, "gacha", query, nil, accounts.Tokens{})
	var failure *Error
	if errors.As(err, &failure) && failure.Code == "plugin.upstream_gacha_expired" {
		key, err = c.gachaAuthKey(ctx, tokens, role, true)
		if err != nil {
			return nil, err
		}
		query.Set("authkey", key)
		data, err = c.request(ctx, name, spec.host, path, "gacha", query, nil, accounts.Tokens{})
	}
	if tokens.Realm != "os" && role.Game == "starrail" && errors.As(err, &failure) && failure.Code == "plugin.upstream_gacha_expired" {
		return nil, &Error{Code: "plugin.upstream_gacha_unavailable", Operation: name, RetCode: failure.RetCode, Reason: "stoken_authkey_not_supported"}
	}
	if err != nil {
		return nil, err
	}
	// Record checks, pool conversion and the business timezone belong to the
	// game plugin; the page leaves here without credentials only.
	clean, ok := sanitizeBusinessValue(data, tokens, 0)
	result, isObject := clean.(map[string]any)
	encoded, _ := json.Marshal(result)
	if !ok || !isObject || strings.Contains(string(encoded), key) {
		return nil, invalid(name)
	}
	return result, nil
}
