package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var publicUID = regexp.MustCompile(`^[0-9]{6,12}$`)

func (a *App) publicExecute(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, request rayleabot.ServiceRequest, games []string) (map[string]any, error) {
	for key := range request.Params {
		if !slices.Contains([]string{"game", "uid", "region"}, key) {
			return nil, failure("plugin.account_input_invalid")
		}
	}
	if !management(request.Origin) && request.Origin.EventType != "message.private" && request.Origin.EventType != "message.group" {
		return nil, failure("plugin.account_subject_denied")
	}
	var q struct {
		Game   string `json:"game"`
		UID    string `json:"uid"`
		Region string `json:"region"`
	}
	if decode(request.Params, &q) != nil || !publicUID.MatchString(q.UID) || !slices.Contains(games, q.Game) {
		return nil, failure("plugin.account_input_invalid")
	}
	regions := []string{"prod_gf_cn"}
	if q.Game == "genshin" {
		regions = []string{"cn_gf01", "cn_qd01"}
	} else if q.Game == "starrail" {
		regions = append(regions, "prod_qd_cn")
	}
	if !slices.Contains(regions, q.Region) {
		return nil, failure("plugin.account_region_unsupported")
	}
	refs, err := a.Repo.PublicRefs(ctx, store, cipher)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, failure("plugin.account_public_unavailable")
	}
	seed := sha256.Sum256([]byte(request.Origin.EventID + q.UID))
	start := int(seed[0]) % len(refs)
	for i := range len(refs) {
		ref := refs[(start+i)%len(refs)]
		release, err := a.acquire(ctx, ref)
		if err != nil {
			return nil, err
		}
		record, err := a.Repo.ReservePublic(ctx, store, cipher, ref, request.CallerPluginID, q.Game, time.Now())
		if errors.Is(err, accounts.ErrPublicUnavailable) || errors.Is(err, accounts.ErrNotFound) {
			release()
			continue
		}
		if err != nil {
			release()
			return nil, err
		}
		result, err := func() (map[string]any, error) {
			defer release()
			lent, err := a.lendDefaultDevice(ctx, store, cipher, record.Tokens)
			if err != nil {
				return nil, err
			}
			if record.Tokens.DeviceFP == "" || record.Tokens.DeviceFPExpiresAtMS <= time.Now().UnixMilli() {
				tokens, err := a.API.Fingerprint(ctx, lent)
				if err != nil {
					return nil, err
				}
				tokens.Device = record.Tokens.Device
				record.Tokens = tokens
				lent.DeviceFP, lent.DeviceFPExpiresAtMS = tokens.DeviceFP, tokens.DeviceFPExpiresAtMS
				if err = a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
					return nil, err
				}
				record.Revision++
			}
			role := accounts.Role{Game: q.Game, UID: q.UID, Region: q.Region, Nickname: "公开角色"}
			data, err := a.API.Execute(ctx, lent, role, q.Game+".profile", map[string]any{})
			if err != nil {
				delay := time.Minute
				if code := publicError(err).Code; code == "plugin.upstream_device_required" || code == "plugin.upstream_challenge_required" || code == "plugin.upstream_auth_invalid" {
					delay = 24 * time.Hour
				} else if code == "plugin.upstream_rate_limited" {
					delay = time.Hour
				}
				record.PublicQuery.NextUseMS = time.Now().Add(delay).UnixMilli()
				_ = a.Repo.Update(ctx, store, cipher, record, record.Revision)
				if publicError(err).Code == "plugin.upstream_auth_invalid" {
					return nil, failure("plugin.account_public_unavailable")
				}
				return nil, err
			}
			return map[string]any{"operation": q.Game + ".profile", "role": role, "data": data, "fetched_at_ms": time.Now().UnixMilli(), "source": "public_query_pool"}, nil
		}()
		return result, err
	}
	return nil, failure("plugin.account_public_unavailable")
}
func (a *App) publicCommand(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, owner accounts.Subject, items []accounts.Summary) error {
	args := event.Event.Args()
	if len(args) == 0 {
		return event.SendText("共享查询账号 开启 [序号] [每日额度] [原神,星铁,绝区零]\n共享查询账号 关闭 [序号]\n开启后，仅已授权游戏可用此账号查询其他玩家主动公开的基础资料；不交出 CK。默认每日 100，最多 1000。仅本人私聊可开启。")
	}
	if args[0] != "开启" && args[0] != "关闭" {
		return event.SendText("请使用开启或关闭。")
	}
	index, limit := 1, 100
	var err error
	if len(args) > 1 {
		index, err = strconv.Atoi(args[1])
		if err != nil {
			return event.SendText("账号序号无效。")
		}
	}
	if index < 1 || index > len(items) {
		return event.SendText("账号序号不存在，请先查看“我的CK”。")
	}
	if len(args) > 2 {
		limit, err = strconv.Atoi(args[2])
		if err != nil || limit < 1 || limit > 1000 {
			return event.SendText("公共查询每日额度为 1–1000。")
		}
	}
	games := []string{}
	if len(args) > 3 {
		for _, name := range strings.Split(strings.ReplaceAll(args[3], "，", ","), ",") {
			game := map[string]string{"原神": "genshin", "星铁": "starrail", "星穹铁道": "starrail", "绝区零": "zzz"}[name]
			if game == "" {
				return event.SendText("游戏使用原神、星铁、绝区零，以逗号分隔。")
			}
			games = append(games, game)
		}
	} else {
		for _, role := range items[index-1].Roles {
			if !slices.Contains(games, role.Game) {
				games = append(games, role.Game)
			}
		}
	}
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return event.SendText(publicError(err).Message)
	}
	ref := items[index-1].Ref
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return event.SendText(publicError(err).Message)
	}
	defer release()
	updated, err := a.Repo.SetPublic(ctx, store, cipher, ref, owner, args[0] == "开启", limit, games)
	if err != nil {
		return event.SendText("公共查询用途未能更新，请核对账号、绑定游戏与额度后重试。")
	}
	if args[0] == "关闭" {
		return event.SendText("此账号已退出公共查询池。")
	}
	return event.SendText(fmt.Sprintf("已由本人开启公共查询，覆盖 %d 个已绑定游戏，每日最多 %d 次。可随时发送“共享查询账号 关闭 %d”撤销。", len(updated.PublicQuery.Games), limit, index))
}
