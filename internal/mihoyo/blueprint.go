package mihoyo

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"net/url"
	"regexp"
	"slices"
	"strconv"
)

func init() {
	operations["genshin.blueprint_read"] = operation{game: "genshin"}
	operations["genshin.blueprint_compute"] = operation{game: "genshin"}
}

var blueprintCode = regexp.MustCompile(`^[0-9]{10,15}$`)

func allowedBlueprintEndpoint(host, path, profile string) bool {
	return host == "https://sg-public-api.hoyolab.com" && profile == "record" && slices.Contains([]string{"/event/calculateos/furniture/blueprint", "/event/calculateos/furniture/compute"}, path)
}
func (c *Client) blueprint(ctx context.Context, tokens accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	host, root := webHost, "/event/e20200928calculate/v1/furniture/"
	overseas := slices.Contains([]string{"os_usa", "os_euro", "os_asia", "os_cht"}, role.Region)
	if overseas {
		host, root = "https://sg-public-api.hoyolab.com", "/event/calculateos/furniture/"
	} else if !slices.Contains([]string{"cn_gf01", "cn_qd01"}, role.Region) {
		return nil, &Error{Code: "plugin.account_region_unsupported", Operation: name}
	}
	var query url.Values
	var body any
	path := ""
	if name == "genshin.blueprint_read" {
		if len(input) != 1 || !blueprintCode.MatchString(text(input["share_code"])) {
			return nil, invalidInput(name)
		}
		query = url.Values{"share_code": {text(input["share_code"])}, "region": {role.Region}}
		path = root + "blueprint"
	} else {
		if len(input) != 1 {
			return nil, invalidInput(name)
		}
		raw, err := json.Marshal(input["list"])
		var list []struct {
			ID    int `json:"id"`
			Count int `json:"cnt"`
		}
		if err != nil || json.Unmarshal(raw, &list) != nil || len(list) < 1 || len(list) > 500 {
			return nil, invalidInput(name)
		}
		seen := map[int]bool{}
		rows := []any{}
		for _, v := range list {
			if v.ID < 1 || v.ID > 1000000000 || v.Count < 1 || v.Count > 9999 || seen[v.ID] {
				return nil, invalidInput(name)
			}
			seen[v.ID] = true
			rows = append(rows, map[string]any{"id": v.ID, "cnt": v.Count})
		}
		m := map[string]any{"list": rows}
		if overseas {
			m["lang"] = "zh-cn"
		}
		body = m
		path = root + "compute"
	}
	if overseas && query != nil {
		query.Set("lang", "zh-cn")
	}
	profile := "record"
	if overseas {
		profile = "record.os"
	}
	data, err := c.request(ctx, name, host, path, profile, query, body, tokens)
	if err != nil {
		return nil, err
	}
	list, ok := data["list"].([]any)
	if !ok || len(list) > 2000 {
		return nil, invalid(name)
	}
	out := []any{}
	for _, v := range list {
		row := object(v)
		clean := map[string]any{}
		for _, key := range words("id name num cnt icon icon_url rarity materials material_list list furniture_name") {
			if value, ok := row[key]; ok {
				clean[key] = value
			}
		}
		if id := text(clean["id"]); id != "" {
			if _, err := strconv.ParseUint(id, 10, 64); err != nil {
				return nil, invalid(name)
			}
			clean["id"] = id
		}
		out = append(out, clean)
	}
	result := map[string]any{"list": out}
	if !safeBusinessValue(result, tokens, 0) {
		return nil, invalid(name)
	}
	return result, nil
}
