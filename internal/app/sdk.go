package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

type sdkStore struct{ event *rayleabot.EventContext }

func (s sdkStore) Read(ctx context.Context, key string) (string, bool, error) {
	result, err := s.event.Actions().SecretRead(ctx, key)
	if err != nil {
		return "", false, err
	}
	exists, _ := result["exists"].(bool)
	return stringValue(result["value"]), exists, nil
}
func (s sdkStore) Write(ctx context.Context, values map[string]string) error {
	_, err := s.event.Actions().SecretWrite(ctx, values)
	return err
}

func policyFor(event *rayleabot.EventContext) Policy {
	var policy Policy
	_ = decode(event.Config, &policy)
	policy.SuperAdmins = event.SuperAdmins
	return policy
}
func (a *App) Services() []rayleabot.Service {
	handler := func(ctx context.Context, event *rayleabot.EventContext, request rayleabot.ServiceRequest) (map[string]any, error) {
		result, err := a.Service(ctx, sdkStore{event}, policyFor(event), request)
		var failure *rayleabot.ActionError
		if errors.As(err, &failure) && strings.HasPrefix(failure.Code, "plugin.upstream_") {
			fields := map[string]any{"code": failure.Code}
			for _, key := range []string{"operation", "retcode", "http_status", "reason"} {
				if value, exists := failure.Details[key]; exists {
					fields[key] = value
				}
			}
			_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: failure.Message, Fields: fields})
		}
		return result, err
	}
	methods := map[string]rayleabot.ServiceHandler{}
	for _, method := range serviceMethods {
		methods[method] = handler
	}
	return []rayleabot.Service{{Name: "accounts", Version: 1, Methods: methods}}
}

// serviceMethods are the accounts v1 methods; info.json declares the same.
var serviceMethods = []string{"list", "roles", "select", "execute", "delegation.create", "delegation.revoke", "public.execute", "ark.request", "ark.status", "uids.bind", "uids.remove", "uids.use", "uids.current", "gacha.link", "device.bind", "device.unbind", "device.default"}

// Handle answers an event within the deadline the host gave it, which ctx
// carries; commands that can take longer move to the background.
func (a *App) Handle(ctx context.Context, event *rayleabot.EventContext) error {
	store, policy := sdkStore{event}, policyFor(event)
	if event.Event.EventType == "management.action" {
		if event.Event.SourceProtocol != "management" || event.Event.SourceAdapter != "management.ui" {
			return event.Fail("plugin.account_subject_denied", message("plugin.account_subject_denied"))
		}
		result, err := a.Manage(ctx, store, policy, stringValue(event.Event.Payload["action"]), object(event.Event.Payload["payload"]))
		if err != nil {
			failure := publicError(err)
			return event.FailDetails(failure.Code, failure.Message, failure.Details)
		}
		if stringValue(event.Event.Payload["action"]) == "status" {
			result["bots"] = event.Bots
		}
		return event.Result(result)
	}
	if event.Event.EventType == "scheduler.trigger" {
		return dropTask(ctx, event)
	}
	if event.Event.EventType != "message.private" && event.Event.EventType != "message.group" {
		return event.Result(map[string]any{"handled": false})
	}
	if pastedCredential.MatchString(event.Event.Message.PlainText) {
		return event.SendText(credentialWarning(event.Event.Target.Type != "private"))
	}
	if handled, err := a.deviceMessage(ctx, event, store); handled {
		return err
	}
	command := ckCommand(event.Event.Command())
	if command == "账号帮助" || command == "账号版本" {
		return a.helpCommand(event, command)
	}
	switch command {
	case "关联账号", "申请关联", "关联进度", "完成关联", "取消关联", "关联列表", "解除关联", "扫码登录", "登录状态", "取消登录", "我的CK", "刷新CK", "检查CK", "删除CK", "共享查询账号":
	case "绑定设备帮助":
		return event.SendText(deviceHelp)
	case "CK帮助":
		return event.SendText(ckTutorial)
	case "绑定设备", "解绑设备":
	default:
		return event.Result(map[string]any{"handled": false})
	}
	// Only the CK list is kept to private chats, as Yunzai's 我的CK; the
	// other account commands work in groups for their sender.
	if command == "我的CK" && event.Event.Target.Type != "private" {
		return event.SendText("请私聊查看")
	}
	owner := accounts.Subject{SourceProtocol: event.Event.SourceProtocol, SourceAdapter: event.Event.SourceAdapter, BotID: event.Bot.ID, ActorID: event.Event.Actor.ID}
	if owner.Validate() != nil {
		return event.SendText(message("plugin.account_subject_denied"))
	}
	if slices.Contains([]string{"关联账号", "申请关联", "关联进度", "完成关联", "取消关联", "关联列表", "解除关联"}, command) {
		return a.associationCommand(ctx, event, store, owner, command)
	}
	if event.Event.Command() == "扫码登录" {
		return a.chatLogin(ctx, event, store, policy, owner)
	}
	if event.Event.Command() == "登录状态" || event.Event.Command() == "取消登录" {
		a.jobsMu.Lock()
		ref := a.owners[owner]
		a.jobsMu.Unlock()
		action := "login.poll"
		if event.Event.Command() == "取消登录" {
			action = "login.cancel"
		}
		result, err := a.Manage(ctx, store, policy, action, map[string]any{"login_ref": ref})
		if err != nil {
			return event.SendText(publicError(err).Message)
		}
		return event.SendText(loginText(result["login"].(LoginView)))
	}
	listed, err := a.Manage(ctx, store, policy, "accounts.list", map[string]any{"subject": owner})
	if err != nil {
		return event.SendText(publicError(err).Message)
	}
	items := listed["items"].([]accounts.Summary)
	if len(items) == 0 {
		return event.SendText("还没有绑定米游社账号，请发送“扫码登录”。")
	}
	if command == "绑定设备" || command == "解绑设备" {
		return a.deviceCommand(ctx, event, store, owner, command)
	}
	if command == "我的CK" {
		var lines []string
		for i, item := range items {
			line := fmt.Sprintf("%d. %s · %s · %d 个角色", i+1, item.AccountLabel, accountStatus(item.Status), len(item.Roles))
			if item.LastCheck != nil {
				line += "\n最近" + map[string]string{"check": "检查", "refresh": "刷新"}[item.LastCheck.Kind] + "：" + checkLabel(item.LastCheck.State) + " · " + time.UnixMilli(item.LastCheck.CheckedAtMS).In(event.Location).Format("01-02 15:04")
			}
			lines = append(lines, line)
		}
		return event.SendText(strings.Join(lines, "\n") + "\n检查CK [序号] · 刷新CK [序号] · 删除CK [序号]")
	}
	if command == "共享查询账号" {
		return a.publicCommand(ctx, event, store, owner, items)
	}
	index := 1
	if args := event.Event.Args(); len(args) > 0 {
		index, err = strconv.Atoi(args[0])
		if err != nil {
			return event.SendText("请输入账号序号，可先发送“我的CK”查看。")
		}
	}
	if index < 1 || index > len(items) {
		return event.SendText("账号序号不存在，可先发送“我的CK”查看。")
	}
	action := "accounts.refresh"
	if command == "检查CK" {
		action = "accounts.check"
	}
	if command == "删除CK" {
		action = "accounts.remove"
	}
	if action == "accounts.refresh" {
		// Refreshing reads each game's roles as well, several requests that
		// can outlast the event deadline.
		if detached, err := background(ctx, event); !detached {
			if err != nil {
				return err
			}
			return event.SendText(busyText)
		}
	}
	ref := items[index-1].Ref
	if _, err := a.Manage(ctx, store, policy, action, map[string]any{"account_ref": ref, "subject": owner}); err != nil {
		return event.SendText(publicError(err).Message)
	}
	if action == "accounts.remove" {
		return event.SendText("账号已移除，游戏插件无法继续使用。")
	}
	if action == "accounts.check" {
		return event.SendText("账号角色接口检查通过，凭据未刷新。具体游戏的设备验证状态需单独查询。")
	}
	// As xiaoyao-cvs-plugin's 刷新ck, which binds the new cookie through
	// Yunzai's bing and so reads the account's roles again, each game's roles
	// are read again; a game that cannot be read keeps its roles.
	var failed []string
	for _, game := range cardGames {
		if _, err := a.Manage(ctx, store, policy, "accounts.roles", map[string]any{"account_ref": ref, "game": game.id, "subject": owner}); err != nil {
			failed = append(failed, game.name)
		}
	}
	if len(failed) > 0 {
		return event.SendText("账号凭据已刷新；" + strings.Join(failed, "、") + "角色未能读取，已保留原有角色，可稍后再次发送“刷新CK”。")
	}
	return event.SendText("账号凭据已刷新，已重新读取游戏角色。")
}

func checkLabel(state string) string {
	return map[string]string{"available": "通过", "auth_invalid": "登录失效", "device_required": "需要设备验证", "challenge_required": "需要官方验证", "unavailable": "暂未确认"}[state]
}

func accountStatus(state string) string {
	if state == "valid" {
		return "可用"
	}
	if state == "invalid" {
		return "需重新登录"
	}
	return "待检查"
}
func loginText(view LoginView) string {
	switch view.State {
	case "bound":
		return fmt.Sprintf("登录成功，已发现 %d 个游戏角色。可发送“我的CK”查看。", len(view.Account.Roles))
	case "scanned":
		if view.Phase == "scan" {
			return "已扫描，请在米游社确认登录。"
		}
		return "已确认，正在读取游戏角色并保存账号。"
	case "expired":
		return "二维码已过期，请重新发送“扫码登录”。"
	case "canceled":
		return "登录已取消。"
	case "failed":
		result := message(view.ErrorCode)
		if view.Failure != nil {
			stage := map[string]string{"scan": "读取扫码确认", "exchange": "换取账号凭据", "roles": "发现游戏角色", "saving": "保存账号"}[view.Failure.Stage]
			if view.Failure.Operation == "tokens.ltoken" {
				stage = "换取 LToken"
			}
			if view.Failure.Operation == "tokens.cookie" {
				stage = "换取 Cookie Token"
			}
			if stage != "" {
				result += "\n步骤：" + stage
			}
			if view.Failure.RetCode != 0 {
				result += fmt.Sprintf("，米游社错误码 %d", view.Failure.RetCode)
			}
			if view.Failure.HTTPStatus != 0 {
				result += fmt.Sprintf("，HTTP %d", view.Failure.HTTPStatus)
			}
		}
		return result
	default:
		return "等待米游社扫码确认。"
	}
}

var (
	ckMine    = regexp.MustCompile(`(?i)^(?:原神|星铁|绝区零)?我的c(?:oo)?k(?:ie)?$`)
	ckRemove  = regexp.MustCompile(`(?i)^(?:原神|星铁|绝区零)?删除c(?:oo)?k(?:ie)?$`)
	ckRefresh = regexp.MustCompile(`(?i)^刷新c(?:oo)?k(?:ie)?$`)
	ckCheck   = regexp.MustCompile(`(?i)^(?:检查|我的)*c(?:oo)?k(?:ie)?(?:状态)*$`)
	// ckHelp are the Yunzai words that send the Cookie binding tutorial.
	ckHelp = regexp.MustCompile(`(?i)^(?:(?:体力|c(?:oo)?k(?:ie)?)帮助|绑定c(?:oo)?k(?:ie)?|c(?:oo)?k(?:ie)?代码)$`)
)

// ckCommand names a CK command word the way the upstream user commands read
// it: any case, cookie for CK, a game name before 我的 and 删除, and 检查,
// 我的 or 状态 around CK for the check.
func ckCommand(word string) string {
	switch {
	case ckMine.MatchString(word):
		return "我的CK"
	case ckRemove.MatchString(word):
		return "删除CK"
	case ckRefresh.MatchString(word):
		return "刷新CK"
	case ckCheck.MatchString(word):
		return "检查CK"
	case ckHelp.MatchString(word):
		return "CK帮助"
	}
	return strings.ToUpper(word)
}

// ckTutorial answers Yunzai's 体力帮助, CK帮助, 绑定CK and CK代码, which
// explain binding a pasted Cookie; accounts here are bound by scanning.
const ckTutorial = "绑定米游社账号：发送“扫码登录”，用米游社扫码并确认，无需获取 Cookie。聊天中不接收 Cookie 原文；发送“账号帮助”查看全部账号命令。"

// deviceHelp is ZZZ-Plugin's 绑定设备帮助.
const deviceHelp = `[绑定设备]
用于尝试解决10041报错等问题（无法100%解决）。
方法一：
1. 使用抓包软件抓取米游社APP的请求
2. 在请求头内找到【x-rpc-device_id】和【x-rpc-device_fp】
3. 自行构造如下格式的信息：
    {"device_id": "x-rpc-device_id的内容", "device_fp": "x-rpc-device_fp的内容"}
4. 给机器人发送“绑定设备”指令
5. 机器人会提示发送设备信息
6. 粘贴自行构造的信息发送
7. 提示绑定成功
--------------------------------
方法二（仅适用于安卓设备）：
1. 在常用米游社的手机上安装 forchannot/get_device_info 的设备信息工具
2. 打开后点击按钮复制
3. 给机器人发送“绑定设备”指令
4. 机器人会提示发送设备信息
5. 粘贴设备信息发送
6. 提示绑定成功
--------------------------------
[解绑设备]
发送“解绑设备”即可`

// dropTask deletes the task a scheduler trigger came from: the plugin keeps
// no scheduled tasks, so it knows no task a trigger can name.
func dropTask(ctx context.Context, event *rayleabot.EventContext) error {
	if id := event.Event.TaskID(); id != "" {
		if _, err := event.Actions().SchedulerDelete(ctx, id); err != nil {
			return err
		}
	}
	return event.Result(map[string]any{"handled": true})
}

// busyText answers a chat command the plugin cannot move to the background.
const busyText = "当前处理中的请求较多，请稍后重试。"

// background moves a chat command that can outlast its event deadline to
// the background. It is false with no error when the plugin already holds the
// host's limit of background events; the command is then answered busyText.
func background(ctx context.Context, event *rayleabot.EventContext) (bool, error) {
	_, err := event.Detach(ctx, map[string]any{"handled": true})
	if isFailure(err, "platform.rate_limited") {
		return false, nil
	}
	return err == nil, err
}
