package app

import (
	"context"
	"slices"
	"strings"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func (a *App) manageSubject(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, action string, input map[string]any) (map[string]any, error) {
	var owner accounts.Subject
	if decode(input["subject"], &owner) != nil || owner.Validate() != nil {
		return nil, failure("plugin.account_input_invalid")
	}
	if err := checkManagementOwner(ctx, owner); err != nil {
		return nil, err
	}
	if action == "subjects.get" {
		items, err := a.Repo.ListForBotSubject(ctx, store, cipher, owner)
		if err != nil {
			return nil, err
		}
		defaults, err := a.Repo.Defaults(ctx, store, cipher, owner)
		if err != nil {
			return nil, err
		}
		for game, selected := range defaults {
			valid := false
			for _, item := range items {
				for _, role := range item.Roles {
					if item.Ref == selected.AccountRef && role.Ref == selected.RoleRef && role.Game == game {
						valid = true
					}
				}
			}
			if !valid {
				delete(defaults, game)
			}
		}
		bindings, err := a.Repo.UIDBindings(ctx, store, cipher, owner)
		return map[string]any{"items": items, "default_roles": defaults, "uid_bindings": bindings}, err
	}
	if action == "subjects.select" {
		ref := stringValue(input["account_ref"])
		release, err := a.acquire(ctx, ref)
		if err != nil {
			return nil, err
		}
		defer release()
		record, err := a.Repo.Get(ctx, store, cipher, ref)
		if err != nil {
			return nil, err
		}
		if err := checkManagementOwner(ctx, record.Owner); err != nil {
			return nil, err
		}
		selection, err := a.Repo.Select(ctx, store, cipher, owner, ref, stringValue(input["role_ref"]))
		return map[string]any{"selection": selection}, err
	}
	game, uid := stringValue(input["game"]), stringValue(input["uid"])
	if (game != "genshin" && game != "starrail" && game != "zzz") || !uidPattern.MatchString(uid) || game != "zzz" && len(uid) != 9 && len(uid) != 10 {
		return nil, failure("plugin.account_input_invalid")
	}
	binding, err := a.Repo.ChangeUIDBinding(ctx, store, cipher, owner, game, strings.TrimPrefix(action, "subjects.uids."), uid)
	return map[string]any{"binding": binding}, err
}

func (a *App) manageHistory(ctx context.Context, store accounts.SecretStore, action string, input map[string]any) (result map[string]any, err error) {
	defer func() {
		if err != nil {
			err = publicError(err)
		}
	}()
	a.vaultGate.Lock()
	defer a.vaultGate.Unlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	switch action {
	case "history.delegations.list":
		var q struct {
			Page int `json:"page"`
		}
		if decode(input, &q) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		page, err := a.Repo.ListPage(ctx, store, cipher, q.Page)
		if err != nil {
			return nil, err
		}
		items := []map[string]any{}
		for _, item := range page.Items {
			record, err := a.Repo.Get(ctx, store, cipher, item.Ref)
			if err != nil {
				return nil, err
			}
			for _, grant := range record.Delegations {
				owner := grant.Owner
				if owner == (accounts.Subject{}) {
					owner = record.Owner
				}
				items = append(items, map[string]any{"account_ref": record.Ref, "ref": grant.Ref, "owner": owner, "operation": grant.Operation, "task_id": grant.TaskID})
			}
		}
		return map[string]any{"items": items, "next_page": page.NextPage}, nil
	case "history.delegations.remove":
		if input["confirm"] != true {
			return nil, failure("plugin.account_write_confirmation_required")
		}
		record, err := a.Repo.Get(ctx, store, cipher, stringValue(input["account_ref"]))
		if err != nil {
			return nil, err
		}
		record.Delegations = slices.DeleteFunc(record.Delegations, func(grant accounts.Delegation) bool { return grant.Ref == stringValue(input["ref"]) })
		err = a.Repo.Update(ctx, store, cipher, record, record.Revision)
		return map[string]any{"removed": err == nil}, err
	case "history.assign":
		bot, ok := scopedBot(ctx)
		if !ok || bot.SourceProtocol != "onebot11" || input["confirm"] != true {
			return nil, failure("plugin.account_subject_denied")
		}
		var old accounts.Subject
		if decode(input["legacy_subject"], &old) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		err = a.Repo.AssignMissingBot(ctx, store, cipher, old, bot.BotID)
		return map[string]any{"assigned": err == nil}, err
	case "history.subjects.list":
		var q struct {
			Keys []string `json:"keys"`
		}
		if decode(input, &q) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		items, err := a.Repo.SubjectData(ctx, store, cipher, q.Keys)
		return map[string]any{"items": items}, err
	case "history.subjects.remove":
		if input["confirm"] != true {
			return nil, failure("plugin.account_write_confirmation_required")
		}
		err = a.Repo.RemoveSubjectData(ctx, store, cipher, stringValue(input["key"]))
		return map[string]any{"removed": err == nil}, err
	}
	return nil, failure("plugin.account_operation_denied")
}
