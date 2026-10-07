package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/mihoyo-accounts/internal/pluginmeta"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

type API interface {
	CreateQR(context.Context) (mihoyo.QR, error)
	PollQR(context.Context, mihoyo.QR) (mihoyo.Confirmation, error)
	Refresh(context.Context, accounts.Tokens) (accounts.Tokens, error)
	RolesForGame(context.Context, accounts.Tokens, string) ([]accounts.Role, error)
	Fingerprint(context.Context, accounts.Tokens) (accounts.Tokens, error)
	Execute(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error)
}

type Policy struct {
	AllowedPlugins []Caller `json:"allowed_plugins"`
	// SuperAdmins are the host's super administrators, whom only
	// 设置默认设备 needs; they are never read from the configuration.
	SuperAdmins []string `json:"-"`
}
type Caller struct {
	PluginID string   `json:"plugin_id"`
	Games    []string `json:"games"`
}

func (p Policy) Games(id string) []string {
	for _, caller := range p.AllowedPlugins {
		if caller.PluginID == id {
			return caller.Games
		}
	}
	return nil
}
func (p Policy) IDs() []string {
	result := []string{}
	for _, caller := range p.AllowedPlugins {
		if caller.PluginID != "" && len(caller.Games) > 0 && !slices.Contains(result, caller.PluginID) {
			result = append(result, caller.PluginID)
		}
	}
	return result
}

type App struct {
	Manifest     pluginmeta.Manifest
	imports      cookieImports
	associations associationRequests
	batches      accountBatches
	Vault        *vault.Manager
	Repo         accounts.Repository
	API          API
	ArkHTTP      HTTPDoer
	ctx          context.Context
	cancel       context.CancelFunc
	vaultGate    sync.RWMutex
	locksMu      sync.Mutex
	locks        map[string]*accountLock
	devices      deviceWaits
	defaultDev   defaultDeviceCache
	jobsMu       sync.Mutex
	jobs         map[string]*login
	owners       map[accounts.Subject]string
	refreshMu    sync.Mutex
	refreshes    map[string]*refreshFlight
	pollInterval time.Duration
	// recallDelay is how long a 扫码登录 code stays in the chat.
	recallDelay time.Duration
}
type accountLock struct {
	permit chan struct{}
	users  int
}

func New(directory string, api API) (*App, error) {
	manager, err := vault.NewManager(directory)
	if err != nil {
		return nil, err
	}
	if api == nil {
		api = mihoyo.New(nil, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &App{Vault: manager, API: api, ctx: ctx, cancel: cancel, locks: map[string]*accountLock{}, jobs: map[string]*login{}, owners: map[accounts.Subject]string{}, refreshes: map[string]*refreshFlight{}, pollInterval: 5 * time.Second, recallDelay: 30 * time.Second}, nil
}
func (a *App) Close() {
	a.cancel()
	a.cancelLogins()
	a.batches.clear()
	a.associations.clear()
	a.imports.clear()
}

func (a *App) acquire(ctx context.Context, ref string) (func(), error) {
	a.locksMu.Lock()
	lock := a.locks[ref]
	if lock == nil {
		lock = &accountLock{permit: make(chan struct{}, 1)}
		a.locks[ref] = lock
	}
	lock.users++
	a.locksMu.Unlock()
	releaseRef := func() {
		a.locksMu.Lock()
		defer a.locksMu.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(a.locks, ref)
		}
	}
	select {
	case lock.permit <- struct{}{}:
		return func() { <-lock.permit; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}

func decode(value any, out any) error {
	raw, err := json.Marshal(value)
	if err != nil || json.Unmarshal(raw, out) != nil {
		return failure("plugin.account_input_invalid")
	}
	return nil
}
func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}
func stringValue(value any) string { result, _ := value.(string); return result }

func failure(code string) *rayleabot.ActionError {
	return &rayleabot.ActionError{Code: code, Message: message(code)}
}

func isFailure(err error, code string) bool {
	var actual *rayleabot.ActionError
	return errors.As(err, &actual) && actual.Code == code
}

func publicError(err error) *rayleabot.ActionError {
	var upstream *mihoyo.Error
	var typed *rayleabot.ActionError
	switch {
	case errors.As(err, &upstream):
		msg := message(upstream.Code)
		if upstream.RetCode != 0 {
			msg += fmt.Sprintf("（错误码 %d）", upstream.RetCode)
		}
		return &rayleabot.ActionError{Code: upstream.Code, Message: msg, Details: map[string]any{"operation": upstream.Operation, "retcode": upstream.RetCode, "http_status": upstream.HTTPStatus, "reason": upstream.Reason}}
	case errors.As(err, &typed):
		if typed.Code == "plugin.vault_locked" && typed.Details["reason"] == "native_key_unavailable" {
			return nativeKeyUnavailableError()
		}
		return failure(typed.Code)
	case errors.Is(err, vault.ErrNativeKeyUnavailable):
		return nativeKeyUnavailableError()
	case errors.Is(err, vault.ErrLocked):
		return failure("plugin.vault_locked")
	case errors.Is(err, vault.ErrMissingKey):
		return failure("plugin.vault_missing")
	case errors.Is(err, vault.ErrPassword):
		return failure("plugin.vault_password_invalid")
	case errors.Is(err, vault.ErrNativeUnavailable):
		return failure("plugin.vault_native_unavailable")
	case errors.Is(err, vault.ErrCiphertext):
		return failure("plugin.vault_ciphertext_invalid")
	case errors.Is(err, accounts.ErrNotFound):
		return failure("plugin.account_not_found")
	case errors.Is(err, accounts.ErrConflict):
		return failure("plugin.account_changed")
	case errors.Is(err, accounts.ErrAssociation):
		return failure("plugin.account_association_invalid")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return failure("plugin.account_operation_canceled")
	default:
		return failure("plugin.account_operation_failed")
	}
}

func nativeKeyUnavailableError() *rayleabot.ActionError {
	return &rayleabot.ActionError{
		Code:    "plugin.vault_locked",
		Message: "当前系统用户无法解密 Windows 保护的账号库，请管理员在原 Windows 用户环境解锁、切换为恢复口令后迁移密钥。",
		Details: map[string]any{"reason": "native_key_unavailable"},
	}
}

func message(code string) string {
	switch code {
	case "plugin.account_overseas_missing":
		return "Cookie 导入流程不存在或已过期，请重新加密导入。"
	case "plugin.account_overseas_sequence":
		return "Cookie 导入进度已变化，请使用当前步骤继续。"
	case "plugin.account_binding_limit":
		return "每个游戏最多绑定 20 个 UID，请先删除不用的 UID。"
	case "plugin.account_association_invalid":
		return "关联存在冲突：不允许环路、链式共享或同一附属身份连接多个主身份。"
	case "plugin.account_association_missing":
		return "关联申请不存在、已取消或超过5分钟，请重新发起。"
	case "plugin.account_association_limit":
		return "待确认的关联申请已达上限，请完成或取消其他申请。"
	case "plugin.account_redeem_unavailable":
		return "固定参考未提供可用的国服网页兑换接口，请在官方游戏内使用兑换码。"
	case "plugin.upstream_billing_unavailable":
		return "官方客服记录暂不接受此授权方式；普通游戏 CK 已保留，请稍后重试。"
	case "plugin.upstream_redeem_unavailable":
		return "官方暂未接受兑换请求，请在官方页面核对账号、区服及兑换码状态。"
	case "plugin.redeem_expired":
		return "兑换码已过期。"
	case "plugin.redeem_invalid":
		return "兑换码无效、未激活或已达使用上限。"
	case "plugin.redeem_cooldown":
		return "兑换操作过于频繁，请稍后再试。"
	case "plugin.redeem_level_low":
		return "游戏等级尚未达到兑换要求。"
	case "plugin.upstream_community_unavailable":
		return "社区认证暂不可用，请刷新账号凭据或在官方米游社完成验证；游戏 CK 状态已保留。"
	case "plugin.account_cloud_unavailable":
		return "此账号尚未配置相应的云游戏授权，请在账号管理中设置加密的云授权。"
	case "plugin.upstream_cloud_unavailable":
		return "云游戏授权暂不可用，请在官方云客户端检查授权或防沉迷状态；普通游戏 CK 已保留。"
	case "plugin.account_refresh_unavailable":
		return "此账号缺少可用的刷新材料，现有登录状态已保留；请在账号页重新授权。"
	case "plugin.upstream_birthday_unavailable":
		return "留影叙佳期活动认证暂不可用，请在官方活动页面核对；普通游戏登录状态已保留。"
	case "plugin.account_auth_material_missing":
		return "此业务需要海外 CookieToken v2 和 MID；普通账号登录状态已保留，请在账号页补充授权。"
	case "plugin.account_batch_missing":
		return "批量任务不存在或已过期，请重新选择账号开始。"
	case "plugin.account_batch_limit":
		return "批量任务或统计范围已达上限，请完成已有任务或缩小范围。"
	case "plugin.account_batch_sequence":
		return "批量任务进度已变化，请使用最新进度继续。"
	case "plugin.account_input_invalid":
		return "输入不完整或格式不正确。"
	case "plugin.account_write_confirmation_required":
		return "此操作需要明确确认后执行。"
	case "plugin.account_public_unavailable":
		return "暂无已获本人授权且有可用额度的公共查询账号，请稍后重试或使用自己的账号查询。"
	case "plugin.upstream_first_sign_required":
		return "请先在米游社官方应用完成首次签到，再使用此功能。"
	case "plugin.upstream_sign_unavailable":
		return "官方签到暂不接受此活动认证，账号绑定已保留。可稍后重试或在官方应用签到。"
	case "plugin.account_delegation_denied":
		return "定时委托不存在、已到期或与当前任务不匹配，请重新开启提醒。"
	case "plugin.account_delegation_throttled":
		return "定时委托尚未到下一次可查询时间。"
	case "plugin.account_not_found":
		return "账号不存在或已移除。"
	case "plugin.account_device_ambiguous":
		return "有多个米游社账号，请先切换到要绑定设备的绝区零 UID。"
	case "plugin.account_ark_unconfigured":
		return "尚未配置 ark 授权令牌，请机器人管理员在账号插件管理页配置。"
	case "plugin.account_ark_rejected":
		return "ark 拒绝了授权令牌，请检查令牌及权限。"
	case "plugin.account_ark_rate_limited":
		return "ark 查询额度已用尽或请求过于频繁，请稍后再试。"
	case "plugin.account_ark_unavailable":
		return "ark 云服务暂不可用或返回了无法读取的数据。"
	case "plugin.account_changed":
		return "账号已变更，请刷新页面后重试。"
	case "plugin.account_caller_denied":
		return "此插件未获准使用账号服务。"
	case "plugin.account_subject_denied":
		return "此请求无权使用该账号。"
	case "plugin.account_role_denied":
		return "所选角色不属于此账号或此游戏。"
	case "plugin.account_operation_denied":
		return "该游戏操作尚未开放。"
	case "plugin.account_region_unsupported":
		return "此业务暂未适配所选账号区服。"
	case "plugin.upstream_gacha_expired":
		return "抽卡记录授权暂不可用，请稍后继续同步。"
	case "plugin.upstream_gacha_unavailable":
		return "星铁官方暂不接受此抽卡授权方式：请在游戏内打开跃迁记录，把链接发给机器人，或导入 UIGF、SRGF 记录。现有 CK 仍可用于其他游戏查询。"
	case "plugin.account_operation_canceled":
		return "操作已取消或超时，可稍后重试。"
	case "plugin.vault_locked":
		return "账号库已锁定，请机器人管理员在账号管理页解锁。"
	case "plugin.vault_missing":
		return "缺少原密钥文件，请恢复密钥后重试。"
	case "plugin.vault_ciphertext_invalid":
		return "账号密文无法读取，请核对密钥与备份。"
	case "plugin.vault_uninitialized":
		return "账号库尚未初始化，请管理员在米游社账号管理页完成初始化。"
	case "plugin.vault_password_invalid":
		return "口令需为 12 到 128 字节。"
	case "plugin.vault_native_unavailable":
		return "当前系统不支持用系统账户保护密钥，请使用恢复口令。"
	case "plugin.login_not_found":
		return "登录任务已失效，请重新扫码。"
	case "plugin.login_limit":
		return "当前登录任务较多，请稍后重试。"
	case "plugin.login_processing_timeout":
		return "扫码已确认，但账号处理超时，请重新扫码。"
	case "plugin.upstream_rejected":
		return "米游社拒绝了本次请求，请稍后重试。"
	case "plugin.upstream_auth_invalid":
		return "米游社凭据已失效，请重新扫码。"
	case "plugin.upstream_challenge_required":
		return "米游社要求完成验证，请在官方应用处理后重试。"
	case "plugin.upstream_device_required":
		return "米游社账号异常，暂时无法查询，请发送“绑定设备帮助”查看如何绑定设备。"
	case "plugin.upstream_rate_limited":
		return "米游社请求过于频繁，请稍后重试。"
	case "plugin.upstream_unavailable":
		return "暂时无法连接米游社，请稍后重试。"
	case "plugin.upstream_response_invalid":
		return "米游社返回的数据暂不兼容，请稍后重试。"
	case "plugin.upstream_request_invalid":
		return "查询参数无效。"
	default:
		return "操作未完成，请稍后重试。"
	}
}
