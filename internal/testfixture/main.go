//go:build integration_fixture

// Synthetic provider for the four-process development smoke test. It is never
// part of the normal plugin entrypoint or release artifact.
package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/app"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

type api struct{}

func (api) CreateQR(context.Context) (mihoyo.QR, error) { return mihoyo.QR{}, nil }
func (api) PollQR(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
	return mihoyo.Confirmation{}, nil
}
func (api) Fingerprint(_ context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	tokens.DeviceFP = "syntheticfingerprint"
	tokens.DeviceFPExpiresAtMS = 4102444800000
	return tokens, nil
}
func (api) Refresh(_ context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	tokens.LToken = "synthetic-native-ltoken"
	tokens.CookieToken = "synthetic-native-cookie"
	return tokens, nil
}
func (api) RolesForGame(_ context.Context, tokens accounts.Tokens, game string) ([]accounts.Role, error) {
	if tokens.Realm == "os" {
		region := map[string]string{"genshin": "os_usa", "starrail": "prod_official_usa", "zzz": "prod_gf_us"}[game]
		return []accounts.Role{{Ref: game + ":overseas-role", Game: game, GameBiz: game + "_global", UID: "600000002", Region: region, Nickname: "Synthetic overseas", Level: 60}}, nil
	}
	return nil, nil
}
func (api) Execute(_ context.Context, _ accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	if strings.Contains(name, ".community_") {
		action := strings.TrimPrefix(name, role.Game+".community_")
		if action == "run" {
			action, _ = input["action"].(string)
		}
		if action == "status" {
			return map[string]any{"total_points": 100, "already_received_points": 60, "can_get_points": 0, "states": []any{}}, nil
		}
		if action == "posts" {
			return map[string]any{"posts": []any{map[string]any{"post_id": "123", "title": "Synthetic post"}}}, nil
		}
		return map[string]any{"accepted": true}, nil
	}
	if strings.HasSuffix(name, ".cloud_sign") {
		return map[string]any{"checked": true, "free_time": 120, "send_freetime": 15, "coin_num": 0}, nil
	}
	if strings.HasSuffix(name, ".monthly") {
		month := any(202609)
		if role.Game == "genshin" {
			month = 9
		}
		return map[string]any{"data_month": month, "month_data": map[string]any{"current_primogems": 1000, "current_hcoin": 1000, "list": []any{map[string]any{"data_type": "PolychromesData", "count": 1000}}}, "optional_month": []any{month}}, nil
	}
	if name == "genshin.abyss" {
		return map[string]any{"schedule_id": 12, "max_floor": "12-3", "floors": []any{map[string]any{"index": 12, "star": 7}}, "total_battle_times": 20}, nil
	}
	if name == "starrail.challenge" {
		return map[string]any{"schedule_id": 12, "max_floor": 12, "all_floor_detail": []any{map[string]any{"star_num": 2, "round_num": 4}}}, nil
	}
	if name == "zzz.deadly" {
		return map[string]any{"start_time": 1760000000, "total_star": 6, "total_score": 90000}, nil
	}
	if strings.HasSuffix(name, ".sign_info") {
		return map[string]any{"signed": false, "total_days": 2, "first_bind": false, "date": "2026-09-19"}, nil
	}
	if strings.HasSuffix(name, ".sign_rewards") {
		return map[string]any{"awards": []any{map[string]any{"name": "Synthetic reward", "count": 10}}}, nil
	}
	if strings.HasSuffix(name, ".sign") {
		return map[string]any{"signed": true, "outcome": "signed", "date": "2026-09-19"}, nil
	}
	if strings.HasSuffix(name, ".growth_detail") {
		if role.Game == "genshin" {
			return map[string]any{"avatar": map[string]any{"name": "Synthetic", "level_current": 50, "max_level": 90}, "skill_list": []any{map[string]any{"group_id": 100101, "name": "Skill", "level_current": 3, "max_level": 10}}}, nil
		}
		return map[string]any{"avatar": map[string]any{"item_name": "Synthetic", "cur_level": 50, "max_level": 80}, "skills": []any{map[string]any{"point_id": "100101", "cur_level": 3, "max_level": 10}}}, nil
	}
	if strings.HasSuffix(name, ".growth_compute") {
		if role.Game == "genshin" {
			return map[string]any{"overall_material_consume": map[string]any{"avatar_consume": []any{map[string]any{"consume": []any{map[string]any{"id": 1, "name": "Synthetic material", "num": 100}}}}}}, nil
		}
		return map[string]any{"avatar_consume": []any{map[string]any{"item_id": "1", "item_name": "Synthetic material", "num": 100}}}, nil
	}
	if strings.HasSuffix(name, ".gacha") {
		return map[string]any{"list": []any{}, "next_end_id": "0", "has_more": false, "timezone": 8, "lang": "zh-cn"}, nil
	}
	if strings.HasSuffix(name, ".character") {
		selector := "character_ids"
		if role.Game == "zzz" {
			selector = "id_list"
		}
		if ids, ok := input[selector].([]any); ok && len(ids) == 1 && ids[0] != "1001" {
			if path := os.Getenv("RAYLEA_SYNTHETIC_PANELS"); path != "" {
				raw, err := os.ReadFile(path)
				if err != nil {
					return nil, err
				}
				var fixtures map[string]map[string]any
				if err = json.Unmarshal(raw, &fixtures); err != nil {
					return nil, err
				}
				if panel := fixtures[role.Game]; panel != nil {
					return panel, nil
				}
			}
		}
		atkID, critID, bonusID := "2001", "20", "40"
		if role.Game == "starrail" {
			atkID, critID, bonusID = "2", "5", "14"
		}
		if role.Game == "zzz" {
			atkID, critID, bonusID = "2", "5", "31603"
		}
		stats := []any{}
		for id, value := range map[string]string{atkID: "2500", critID: "50%", bonusID: "46.6%"} {
			stats = append(stats, map[string]any{"property_type": id, "property_id": id, "final": value})
		}
		return map[string]any{"list": []any{map[string]any{"id": "1001", "name": "Synthetic", "level": 60, "element": "fire", "element_type": 201, "properties": stats}}}, nil
	}
	switch role.Game {
	case "genshin":
		return map[string]any{"current_resin": 80, "max_resin": 200, "resin_recovery_time": "9600"}, nil
	case "starrail":
		return map[string]any{"current_stamina": 120, "max_stamina": 300, "stamina_recover_time": "10800"}, nil
	default:
		return map[string]any{"energy": map[string]any{"progress": map[string]any{"current": 100, "max": 240}, "restore": 8400}}, nil
	}
}

type store struct{ event *rayleabot.EventContext }

func (s store) Read(ctx context.Context, key string) (string, bool, error) {
	result, err := s.event.Actions().SecretRead(ctx, key)
	value, _ := result["value"].(string)
	exists, _ := result["exists"].(bool)
	return value, exists, err
}
func (s store) Write(ctx context.Context, values map[string]string) error {
	_, err := s.event.Actions().SecretWrite(ctx, values)
	return err
}
func main() {
	a, err := app.New(os.Getenv("RAYLEABOT_PLUGIN_DATA_DIR"), api{})
	if err != nil {
		os.Exit(1)
	}
	defer a.Close()
	if err := a.Vault.Initialize("synthetic-native-test-password", false); err != nil {
		os.Exit(1)
	}
	handler := rayleabot.HandlerFunc(func(ctx context.Context, event *rayleabot.EventContext) error {
		if event.Event.Payload["action"] != "fixture.bind" {
			return a.Handle(ctx, event)
		}
		roles := []accounts.Role{}
		for _, game := range []string{"genshin", "starrail", "zzz"} {
			region := "prod_gf_cn"
			if game == "genshin" {
				region = "cn_gf01"
			}
			roles = append(roles, accounts.Role{Ref: game + ":native-role", Game: game, UID: "100000001", Region: region, Nickname: "Synthetic", Level: 60})
		}
		cipher, _ := a.Vault.Cipher()
		record, err := a.Repo.Bind(ctx, store{event}, cipher, accounts.Subject{SourceProtocol: "onebot11", SourceAdapter: "fixture", BotID: "fixture-bot", ActorID: "fixture-user"}, accounts.Tokens{AccountID: "123456789", SToken: "synthetic-native-stoken", LToken: "synthetic-native-ltoken", CookieToken: "synthetic-native-cookie", MID: "synthetic-native-mid", DeviceID: "synthetic-native-device"}, roles, []string{"raylea.genshin", "raylea.starrail", "raylea.zzz"})
		if err != nil {
			return event.Fail("plugin.fixture_failed", "fixture failed")
		}
		return event.Result(map[string]any{"account": record.Summary()})
	})
	if rayleabot.Run(context.Background(), rayleabot.Options{Services: a.Services()}, handler) != nil {
		os.Exit(1)
	}
}
