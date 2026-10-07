package mihoyo

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type growthLevel struct {
	ID      int `json:"id"`
	Current int `json:"current_level"`
	Target  int `json:"target_level"`
}
type growthInput struct {
	CharacterID   int           `json:"character_id"`
	Current       int           `json:"current_level"`
	Target        int           `json:"target_level"`
	WeaponID      int           `json:"weapon_id"`
	WeaponCurrent int           `json:"current_weapon_level"`
	WeaponTarget  int           `json:"target_weapon_level"`
	Skills        []growthLevel `json:"skills"`
}

func (c *Client) growth(ctx context.Context, tokens accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	allowed := words("character_id")
	detail := strings.HasSuffix(name, ".growth_detail")
	if !detail {
		allowed = words("character_id current_level target_level weapon_id current_weapon_level target_weapon_level skills")
	}
	for key := range input {
		if !slices.Contains(allowed, key) {
			return nil, invalidInput(name)
		}
	}
	// IDs arriving through SDK JSON may be decimal strings. Normalize only the
	// documented ID fields; levels remain strict integers.
	normalized := map[string]any{}
	for k, v := range input {
		normalized[k] = v
	}
	for _, key := range []string{"character_id", "weapon_id"} {
		if value, ok := normalized[key]; ok {
			n, err := strconv.Atoi(stringNumber(value))
			if err != nil {
				return nil, invalidInput(name)
			}
			normalized[key] = n
		}
	}
	raw, err := json.Marshal(normalized)
	var in growthInput
	if err != nil || json.Unmarshal(raw, &in) != nil || in.CharacterID <= 0 || in.CharacterID > 1000000000 {
		return nil, invalidInput(name)
	}
	query := url.Values{"uid": {role.UID}, "region": {role.Region}}
	var body any
	path := ""
	fields := []string{}
	if detail {
		if role.Game == "genshin" {
			path = "/event/e20200928calculate/v1/sync/avatar/detail"
			query.Set("avatar_id", strconv.Itoa(in.CharacterID))
			fields = words("avatar avatar_id name level skill_list skills weapon")
		} else {
			path = "/event/rpgcalc/avatar/detail"
			query.Set("game", "hkrpg")
			query.Set("lang", "zh-cn")
			query.Set("item_id", strconv.Itoa(in.CharacterID))
			query.Set("tab_from", "TabOwned")
			query.Set("change_target_level", "0")
			fields = words("avatar skills equipment skill_list")
		}
	} else {
		limit, skillLimit := 90, 10
		if role.Game == "starrail" {
			limit, skillLimit = 80, 15
		}
		valid := func(id, cur, target, max int) bool {
			return id > 0 && id <= 1000000000 && cur >= 1 && target >= cur && target <= max
		}
		if !valid(in.CharacterID, in.Current, in.Target, limit) || len(in.Skills) > 32 || in.WeaponID == 0 && (in.WeaponCurrent != 0 || in.WeaponTarget != 0) || in.WeaponID != 0 && !valid(in.WeaponID, in.WeaponCurrent, in.WeaponTarget, limit) {
			return nil, invalidInput(name)
		}
		seen := map[int]bool{}
		for _, skill := range in.Skills {
			if skill.ID <= 0 || skill.ID > 1000000000 || skill.Current < 0 || role.Game == "genshin" && skill.Current == 0 || skill.Target < skill.Current || skill.Target > skillLimit || seen[skill.ID] {
				return nil, invalidInput(name)
			}
			seen[skill.ID] = true
		}
		if role.Game == "genshin" {
			skills := []any{}
			for _, s := range in.Skills {
				skills = append(skills, map[string]any{"id": s.ID, "level_current": s.Current, "level_target": s.Target})
			}
			item := map[string]any{"avatar_id": in.CharacterID, "avatar_level_current": in.Current, "avatar_level_target": in.Target, "skill_list": skills}
			if in.WeaponID != 0 {
				item["weapon"] = map[string]any{"id": in.WeaponID, "level_current": in.WeaponCurrent, "level_target": in.WeaponTarget}
			}
			body = map[string]any{"items": []any{item}}
			path = "/event/e20200928calculate/v3/batch_compute"
			query = nil
			fields = words("overall_material_consume items")
		} else {
			skills := []any{}
			for _, s := range in.Skills {
				skills = append(skills, map[string]any{"item_id": s.ID, "cur_level": s.Current, "target_level": s.Target})
			}
			value := map[string]any{"game": "hkrpg", "avatar": map[string]any{"item_id": in.CharacterID, "cur_level": in.Current, "target_level": in.Target}, "skill_list": skills}
			if in.WeaponID != 0 {
				value["equipment"] = map[string]any{"item_id": in.WeaponID, "cur_level": in.WeaponCurrent, "target_level": in.WeaponTarget}
			}
			body = value
			path = "/event/rpgcalc/compute"
			query = url.Values{"game": {"hkrpg"}}
			fields = words("avatar_consume equipment_consume skill_consume avatar equipment skills total_consume coin_id")
		}
	}
	host, profile := webHost, "record"
	if tokens.Realm == "os" {
		host, profile = "https://sg-public-api.hoyolab.com", "record.os"
		if query != nil {
			query.Set("lang", "zh-cn")
		}
		if data, ok := body.(map[string]any); ok {
			data["lang"] = "zh-cn"
		}
	}
	data, err := c.request(ctx, name, host, path, profile, query, body, tokens)
	if err != nil {
		return nil, err
	}
	selected := map[string]any{}
	for _, field := range fields {
		if value, ok := data[field]; ok {
			selected[field] = value
		}
	}
	if len(selected) == 0 || !safeBusinessValue(selected, tokens, 0) {
		return nil, invalid(name)
	}
	return selected, nil
}
