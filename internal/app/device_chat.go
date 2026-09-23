package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
)

// 绑定设备 in chat follows ZZZ-Plugin: after the command, the owner's next
// private message that holds device information is imported, within two
// minutes, and 取消 ends the wait. Device information is no credential, but
// it is still only taken in private.

type deviceWait struct {
	ref   string
	until time.Time
}

// deviceWaits are the owners 绑定设备 is waiting on, with the account each
// binds.
type deviceWaits struct {
	mu    sync.Mutex
	items map[accounts.Subject]deviceWait
}

func (w *deviceWaits) start(owner accounts.Subject, ref string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if w.items == nil {
		w.items = map[accounts.Subject]deviceWait{}
	}
	for other, wait := range w.items {
		if now.After(wait.until) {
			delete(w.items, other)
		}
	}
	w.items[owner] = deviceWait{ref: ref, until: now.Add(2 * time.Minute)}
}

func (w *deviceWaits) waiting(owner accounts.Subject) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wait, ok := w.items[owner]
	return wait.ref, ok && time.Now().Before(wait.until)
}

func (w *deviceWaits) end(owner accounts.Subject) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.items, owner)
}

var zzzOverseasRegions = []string{"prod_gf_us", "prod_gf_eu", "prod_gf_jp", "prod_gf_sg"}

// deviceAccount is the account 绑定设备 acts on, as ZZZ-Plugin binds the
// account of the current UID: the one holding the owner's current Zenless
// UID, else the owner's only account. The role is the Zenless role, when the
// account has it. caller, when given, must be allowed to use the account.
func (a *App) deviceAccount(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, owner accounts.Subject, caller string) (accounts.Summary, accounts.Role, error) {
	items, err := a.Repo.List(ctx, store, cipher, owner)
	if err != nil {
		return accounts.Summary{}, accounts.Role{}, err
	}
	items = slices.DeleteFunc(items, func(item accounts.Summary) bool { return caller != "" && !slices.Contains(item.AllowedCallers, caller) })
	bindings, err := a.Repo.UIDBindings(ctx, store, cipher, owner)
	if err != nil {
		return accounts.Summary{}, accounts.Role{}, err
	}
	defaults, err := a.Repo.Defaults(ctx, store, cipher, owner)
	if err != nil {
		return accounts.Summary{}, accounts.Role{}, err
	}
	pick := func(item accounts.Summary, role accounts.Role) (accounts.Summary, accounts.Role, error) {
		if slices.Contains(zzzOverseasRegions, role.Region) {
			return item, role, failure("plugin.account_region_unsupported")
		}
		return item, role, nil
	}
	for _, item := range items {
		for _, role := range item.Roles {
			if role.Game != "zzz" {
				continue
			}
			selected := defaults["zzz"]
			if current := bindings["zzz"].Current; current == role.UID || current == "" && selected.AccountRef == item.Ref && selected.RoleRef == role.Ref {
				return pick(item, role)
			}
		}
	}
	switch len(items) {
	case 0:
		return accounts.Summary{}, accounts.Role{}, failure("plugin.account_not_found")
	case 1:
		for _, role := range items[0].Roles {
			if role.Game == "zzz" {
				return pick(items[0], role)
			}
		}
		return items[0], accounts.Role{}, nil
	}
	return accounts.Summary{}, accounts.Role{}, failure("plugin.account_device_ambiguous")
}

// deviceService answers device.bind, device.unbind and device.default for a
// game plugin's 绑定设备, 解绑设备 and 设置默认设备.
func (a *App) deviceService(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, owner accounts.Subject, request rayleabot.ServiceRequest, games, superAdmins []string) (map[string]any, error) {
	private := request.Origin.EventType == "message.private" && request.Origin.Target != nil && request.Origin.Target.Type == "private"
	if !slices.Contains(games, "zzz") || !strings.HasPrefix(request.Origin.EventType, "message.") || request.Method != "device.unbind" && !private {
		return nil, failure("plugin.account_operation_denied")
	}
	if request.Method == "device.default" {
		// The host's super administrators are OneBot11 accounts.
		if owner.SourceProtocol != "onebot11" || !slices.Contains(superAdmins, owner.ActorID) {
			return nil, failure("plugin.account_subject_denied")
		}
		a.devices.start(owner, defaultDeviceWait)
		return map[string]any{}, nil
	}
	item, role, err := a.deviceAccount(ctx, store, cipher, owner, request.CallerPluginID)
	if err != nil {
		return nil, err
	}
	if request.Method == "device.bind" {
		a.devices.start(owner, item.Ref)
	} else if err := a.resetDevice(ctx, store, cipher, item.Ref); err != nil {
		return nil, err
	}
	return map[string]any{"uid": role.UID, "account_label": item.AccountLabel}, nil
}

// deviceCommand answers this plugin's own 绑定设备 and 解绑设备.
func (a *App) deviceCommand(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, owner accounts.Subject, command string) error {
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return event.SendText(deviceReply(err))
	}
	item, role, err := a.deviceAccount(ctx, store, cipher, owner, "")
	if err != nil {
		return event.SendText(deviceReply(err))
	}
	if command == "解绑设备" {
		if err := a.resetDevice(ctx, store, cipher, item.Ref); err != nil {
			return event.SendText(deviceReply(err))
		}
		return event.SendText("解绑设备成功")
	}
	a.devices.start(owner, item.Ref)
	return event.SendText(DevicePrompt(role.UID, item.AccountLabel))
}

// DevicePrompt is ZZZ-Plugin's reply to 绑定设备, without its advice to send
// privately, which is required here.
func DevicePrompt(uid, label string) string {
	if uid == "" {
		return fmt.Sprintf("为账号 %s 绑定设备，请发送设备信息，或者发送“取消”取消绑定", label)
	}
	return fmt.Sprintf("为UID %s绑定设备，请发送设备信息，或者发送“取消”取消绑定", uid)
}

// deviceReply words a failure of 绑定设备 or 解绑设备 as ZZZ-Plugin does
// where it has a reply.
func deviceReply(err error) string {
	switch publicError(err).Code {
	case "plugin.account_region_unsupported":
		return "国际服不需要绑定设备"
	case "plugin.account_not_found":
		return "还没有绑定米游社账号，请发送“扫码登录”。"
	case "plugin.account_input_invalid":
		return "设备信息格式错误"
	}
	return publicError(err).Message
}

// deviceMessage imports the device information an owner sends after
// 绑定设备; handled is false for other messages, which leave the wait
// running.
func (a *App) deviceMessage(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore) (bool, error) {
	if event.Event.Target.Type != "private" {
		return false, nil
	}
	owner := accounts.Subject{SourceProtocol: event.Event.SourceProtocol, SourceAdapter: event.Event.SourceAdapter, BotID: event.Bot.ID, ActorID: event.Event.Actor.ID}
	ref, ok := a.devices.waiting(owner)
	if !ok {
		return false, nil
	}
	text := strings.TrimSpace(event.Event.Message.PlainText)
	if text == "取消" {
		a.devices.end(owner)
		return true, event.SendText("已取消")
	}
	if !strings.HasPrefix(text, "{") {
		return false, nil
	}
	a.devices.end(owner)
	if ref == defaultDeviceWait {
		if err := a.saveDefaultDevice(ctx, store, []byte(text)); err != nil {
			return true, event.SendText(deviceReply(err))
		}
		return true, event.SendText("默认设备信息设置成功")
	}
	if err := a.saveDevice(ctx, store, ref, []byte(text)); err != nil {
		return true, event.SendText(deviceReply(err))
	}
	return true, event.SendText("绑定设备成功")
}

// saveDevice imports device information into an account; Android device
// information is registered for a fingerprint at once, as ZZZ-Plugin does.
func (a *App) saveDevice(ctx context.Context, store accounts.SecretStore, ref string, raw []byte) error {
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return err
	}
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return err
	}
	defer release()
	record, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return err
	}
	tokens, err := importDevice(raw, record.Tokens)
	clear(raw)
	if err != nil {
		return err
	}
	if tokens.Device != nil {
		if tokens, err = a.API.Fingerprint(ctx, tokens); err != nil {
			return err
		}
	}
	record.Tokens = tokens
	return a.Repo.Update(ctx, store, cipher, record, record.Revision)
}

// defaultDeviceWait marks the wait 设置默认设备 starts; account references
// never take this form.
const defaultDeviceWait = "*default"

// defaultDeviceSetting keeps the default device, encrypted like the
// accounts.
const defaultDeviceSetting = "device.default"

// saveDefaultDevice keeps the Android device information ZZZ-Plugin's
// 设置默认设备 takes, with the same required fields, for accounts without
// a device of their own.
func (a *App) saveDefaultDevice(ctx context.Context, store accounts.SecretStore, raw []byte) error {
	defer clear(raw)
	var device accounts.Device
	if len(raw) > 8192 || json.Unmarshal(raw, &device) != nil {
		return failure("plugin.account_input_invalid")
	}
	for _, value := range []string{device.Name, device.Board, device.Model, device.AndroidVersion, device.Fingerprint, device.Product} {
		if !devicePattern.MatchString(value) {
			return failure("plugin.account_input_invalid")
		}
	}
	// Each account registers the device under its own identifier, as
	// upstream fills the advertising ID from the user's device ID.
	device.OAID = ""
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return err
	}
	a.defaultDev.mu.Lock()
	defer a.defaultDev.mu.Unlock()
	if err := a.Repo.WriteSetting(ctx, store, cipher, defaultDeviceSetting, device); err != nil {
		a.defaultDev.loaded = false
		return err
	}
	a.defaultDev.device, a.defaultDev.loaded = &device, true
	return nil
}

// defaultDeviceCache keeps the default device once read, so requests do not
// read it from the secret store each time; saving replaces it, and opening
// or locking the vault forgets it.
type defaultDeviceCache struct {
	mu     sync.Mutex
	loaded bool
	device *accounts.Device
}

func (c *defaultDeviceCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaded, c.device = false, nil
}

// lendDefaultDevice gives tokens without a device of their own the default
// device, as ZZZ-Plugin's device.yaml serves users who bound none. Callers
// must not save the lent device into the account.
func (a *App) lendDefaultDevice(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, tokens accounts.Tokens) (accounts.Tokens, error) {
	if tokens.Device != nil || tokens.DeviceFPImported {
		return tokens, nil
	}
	a.defaultDev.mu.Lock()
	defer a.defaultDev.mu.Unlock()
	if !a.defaultDev.loaded {
		var device accounts.Device
		exists, err := a.Repo.ReadSetting(ctx, store, cipher, defaultDeviceSetting, &device)
		if err != nil {
			return tokens, err
		}
		a.defaultDev.device, a.defaultDev.loaded = nil, true
		if exists && device.Model != "" {
			a.defaultDev.device = &device
		}
	}
	if a.defaultDev.device == nil {
		return tokens, nil
	}
	device := *a.defaultDev.device
	device.OAID = tokens.DeviceID
	tokens.Device = &device
	return tokens, nil
}

// resetDevice gives an account a generated device again.
func (a *App) resetDevice(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, ref string) error {
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return err
	}
	defer release()
	record, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return err
	}
	resetTokens(&record.Tokens, mihoyo.RandomDeviceID())
	err = a.Repo.Update(ctx, store, cipher, record, record.Revision)
	if errors.Is(err, accounts.ErrConflict) {
		return failure("plugin.account_changed")
	}
	return err
}

func resetTokens(tokens *accounts.Tokens, id string) {
	tokens.DeviceID, tokens.DeviceFP, tokens.DeviceFPExpiresAtMS = id, "", 0
	tokens.Device, tokens.DeviceFPImported = nil, false
}
