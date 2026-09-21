package app

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

func (a *App) Manage(ctx context.Context, store accounts.SecretStore, policy Policy, action string, input map[string]any) (result map[string]any, err error) {
	defer func() {
		if err != nil {
			err = publicError(err)
		}
	}()
	if _, exists := input["password"]; exists {
		return nil, failure("plugin.account_input_invalid")
	}
	if strings.HasPrefix(action, "accounts.batch.") {
		return a.manageBatch(ctx, store, policy, action, input)
	}
	if action == "accounts.refresh" || action == "accounts.check" {
		scope := "management"
		if raw, ok := input["subject"]; ok {
			var owner accounts.Subject
			if decode(raw, &owner) != nil || owner.Validate() != nil {
				return nil, failure("plugin.account_subject_denied")
			}
			scope = owner.Key()
		}
		return a.shareRefresh(ctx, action+":"+stringValue(input["account_ref"])+":"+scope, func() (map[string]any, error) { return a.manage(ctx, store, policy, action, input) })
	}
	return a.manage(ctx, store, policy, action, input)
}

func (a *App) manage(ctx context.Context, store accounts.SecretStore, policy Policy, action string, input map[string]any) (result map[string]any, err error) {
	if strings.HasPrefix(action, "accounts.overseas.") {
		return a.manageOverseas(ctx, store, policy, action, input)
	}
	if strings.HasPrefix(action, "accounts.associations.") {
		return a.manageAssociations(ctx, store, action, input)
	}
	if strings.HasPrefix(action, "accounts.device.") {
		return a.manageDevice(ctx, store, action, input)
	}
	if _, exists := input["password"]; exists {
		return nil, failure("plugin.account_input_invalid")
	}
	switch action {
	case "vault.public_key":
		return map[string]any{"public_key": a.Vault.PublicKey()}, nil
	case "vault.lock":
		a.vaultGate.Lock()
		defer a.vaultGate.Unlock()
		a.cancelLogins()
		a.associations.clear()
		a.overseas.clear()
		a.Vault.Lock()
		return map[string]any{"state": "locked"}, nil
	case "vault.mode":
		// Management actions come from the authenticated host management page,
		// so only an administrator can change how the key is protected.
		a.vaultGate.Lock()
		defer a.vaultGate.Unlock()
		current, next := "", ""
		for field, target := range map[string]*string{"password_ciphertext": &current, "new_password_ciphertext": &next} {
			if value := stringValue(input[field]); value != "" {
				if *target, err = a.Vault.DecodePassword(value); err != nil {
					return nil, err
				}
			}
		}
		if err = a.Vault.SwitchMode(stringValue(input["mode"]), current, next); err != nil {
			return nil, err
		}
		return map[string]any{"key_protection": a.Vault.Mode()}, nil
	case "vault.initialize", "vault.unlock":
		a.vaultGate.Lock()
		defer a.vaultGate.Unlock()
		password := ""
		if value := stringValue(input["password_ciphertext"]); value != "" {
			password, err = a.Vault.DecodePassword(value)
			if err != nil {
				return nil, err
			}
		}
		if action == "vault.initialize" {
			has, readErr := a.Repo.HasRecords(ctx, store)
			if readErr != nil {
				return nil, readErr
			}
			err = a.Vault.Initialize(password, has)
		} else {
			err = a.Vault.Unlock(password)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"state": a.Vault.State()}, nil
	case "status":
		state := a.Vault.State()
		if state == "uninitialized" {
			has, readErr := a.Repo.HasRecords(ctx, store)
			if readErr != nil {
				return nil, readErr
			}
			if has {
				state = "missing"
			}
		}
		return map[string]any{"vault_state": state, "key_protection": a.Vault.Mode(), "native_key_available": runtime.GOOS == "windows", "allowed_plugins": policy.AllowedPlugins, "operations": mihoyo.OperationNames()}, nil
	}
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(action, "ark.") {
		return a.manageArk(ctx, store, cipher, action, input)
	}
	switch action {
	case "accounts.list":
		if raw, exists := input["subject"]; exists {
			var owner accounts.Subject
			if decode(raw, &owner) != nil || owner.Validate() != nil {
				return nil, failure("plugin.account_input_invalid")
			}
			items, listErr := a.Repo.List(ctx, store, cipher, owner)
			return map[string]any{"items": items}, listErr
		}
		var query struct {
			Page int `json:"page"`
		}
		if decode(input, &query) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		page, listErr := a.Repo.ListPage(ctx, store, cipher, query.Page)
		return map[string]any{"items": page.Items, "total": page.Total, "next_page": page.NextPage}, listErr
	case "login.start":
		var owner accounts.Subject
		if decode(input["subject"], &owner) != nil || owner.Validate() != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		view, loginErr := a.startLogin(ctx, owner)
		return map[string]any{"login": view}, loginErr
	case "login.poll", "login.cancel":
		ref := stringValue(input["login_ref"])
		if action == "login.cancel" {
			view, cancelErr := a.cancelLogin(ref)
			return map[string]any{"login": view}, cancelErr
		}
		view, pollErr := a.pollLogin(ctx, store, cipher, policy, ref)
		return map[string]any{"login": view}, pollErr
	}
	ref := stringValue(input["account_ref"])
	// Capture the revision before waiting: concurrent refresh callers can use
	// the already refreshed record instead of issuing a second token exchange.
	initial, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return nil, err
	}
	release, err := a.acquire(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer release()
	record, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return nil, err
	}
	if raw, ok := input["subject"]; ok {
		var owner accounts.Subject
		if decode(raw, &owner) != nil || owner.Validate() != nil {
			return nil, failure("plugin.account_subject_denied")
		}
		allowed, err := a.Repo.CanAccess(ctx, store, cipher, record.Owner, owner)
		if err != nil {
			return nil, err
		}
		if !allowed || action == "accounts.remove" && record.Owner != owner {
			return nil, failure("plugin.account_subject_denied")
		}
	}
	switch action {
	case "accounts.cloud.save", "accounts.cloud.clear":
		return a.manageCloudCredential(ctx, store, cipher, record, action, input)
	case "accounts.public.disable":
		updated, err := a.Repo.SetPublic(ctx, store, cipher, ref, record.Owner, false, 0, nil)
		return map[string]any{"account": updated.Summary()}, err
	case "accounts.check":
		_, checkErr := a.API.RolesForGame(ctx, record.Tokens, "genshin")
		record.LastCheck = checkResult("check", checkErr)
		if checkErr == nil {
			record.Status = "valid"
		} else if publicError(checkErr).Code == "plugin.upstream_auth_invalid" {
			record.Status = "invalid"
		}
		if saveErr := a.Repo.Update(ctx, store, cipher, record, record.Revision); saveErr != nil {
			return nil, saveErr
		}
		if checkErr != nil {
			return nil, checkErr
		}
		saved, readErr := a.Repo.Get(ctx, store, cipher, ref)
		return map[string]any{"account": saved.Summary()}, readErr
	case "accounts.remove_invalid":
		var q struct {
			Revision uint64 `json:"revision"`
			Confirm  bool   `json:"confirm"`
		}
		if decode(input, &q) != nil || !q.Confirm || q.Revision == 0 {
			return nil, failure("plugin.account_input_invalid")
		}
		if record.Status != "invalid" || record.Revision != q.Revision {
			return map[string]any{"removed": false, "skipped": true}, nil
		}
		removed, removeErr := a.Repo.RemoveIfInvalid(ctx, store, cipher, ref, q.Revision)
		return map[string]any{"removed": removed, "skipped": !removed}, removeErr
	case "accounts.refresh":
		if !record.Summary().CanRefresh {
			return nil, failure("plugin.account_refresh_unavailable")
		}
		if !sameCredentials(record.Tokens, initial.Tokens) {
			return map[string]any{"account": record.Summary()}, nil
		}
		refreshInput := record.Tokens
		refreshInput.LToken = ""
		tokens, refreshErr := a.API.Refresh(ctx, refreshInput)
		record.LastCheck = checkResult("refresh", refreshErr)
		if refreshErr != nil {
			if publicError(refreshErr).Code == "plugin.upstream_auth_invalid" {
				record.Status = "invalid"
			}
			if saveErr := a.Repo.Update(ctx, store, cipher, record, record.Revision); saveErr != nil {
				return nil, saveErr
			}
			return nil, refreshErr
		}
		record.Tokens, record.Status = tokens, "valid"
	case "accounts.roles":
		game := stringValue(input["game"])
		if !slices.Contains([]string{"genshin", "starrail", "zzz"}, game) {
			return nil, failure("plugin.account_input_invalid")
		}
		roles, roleErr := a.API.RolesForGame(ctx, record.Tokens, game)
		if roleErr != nil {
			return nil, roleErr
		}
		record.Roles = append(slices.DeleteFunc(record.Roles, func(role accounts.Role) bool { return role.Game == game }), roles...)
	case "accounts.permissions":
		var update struct {
			Callers  []string `json:"callers"`
			Revision uint64   `json:"revision"`
		}
		if decode(input, &update) != nil || len(update.Callers) > 32 {
			return nil, failure("plugin.account_input_invalid")
		}
		if update.Revision != record.Revision {
			return nil, accounts.ErrConflict
		}
		for _, caller := range update.Callers {
			if len(policy.Games(caller)) == 0 {
				return nil, failure("plugin.account_caller_denied")
			}
		}
		slices.Sort(update.Callers)
		record.AllowedCallers = slices.Compact(update.Callers)
	case "accounts.remove":
		a.cancelOwnerLogin(record.Owner)
		if err := a.Repo.Remove(ctx, store, cipher, ref); err != nil {
			return nil, err
		}
		return map[string]any{"removed": true}, nil
	default:
		return nil, failure("plugin.account_operation_denied")
	}
	if err := a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
		return nil, err
	}
	saved, err := a.Repo.Get(ctx, store, cipher, ref)
	if err != nil {
		return nil, err
	}
	return map[string]any{"account": saved.Summary()}, nil
}

func checkResult(kind string, err error) *accounts.CheckResult {
	result := &accounts.CheckResult{Kind: kind, State: "available", CheckedAtMS: time.Now().UnixMilli()}
	if err == nil {
		return result
	}
	result.Code = publicError(err).Code
	if !accounts.KnownCheckCode(result.Code) {
		result.Code = "plugin.account_operation_failed"
	}
	result.State = map[string]string{"plugin.upstream_auth_invalid": "auth_invalid", "plugin.upstream_device_required": "device_required", "plugin.upstream_challenge_required": "challenge_required"}[result.Code]
	if result.State == "" {
		result.State = "unavailable"
	}
	return result
}

func sameCredentials(a, b accounts.Tokens) bool {
	return a.RealmName() == b.RealmName() && a.LTokenV2 == b.LTokenV2 && a.CookieTokenV2 == b.CookieTokenV2 && a.STokenV2 == b.STokenV2 && a.AccountID == b.AccountID && a.MID == b.MID && a.SToken == b.SToken && a.LToken == b.LToken && a.CookieToken == b.CookieToken
}

type refreshFlight struct {
	done   chan struct{}
	result map[string]any
	err    error
}

func (a *App) shareRefresh(ctx context.Context, ref string, run func() (map[string]any, error)) (map[string]any, error) {
	a.refreshMu.Lock()
	if flight := a.refreshes[ref]; flight != nil {
		a.refreshMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-flight.done:
			return flight.result, flight.err
		}
	}
	flight := &refreshFlight{done: make(chan struct{})}
	a.refreshes[ref] = flight
	a.refreshMu.Unlock()
	flight.result, flight.err = run()
	a.refreshMu.Lock()
	delete(a.refreshes, ref)
	close(flight.done)
	a.refreshMu.Unlock()
	return flight.result, flight.err
}
