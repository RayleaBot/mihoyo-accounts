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
}

// Only these business operations can send authenticated requests. Neither the
// caller nor saved plugin configuration can choose an endpoint or headers.
var operations = map[string]operation{
	"genshin.gacha":          {game: "genshin"},
	"starrail.gacha":         {game: "starrail"},
	"zzz.gacha":              {game: "zzz"},
	"genshin.profile":        {game: "genshin", path: "/game_record/app/genshin/api/index"},
	"genshin.note":           {game: "genshin", path: "/game_record/app/genshin/api/dailyNote"},
	"genshin.characters":     {game: "genshin", path: "/game_record/app/genshin/api/character/list", post: true},
	"genshin.character":      {game: "genshin", path: "/game_record/app/genshin/api/character/detail", post: true, inputs: words("character_ids")},
	"genshin.abyss":          {game: "genshin", path: "/game_record/app/genshin/api/spiralAbyss", inputs: words("schedule_type")},
	"genshin.theater":        {game: "genshin", path: "/game_record/app/genshin/api/role_combat", inputs: words("need_detail")},
	"genshin.hard_challenge": {game: "genshin", path: "/game_record/app/genshin/api/hard_challenge", inputs: words("need_detail")},
	// The characters this period's 赋光之人 buff favours, which miao marks
	// on the 幽境危战 image.
	"genshin.hard_challenge_popularity": {game: "genshin", path: "/game_record/app/genshin/api/hard_challenge/popularity"},
	"starrail.profile":                  {game: "starrail", path: "/game_record/app/hkrpg/api/index"},
	"starrail.note":                     {game: "starrail", path: "/game_record/app/hkrpg/api/note"},
	"starrail.characters":               {game: "starrail", path: "/game_record/app/hkrpg/api/avatar/basic"},
	"starrail.character":                {game: "starrail", path: "/game_record/app/hkrpg/api/avatar/info", inputs: words("need_wiki character_ids")},
	"starrail.challenge":                {game: "starrail", path: "/game_record/app/hkrpg/api/challenge", inputs: words("schedule_type need_all")},
	"starrail.story":                    {game: "starrail", path: "/game_record/app/hkrpg/api/challenge_story", inputs: words("schedule_type need_all")},
	"starrail.boss":                     {game: "starrail", path: "/game_record/app/hkrpg/api/challenge_boss", inputs: words("schedule_type need_all")},
	"zzz.profile":                       {game: "zzz", path: "/event/game_record_zzz/api/zzz/index"},
	"zzz.note":                          {game: "zzz", path: "/event/game_record_zzz/api/zzz/note"},
	"zzz.characters":                    {game: "zzz", path: "/event/game_record_zzz/api/zzz/avatar/basic"},
	"zzz.character":                     {game: "zzz", path: "/event/game_record_zzz/api/zzz/avatar/info", inputs: words("id_list")},
	"zzz.buddies":                       {game: "zzz", path: "/event/game_record_zzz/api/zzz/buddy/info"},
	"zzz.challenge":                     {game: "zzz", path: "/event/game_record_zzz/api/zzz/hadal_info_v2", inputs: words("schedule_type")},
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
			if name == "starrail.peak" {
				maximum = 3
			}
			if raw, ok := input[key]; ok {
				parsed, err := strconv.Atoi(stringNumber(raw))
				if err != nil || parsed < 1 || parsed > maximum {
					return nil, invalidInput(name)
				}
				value = parsed
			}
			// Anomaly Arbitration answers only the current run (1) or the three
			// most recent (3); the last run is the second of those.
			if name == "starrail.peak" && value == 2 {
				value = 3
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
	// The game plugin picks fields and trims periods; only credentials are
	// removed here.
	clean, ok := sanitizeBusinessValue(data, tokens, 0)
	result, isObject := clean.(map[string]any)
	if !ok || !isObject || len(result) == 0 {
		return nil, invalid(name)
	}
	return result, nil
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
// credentialKey reports field names that carry credentials in any form.
func credentialKey(key string) bool {
	lower := strings.ToLower(key)
	for _, forbidden := range []string{"token", "cookie", "password", "authkey", "authorization", "ticket", "set-cookie"} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return false
}

// sanitizeBusinessValue copies an official response without fields named
// like credentials. A string that contains an account credential or a
// credential fragment rejects the whole value.
func sanitizeBusinessValue(value any, tokens accounts.Tokens, depth int) (any, bool) {
	if depth > 32 {
		return nil, false
	}
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if credentialKey(key) {
				continue
			}
			clean, ok := sanitizeBusinessValue(item, tokens, depth+1)
			if !ok {
				return nil, false
			}
			out[key] = clean
		}
		return out, true
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			clean, ok := sanitizeBusinessValue(item, tokens, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, clean)
		}
		return out, true
	case string:
		return v, safeBusinessValue(v, tokens, depth)
	}
	return value, true
}

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
