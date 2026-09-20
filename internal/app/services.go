package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

func management(origin rayleabot.ServiceOrigin) bool {
	return origin.SourceProtocol == "management" && origin.SourceAdapter == "management.ui" && origin.EventType == "management.action"
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
	if request.Method == "list" {
		var items []accounts.Summary
		var next *int
		if owner.Validate() == nil {
			items, err = a.Repo.List(ctx, store, cipher, owner)
		} else {
			var input struct {
				Page int `json:"page"`
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
		return map[string]any{"items": visible, "next_page": next, "default_roles": defaults}, nil
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
	if !scheduled && (!admin || owner.Validate() == nil) {
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
		return nil, failure("plugin.upstream_auth_invalid")
	}
	if scheduled {
		if err := a.admitDelegation(ctx, store, cipher, &record, request); err != nil {
			return nil, err
		}
	}
	refreshFingerprint := func() error {
		tokens, err := a.API.Fingerprint(ctx, record.Tokens)
		if err != nil {
			return err
		}
		record.Tokens = tokens
		if err := a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
			return err
		}
		record.Revision++
		return nil
	}
	cachedFingerprint := record.Tokens.DeviceFP != "" && record.Tokens.DeviceFPExpiresAtMS > time.Now().UnixMilli()
	if !cachedFingerprint && !strings.Contains(name, ".cloud_") {
		if err := refreshFingerprint(); err != nil {
			return nil, err
		}
	}
	data, err := a.API.Execute(ctx, record.Tokens, role, name, object(request.Params["input"]))
	// Only reads may renew a rejected cached fingerprint and retry once.
	// Explicit writes are never automatically replayed.
	var upstream *mihoyo.Error
	if !mihoyo.OperationMutates(name) && cachedFingerprint && errors.As(err, &upstream) && upstream.Code == "plugin.upstream_device_required" {
		if err := refreshFingerprint(); err != nil {
			return nil, err
		}
		data, err = a.API.Execute(ctx, record.Tokens, role, name, object(request.Params["input"]))
	}
	if err != nil {
		if publicError(err).Code == "plugin.upstream_auth_invalid" {
			record.Status = "invalid"
			if saveErr := a.Repo.Update(ctx, store, cipher, record, record.Revision); saveErr != nil {
				return nil, saveErr
			}
		}
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
