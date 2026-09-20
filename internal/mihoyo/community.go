package mihoyo

import (
	"context"
	"encoding/json"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const communityHost = "https://bbs-api.miyoushe.com"
const communitySalt = "47f15f1b66bee46b816115d8e8e6ebb6"
const communitySignSalt = "t0qEgfub6cvueAPgR5m9aQWWVciEer7v"

var communityGames = map[string][2]string{"genshin": {"2", "26"}, "starrail": {"6", "52"}, "zzz": {"8", "57"}}
var postIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,23}$`)

func init() {
	for game := range communityGames {
		for _, name := range []string{"community_run", "community_status", "community_posts", "community_sign", "community_read", "community_like", "community_unlike", "community_share"} {
			operations[game+"."+name] = operation{game: game, accountScoped: true, mutates: !slices.Contains([]string{"community_status", "community_posts"}, name)}
		}
	}
}
func allowedCommunityEndpoint(host, path, profile string) bool {
	if host != communityHost {
		return false
	}
	switch profile {
	case "community.status":
		return path == "/apihub/wapi/getUserMissionsState"
	case "community.sign":
		return path == "/apihub/app/api/signIn"
	case "community":
		return slices.Contains([]string{"/post/api/getForumPostList", "/post/api/getPostFull", "/apihub/api/getShareConf", "/apihub/sapi/upvotePost"}, path)
	}
	return false
}
func safeCommunityText(value any) string {
	v := []rune(text(value))
	if len(v) > 160 {
		v = v[:160]
	}
	return string(v)
}
func (c *Client) community(ctx context.Context, tokens accounts.Tokens, game, name string, input map[string]any) (map[string]any, error) {
	kind := strings.TrimPrefix(name, game+".")
	if kind == "community_run" {
		if !ValidCommunityDelegation(input) {
			return nil, invalidInput(name)
		}
		kind = "community_" + text(input["action"])
		next := map[string]any{}
		if v, ok := input["post_id"]; ok {
			next["post_id"] = v
		}
		input = next
	}
	forum, ok := communityGames[game]
	if !ok {
		return nil, invalidInput(name)
	}
	path, profile := "", "community"
	var query url.Values
	var body any
	if kind != "community_status" && tokens.SToken == "" {
		return nil, &Error{Code: "plugin.upstream_community_unavailable", Operation: name, Reason: "stoken_missing"}
	}
	switch kind {
	case "community_status":
		if len(input) != 0 {
			return nil, invalidInput(name)
		}
		path = "/apihub/wapi/getUserMissionsState"
		profile = "community.status"
		query = url.Values{"point_sn": {"myb"}}
	case "community_posts":
		if len(input) != 0 {
			return nil, invalidInput(name)
		}
		path = "/post/api/getForumPostList"
		query = url.Values{"forum_id": {forum[1]}, "is_good": {"false"}, "is_hot": {"false"}, "page_size": {"20"}, "sort_type": {"1"}}
	case "community_sign":
		if len(input) != 0 {
			return nil, invalidInput(name)
		}
		path = "/apihub/app/api/signIn"
		profile = "community.sign"
		body = map[string]any{"gids": forum[0]}
	case "community_read", "community_like", "community_unlike", "community_share":
		if len(input) != 1 || !postIDPattern.MatchString(text(input["post_id"])) {
			return nil, invalidInput(name)
		}
		id := text(input["post_id"])
		switch kind {
		case "community_read":
			path = "/post/api/getPostFull"
			query = url.Values{"post_id": {id}}
		case "community_share":
			path = "/apihub/api/getShareConf"
			query = url.Values{"entity_id": {id}, "entity_type": {"1"}}
		default:
			path = "/apihub/sapi/upvotePost"
			body = map[string]any{"post_id": id, "is_cancel": kind == "community_unlike"}
		}
	default:
		return nil, invalidInput(name)
	}
	data, err := c.request(ctx, name, communityHost, path, profile, query, body, tokens)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	switch kind {
	case "community_status":
		for _, key := range []string{"total_points", "already_received_points", "can_get_points"} {
			if n, err := strconv.ParseInt(text(data[key]), 10, 64); err != nil || n < 0 {
				return nil, invalid(name)
			}
			out[key] = data[key]
		}
		states, _ := data["states"].([]any)
		if len(states) > 100 {
			return nil, invalid(name)
		}
		rows := []any{}
		for _, raw := range states {
			state := object(raw)
			row := map[string]any{}
			for _, key := range []string{"mission_id", "happened_times", "mission_cnt", "is_get_award", "finished_times"} {
				if state[key] != nil {
					row[key] = state[key]
				}
			}
			rows = append(rows, row)
		}
		out["states"] = rows
	case "community_posts":
		list, _ := data["list"].([]any)
		if len(list) > 100 {
			return nil, invalid(name)
		}
		rows := []any{}
		for _, raw := range list {
			post := object(object(raw)["post"])
			id := text(post["post_id"])
			if !postIDPattern.MatchString(id) {
				continue
			}
			rows = append(rows, map[string]any{"post_id": id, "title": safeCommunityText(post["subject"])})
		}
		out["posts"] = rows
	default:
		out = map[string]any{"accepted": true, "kind": kind, "gid": json.Number(forum[0])}
	}
	if !safeBusinessValue(out, tokens, 0) {
		return nil, invalid(name)
	}
	return out, nil
}

func ValidCommunityDelegation(input map[string]any) bool {
	step := text(input["action"])
	if slices.Contains([]string{"status", "posts", "sign"}, step) {
		return len(input) == 1
	}
	return slices.Contains([]string{"read", "like", "unlike", "share"}, step) && len(input) == 2 && postIDPattern.MatchString(text(input["post_id"]))
}
