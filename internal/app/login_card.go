package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

// cardGames are the games of the login card in Yunzai's showUid order, with
// the common banner the game plugins' 我的uid draws for each: miao's meta-gs
// and meta-sr character/common banners and the 原神插件's ZZZero banner.
// scripts/bundle-assets.py bundles these and the files of cardArtwork.
var cardGames = []struct{ id, name, banner string }{
	{"genshin", "原神", "assets/miao-plugin/resources/meta-gs/character/common/imgs/banner.webp"},
	{"starrail", "星穹铁道", "assets/miao-plugin/resources/meta-sr/character/common/imgs/banner.webp"},
	{"zzz", "绝区零", "assets/yunzai-genshin/resources/ZZZero/img/other/banner.png"},
}

// cardArtwork are the fonts and images the converted stylesheet names, and
// miao's common face.
var cardArtwork = []rayleabot.RenderImageResource{
	{ID: "Number", Path: "assets/miao-plugin/resources/common/font/tttgbnumber.woff"},
	{ID: "YS", Path: "assets/miao-plugin/resources/common/font/HYWH-65W.woff"},
	{ID: "common-bg-bg-hydro", Path: "assets/miao-plugin/resources/common/bg/bg-hydro.webp"},
	{ID: "face-common", Path: "assets/miao-plugin/resources/common/item/face.webp"},
}

// maxFaces is how many roles have their in-game avatar read; the host fetches
// at most 16 linked images for one picture.
const maxFaces = 8

// faceTime bounds reading the avatars, so a slow game record leaves time to
// draw and send the result.
const faceTime = 8 * time.Second

// loginResult is the message for an ended login: for a bound one with roles,
// "登录成功" and the roles drawn as a card, else, or when the card is not
// drawn, the result's text. image tells which.
func (a *App) loginResult(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, view LoginView) ([]rayleabot.Segment, bool) {
	text := []rayleabot.Segment{rayleabot.Text(loginText(view))}
	if view.State != "bound" || view.Account == nil || len(view.Account.Roles) == 0 {
		return text, false
	}
	faceCtx, cancel := context.WithTimeout(ctx, faceTime)
	faces := a.roleFaces(faceCtx, store, view.Account.Ref)
	cancel()
	prefix := ""
	if len(event.CommandPrefixes) > 0 {
		prefix = event.CommandPrefixes[0]
	}
	data, resources := loginCard(view, faces)
	data["prefix"] = prefix
	result, err := event.Actions().RenderImage(ctx, rayleabot.RenderImageRequest{Template: "uid-list", Output: "png", FallbackText: loginText(view), Data: data, Resources: resources})
	path := stringValue(result["image_path"])
	if err != nil || path == "" {
		return text, false
	}
	return []rayleabot.Segment{rayleabot.Text("登录成功\n"), rayleabot.Image(path)}, true
}

// loginCard draws a bound login's roles as Yunzai's showUid draws 我的uid
// in the 原神插件's html/user/uid-list: per game, each role's UID with the CK
// tag, its name and level from the official role list, its in-game avatar or
// miao's common face, and the game's common banner. A game whose roles were
// not read says so; one without roles is left out.
func loginCard(view LoginView, faces map[string]string) (map[string]any, []rayleabot.RenderImageResource) {
	resources := append([]rayleabot.RenderImageResource{}, cardArtwork...)
	games := []any{}
	for _, game := range cardGames {
		uids := []any{}
		for _, role := range view.Account.Roles {
			if role.Game != game.id {
				continue
			}
			face := "face-common"
			if url := faces[role.Ref]; url != "" {
				face = "face-" + strconv.Itoa(len(resources))
				resources = append(resources, rayleabot.RenderImageResource{ID: face, URL: url})
			}
			item := map[string]any{"index": len(uids) + 1, "uid": role.UID, "face": face, "banner": "banner-" + game.id}
			if role.Nickname != "" && role.Level > 0 {
				item["name"], item["level"] = role.Nickname, role.Level
			}
			uids = append(uids, item)
		}
		if len(uids) > 0 {
			resources = append(resources, rayleabot.RenderImageResource{ID: "banner-" + game.id, Path: game.banner})
			games = append(games, map[string]any{"name": game.name, "uids": uids})
		} else if state := view.RoleStates[game.id]; state != "" && state != "empty" && state != "ready" {
			games = append(games, map[string]any{"name": game.name, "uids": uids, "notice": "角色读取失败，可在账号管理页按游戏重试"})
		}
	}
	return map[string]any{"games": games}, resources
}

// roleFaces reads each role's in-game avatar once from its game record index
// right after the login is saved: 原神 role.game_head_icon, 星穹铁道 and 绝区零
// cur_head_icon_url. The requests go through the account's device as the
// games' own queries do; a role whose index is not read keeps miao's common
// face, as the game plugins show without a saved panel.
func (a *App) roleFaces(ctx context.Context, store accounts.SecretStore, ref string) map[string]string {
	faces := map[string]string{}
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return faces
	}
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return faces
	}
	defer release()
	record, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return faces
	}
	for index, role := range record.Roles {
		if index >= maxFaces || record.Status == "invalid" || ctx.Err() != nil {
			break
		}
		data, err := a.executeRole(ctx, store, cipher, &record, role, role.Game+".profile", map[string]any{})
		if err != nil {
			continue
		}
		icon := data["cur_head_icon_url"]
		if role.Game == "genshin" {
			icon = object(data["role"])["game_head_icon"]
		}
		if url := stringValue(icon); strings.HasPrefix(url, "https://") {
			faces[role.Ref] = url
		}
	}
	return faces
}
