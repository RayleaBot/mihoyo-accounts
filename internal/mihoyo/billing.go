package mihoyo

import (
	"context"
	"errors"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

func init() {
	for _, g := range []string{"genshin", "starrail", "zzz"} {
		operations[g+".redeem"] = operation{game: g, mutates: true}
	}
	for _, g := range []string{"genshin", "starrail"} {
		operations[g+".billing"] = operation{game: g}
	}
}

// starrailBillingPage is how many records the Star Rail self-service list
// returns per page. The list now pages by end_id, the last record's ID, like
// Genshin's; it ignores the page and page_size it is sent.
const starrailBillingPage = 20

// starrailBillingWalk bounds how far a page number is walked through the
// cursors when no end_id is given.
const starrailBillingWalk = 50

var billingPaths = map[string]map[string]string{"genshin": {"crystal": "GetCrystalLog", "primogem": "GetPrimogemLog"}, "starrail": {"dreams": "Dreams", "stellar": "Stellar", "power": "Power", "relic": "Relic", "cone": "Cone"}}

func allowedBillingEndpoint(host, path, profile string) bool {
	if profile != "billing" {
		return false
	}
	if host == ledgerHost {
		return slices.Contains([]string{"/common/hk4e_self_help_query/User/GetUserInfo", "/common/hk4e_self_help_query/User/GetCrystalLog", "/common/hk4e_self_help_query/User/GetPrimogemLog"}, path)
	}
	if host == webHost {
		for _, v := range billingPaths["starrail"] {
			if path == "/common/hkrpg_self_help_inquiry/"+v+"/GetList" {
				return true
			}
		}
	}
	return false
}
func (c *Client) billing(ctx context.Context, tokens accounts.Tokens, role accounts.Role, input map[string]any) (map[string]any, error) {
	name := role.Game + ".billing"
	os := tokens.Realm == "os"
	if !os && !slices.Contains(gachaProfiles[role.Game].regions, role.Region) || os && (!overseasRole(role) || role.Game != "genshin") {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	for key := range input {
		if !slices.Contains([]string{"category", "direction", "end_id", "page", "page_size"}, key) {
			return nil, invalidInput(name)
		}
	}
	category := text(input["category"])
	path := billingPaths[role.Game][category]
	direction := text(input["direction"])
	if direction == "" {
		direction = "all"
	}
	if path == "" || !slices.Contains([]string{"all", "produce", "consume"}, direction) {
		return nil, invalidInput(name)
	}
	end := text(input["end_id"])
	if end == "" {
		end = "0"
	}
	page := 1
	if raw, ok := input["page"]; ok {
		n, err := strconv.Atoi(stringNumber(raw))
		if err != nil || n < 1 || n > 10000 {
			return nil, invalidInput(name)
		}
		page = n
	}
	if !recordDigits.MatchString(end) || role.Game == "starrail" && end == "0" && page > starrailBillingWalk {
		return nil, invalidInput(name)
	}
	// 星铁的开拓力记录很多，StarRail-plugin 的在线时长按每页 50 条读取。
	pageSize := 10
	if raw, ok := input["page_size"]; ok {
		n, err := strconv.Atoi(stringNumber(raw))
		if err != nil || n < 10 || n > 50 || role.Game != "starrail" {
			return nil, invalidInput(name)
		}
		pageSize = n
	}
	key, err := c.businessAuthKey(ctx, tokens, role, "csc", "billing.authorize", false)
	if err != nil {
		var upstream *Error
		if errors.As(err, &upstream) && (upstream.Code == "plugin.upstream_auth_invalid" || upstream.Code == "plugin.upstream_rejected") {
			return nil, &Error{Code: "plugin.upstream_billing_unavailable", Operation: name, RetCode: upstream.RetCode}
		}
		return nil, err
	}
	q := url.Values{"selfquery_type": {"1"}, "sign_type": {"2"}, "auth_appid": {"csc"}, "authkey_ver": {"1"}, "game_biz": {gachaProfiles[role.Game].biz}, "win_direction": {"portrait"}, "bbs_auth_required": {"true"}, "bbs_game_role_required": {gachaProfiles[role.Game].biz}, "app_client": {"bbs"}, "lang": {"zh-cn"}, "csc_authkey_required": {"true"}, "authkey": {key}}
	if os {
		q.Set("game_biz", overseasBiz(role.Game))
		q.Set("bbs_game_role_required", overseasBiz(role.Game))
	}
	host := webHost
	if role.Game == "genshin" {
		pageSize = 20
		host = ledgerHost
		if os {
			host = "https://hk4e-api-os.hoyoverse.com"
		}
		q.Set("page_id", "1")
		identity, err := c.request(ctx, name, host, "/common/hk4e_self_help_query/User/GetUserInfo", "billing", q, nil, accounts.Tokens{})
		if err != nil {
			return nil, err
		}
		if text(identity["uid"]) != role.UID {
			return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: name, Reason: "billing_uid_mismatch"}
		}
		path = "/common/hk4e_self_help_query/User/" + path
		q.Set("size", "20")
		q.Set("end_id", end)
		if direction != "all" {
			q.Set("add_type", direction)
		}
	} else {
		path = "/common/hkrpg_self_help_inquiry/" + path + "/GetList"
		q.Set("page_id", "4")
		q.Set("page", strconv.Itoa(page))
		q.Set("page_size", strconv.Itoa(pageSize))
		q.Set("end_id", end)
	}
	data, err := c.request(ctx, name, host, path, "billing", q, nil, accounts.Tokens{})
	if err != nil {
		return nil, err
	}
	list, ok := data["list"].([]any)
	// A page number without a cursor walks the cursors from the first page.
	for walked := 1; role.Game == "starrail" && end == "0" && walked < page && ok && len(list) >= starrailBillingPage; walked++ {
		cursor := text(object(list[len(list)-1])["id"])
		if !recordDigits.MatchString(cursor) || cursor == q.Get("end_id") {
			return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: name, Reason: "billing_cursor_invalid"}
		}
		q.Set("end_id", cursor)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		if data, err = c.request(ctx, name, host, path, "billing", q, nil, accounts.Tokens{}); err != nil {
			return nil, err
		}
		list, ok = data["list"].([]any)
		if walked+1 < page && len(list) < starrailBillingPage {
			list = []any{}
		}
	}
	// A page may hold more records than asked for: the Star Rail list answers
	// its own page length whatever page_size says.
	if !ok || len(list) > max(pageSize, starrailBillingPage) {
		return nil, invalid(name)
	}
	out := []any{}
	for _, raw := range list {
		item := object(raw)
		clean := map[string]any{}
		for _, field := range []string{"id", "datetime", "time", "action", "add_num", "sub_num", "change_num", "item_name", "item_id", "reason"} {
			if value, exists := item[field]; exists {
				clean[field] = value
			}
		}
		// Relic records name the relic in fields of their own.
		if _, exists := clean["item_name"]; !exists && text(item["relic_name"]) != "" {
			clean["item_name"] = item["relic_name"]
		}
		if v, exists := clean["id"]; exists {
			clean["id"] = text(v)
		}
		if role.Game == "starrail" && direction != "all" {
			amount, err := strconv.ParseFloat(text(item["add_num"]), 64)
			if err == nil && (direction == "produce" && amount < 0 || direction == "consume" && amount >= 0) {
				continue
			}
		}
		out = append(out, clean)
	}
	next := ""
	// A full page may have another after it. Star Rail pages are full at the
	// list's own length, or at a smaller page_size should it be honoured again.
	full := pageSize
	if role.Game == "starrail" {
		full = min(pageSize, starrailBillingPage)
	}
	hasMore := len(list) > 0 && len(list) >= full
	if hasMore {
		next = text(object(list[len(list)-1])["id"])
		if !recordDigits.MatchString(next) || next == q.Get("end_id") {
			return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: name, Reason: "billing_cursor_invalid"}
		}
	}
	result := map[string]any{"items": out, "has_more": hasMore, "next_end_id": next, "next_page": strconv.Itoa(page + 1), "category": category, "direction": direction, "coverage": "official_available_history"}
	if !safeBusinessValue(result, tokens, 0) || !safeBusinessValue(result, accounts.Tokens{SToken: key}, 0) {
		return nil, invalid(name)
	}
	return result, nil
}

var redeemProfiles = map[string][2]string{"genshin": {"https://sg-hk4e-api.hoyolab.com", "hk4e_global"}, "starrail": {"https://sg-hkrpg-api.hoyolab.com", "hkrpg_global"}, "zzz": {"https://public-operation-nap.hoyolab.com", "nap_global"}}

func allowedRedeemEndpoint(host, path, profile string) bool {
	if profile != "redeem" || path != "/common/apicdkey/api/webExchangeCdkeyHyl" {
		return false
	}
	for _, p := range redeemProfiles {
		if p[0] == host {
			return true
		}
	}
	return false
}
func (c *Client) redeem(ctx context.Context, tokens accounts.Tokens, role accounts.Role, input map[string]any) (map[string]any, error) {
	name := role.Game + ".redeem"
	if len(input) != 1 {
		return nil, invalidInput(name)
	}
	code := strings.TrimSpace(text(input["code"]))
	if len(code) < 4 || len(code) > 64 || strings.Trim(code, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") != "" {
		return nil, invalidInput(name)
	}
	if slices.Contains(gachaProfiles[role.Game].regions, role.Region) {
		return nil, &Error{Code: "plugin.account_redeem_unavailable", Operation: name}
	}
	allowed := map[string][]string{"genshin": {"os_usa", "os_euro", "os_asia", "os_cht"}, "starrail": {"prod_official_usa", "prod_official_eur", "prod_official_asia", "prod_official_cht"}, "zzz": {"prod_gf_us", "prod_gf_eu", "prod_gf_jp", "prod_gf_sg"}}
	if !slices.Contains(allowed[role.Game], role.Region) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	p := redeemProfiles[role.Game]
	q := url.Values{"cdkey": {code}, "game_biz": {p[1]}, "lang": {"zh-cn"}, "region": {role.Region}, "uid": {role.UID}, "t": {strconv.FormatInt(c.now().UnixMilli(), 10)}}
	data, err := c.request(ctx, name, p[0], "/common/apicdkey/api/webExchangeCdkeyHyl", "redeem", q, nil, tokens)
	if err != nil {
		return nil, err
	}
	outcome := "redeemed"
	if data["already_redeemed"] == true {
		outcome = "already_redeemed"
	}
	return map[string]any{"outcome": outcome, "code": code}, nil
}
