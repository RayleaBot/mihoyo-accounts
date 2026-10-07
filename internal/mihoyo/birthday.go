package mihoyo

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/url"
	"strings"
)

func init() {
	operations["genshin.birthday_list"] = operation{game: "genshin"}
	operations["genshin.birthday_claim"] = operation{game: "genshin", mutates: true}
}
func allowedBirthdayEndpoint(host, path, profile string) bool {
	return (profile == "birthday" || profile == "birthday.os") && ((host == ledgerHost && (path == "/event/birthdaystar/account/index" || path == "/event/birthdaystar/account/post_my_draw")) || (host == webHost || host == "https://api-os-takumi.mihoyo.com") && path == "/common/badge/v1/login/account")
}
func (c *Client) birthday(ctx context.Context, tokens accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	claim := name == "genshin.birthday_claim"
	id := text(input["role_id"])
	if !claim && len(input) != 0 || claim && (len(input) != 1 || !recordDigits.MatchString(id)) {
		return nil, invalidInput(name)
	}
	biz, loginHost, profile := "hk4e_cn", webHost, "birthday"
	if tokens.Realm == "os" {
		biz, loginHost, profile = "hk4e_global", "https://api-os-takumi.mihoyo.com", "birthday.os"
	}
	cookies := map[string]string{}
	_, err := c.requestWithCookies(ctx, "genshin.birthday_login", loginHost, "/common/badge/v1/login/account", profile, nil, map[string]any{"uid": role.UID, "region": role.Region, "game_biz": biz, "lang": "zh-cn"}, tokens, cookies)
	if err != nil {
		return nil, err
	}
	badge := cookies["e_hk4e_token"]
	if badge == "" {
		return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: name, Reason: "birthday_session_missing"}
	}
	q := url.Values{"lang": {"zh-cn"}, "badge_uid": {role.UID}, "badge_region": {role.Region}, "game_biz": {biz}, "activity_id": {"20220301153521"}}
	data, err := c.requestWithSession(ctx, name, ledgerHost, "/event/birthdaystar/account/index", profile, q, nil, tokens, nil, cookies)
	if err != nil {
		return nil, err
	}
	roles, ok := data["role"].([]any)
	if !ok || len(roles) > 32 {
		return nil, invalid(name)
	}
	items := []any{}
	found := false
	for _, raw := range roles {
		v := object(raw)
		roleID := text(v["role_id"])
		if !recordDigits.MatchString(roleID) {
			return nil, invalid(name)
		}
		if roleID == id {
			found = true
		}
		item := map[string]any{"role_id": roleID, "name": text(v["name"])}
		image := text(v["take_picture"])
		if image != "" {
			u, err := url.Parse(image)
			if err != nil || u.Scheme != "https" || u.User != nil || !(strings.HasSuffix(u.Hostname(), ".mihoyo.com") || strings.HasSuffix(u.Hostname(), ".hoyoverse.com")) {
				return nil, invalid(name)
			}
			item["image_url"] = image
		}
		items = append(items, item)
	}
	if claim {
		if !found {
			return nil, invalidInput(name)
		}
		_, err = c.requestWithSession(ctx, name, ledgerHost, "/event/birthdaystar/account/post_my_draw", profile, q, map[string]any{"role_id": json.Number(id)}, tokens, nil, cookies)
		if err != nil {
			return nil, err
		}
		return map[string]any{"claimed": true, "role_id": id}, nil
	}
	out := map[string]any{"items": items}
	if !safeBusinessValue(out, tokens, 0) || !safeBusinessValue(out, accounts.Tokens{SToken: badge}, 0) {
		return nil, invalid(name)
	}
	return out, nil
}
