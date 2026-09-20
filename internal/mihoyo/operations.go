package mihoyo

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

type operation struct {
	accountScoped bool
	mutates       bool
	game, path    string
	host          string
	identity      string
	defaults      map[string]string
	post          bool
	inputs        []string
	fields        []string
}

// Only these business operations can send authenticated requests. Neither the
// caller nor saved plugin configuration can choose an endpoint or headers.
var operations = map[string]operation{
	"genshin.gacha":          {game: "genshin"},
	"starrail.gacha":         {game: "starrail"},
	"zzz.gacha":              {game: "zzz"},
	"genshin.profile":        {game: "genshin", path: "/game_record/app/genshin/api/index", fields: words("role stats avatars world_explorations homes city_explorations")},
	"genshin.note":           {game: "genshin", path: "/game_record/app/genshin/api/dailyNote", fields: words("current_resin max_resin resin_recovery_time finished_task_num total_task_num is_extra_task_reward_received remain_resin_discount_num resin_discount_num_limit current_expedition_num max_expedition_num expeditions current_home_coin max_home_coin home_coin_recovery_time calendar_url transformer daily_task attendance_reward_status")},
	"genshin.characters":     {game: "genshin", path: "/game_record/app/genshin/api/character/list", post: true, fields: words("list avatars")},
	"genshin.character":      {game: "genshin", path: "/game_record/app/genshin/api/character/detail", post: true, inputs: words("character_ids"), fields: words("list avatars property_map")},
	"genshin.abyss":          {game: "genshin", path: "/game_record/app/genshin/api/spiralAbyss", inputs: words("schedule_type"), fields: words("schedule_id start_time end_time total_battle_times total_win_times max_floor total_star is_unlock reveal_rank defeat_rank damage_rank take_damage_rank normal_skill_rank energy_skill_rank floors")},
	"genshin.theater":        {game: "genshin", path: "/game_record/app/genshin/api/role_combat", inputs: words("need_detail"), fields: words("data is_unlock tarot_card_state talent_state statistic")},
	"genshin.hard_challenge": {game: "genshin", path: "/game_record/app/genshin/api/hard_challenge", inputs: words("need_detail"), fields: words("data is_unlock statistic")},
	"starrail.profile":       {game: "starrail", path: "/game_record/app/hkrpg/api/index", fields: words("stats avatar_list cur_head_icon_url phone_background_image_url achievement_list")},
	"starrail.note":          {game: "starrail", path: "/game_record/app/hkrpg/api/note", fields: words("current_stamina max_stamina stamina_recover_time accepted_epedition_num total_expedition_num expeditions current_train_score max_train_score current_rogue_score max_rogue_score weekly_cocoon_cnt weekly_cocoon_limit current_reserve_stamina is_reserve_stamina_full rogue_tourn_weekly_unlocked rogue_tourn_weekly_cur rogue_tourn_weekly_max")},
	"starrail.characters":    {game: "starrail", path: "/game_record/app/hkrpg/api/avatar/basic", fields: words("avatar_list")},
	"starrail.character":     {game: "starrail", path: "/game_record/app/hkrpg/api/avatar/info", inputs: words("need_wiki character_ids"), fields: words("avatar_list equip_wiki relic_wiki avatar_wiki property_info")},
	"starrail.challenge":     {game: "starrail", path: "/game_record/app/hkrpg/api/challenge", inputs: words("schedule_type need_all"), fields: words("schedule_id begin_time end_time star_num max_floor max_floor_detail battle_num has_data all_floor_detail groups")},
	"starrail.story":         {game: "starrail", path: "/game_record/app/hkrpg/api/challenge_story", inputs: words("schedule_type need_all"), fields: words("schedule_id begin_time end_time star_num max_floor max_floor_detail battle_num has_data all_floor_detail groups")},
	"starrail.boss":          {game: "starrail", path: "/game_record/app/hkrpg/api/challenge_boss", inputs: words("schedule_type need_all"), fields: words("schedule_id begin_time end_time star_num max_floor max_floor_detail battle_num has_data all_floor_detail groups")},
	"zzz.profile":            {game: "zzz", path: "/event/game_record_zzz/api/zzz/index", fields: words("stats avatar_list avatars buddy_list cur_head_icon_url zone_state")},
	"zzz.note":               {game: "zzz", path: "/event/game_record_zzz/api/zzz/note", fields: words("energy vitality vhs_sale vhs_sale_state card_sign bounty_commission bounties weekly_task abyss abyss_refresh refresh_time current_energy max_energy energy_recover_time")},
	"zzz.characters":         {game: "zzz", path: "/event/game_record_zzz/api/zzz/avatar/basic", fields: words("avatar_list")},
	"zzz.character":          {game: "zzz", path: "/event/game_record_zzz/api/zzz/avatar/info", inputs: words("id_list"), fields: words("avatar_list weapon_wiki equip_wiki")},
	"zzz.buddies":            {game: "zzz", path: "/event/game_record_zzz/api/zzz/buddy/info", fields: words("list buddy_list")},
	"zzz.challenge":          {game: "zzz", path: "/event/game_record_zzz/api/zzz/hadal_info_v2", inputs: words("schedule_type"), fields: words("hadal_ver hadal_info_v2 nick_name icon schedule_id begin_time end_time rating_list all_floor_detail has_data max_layer highest_floor star_num")},
}

func words(s string) []string { return strings.Fields(s) }

func OperationGame(name string) string        { return operations[name].game }
func OperationMutates(name string) bool       { return operations[name].mutates }
func OperationAccountScoped(name string) bool { return operations[name].accountScoped }

func OperationNames() []string {
	result := make([]string, 0, len(operations))
	for name := range operations {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

func (c *Client) Execute(ctx context.Context, tokens accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	spec, exists := operations[name]
	if !exists || spec.game != role.Game || !spec.accountScoped && (role.UID == "" || role.Region == "") {
		return nil, &Error{Code: "plugin.account_operation_denied", Operation: name}
	}
	if tokens.Realm == "os" && (spec.accountScoped || !overseasRole(role)) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	if tokens.Realm != "os" && overseasRole(role) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	if strings.HasPrefix(name, "genshin.birthday_") {
		return c.birthday(ctx, tokens, role, name, input)
	}
	if strings.Contains(name, ".community_") {
		return c.community(ctx, tokens, role.Game, name, input)
	}
	if strings.Contains(name, ".cloud_") {
		return c.cloudGame(ctx, tokens, role.Game, name, input)
	}
	if strings.HasSuffix(name, ".billing") {
		return c.billing(ctx, tokens, role, input)
	}
	if name == "genshin.blueprint_read" || name == "genshin.blueprint_compute" {
		return c.blueprint(ctx, tokens, role, name, input)
	}
	if strings.HasSuffix(name, ".redeem") {
		return c.redeem(ctx, tokens, role, input)
	}
	if strings.HasSuffix(name, ".gacha") {
		return c.gachaPage(ctx, tokens, role, input)
	}
	if name == "starrail.gacha_summary" {
		return c.starRailGachaSummary(ctx, tokens, role, input)
	}
	if strings.HasSuffix(name, ".sign") || strings.HasSuffix(name, ".sign_info") || strings.HasSuffix(name, ".sign_rewards") {
		return c.gameSign(ctx, tokens, role, name, input)
	}
	if strings.HasSuffix(name, ".growth_detail") || strings.HasSuffix(name, ".growth_compute") {
		return c.growth(ctx, tokens, role, name, input)
	}
	query := url.Values{"role_id": {role.UID}, "server": {role.Region}}
	if spec.identity == "uid" {
		query = url.Values{"uid": {role.UID}, "region": {role.Region}}
	}
	if spec.identity == "bind" {
		query = url.Values{"bind_uid": {role.UID}, "bind_region": {role.Region}}
	}
	for key, value := range spec.defaults {
		query.Set(key, value)
	}
	if role.Game == "zzz" {
		query.Set("lang", "zh-cn")
		if name == "zzz.character" {
			query.Set("need_wiki", "false")
		}
	}
	body := map[string]any{"role_id": role.UID, "server": role.Region}
	for key := range input {
		if !slices.Contains(spec.inputs, key) {
			return nil, &Error{Code: "plugin.upstream_request_invalid", Operation: name}
		}
	}
	for _, key := range spec.inputs {
		switch key {
		case "schedule_type":
			value := 1
			maximum := 2
			if name == "starrail.rogue" {
				value = 3
				maximum = 3
			}
			if raw, ok := input[key]; ok {
				parsed, err := strconv.Atoi(stringNumber(raw))
				if err != nil || parsed < 1 || parsed > maximum {
					return nil, invalidInput(name)
				}
				value = parsed
			}
			query.Set(key, strconv.Itoa(value))
		case "month":
			if raw, exists := input[key]; exists && stringNumber(raw) != "" {
				month, err := strconv.Atoi(stringNumber(raw))
				if err != nil {
					return nil, invalidInput(name)
				}
				if role.Game == "genshin" {
					if month < 1 || month > 12 {
						return nil, invalidInput(name)
					}
				} else if month < 202001 || month > 210012 || month%100 < 1 || month%100 > 12 {
					return nil, invalidInput(name)
				}
				query.Set(key, strconv.Itoa(month))
			}
		case "need_detail", "need_all", "need_wiki":
			query.Set(key, "true")
		case "character_ids", "id_list":
			if name == "starrail.character" && input[key] == nil {
				continue
			}
			list, ok := input[key].([]any)
			if !ok || len(list) == 0 || len(list) > 50 {
				return nil, invalidInput(name)
			}
			ids := make([]int, 0, len(list))
			for _, raw := range list {
				id, err := strconv.Atoi(stringNumber(raw))
				if err != nil || id < 1 || id > 1000000000 {
					return nil, invalidInput(name)
				}
				ids = append(ids, id)
			}
			if name == "starrail.character" {
				continue
			}
			if spec.post {
				body[key] = ids
			} else {
				for _, id := range ids {
					query.Add(key+"[]", strconv.Itoa(id))
				}
			}
		}
	}
	var post any
	if spec.post {
		post = body
		query = nil
	}
	profile := "record"
	if role.Game == "zzz" {
		profile = "record.zzz"
	}
	host := spec.host
	if host == "" {
		host = recordHost
	}
	if tokens.Realm == "os" {
		var ok bool
		host, spec.path, ok = overseasRecord(spec)
		if !ok {
			return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
		}
		profile = "record.os"
		if query != nil {
			query.Set("lang", "zh-cn")
		}
	}
	data, err := c.request(ctx, name, host, spec.path, profile, query, post, tokens)
	if err != nil {
		return nil, err
	}
	if name == "starrail.rogue" {
		if query.Get("schedule_type") == "1" {
			delete(data, "last_record")
		}
		if query.Get("schedule_type") == "2" {
			delete(data, "current_record")
		}
	}
	if name == "starrail.character" && input["character_ids"] != nil {
		wanted := map[string]bool{}
		for _, id := range input["character_ids"].([]any) {
			wanted[stringNumber(id)] = true
		}
		list := []any{}
		avatars, ok := data["avatar_list"].([]any)
		if !ok {
			return nil, invalid(name)
		}
		for _, raw := range avatars {
			if wanted[text(object(raw)["id"])] {
				list = append(list, raw)
			}
		}
		data["avatar_list"] = list
	}
	selected := map[string]any{}
	for _, key := range spec.fields {
		if value, ok := data[key]; ok {
			selected[key] = value
		}
	}
	if len(selected) == 0 {
		return nil, invalid(name)
	}
	if !safeBusinessValue(selected, tokens, 0) {
		return nil, invalid(name)
	}
	return selected, nil
}

func stringNumber(value any) string {
	switch value := value.(type) {
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	default:
		return text(value)
	}
}
func invalidInput(name string) error {
	return &Error{Code: "plugin.upstream_request_invalid", Operation: name}
}

// A top-level response whitelist is followed by recursive credential rejection.
// Unexpected upstream fields cannot quietly turn a business response into a
// credential export. Errors return no original field, value or response body.
func safeBusinessValue(value any, tokens accounts.Tokens, depth int) bool {
	if depth > 32 {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			lower := strings.ToLower(key)
			for _, forbidden := range []string{"token", "cookie", "password", "authkey", "authorization", "ticket", "set-cookie"} {
				if strings.Contains(lower, forbidden) {
					return false
				}
			}
			if !safeBusinessValue(item, tokens, depth+1) {
				return false
			}
		}
	case []any:
		for _, item := range v {
			if !safeBusinessValue(item, tokens, depth+1) {
				return false
			}
		}
	case string:
		for _, secret := range []string{tokens.SToken, tokens.LToken, tokens.CookieToken, tokens.MID, tokens.DeviceFP, tokens.CloudGenshin, tokens.CloudZZZ} {
			if len(secret) >= 8 && strings.Contains(v, secret) {
				return false
			}
		}
		lower := strings.ToLower(v)
		for _, part := range []string{"stoken=", "ltoken=", "cookie_token=", "authkey=", "authorization:", "set-cookie:"} {
			if strings.Contains(lower, part) {
				return false
			}
		}
	case nil, bool, json.Number, float64:
	default:
		return false
	}
	return true
}
