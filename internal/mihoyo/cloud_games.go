package mihoyo

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/url"
	"regexp"
	"strings"
)

var cloudRewardIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

const cloudGenshinHost = "https://api-cloudgame.mihoyo.com"
const cloudZZZHost = "https://cg-nap-api.mihoyo.com"

func init() {
	for _, game := range []string{"genshin", "zzz"} {
		for _, name := range []string{"cloud_status", "cloud_sign"} {
			operations[game+"."+name] = operation{game: game, accountScoped: true, mutates: name == "cloud_sign"}
		}
	}
	operations["genshin.cloud_rewards"] = operation{game: "genshin", accountScoped: true}
	operations["genshin.cloud_claim"] = operation{game: "genshin", accountScoped: true, mutates: true}
}
func allowedCloudEndpoint(host, path, profile string) bool {
	if host == cloudGenshinHost && profile == "cloud.genshin" {
		return path == "/hk4e_cg_cn/wallet/wallet/get" || path == "/hk4e_cg_cn/gamer/api/listNotifications" || path == "/hk4e_cg_cn/gamer/api/ackNotification"
	}
	return host == cloudZZZHost && profile == "cloud.zzz" && path == "/nap_cn/cg/wallet/wallet/get"
}
func (c *Client) cloudGame(ctx context.Context, tokens accounts.Tokens, game, name string, input map[string]any) (map[string]any, error) {
	token, host, path := tokens.CloudGenshin, cloudGenshinHost, "/hk4e_cg_cn/wallet/wallet/get"
	if game == "zzz" {
		token, host, path = tokens.CloudZZZ, cloudZZZHost, "/nap_cn/cg/wallet/wallet/get"
	}
	if token == "" {
		return nil, &Error{Code: "plugin.account_cloud_unavailable", Operation: name}
	}
	var query url.Values
	var body any
	switch {
	case strings.HasSuffix(name, ".cloud_sign"):
		if len(input) != 0 {
			return nil, invalidInput(name)
		}
	case name == "genshin.cloud_rewards":
		if len(input) != 0 {
			return nil, invalidInput(name)
		}
		path = "/hk4e_cg_cn/gamer/api/listNotifications"
		query = url.Values{"status": {"NotificationStatusUnread"}, "type": {"NotificationTypePopup"}, "is_sort": {"true"}}
	case name == "genshin.cloud_claim":
		if len(input) != 1 || !cloudRewardIDPattern.MatchString(text(input["reward_id"])) {
			return nil, invalidInput(name)
		}
		path = "/hk4e_cg_cn/gamer/api/ackNotification"
		body = map[string]any{"id": text(input["reward_id"])}
	default:
		return nil, invalidInput(name)
	}
	data, err := c.request(ctx, name, host, path, "cloud."+game, query, body, tokens)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if strings.HasSuffix(name, ".cloud_sign") {
		for _, pair := range [][2]string{{"free_time", "free_time"}, {"free_time", "send_freetime"}, {"coin", "coin_num"}} {
			group := object(data[pair[0]])
			if group[pair[1]] != nil {
				out[pair[1]] = group[pair[1]]
			}
		}
		if v := data["total_time"]; v != nil {
			out["total_time"] = v
		}
		if msg := text(object(data["play_card"])["short_msg"]); msg != "" {
			out["play_card"] = safeCommunityText(msg)
		}
		out["checked"] = true
	} else if name == "genshin.cloud_rewards" {
		list, _ := data["list"].([]any)
		if len(list) > 100 {
			return nil, invalid(name)
		}
		rows := []any{}
		for _, v := range list {
			item := object(v)
			if id := text(item["id"]); cloudRewardIDPattern.MatchString(id) {
				rows = append(rows, map[string]any{"id": id, "message": safeCommunityText(item["msg"])})
			}
		}
		out["items"] = rows
	} else {
		out["accepted"] = true
	}
	if !safeBusinessValue(out, tokens, 0) {
		return nil, invalid(name)
	}
	return out, nil
}

// Cached wallet responses still pass the same fixed projection and secret check.
func CloudStatusData(tokens accounts.Tokens, data map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, key := range []string{"checked", "checked_at_ms", "free_time", "send_freetime", "coin_num", "total_time", "play_card"} {
		if v, ok := data[key]; ok {
			out[key] = v
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, invalid("cloud.status")
	}
	var normalized map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil || !safeBusinessValue(normalized, tokens, 0) {
		return nil, invalid("cloud.status")
	}
	return normalized, nil
}
