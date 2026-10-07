package app

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

const genshinHead = "https://act-webstatic.mihoyo.com/hk4e/synthetic-head.png"

func TestBoundLoginIsSentAsTheRoleCard(t *testing.T) {
	var mu sync.Mutex
	var reads []string
	api := &fakeAPI{roleError: "zzz", execute: func(_ context.Context, _ accounts.Tokens, role accounts.Role, name string, _ map[string]any) (map[string]any, error) {
		mu.Lock()
		reads = append(reads, name)
		mu.Unlock()
		if name == "genshin.profile" {
			return map[string]any{"role": map[string]any{"game_head_icon": genshinHead}}, nil
		}
		return nil, &mihoyo.Error{Code: "plugin.upstream_challenge_required", Operation: name}
	}}
	_, host := chatLogin(t, api)
	_, actions := host.message("group", "#扫码登录", "扫码登录")
	// Each role's game record index is read once for its avatar.
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(reads, []string{"genshin.profile", "starrail.profile"}) {
		t.Fatalf("avatar reads: %v", reads)
	}
	renders := actionsNamed(actions, "render.image")
	if len(renders) != 1 || renders[0].Data["template"] != "uid-list" {
		t.Fatalf("the card was not drawn: %v", actions)
	}
	data := object(renders[0].Data["data"])
	resources := map[string]map[string]any{}
	for _, raw := range renders[0].Data["resources"].([]any) {
		resources[stringValue(object(raw)["id"])] = object(raw)
	}
	games := data["games"].([]any)
	// The tips write the plugin's first prefix; the host here uses / * ~.
	if data["prefix"] != "/" || len(games) != 3 {
		t.Fatalf("card data: %v", data)
	}
	genshin, starrail, zzz := object(games[0]), object(games[1]), object(games[2])
	card := object(genshin["uids"].([]any)[0])
	if genshin["name"] != "原神" || card["uid"] != "1234" || card["name"] != "测试角色" || card["level"] != float64(50) || resources[stringValue(card["face"])]["url"] != genshinHead {
		t.Fatalf("原神 card: %v %v", genshin, resources)
	}
	if resources[stringValue(card["banner"])]["path"] != "assets/miao-plugin/resources/meta-gs/character/common/imgs/banner.webp" {
		t.Fatalf("原神 banner: %v", resources)
	}
	// A role whose index was not read shows miao's common face.
	card = object(starrail["uids"].([]any)[0])
	if starrail["name"] != "星穹铁道" || card["face"] != "face-common" || resources["face-common"]["path"] != "assets/miao-plugin/resources/common/item/face.webp" || resources[stringValue(card["banner"])]["path"] != "assets/miao-plugin/resources/meta-sr/character/common/imgs/banner.webp" {
		t.Fatalf("星穹铁道 card: %v %v", starrail, resources)
	}
	if zzz["name"] != "绝区零" || len(zzz["uids"].([]any)) != 0 || zzz["retry"] != "/刷新ck" {
		t.Fatalf("绝区零 notice: %v", zzz)
	}
	results := resultSends(actions)
	if len(results) != 1 {
		t.Fatalf("result: %v", actions)
	}
	segments := object(results[0].Data["message"])["segments"].([]any)
	if len(segments) != 3 || object(segments[0])["type"] != "at" || object(object(segments[1])["data"])["text"] != "\n登录成功\n" || object(object(segments[2])["data"])["file"] != "file:///rendered/uid-list.png" {
		t.Fatalf("the card was not sent to the group: %v", segments)
	}
}

func TestLoginCardFallsBackToText(t *testing.T) {
	t.Run("render fails", func(t *testing.T) {
		_, host := chatLogin(t, &fakeAPI{})
		host.renderFails = true
		_, actions := host.message("group", "#扫码登录", "扫码登录")
		results := resultSends(actions)
		if len(results) != 1 || !strings.Contains(actionText(results[0]), "登录成功，已发现 3 个游戏角色") {
			t.Fatalf("no text result: %v", actions)
		}
	})
	t.Run("picture refused", func(t *testing.T) {
		_, host := chatLogin(t, &fakeAPI{})
		host.cardRefused = true
		reply, actions := host.message("group", "#扫码登录", "扫码登录")
		results := resultSends(actions)
		if reply["type"] != "result" || len(results) != 2 || !strings.Contains(actionText(results[1]), "登录成功，已发现 3 个游戏角色") {
			t.Fatalf("the refused card was not replaced by text: %v", actions)
		}
	})
}
