package mihoyo

import (
	"bytes"
	"context"
	"crypto/md5" // Upstream DS signing only; credential storage uses AES-GCM.
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

const passportHost = "https://passport-api.mihoyo.com"
const webHost = "https://api-takumi.mihoyo.com"
const recordHost = "https://api-takumi-record.mihoyo.com"
const fingerprintHost = "https://public-data-api.mihoyo.com"
const ledgerHost = "https://hk4e-api.mihoyo.com"
const activityHost = "https://act-api-takumi.mihoyo.com"
const appVersion = "2.70.1"
const passportSalt = "JwYDpKvLj6MrMqqYU6jTKF17KNO2PXoS"
const recordSalt = "xV8v4Qu54lUKrEYFZkJhB8cuOh9Asafs"
const webSalt = "sjdNFJB7XxyDWGIAk0eTV8AOCfMJmyEo"
const maxResponseBytes = 4 * 1024 * 1024

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type Client struct {
	http     HTTPDoer
	now      func() time.Time
	gachaMu  sync.Mutex
	authKeys map[string]cachedAuthKey
}
type Error struct {
	Code       string
	RetCode    int
	HTTPStatus int
	Operation  string
	Reason     string
}

func (e *Error) Error() string { return e.Code }

func New(client HTTPDoer, now func() time.Time) *Client {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxConnsPerHost = 8
		client = &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if now == nil {
		now = time.Now
	}
	return &Client{http: client, now: now}
}

func RandomDeviceID() string {
	data := make([]byte, 16)
	_, _ = rand.Read(data)
	return strings.ToUpper(hex.EncodeToString(data))
}

func (c *Client) request(ctx context.Context, operation, host, path, profile string, query url.Values, body any, tokens accounts.Tokens) (map[string]any, error) {
	return c.requestWithCookies(ctx, operation, host, path, profile, query, body, tokens, nil)
}

func (c *Client) requestWithCookies(ctx context.Context, operation, host, path, profile string, query url.Values, body any, tokens accounts.Tokens, received map[string]string) (map[string]any, error) {
	return c.requestWithSession(ctx, operation, host, path, profile, query, body, tokens, received, nil)
}

func (c *Client) requestWithSession(ctx context.Context, operation, host, path, profile string, query url.Values, body any, tokens accounts.Tokens, received map[string]string, activityCookies map[string]string) (map[string]any, error) {
	if host != passportHost && host != webHost && host != recordHost && !(host == fingerprintHost && path == "/device-fp/api/getFp" && profile == "fingerprint") && !(host == ledgerHost && path == "/event/ys_ledger/monthInfo" && profile == "record") && !(host == activityHost && profile == "badge" && (path == "/event/rpg_gacha_record/pool_stat" || path == "/event/rpg_gacha_record/five_star_list")) && !allowedGachaEndpoint(host, path, profile) && !allowedSignEndpoint(host, path, profile) && !allowedCommunityEndpoint(host, path, profile) && !allowedCloudEndpoint(host, path, profile) && !allowedBillingEndpoint(host, path, profile) && !allowedBlueprintEndpoint(host, path, profile) && !allowedOverseasEndpoint(host, path, profile) && !allowedBirthdayEndpoint(host, path, profile) && !allowedRedeemEndpoint(host, path, profile) {
		return nil, &Error{Code: "plugin.upstream_endpoint_invalid", Operation: operation}
	}
	var payload []byte
	method := http.MethodGet
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, &Error{Code: "plugin.upstream_request_invalid", Operation: operation}
		}
		method = http.MethodPost
	}
	address := host + path
	encodedQuery := query.Encode()
	if encodedQuery != "" {
		address += "?" + encodedQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(payload))
	if err != nil {
		return nil, &Error{Code: "plugin.upstream_request_invalid", Operation: operation}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/4.8.0")
	req.Header.Set("x-rpc-app_version", appVersion)
	req.Header.Set("x-rpc-device_id", tokens.DeviceID)
	req.Header.Set("x-rpc-device_name", "RayleaBot")
	req.Header.Set("x-rpc-device_model", "RayleaBot")
	req.Header.Set("x-rpc-client_type", "5")
	req.Header.Set("x-rpc-sys_version", "11")
	if tokens.DeviceFP != "" {
		req.Header.Set("x-rpc-device_fp", tokens.DeviceFP)
	}
	cookie := cookieHeader(tokens, false)
	switch profile {
	case "community", "community.sign", "community.status":
		req.Header.Set("x-rpc-app_version", signVersion)
		req.Header.Set("x-rpc-client_type", "2")
		req.Header.Set("x-rpc-channel", "miyousheluodi")
		req.Header.Set("x-rpc-verify_key", "bll8iq97cem8")
		req.Header.Set("x-rpc-csm_source", "discussion")
		req.Header.Set("Referer", "https://app.mihoyo.com")
		req.Header.Set("DS", c.ds1(communitySalt))
		cookie = cookieHeader(tokens, true)
		if profile == "community.sign" {
			req.Header.Set("DS", c.ds2(communitySignSalt, "", payload))
		}
		if profile == "community.status" {
			cookie = cookieHeader(tokens, false)
			req.Header.Set("Origin", "https://webstatic.mihoyo.com")
			req.Header.Set("Referer", "https://webstatic.mihoyo.com/")
			req.Header.Set("x-rpc-client_type", "5")
			req.Header.Del("DS")
		}
	case "cloud.genshin", "cloud.zzz":
		cookie = ""
		token := tokens.CloudGenshin
		if profile == "cloud.zzz" {
			token = tokens.CloudZZZ
		}
		req.Header.Set("x-rpc-combo_token", token)
		req.Header.Set("User-Agent", "okhttp/4.10.0")
		req.Header.Set("Referer", "https://app.mihoyo.com")
		for _, key := range []string{"x-rpc-device_fp", "x-rpc-device_id", "x-rpc-device_name", "x-rpc-device_model", "x-rpc-app_version", "x-rpc-client_type", "x-rpc-sys_version"} {
			req.Header.Del(key)
		}
	case "passport":
		req.Header.Set("x-rpc-app_id", "bll8iq97cem8")
		req.Header.Set("x-rpc-client_type", "2")
		req.Header.Set("x-rpc-game_biz", "bbs_cn")
		req.Header.Set("x-rpc-sdk_version", "1.3.1.2")
		req.Header.Set("x-rpc-channel", "appstore")
		// Passport signing excludes legacy auth query parameters. Record APIs
		// use their own profile below, where the complete query is signed.
		req.Header.Set("DS", c.ds2(passportSalt, "", payload))
		cookie = cookieHeader(tokens, true)
	case "session":
		// Legacy token exchanges authenticate the SToken and MID directly.
		// They do not need the QR/record signature headers.
		cookie = cookieHeader(tokens, true)
	case "authkey":
		cookie = cookieHeader(tokens, true)
		req.Header.Set("DS", c.ds1(webSalt))
		req.Header.Set("Referer", "https://app.mihoyo.com")
		req.Header.Set("Origin", "https://webstatic.mihoyo.com")
	case "billing":
		cookie = ""
		req.Header.Set("Origin", "https://webstatic.mihoyo.com")
		req.Header.Set("Referer", "https://webstatic.mihoyo.com/")
	case "redeem":
		req.Header.Set("Origin", "https://act.hoyolab.com")
		req.Header.Set("Referer", "https://act.hoyolab.com/")
		req.Header.Set("x-rpc-language", "zh-cn")
		req.Header.Del("x-rpc-device_fp")
	case "gacha":
		cookie = ""
	case "badge":
		req.Header.Set("Origin", "https://act.mihoyo.com")
		req.Header.Set("Referer", "https://act.mihoyo.com/sr/event/gt-aio/gacha-records/index.html")
		req.Header.Set("x-rpc-jump_source", "wechatmp")
		req.Header.Set("x-rpc-platform", "4")
		for _, name := range []string{"e_hkrpg_token", "aliyungf_tc"} {
			if value := activityCookies[name]; value != "" && !strings.ContainsAny(value, "\r\n;") {
				cookie += "; " + name + "=" + value
			}
		}
	case "record", "record.zzz":
		req.Header.Set("DS", c.ds2(recordSalt, encodedQuery, payload))
		req.Header.Set("x-rpc-app_version", "2.40.1")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 12; XQ-BC52) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.73 Mobile Safari/537.36 miHoYoBBS/2.40.1")
		req.Header.Set("Referer", "https://webstatic.mihoyo.com/")
		if profile == "record.zzz" {
			req.Header.Set("x-rpc-app_version", "2.73.1")
			req.Header.Set("x-rpc-client_type", "2")
			req.Header.Set("x-rpc-sys_version", "12")
			req.Header.Set("x-rpc-channel", "mihoyo")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13; XQ-BC52 Build/61.2.A.0.472A; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/111.0.5563.116 Mobile Safari/537.36 miHoYoBBS/2.73.1")
			req.Header.Set("Origin", "https://act.mihoyo.com")
			req.Header.Set("Referer", "https://act.mihoyo.com/")
		}
	case "fingerprint":
		// Fingerprint registration is anonymous, even if a caller passes tokens.
		cookie = ""
		req.Header.Del("x-rpc-device_fp")
	case "web":
		req.Header.Set("DS", c.ds1(webSalt))
	case "sign.genshin", "sign.starrail", "sign.zzz":
		req.Header.Set("DS", c.ds1(signSalt))
		req.Header.Set("x-rpc-app_version", signVersion)
		req.Header.Set("x-rpc-channel", "miyousheluodi")
		req.Header.Set("Origin", "https://act.mihoyo.com")
		req.Header.Set("Referer", "https://act.mihoyo.com/")
		req.Header.Set("X-Requested-With", "com.mihoyo.hyperion")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 Chrome/103.0.5060.129 Mobile Safari/537.36 miHoYoBBS/"+signVersion)
		if profile == "sign.genshin" {
			req.Header.Set("x-rpc-signgame", "hk4e")
		}
		if profile == "sign.zzz" {
			req.Header.Set("x-rpc-signgame", "zzz")
		}
	}
	if profile == "birthday.os" || profile == "record.os" || strings.HasPrefix(profile, "os.") || strings.HasPrefix(profile, "sign.os.") {
		for _, key := range []string{"x-rpc-device_name", "x-rpc-device_model", "x-rpc-sys_version", "x-rpc-game_biz", "x-rpc-channel", "x-rpc-platform"} {
			req.Header.Del(key)
		}
		req.Header.Set("x-rpc-app_version", "1.5.0")
		req.Header.Set("x-rpc-client_type", "5")
		req.Header.Set("x-rpc-language", "zh-cn")
		req.Header.Set("x-rpc-lang", "zh-cn")
		req.Header.Set("Origin", "https://act.hoyolab.com")
		req.Header.Set("Referer", "https://act.hoyolab.com/")
		req.Header.Set("DS", c.ds1(overseasSalt))
		cookie = overseasCookie(tokens, false)
		if profile == "os.refresh" {
			cookie = overseasCookie(tokens, true)
			req.Header.Set("x-rpc-app_id", "c9oqaq3s3gu8")
			req.Header.Set("DS", c.ds1(overseasAppSalt))
		}
		if profile == "os.authkey" {
			req.Header.Del("DS")
		}
		if profile == "os.legacy" {
			cookie = ""
		}
		if strings.HasPrefix(profile, "sign.os.") {
			req.Header.Del("DS")
			req.Header.Set("x-rpc-signgame", map[string]string{"genshin": "hk4e", "starrail": "hkrpg", "zzz": "zzz"}[strings.TrimPrefix(profile, "sign.os.")])
		}
	}
	if strings.HasPrefix(profile, "birthday") {
		req.Header.Del("DS")
		req.Header.Set("Referer", "https://webstatic.mihoyo.com/")
		if v := activityCookies["e_hk4e_token"]; v != "" && !strings.ContainsAny(v, "\r\n;") {
			cookie += "; e_hk4e_token=" + v
		}
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Code: "plugin.upstream_unavailable", Operation: operation}
	}
	defer response.Body.Close()
	if received != nil {
		for _, cookie := range response.Cookies() {
			switch cookie.Name {
			case "stoken", "stoken_v2", "stuid", "stuid_v2", "mid", "stmid_v2", "ltoken", "ltoken_v2", "ltuid", "ltuid_v2", "ltmid_v2", "cookie_token", "cookie_token_v2", "account_id", "account_id_v2", "account_mid_v2", "e_hkrpg_token", "e_hk4e_token", "aliyungf_tc":
				if cookie.Value != "" && cookie.MaxAge >= 0 {
					received[cookie.Name] = cookie.Value
				}
			}
		}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, &Error{Code: "plugin.upstream_rate_limited", Operation: operation, HTTPStatus: response.StatusCode}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &Error{Code: "plugin.upstream_unavailable", Operation: operation, HTTPStatus: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: operation}
	}
	defer clear(raw)
	var envelope struct {
		RetCode *int           `json:"retcode"`
		Data    map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&envelope) != nil || envelope.RetCode == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: operation}
	}
	if *envelope.RetCode != 0 {
		if profile == "redeem" && (*envelope.RetCode == -2017 || *envelope.RetCode == -2018) {
			return map[string]any{"already_redeemed": true}, nil
		}
		if strings.HasPrefix(profile, "sign.") && strings.HasSuffix(operation, ".sign") && *envelope.RetCode == -5003 {
			return map[string]any{"already_signed": true}, nil
		}
		code := "plugin.upstream_rejected"
		switch *envelope.RetCode {
		case 10001, -100:
			code = "plugin.upstream_auth_invalid"
		case 1034, 10035:
			code = "plugin.upstream_challenge_required"
		case 10041:
			code = "plugin.upstream_device_required"
		}
		if profile == "gacha" && (*envelope.RetCode == -100 || *envelope.RetCode == -101) {
			code = "plugin.upstream_gacha_expired"
		}
		if profile == "badge" && host == activityHost && code == "plugin.upstream_auth_invalid" {
			code = "plugin.upstream_gacha_unavailable"
		}
		if profile == "billing" && (*envelope.RetCode == -100 || *envelope.RetCode == -101) {
			code = "plugin.upstream_billing_unavailable"
		}
		if profile == "redeem" {
			code = "plugin.upstream_redeem_unavailable"
			switch *envelope.RetCode {
			case -2001:
				code = "plugin.redeem_expired"
			case -1065, -2003, -2004, -2006, -2014:
				code = "plugin.redeem_invalid"
			case -2016:
				code = "plugin.redeem_cooldown"
			case -2011, -2021:
				code = "plugin.redeem_level_low"
			}
		}
		if strings.HasPrefix(profile, "birthday") && code == "plugin.upstream_auth_invalid" {
			code = "plugin.upstream_birthday_unavailable"
		}
		if strings.HasPrefix(profile, "community") && code == "plugin.upstream_auth_invalid" {
			code = "plugin.upstream_community_unavailable"
		}
		if strings.HasPrefix(profile, "cloud.") && code == "plugin.upstream_auth_invalid" {
			code = "plugin.upstream_cloud_unavailable"
		}
		if strings.HasPrefix(profile, "sign.") && code == "plugin.upstream_auth_invalid" {
			code = "plugin.upstream_sign_unavailable"
		}
		return nil, &Error{Code: code, RetCode: *envelope.RetCode, Operation: operation}
	}
	if envelope.Data == nil && (profile == "redeem" || strings.HasPrefix(profile, "birthday") && operation == "genshin.birthday_claim" || strings.HasPrefix(profile, "sign.os.") && strings.HasSuffix(operation, ".sign")) {
		return map[string]any{}, nil
	}
	if envelope.Data == nil && (profile == "community" && operation != "" && strings.Contains(operation, ".community_") && !strings.HasSuffix(operation, ".community_posts") || profile == "community.sign" || strings.HasSuffix(operation, ".cloud_claim")) {
		return map[string]any{}, nil
	}
	if envelope.Data == nil {
		return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: operation}
	}
	return envelope.Data, nil
}

func cookieHeader(tokens accounts.Tokens, session bool) string {
	if tokens.Realm == "os" {
		return overseasCookie(tokens, session)
	}
	var pairs []string
	add := func(name, value string) {
		if value != "" && !strings.ContainsAny(value, "\r\n;") {
			pairs = append(pairs, name+"="+value)
		}
	}
	if session {
		add("stuid", tokens.AccountID)
		add("stoken", tokens.SToken)
		add("mid", tokens.MID)
	} else {
		add("ltuid", tokens.AccountID)
		add("account_id", tokens.AccountID)
		add("ltoken", tokens.LToken)
		add("cookie_token", tokens.CookieToken)
		if tokens.MID != "" {
			add("ltmid_v2", tokens.MID)
			add("account_mid_v2", tokens.MID)
		}
	}
	return strings.Join(pairs, "; ")
}

func (c *Client) ds1(salt string) string {
	stamp := strconv.FormatInt(c.now().Unix(), 10)
	nonce := strings.ToLower(rand.Text()[:6])
	digest := md5.Sum([]byte("salt=" + salt + "&t=" + stamp + "&r=" + nonce))
	return stamp + "," + nonce + "," + hex.EncodeToString(digest[:])
}

func (c *Client) ds2(salt, query string, body []byte) string {
	stamp := strconv.FormatInt(c.now().Unix(), 10)
	limit, offset := int64(100000), int64(100001)
	if salt == recordSalt {
		limit, offset = 900000, 100000
	}
	number, _ := rand.Int(rand.Reader, big.NewInt(limit))
	nonce := strconv.FormatInt(number.Int64()+offset, 10)
	digest := md5.Sum([]byte("salt=" + salt + "&t=" + stamp + "&r=" + nonce + "&b=" + string(body) + "&q=" + query))
	return stamp + "," + nonce + "," + hex.EncodeToString(digest[:])
}

func text(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return ""
	}
}
func object(value any) map[string]any { result, _ := value.(map[string]any); return result }
func integer(value any) int           { v, _ := strconv.Atoi(text(value)); return v }
func invalid(operation string) error {
	return &Error{Code: "plugin.upstream_response_invalid", Operation: operation}
}
func roleReference(game, region, uid string) string {
	return fmt.Sprintf("%s:%s:%s", game, region, uid)
}
