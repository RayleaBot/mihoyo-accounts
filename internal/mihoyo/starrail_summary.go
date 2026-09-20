package mihoyo

import (
	"context"
	"net/url"
	"slices"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func (c *Client) starRailGachaSummary(ctx context.Context, tokens accounts.Tokens, role accounts.Role, input map[string]any) (map[string]any, error) {
	const name = "starrail.gacha_summary"
	if role.Game != "starrail" || !slices.Contains([]string{"prod_gf_cn", "prod_qd_cn"}, role.Region) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	for key := range input {
		if !slices.Contains([]string{"gacha_type", "version_id", "max_id"}, key) {
			return nil, invalidInput(name)
		}
	}
	pool := "11"
	if raw, exists := input["gacha_type"]; exists {
		pool = stringNumber(raw)
	}
	if !slices.Contains(gachaProfiles["starrail"].pools, pool) {
		return nil, invalidInput(name)
	}
	query := url.Values{"badge_uid": {role.UID}, "badge_region": {role.Region}, "uid": {role.UID}, "region": {role.Region}, "game_biz": {"hkrpg_cn"}, "gacha_type": {pool}}
	version, maxID := text(input["version_id"]), text(input["max_id"])
	if version != "" || maxID != "" {
		if !recordDigits.MatchString(version) || !recordDigits.MatchString(maxID) {
			return nil, invalidInput(name)
		}
		query.Set("version_id", version)
		query.Set("max_id", maxID)
	}
	cookies := map[string]string{}
	_, err := c.requestWithCookies(ctx, "starrail.gacha_login", webHost, "/common/badge/v1/login/account", "badge", nil, map[string]any{"uid": role.UID, "region": role.Region, "game_biz": "hkrpg_cn", "lang": "zh-cn"}, tokens, cookies)
	if err != nil {
		return nil, err
	}
	badge := cookies["e_hkrpg_token"]
	if badge == "" {
		return nil, &Error{Code: "plugin.upstream_response_invalid", Operation: name, Reason: "activity_cookie_missing"}
	}
	poolData, err := c.requestWithSession(ctx, name, activityHost, "/event/rpg_gacha_record/pool_stat", "badge", query, nil, tokens, cookies, cookies)
	if err != nil {
		return nil, err
	}
	stars, err := c.requestWithSession(ctx, name, activityHost, "/event/rpg_gacha_record/five_star_list", "badge", query, nil, tokens, cookies, cookies)
	if err != nil {
		return nil, err
	}
	selectFields := func(data map[string]any, fields string) map[string]any {
		out := map[string]any{}
		for _, key := range words(fields) {
			if value, exists := data[key]; exists {
				out[key] = value
			}
		}
		return out
	}
	result := map[string]any{"coverage": "five_star_and_pool_summary", "gacha_type": pool, "pool": selectFields(poolData, "cards total_count up_count"), "five_star": selectFields(stars, "list has_more next_max_id version_id")}
	if len(object(result["pool"])) == 0 || len(object(result["five_star"])) == 0 || !safeBusinessValue(result, tokens, 0) || !safeBusinessValue(result, accounts.Tokens{SToken: badge}, 0) {
		return nil, invalid(name)
	}
	for _, value := range cookies {
		if !safeBusinessValue(result, accounts.Tokens{SToken: value}, 0) {
			return nil, invalid(name)
		}
	}
	return result, nil
}
