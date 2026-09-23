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
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
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

func (a *App) Handle(ctx context.Context, event *rayleabot.EventContext) error {
	// Ordinary event deadlines are owned by the host; keep local HTTP work
	// shorter than the standard host event window as well.
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
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
		return a.notifyLogins(ctx, event, store, policy)
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
	if event.Event.Target.Type != "private" {
		return event.SendText("请私聊机器人使用账号命令。发送“扫码登录”开始绑定。")
	}
	owner := accounts.Subject{SourceProtocol: event.Event.SourceProtocol, SourceAdapter: event.Event.SourceAdapter, BotID: event.Bot.ID, ActorID: event.Event.Actor.ID}
	if owner.Validate() != nil {
		return event.SendText(message("plugin.account_subject_denied"))
	}
	if slices.Contains([]string{"关联账号", "申请关联", "关联进度", "完成关联", "取消关联", "关联列表", "解除关联"}, command) {
		return a.associationCommand(ctx, event, store, owner, command)
	}
	if event.Event.Command() == "扫码登录" {
		// Only an administrator initializes the account store and chooses how its
		// key is protected; a chat user must not decide that by logging in first.
		if a.Vault.State() == "uninitialized" {
			return event.SendText(message("plugin.vault_uninitialized"))
		}
		result, err := a.Manage(ctx, store, policy, "login.start", map[string]any{"subject": owner})
		if err != nil {
			return event.SendText(publicError(err).Message)
		}
		view := result["login"].(LoginView)
		job, _ := a.getLogin(view.Ref)
		job.mu.Lock()
		job.chat = true
		job.mu.Unlock()
		if _, err := event.Actions().SchedulerCreate(ctx, rayleabot.SchedulerCreateRequest{TaskID: loginTask, Cron: "* * * * *", LogLabel: "米游社扫码结果", Payload: map[string]any{"kind": "login_notifications"}}); err != nil {
			_, _ = a.cancelLogin(view.Ref)
			return event.SendText("扫码任务未能创建，请稍后重试。")
		}
		return event.Send("private", owner.ActorID, rayleabot.Text("使用米游社扫码并确认。登录结果会在一分钟内发送，也可发送“登录状态”立即查看。"), rayleabot.Image("base64://"+strings.TrimPrefix(view.QRCode, "data:image/png;base64,")))
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
	if _, err := a.Manage(ctx, store, policy, action, map[string]any{"account_ref": items[index-1].Ref, "subject": owner}); err != nil {
		return event.SendText(publicError(err).Message)
	}
	if action == "accounts.remove" {
		return event.SendText("账号已移除，游戏插件无法继续使用。")
	}
	if action == "accounts.check" {
		return event.SendText("账号角色接口检查通过，凭据未刷新。具体游戏的设备验证状态需单独查询。")
	}
	return event.SendText("账号凭据已刷新。")
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
const ckTutorial = "绑定米游社账号：私聊机器人发送“扫码登录”，用米游社扫码并确认，无需获取 Cookie。聊天中不接收 Cookie 原文；发送“账号帮助”查看全部账号命令。"

// deviceHelp is ZZZ-Plugin's 绑定设备帮助; the information is sent in private.
const deviceHelp = `[绑定设备]
用于尝试解决10041报错等问题（无法100%解决）。
方法一：
1. 使用抓包软件抓取米游社APP的请求
2. 在请求头内找到【x-rpc-device_id】和【x-rpc-device_fp】
3. 自行构造如下格式的信息：
    {"device_id": "x-rpc-device_id的内容", "device_fp": "x-rpc-device_fp的内容"}
4. 私聊给机器人发送“绑定设备”指令
5. 机器人会提示发送设备信息
6. 粘贴自行构造的信息发送
7. 提示绑定成功
--------------------------------
方法二（仅适用于安卓设备）：
1. 在常用米游社的手机上安装 forchannot/get_device_info 的设备信息工具
2. 打开后点击按钮复制
3. 私聊给机器人发送“绑定设备”指令
4. 机器人会提示发送设备信息
5. 粘贴设备信息发送
6. 提示绑定成功
--------------------------------
[解绑设备]
发送“解绑设备”即可`

// loginTask sends chat logins their result once a minute while any is open.
const loginTask = "mihoyo.accounts.login"

func (a *App) notifyLogins(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, policy Policy) error {
	a.jobsMu.Lock()
	jobs := make([]*login, 0, len(a.jobs))
	for _, s := range a.jobs {
		jobs = append(jobs, s)
	}
	a.jobsMu.Unlock()
	for _, s := range jobs {
		s.mu.Lock()
		notify := s.chat && !s.notified
		ref, owner := s.view.Ref, s.owner
		s.mu.Unlock()
		if !notify {
			continue
		}
		result, err := a.Manage(ctx, store, policy, "login.poll", map[string]any{"login_ref": ref})
		if err != nil {
			continue
		}
		view := result["login"].(LoginView)
		if !terminal(view.State) {
			continue
		}
		if view.State == "failed" && view.Failure != nil {
			_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: loginText(view), Fields: map[string]any{"code": view.ErrorCode, "login_stage": view.Failure.Stage, "operation": view.Failure.Operation, "retcode": view.Failure.RetCode, "http_status": view.Failure.HTTPStatus, "reason": view.Failure.Reason}})
		}
		s.mu.Lock()
		if s.notified {
			s.mu.Unlock()
			continue
		}
		s.notified = true
		s.mu.Unlock()
		_, err = event.Actions().MessageSend(ctx, rayleabot.MessageSendRequest{SourceAdapter: owner.SourceAdapter, SourceProtocol: owner.SourceProtocol, TargetType: "private", TargetID: owner.ActorID, Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(loginText(view))}}})
		if err != nil {
			s.mu.Lock()
			s.notified = false
			s.mu.Unlock()
			return event.Fail("plugin.account_notification_failed", "扫码结果通知未发送，将在下次任务中重试。")
		}
	}
	// With every chat login answered, the per-minute task is no longer needed;
	// the next 扫码登录 creates it again.
	a.jobsMu.Lock()
	pending := false
	for _, s := range a.jobs {
		s.mu.Lock()
		pending = pending || s.chat && !s.notified
		s.mu.Unlock()
	}
	a.jobsMu.Unlock()
	if !pending {
		_, _ = event.Actions().SchedulerDelete(ctx, loginTask)
	}
	return event.Result(map[string]any{"handled": true})
}
