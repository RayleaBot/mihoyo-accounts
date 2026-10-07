package app

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func management(origin rayleabot.ServiceOrigin) bool {
	return origin.SourceProtocol == "management" && origin.SourceAdapter == "management.ui" && origin.EventType == "management.action"
}

// superAdmin is whether a chat message came from one of the host's super
// administrators, who are OneBot11 accounts.
func superAdmin(origin rayleabot.ServiceOrigin, admins []string) bool {
	return strings.HasPrefix(origin.EventType, "message.") && origin.SourceProtocol == "onebot11" && origin.Actor != nil && slices.Contains(admins, origin.Actor.ID)
}

func originSubject(origin rayleabot.ServiceOrigin) accounts.Subject {
	actor := ""
	if origin.Actor != nil {
		actor = origin.Actor.ID
	}
	return accounts.Subject{SourceProtocol: origin.SourceProtocol, SourceAdapter: origin.SourceAdapter, BotID: origin.BotID, ActorID: actor}
}

func (a *App) Service(ctx context.Context, store accounts.SecretStore, policy Policy, request rayleabot.ServiceRequest) (result map[string]any, err error) {
	defer func() {
		if err != nil {
			err = publicError(err)
		}
	}()
	games := policy.Games(request.CallerPluginID)
	if len(games) == 0 {
		return nil, failure("plugin.account_caller_denied")
	}
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	owner := originSubject(request.Origin)
	admin := management(request.Origin)
	scheduled := request.Origin.EventType == "scheduler.trigger" && request.Origin.SourceProtocol == "scheduler" && request.Origin.SourceAdapter == "scheduler.internal" && request.Origin.TaskID != "" && request.Method == "execute"
	if !admin && !scheduled && owner.Validate() != nil {
		return nil, failure("plugin.account_subject_denied")
	}
	// Chat callers cannot replace the host-derived subject. Management calls
	// may filter by an explicit subject; the host authenticated their session.
	if raw, ok := request.Params["subject"]; ok {
		if !admin {
			return nil, failure("plugin.account_subject_denied")
		}
		if decode(raw, &owner) != nil || owner.Validate() != nil {
			return nil, failure("plugin.account_input_invalid")
		}
	}
	if request.Method == "public.execute" {
		return a.publicExecute(ctx, store, cipher, request, games)
	}
	if request.Method == "ark.request" {
		return a.arkRequest(ctx, store, cipher, request, games)
	}
	if request.Method == "ark.status" {
		return a.arkStatus(ctx, store, cipher, games)
	}
	if request.Method == "uids.bind" || request.Method == "uids.remove" || request.Method == "uids.use" {
		return a.changeUIDBinding(ctx, store, cipher, owner, request, games)
	}
	if request.Method == "uids.current" {
		return a.currentUID(ctx, store, cipher, request, games)
	}
	if request.Method == "device.bind" || request.Method == "device.unbind" || request.Method == "device.default" {
		return a.deviceService(ctx, store, cipher, owner, request, games, policy.SuperAdmins)
	}
	if request.Method == "list" {
		var items []accounts.Summary
		var next *int
		// all lists every account by page for a super administrator's
		// 原石任务, which saves each account's ledger as upstream's master does.
		all := request.Params["all"] == true
		if all && !superAdmin(request.Origin, policy.SuperAdmins) {
			return nil, failure("plugin.account_subject_denied")
		}
		if owner.Validate() == nil && !all {
			items, err = a.Repo.List(ctx, store, cipher, owner)
		} else {
			var input struct {
				Page int  `json:"page"`
				All  bool `json:"all"`
			}
			if decode(request.Params, &input) != nil {
				return nil, failure("plugin.account_input_invalid")
			}
			page, listErr := a.Repo.ListPage(ctx, store, cipher, input.Page)
			err, items, next = listErr, page.Items, page.NextPage
		}
		if err != nil {
			return nil, err
		}
		visible := []accounts.Summary{}
		defaults := map[string]accounts.Selection{}
		if owner.Validate() == nil {
			defaults, err = a.Repo.Defaults(ctx, store, cipher, owner)
			if err != nil {
				return nil, err
			}
		}
		for _, item := range items {
			if !slices.Contains(item.AllowedCallers, request.CallerPluginID) {
				continue
			}
			item.Roles = slices.DeleteFunc(item.Roles, func(role accounts.Role) bool { return !slices.Contains(games, role.Game) })
			item.AllowedCallers = nil
			visible = append(visible, item)
		}
		for game, selected := range defaults {
			valid := false
			for _, item := range visible {
				if item.Ref == selected.AccountRef {
					for _, role := range item.Roles {
						if role.Ref == selected.RoleRef && role.Game == game {
							valid = true
						}
					}
				}
			}
			if !valid {
				delete(defaults, game)
			}
		}
		bindings := map[string]accounts.UIDBinding{}
		if owner.Validate() == nil {
			all, err := a.Repo.UIDBindings(ctx, store, cipher, owner)
			if err != nil {
				return nil, err
			}
			for game, binding := range all {
				if slices.Contains(games, game) {
					bindings[game] = binding
				}
			}
		}
		return map[string]any{"items": visible, "next_page": next, "default_roles": defaults, "uid_bindings": bindings}, nil
	}
	ref := stringValue(request.Params["account_ref"])
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer release()
	// Read again after admission so a queued request observes revocation,
	// deletion and re-login before any authenticated request leaves the plugin.
	record, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(record.AllowedCallers, request.CallerPluginID) {
		return nil, failure("plugin.account_caller_denied")
	}
	// A super administrator's 原石任务 reads every account's ledger.
	ledger := request.Method == "execute" && strings.HasSuffix(stringValue(request.Params["operation"]), ".monthly") && superAdmin(request.Origin, policy.SuperAdmins)
	if !scheduled && !ledger && (!admin || owner.Validate() == nil) {
		allowed, err := a.Repo.CanAccess(ctx, store, cipher, record.Owner, owner)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, failure("plugin.account_subject_denied")
		}
	}
	if request.Method == "delegation.create" || request.Method == "delegation.revoke" {
		return a.changeDelegation(ctx, store, cipher, record, request, games)
	}
	if request.Method == "roles" {
		game := stringValue(request.Params["game"])
		roles := []accounts.Role{}
		for _, role := range record.Roles {
			if slices.Contains(games, role.Game) && (game == "" || game == role.Game) {
				roles = append(roles, role)
			}
		}
		return map[string]any{"account_ref": ref, "roles": roles}, nil
	}
	if request.Method == "select" {
		roleRef := stringValue(request.Params["role_ref"])
		for _, role := range record.Roles {
			if role.Ref == roleRef && slices.Contains(games, role.Game) {
				selectedOwner := owner
				if selectedOwner.Validate() != nil {
					selectedOwner = record.Owner
				}
				selection, err := a.Repo.Select(ctx, store, cipher, selectedOwner, ref, roleRef)
				return map[string]any{"game": role.Game, "selection": selection}, err
			}
		}
		return nil, failure("plugin.account_role_denied")
	}
	// gacha.link gives ZZZ-Plugin's 获取抽卡链接 the owner's own gacha link,
	// only in a private chat. An authkey only reads gacha records for a day
	// and is not an account credential; this is the one way it leaves here.
	if request.Method == "gacha.link" {
		if request.Origin.EventType != "message.private" || request.Origin.Target == nil || request.Origin.Target.Type != "private" || !slices.Contains(games, "zzz") {
			return nil, failure("plugin.account_operation_denied")
		}
		roleRef := stringValue(request.Params["role_ref"])
		for _, role := range record.Roles {
			if role.Ref == roleRef && role.Game == "zzz" {
				if record.Status == "invalid" {
					return nil, failure("plugin.upstream_auth_invalid")
				}
				tokens, err := a.lendDefaultDevice(ctx, store, cipher, record.Tokens)
				if err != nil {
					return nil, err
				}
				return a.API.Execute(ctx, tokens, role, mihoyo.ZZZGachaLink, nil)
			}
		}
		return nil, failure("plugin.account_role_denied")
	}
	if request.Method != "execute" {
		return nil, failure("plugin.account_operation_denied")
	}
	name, roleRef := stringValue(request.Params["operation"]), stringValue(request.Params["role_ref"])
	game := mihoyo.OperationGame(name)
	if game == "" || !slices.Contains(games, game) {
		return nil, failure("plugin.account_operation_denied")
	}
	var role accounts.Role
	for _, found := range record.Roles {
		if found.Ref == roleRef && found.Game == game {
			role = found
			break
		}
	}
	if mihoyo.OperationAccountScoped(name) && roleRef == "" {
		role = accounts.Role{Game: game}
	} else if role.Ref == "" {
		return nil, failure("plugin.account_role_denied")
	}
	if mihoyo.OperationMutates(name) && request.Params["write_confirmed"] != true {
		return nil, failure("plugin.account_write_confirmation_required")
	}
	if strings.HasSuffix(name, ".cloud_status") {
		if scheduled {
			return nil, failure("plugin.account_delegation_denied")
		}
		data := record.CloudStates[game]
		if data == nil {
			data = map[string]any{"checked": false}
		}
		clean, err := mihoyo.CloudStatusData(record.Tokens, data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"operation": name, "role": role, "data": clean, "fetched_at_ms": time.Now().UnixMilli()}, nil
	}
	if record.Status == "invalid" && !strings.Contains(name, ".cloud_") {
		// An account whose credentials were refused reads again only once
		// its web tokens are re-issued from the SToken.
		if !record.Summary().CanRefresh {
			return nil, failure("plugin.upstream_auth_invalid")
		}
		if err := a.refreshCredentials(ctx, store, cipher, &record, false); err != nil {
			return nil, err
		}
	}
	if scheduled {
		if err := a.admitDelegation(ctx, store, cipher, &record, request); err != nil {
			return nil, err
		}
	}
	data, err := a.executeRole(ctx, store, cipher, &record, role, name, object(request.Params["input"]))
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, ".cloud_sign") {
		if record.CloudStates == nil {
			record.CloudStates = map[string]map[string]any{}
		}
		data["checked_at_ms"] = time.Now().UnixMilli()
		record.CloudStates[game] = data
		if err := a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
			return nil, err
		}
	}
	return map[string]any{"operation": name, "role": role, "data": data, "fetched_at_ms": time.Now().UnixMilli()}, nil
}

// executeRole sends one business request for a role of a saved account the
// caller holds the lock of. It registers the device fingerprint when there is
// no current one, lends the default device to an account without its own,
// renews a rejected cached fingerprint and retries a read once, re-issues
// refused web tokens from the SToken and retries once, and marks the account
// invalid when the credentials stay refused; record follows each save.
func (a *App) executeRole(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, record *accounts.Record, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	refreshFingerprint := func() error {
		lent, err := a.lendDefaultDevice(ctx, store, cipher, record.Tokens)
		if err != nil {
			return err
		}
		tokens, err := a.API.Fingerprint(ctx, lent)
		if err != nil {
			return err
		}
		// A lent default device is registered but not kept.
		tokens.Device = record.Tokens.Device
		record.Tokens = tokens
		if err := a.Repo.Update(ctx, store, cipher, *record, record.Revision); err != nil {
			return err
		}
		record.Revision++
		return nil
	}
	// An imported fingerprint is the owner's own and is not renewed.
	cachedFingerprint := record.Tokens.DeviceFP != "" && (record.Tokens.DeviceFPImported || record.Tokens.DeviceFPExpiresAtMS > time.Now().UnixMilli())
	if !cachedFingerprint && !strings.Contains(name, ".cloud_") {
		if err := refreshFingerprint(); err != nil {
			return nil, err
		}
	}
	execute := func() (map[string]any, error) {
		tokens, err := a.lendDefaultDevice(ctx, store, cipher, record.Tokens)
		if err != nil {
			return nil, err
		}
		return a.API.Execute(ctx, tokens, role, name, input)
	}
	data, err := execute()
	// Only reads may renew a rejected cached fingerprint and retry once.
	// Explicit writes are never automatically replayed.
	var upstream *mihoyo.Error
	if !mihoyo.OperationMutates(name) && cachedFingerprint && !record.Tokens.DeviceFPImported && errors.As(err, &upstream) && upstream.Code == "plugin.upstream_device_required" {
		if err := refreshFingerprint(); err != nil {
			return nil, err
		}
		data, err = execute()
	}
	// The Cookie Token expires on the official side while the LToken stays
	// valid, within hours of a login at times: the game records still read,
	// the ledgers and sign-in refuse. It is re-issued from the SToken and the
	// request repeated once, as MihoyoBBSTools refreshes it on failure;
	// re-issuing a still valid token returns the same one. A refused request
	// was not carried out, so a write is repeated as safely as a read.
	if credentialsRefused(err) && record.Summary().CanRefresh {
		if err := a.refreshCredentials(ctx, store, cipher, record, false); err != nil {
			return nil, err
		}
		data, err = execute()
	}
	if err != nil {
		if publicError(err).Code == "plugin.upstream_auth_invalid" {
			record.Status = "invalid"
			if saveErr := a.Repo.Update(ctx, store, cipher, *record, record.Revision); saveErr != nil {
				return nil, saveErr
			}
			record.Revision++
		}
		return nil, err
	}
	return data, nil
}

// credentialsRefused reports an official refusal of the web tokens: a
// refused LToken or Cookie Token, or the same refusal on a sign-in or
// birthday endpoint, which keep their own codes so that they never mark the
// account invalid.
func credentialsRefused(err error) bool {
	switch publicError(err).Code {
	case "plugin.upstream_auth_invalid", "plugin.upstream_sign_unavailable", "plugin.upstream_birthday_unavailable":
		return true
	}
	return false
}

// refreshRetryInterval is how long automatic refreshes leave a refused
// SToken alone; an explicit refresh always tries.
const refreshRetryInterval = 10 * time.Minute

// refreshCredentials re-issues the account's LToken and Cookie Token from its
// SToken, as 刷新CK does, and saves them with the check result; a refused
// SToken marks the account invalid. record follows the save. An automatic
// refresh answers a recent refusal without asking again.
func (a *App) refreshCredentials(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, record *accounts.Record, explicit bool) error {
	if last := record.LastCheck; !explicit && last != nil && last.Kind == "refresh" && last.State == "auth_invalid" && time.Now().UnixMilli()-last.CheckedAtMS < refreshRetryInterval.Milliseconds() {
		return failure("plugin.upstream_auth_invalid")
	}
	input := record.Tokens
	input.LToken = ""
	tokens, err := a.API.Refresh(ctx, input)
	record.LastCheck = checkResult("refresh", err)
	if err == nil {
		record.Tokens, record.Status = tokens, "valid"
	} else if publicError(err).Code == "plugin.upstream_auth_invalid" {
		record.Status = "invalid"
	}
	if saveErr := a.Repo.Update(ctx, store, cipher, *record, record.Revision); saveErr != nil {
		return saveErr
	}
	record.Revision++
	return err
}

// uidPattern is a game UID as upstream's 绑定uid reads it; ZZZ also has
// eight-digit UIDs.
var uidPattern = regexp.MustCompile(`^(?:(?:1[0-9]|[1-9])[0-9]{8}|[1-9][0-9]{7})$`)

// changeUIDBinding binds, removes or picks a UID of the caller's own for a
// game the calling plugin may use, without any account credential.
// currentUID answers a mention, as upstream reads the mentioned user's UID:
// the UID another user of the same chat uses for a game, their bound current
// UID first, then their default account role still visible to the caller, or
// their only role. Only the UID, a public game identifier, is returned.
func (a *App) currentUID(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, request rayleabot.ServiceRequest, games []string) (map[string]any, error) {
	game, userID := stringValue(request.Params["game"]), strings.TrimSpace(stringValue(request.Params["user_id"]))
	target := originSubject(request.Origin)
	target.ActorID = userID
	if !strings.HasPrefix(request.Origin.EventType, "message.") || target.Validate() != nil {
		return nil, failure("plugin.account_subject_denied")
	}
	if !slices.Contains(games, game) {
		return nil, failure("plugin.account_input_invalid")
	}
	bindings, err := a.Repo.UIDBindings(ctx, store, cipher, target)
	if err != nil {
		return nil, err
	}
	if uid := bindings[game].Current; uid != "" {
		return map[string]any{"uid": uid}, nil
	}
	items, err := a.Repo.List(ctx, store, cipher, target)
	if err != nil {
		return nil, err
	}
	defaults, err := a.Repo.Defaults(ctx, store, cipher, target)
	if err != nil {
		return nil, err
	}
	roles := []accounts.Role{}
	for _, item := range items {
		if !slices.Contains(item.AllowedCallers, request.CallerPluginID) {
			continue
		}
		for _, role := range item.Roles {
			if role.Game != game {
				continue
			}
			if selected, ok := defaults[game]; ok && selected.AccountRef == item.Ref && selected.RoleRef == role.Ref {
				return map[string]any{"uid": role.UID}, nil
			}
			roles = append(roles, role)
		}
	}
	if len(roles) == 1 {
		return map[string]any{"uid": roles[0].UID}, nil
	}
	return map[string]any{"uid": ""}, nil
}

func (a *App) changeUIDBinding(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, owner accounts.Subject, request rayleabot.ServiceRequest, games []string) (map[string]any, error) {
	game, uid := stringValue(request.Params["game"]), stringValue(request.Params["uid"])
	if owner.Validate() != nil {
		return nil, failure("plugin.account_subject_denied")
	}
	if !slices.Contains(games, game) || !uidPattern.MatchString(uid) || game != "zzz" && len(uid) != 9 && len(uid) != 10 {
		return nil, failure("plugin.account_input_invalid")
	}
	binding, err := a.Repo.ChangeUIDBinding(ctx, store, cipher, owner, game, strings.TrimPrefix(request.Method, "uids."), uid)
	if errors.Is(err, accounts.ErrBindingLimit) {
		return nil, failure("plugin.account_binding_limit")
	}
	return map[string]any{"game": game, "binding": binding}, err
}
