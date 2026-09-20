package mihoyo

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

type QR struct {
	URL      string
	Ticket   string
	DeviceID string
}
type Confirmation struct {
	State  string
	Tokens accounts.Tokens
}

func (c *Client) CreateQR(ctx context.Context) (QR, error) {
	device := RandomDeviceID()
	data, err := c.request(ctx, "login.start", passportHost, "/account/ma-cn-passport/app/createQRLogin", "passport", nil, map[string]any{}, accounts.Tokens{DeviceID: device})
	if err != nil {
		return QR{}, err
	}
	address, ticket := text(data["url"]), text(data["ticket"])
	parsed, parseErr := url.Parse(address)
	if parseErr != nil || parsed.Scheme != "https" || parsed.User != nil || !(parsed.Hostname() == "mihoyo.com" || strings.HasSuffix(parsed.Hostname(), ".mihoyo.com")) {
		return QR{}, invalid("login.start")
	}
	if ticket == "" {
		ticket = parsed.Query().Get("ticket")
	}
	if ticket == "" {
		return QR{}, invalid("login.start")
	}
	return QR{URL: address, Ticket: ticket, DeviceID: device}, nil
}

func (c *Client) PollQR(ctx context.Context, qr QR) (Confirmation, error) {
	cookies := map[string]string{}
	data, err := c.requestWithCookies(ctx, "login.poll", passportHost, "/account/ma-cn-passport/app/queryQRLoginStatus", "passport", nil, map[string]any{"ticket": qr.Ticket}, accounts.Tokens{DeviceID: qr.DeviceID}, cookies)
	if err != nil {
		return Confirmation{}, err
	}
	status := text(data["status"])
	if status == "" {
		status = text(data["stat"])
	}
	switch status {
	case "Created", "Init", "Waiting":
		return Confirmation{State: "waiting"}, nil
	case "Scanned":
		return Confirmation{State: "scanned"}, nil
	case "Expired":
		return Confirmation{State: "expired"}, nil
	case "Canceled":
		return Confirmation{State: "canceled"}, nil
	case "Confirmed":
		info := object(data["user_info"])
		id := text(info["aid"])
		if id == "" {
			id = text(info["account_id"])
		}
		if id == "" {
			id = text(info["uid"])
		}
		tokens := accounts.Tokens{AccountID: id, MID: text(info["mid"]), DeviceID: qr.DeviceID}
		list, _ := data["tokens"].([]any)
		for _, raw := range list {
			item := object(raw)
			name := strings.ToLower(text(item["name"]))
			value, _ := item["token"].(string)
			switch name {
			case "stoken_v2":
				tokens.SToken = value
			case "stoken":
				if tokens.SToken == "" {
					tokens.SToken = value
				}
			case "ltoken_v2":
				tokens.LToken = value
			case "ltoken":
				if tokens.LToken == "" {
					tokens.LToken = value
				}
			}
		}
		// The app QR endpoint also returns a single unnamed root token. Treat
		// it as a candidate only: the login worker must exchange it at the
		// fixed official SToken endpoints before encrypted Bind can succeed.
		// Explicitly unknown names and ambiguous arrays never use this path.
		if tokens.SToken == "" && len(list) == 1 {
			item := object(list[0])
			name, present := item["name"]
			if !present || name == nil || name == "" {
				tokens.SToken, _ = item["token"].(string)
			}
		}
		if tokens.SToken == "" {
			tokens.SToken = cookieValue(cookies, "stoken_v2", "stoken")
		}
		if tokens.LToken == "" {
			tokens.LToken = cookieValue(cookies, "ltoken_v2", "ltoken")
		}
		if tokens.MID == "" {
			tokens.MID = cookieValue(cookies, "mid", "stmid_v2", "account_mid_v2", "ltmid_v2")
		}
		cookieID := cookieValue(cookies, "stuid_v2", "stuid", "account_id_v2", "account_id", "ltuid_v2", "ltuid")
		if tokens.AccountID == "" {
			tokens.AccountID = cookieID
		} else if cookieID != "" && cookieID != tokens.AccountID {
			return Confirmation{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "login.poll", Reason: "account_id_conflict"}
		}
		if tokens.AccountID == "" || tokens.SToken == "" {
			reason := "stoken_missing"
			if len(list) == 0 {
				reason = "qr_tokens_empty"
			} else if len(list) > 1 {
				reason = "qr_stoken_unrecognized_multiple"
			} else {
				reason = "qr_token_name_or_value_unrecognized"
			}
			if tokens.AccountID == "" {
				reason = "account_id_missing"
			}
			return Confirmation{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "login.poll", Reason: reason}
		}
		return Confirmation{State: "confirmed", Tokens: tokens}, nil
	default:
		return Confirmation{}, invalid("login.poll")
	}
}

func cookieValue(cookies map[string]string, names ...string) string {
	for _, name := range names {
		if value := cookies[name]; value != "" {
			return value
		}
	}
	return ""
}

func (c *Client) Refresh(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	if tokens.Realm == "os" {
		return c.refreshOverseas(ctx, tokens)
	}
	if tokens.AccountID == "" || tokens.SToken == "" {
		return accounts.Tokens{}, &Error{Code: "plugin.upstream_auth_invalid", Operation: "accounts.refresh"}
	}
	query := url.Values{"uid": {tokens.AccountID}, "stoken": {tokens.SToken}}
	if tokens.MID != "" {
		query.Set("mid", tokens.MID)
	}
	if tokens.LToken == "" {
		ltokenQuery := url.Values{"stuid": {tokens.AccountID}, "stoken": {tokens.SToken}}
		if tokens.MID != "" {
			ltokenQuery.Set("mid", tokens.MID)
		}
		cookies := map[string]string{}
		data, err := c.requestWithCookies(ctx, "tokens.ltoken", passportHost, "/account/auth/api/getLTokenBySToken", "session", ltokenQuery, nil, tokens, cookies)
		if err != nil {
			return accounts.Tokens{}, err
		}
		tokens.LToken = text(data["ltoken"])
		if tokens.LToken == "" {
			tokens.LToken = text(object(data["token"])["token"])
		}
		if tokens.LToken == "" {
			tokens.LToken = cookieValue(cookies, "ltoken_v2", "ltoken")
		}
		if id := cookieValue(cookies, "ltuid_v2", "ltuid"); id != "" && id != tokens.AccountID {
			return accounts.Tokens{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "tokens.ltoken", Reason: "account_id_conflict"}
		}
		if tokens.LToken == "" {
			return accounts.Tokens{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "tokens.ltoken", Reason: "ltoken_missing"}
		}
	}
	cookies := map[string]string{}
	data, err := c.requestWithCookies(ctx, "tokens.cookie", passportHost, "/account/auth/api/getCookieAccountInfoBySToken", "session", query, nil, tokens, cookies)
	if err != nil {
		return accounts.Tokens{}, err
	}
	tokens.CookieToken = text(data["cookie_token"])
	if tokens.CookieToken == "" {
		tokens.CookieToken = cookieValue(cookies, "cookie_token_v2", "cookie_token")
	}
	returnedID := text(data["uid"])
	if returnedID == "" {
		returnedID = cookieValue(cookies, "account_id_v2", "account_id")
	}
	if returnedID != "" && returnedID != tokens.AccountID {
		return accounts.Tokens{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "tokens.cookie", Reason: "account_id_conflict"}
	}
	if tokens.CookieToken == "" || tokens.LToken == "" {
		return accounts.Tokens{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "tokens.cookie", Reason: "cookie_token_missing"}
	}
	return tokens, nil
}

func (c *Client) Roles(ctx context.Context, tokens accounts.Tokens) ([]accounts.Role, map[string]string) {
	roles := []accounts.Role{}
	states := map[string]string{}
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		found, err := c.RolesForGame(ctx, tokens, game)
		if err != nil {
			states[game] = "unavailable"
			continue
		}
		states[game] = "empty"
		if len(found) > 0 {
			roles = append(roles, found...)
			states[game] = "ready"
		}
	}
	return roles, states
}

func (c *Client) RolesForGame(ctx context.Context, tokens accounts.Tokens, game string) ([]accounts.Role, error) {
	biz := map[string]string{"genshin": "hk4e_cn", "starrail": "hkrpg_cn", "zzz": "nap_cn"}[game]
	host, profile := webHost, "web"
	if tokens.Realm == "os" {
		biz = overseasBiz(game)
		host, profile = "https://api-os-takumi.mihoyo.com", "os.web"
	}
	if biz == "" {
		return nil, &Error{Code: "plugin.upstream_request_invalid", Operation: "accounts.roles"}
	}
	data, err := c.request(ctx, "accounts.roles", host, "/binding/api/getUserGameRolesByCookie", profile, url.Values{"game_biz": {biz}}, nil, tokens)
	if err != nil {
		return nil, err
	}
	list, ok := data["list"].([]any)
	if !ok {
		return nil, invalid("accounts.roles")
	}
	roles := []accounts.Role{}
	for _, raw := range list {
		item := object(raw)
		if tokens.Realm == "os" && text(item["game_biz"]) != "" && text(item["game_biz"]) != biz {
			continue
		}
		uid, region := text(item["game_uid"]), text(item["region"])
		if tokens.Realm == "os" && !slices.Contains(overseasRegions[game], region) {
			return nil, invalid("accounts.roles")
		}
		if uid == "" || region == "" {
			continue
		}
		if !safeBusinessValue(map[string]any{"uid": uid, "region": region, "nickname": text(item["nickname"])}, tokens, 0) {
			return nil, invalid("accounts.roles")
		}
		roles = append(roles, accounts.Role{Ref: roleReference(game, region, uid), Game: game, GameBiz: biz, UID: uid, Region: region, Nickname: text(item["nickname"]), Level: integer(item["level"])})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Ref < roles[j].Ref })
	return roles, nil
}
