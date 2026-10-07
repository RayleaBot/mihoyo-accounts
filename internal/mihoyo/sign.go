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

const zzzSignHost = "https://act-nap-api.mihoyo.com"
const signSalt = "d9200c846b10886e8c874fc33c8f308b"
const signVersion = "2.109.0"

var signActivities = map[string]string{"genshin": "e202311201442471", "starrail": "e202304121516551", "zzz": "e202406242138391"}

func allowedSignEndpoint(host, path, profile string) bool {
	return host == zzzSignHost && profile == "sign.zzz" && (path == "/event/luna/zzz/info" || path == "/event/luna/zzz/sign" || path == "/event/luna/zzz/home")
}
func (c *Client) gameSign(ctx context.Context, tokens accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	if len(input) != 0 {
		return nil, invalidInput(name)
	}
	act, ok := signActivities[role.Game]
	if !ok {
		return nil, invalidInput(name)
	}
	regions := []string{"prod_gf_cn"}
	if role.Game == "genshin" {
		regions = []string{"cn_gf01", "cn_qd01"}
	} else if role.Game == "starrail" {
		regions = append(regions, "prod_qd_cn")
	}
	os := tokens.Realm == "os"
	if !os && !slices.Contains(regions, role.Region) || os && !overseasRole(role) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	host, base := webHost, "/event/luna/"
	if role.Game == "zzz" {
		host, base = zzzSignHost, "/event/luna/zzz/"
	}
	profile := "sign." + role.Game
	query := url.Values{"act_id": {act}, "lang": {"zh-cn"}, "uid": {role.UID}, "region": {role.Region}}
	if os {
		p := overseasSigns[role.Game]
		host, base, act = p[0], p[1], p[2]
		profile = "sign.os." + role.Game
		query = url.Values{"act_id": {act}, "lang": {"zh-cn"}}
	}
	if strings.HasSuffix(name, ".sign_rewards") {
		data, err := c.request(ctx, name, host, base+"home", profile, url.Values{"act_id": {act}, "lang": {"zh-cn"}}, nil, tokens)
		if err != nil {
			return nil, err
		}
		awards, ok := data["awards"].([]any)
		if !ok || len(awards) > 366 {
			return nil, invalid(name)
		}
		clean := []any{}
		for _, raw := range awards {
			a := object(raw)
			amount, err := strconv.Atoi(stringNumber(a["cnt"]))
			if err != nil || amount < 0 || amount > 10000000 || text(a["name"]) == "" {
				return nil, invalid(name)
			}
			clean = append(clean, map[string]any{"name": text(a["name"]), "count": json.Number(strconv.Itoa(amount))})
		}
		out := map[string]any{"awards": clean}
		if month := text(data["month"]); month != "" {
			out["month"] = month
		}
		if !safeBusinessValue(out, tokens, 0) {
			return nil, invalid(name)
		}
		return out, nil
	}
	data, err := c.request(ctx, name, host, base+"info", profile, query, nil, tokens)
	if err != nil {
		return nil, err
	}
	signed, ok := data["is_sign"].(bool)
	if !ok {
		return nil, invalid(name)
	}
	days, err := strconv.Atoi(stringNumber(data["total_sign_day"]))
	if err != nil || days < 0 || days > 366 {
		return nil, invalid(name)
	}
	first, _ := data["first_bind"].(bool)
	out := map[string]any{"signed": signed, "total_days": json.Number(strconv.Itoa(days)), "first_bind": first, "date": text(data["today"])}
	if !safeBusinessValue(out, tokens, 0) {
		return nil, invalid(name)
	}
	if strings.HasSuffix(name, ".sign_info") {
		return out, nil
	}
	if signed {
		out["outcome"] = "already_signed"
		return out, nil
	}
	if first {
		return nil, &Error{Code: "plugin.upstream_first_sign_required", Operation: name}
	}
	var signQuery url.Values
	signBody := map[string]any{"act_id": act, "region": role.Region, "uid": role.UID}
	if os {
		signQuery = url.Values{"act_id": {act}, "lang": {"zh-cn"}}
		signBody = map[string]any{}
	}
	result, err := c.request(ctx, name, host, base+"sign", profile, signQuery, signBody, tokens)
	if err != nil {
		return nil, err
	}
	if result["already_signed"] == true {
		return map[string]any{"signed": true, "outcome": "already_signed", "date": out["date"]}, nil
	}
	if text(result["gt"]) != "" || text(result["challenge"]) != "" {
		return nil, &Error{Code: "plugin.upstream_challenge_required", Operation: name}
	}
	if os {
		risk := object(result["gt_result"])
		if risk != nil && (text(risk["gt"]) != "" || text(risk["challenge"]) != "" || integer(risk["risk_code"]) != 0) {
			return nil, &Error{Code: "plugin.upstream_challenge_required", Operation: name}
		}
		return map[string]any{"signed": true, "outcome": "signed", "account_scope": true, "date": out["date"]}, nil
	}
	success, err := strconv.Atoi(stringNumber(result["success"]))
	if err != nil {
		return nil, invalid(name)
	}
	if success != 0 {
		return nil, &Error{Code: "plugin.upstream_challenge_required", Operation: name}
	}
	return map[string]any{"signed": true, "outcome": "signed", "date": out["date"]}, nil
}
