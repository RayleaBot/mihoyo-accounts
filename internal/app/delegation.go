package app

import (
	"context"
	"crypto/rand"
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
	"regexp"
	"slices"
	"strings"
	"time"
)

var taskPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)

func (a *App) changeDelegation(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, record accounts.Record, req rayleabot.ServiceRequest, games []string) (map[string]any, error) {
	if req.Origin.EventType != "message.private" && req.Origin.EventType != "message.group" && !management(req.Origin) {
		return nil, failure("plugin.account_subject_denied")
	}
	now := time.Now().UnixMilli()
	owner := originSubject(req.Origin)
	if management(req.Origin) {
		owner = record.Owner
		if raw, ok := req.Params["subject"]; ok {
			if decode(raw, &owner) != nil {
				return nil, failure("plugin.account_input_invalid")
			}
		}
	}
	active := slices.DeleteFunc(slices.Clone(record.Delegations), func(d accounts.Delegation) bool { return d.ExpiresAtMS <= now })
	if req.Method == "delegation.revoke" {
		ref := stringValue(req.Params["delegation_ref"])
		if management(req.Origin) {
			for _, grant := range active {
				if grant.Ref == ref && grant.Owner.SourceProtocol != "" && !ownsBot(rayleabot.BotAccount(*req.Origin.Bot), grant.Owner) {
					return nil, failure("plugin.account_subject_denied")
				}
			}
		}
		record.Delegations = slices.DeleteFunc(active, func(d accounts.Delegation) bool {
			grantOwner := d.Owner
			if grantOwner == (accounts.Subject{}) {
				grantOwner = record.Owner
			}
			return d.Caller == req.CallerPluginID && d.Ref == ref && (management(req.Origin) || grantOwner.Equal(owner))
		})
		if err := a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
			return nil, err
		}
		return map[string]any{"revoked": true}, nil
	}
	var input struct {
		TaskID    string `json:"task_id"`
		RoleRef   string `json:"role_ref"`
		Operation string `json:"operation"`
		Days      int    `json:"days"`
	}
	if decode(req.Params, &input) != nil || !taskPattern.MatchString(input.TaskID) || input.Days < 1 || input.Days > 90 {
		return nil, failure("plugin.account_input_invalid")
	}
	valid := input.RoleRef == "" && mihoyo.OperationAccountScoped(input.Operation) && slices.Contains(games, mihoyo.OperationGame(input.Operation)) && (strings.HasSuffix(input.Operation, ".community_run") || strings.HasSuffix(input.Operation, ".cloud_sign"))
	if mihoyo.OperationMutates(input.Operation) && req.Params["write_confirmed"] != true {
		return nil, failure("plugin.account_write_confirmation_required")
	}
	for _, role := range record.Roles {
		// gacha serves the management page's periodic 每日同步.
		gacha := input.Operation == role.Game+".gacha" && (role.Game == "genshin" || role.Game == "zzz" || role.Game == "starrail" && record.Tokens.Realm == "os")
		if role.Ref == input.RoleRef && slices.Contains(games, role.Game) && (input.Operation == role.Game+".note" || input.Operation == role.Game+".monthly" || input.Operation == role.Game+".sign" && req.Params["write_confirmed"] == true || gacha || mihoyo.ChallengeDelegation(input.Operation) && mihoyo.OperationGame(input.Operation) == role.Game) {
			valid = true
		}
	}
	if !valid {
		return nil, failure("plugin.account_operation_denied")
	}
	next := accounts.Delegation{Owner: owner, Ref: rand.Text(), Caller: req.CallerPluginID, TaskID: input.TaskID, RoleRef: input.RoleRef, Operation: input.Operation, ExpiresAtMS: now + int64(time.Duration(input.Days)*24*time.Hour/time.Millisecond)}
	// A task keeps one delegation per operation, as a pair of challenge
	// reminders checked together.
	index := slices.IndexFunc(active, func(d accounts.Delegation) bool {
		return d.Caller == next.Caller && d.TaskID == next.TaskID && d.Operation == next.Operation
	})
	if index >= 0 {
		previousOwner := active[index].Owner
		if previousOwner == (accounts.Subject{}) {
			previousOwner = record.Owner
		}
		if !previousOwner.Equal(owner) && !management(req.Origin) {
			return nil, failure("plugin.account_subject_denied")
		}
		if management(req.Origin) && !ownsBot(rayleabot.BotAccount(*req.Origin.Bot), previousOwner) {
			return nil, failure("plugin.account_subject_denied")
		}
		next.Ref = active[index].Ref
		next.LastUsedAtMS = active[index].LastUsedAtMS
		active[index] = next
	} else {
		if len(active) >= 16 {
			return nil, failure("plugin.account_input_invalid")
		}
		active = append(active, next)
	}
	record.Delegations = active
	if err := a.Repo.Update(ctx, store, cipher, record, record.Revision); err != nil {
		return nil, err
	}
	return map[string]any{"delegation": next}, nil
}
func (a *App) admitDelegation(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, record *accounts.Record, req rayleabot.ServiceRequest) error {
	ref := stringValue(req.Params["delegation_ref"])
	now := time.Now().UnixMilli()
	for i, d := range record.Delegations {
		if d.Ref != ref || d.Caller != req.CallerPluginID || d.TaskID != req.Origin.TaskID || d.RoleRef != stringValue(req.Params["role_ref"]) || d.Operation != stringValue(req.Params["operation"]) || d.ExpiresAtMS <= now {
			continue
		}
		interval := int64(5 * time.Minute / time.Millisecond)
		owner := d.Owner
		if owner == (accounts.Subject{}) {
			owner = record.Owner
		}
		allowed, err := a.Repo.CanAccess(ctx, store, cipher, record.Owner, owner)
		if err != nil {
			return err
		}
		if !allowed {
			return failure("plugin.account_delegation_denied")
		}
		gacha := d.Operation == "genshin.gacha" || d.Operation == "zzz.gacha" || d.Operation == "starrail.gacha" && record.Tokens.Realm == "os"
		community := strings.HasSuffix(d.Operation, ".community_run")
		if community && !mihoyo.ValidCommunityDelegation(object(req.Params["input"])) || gacha && !mihoyo.ValidGachaDelegation(d.Operation, object(req.Params["input"])) || !gacha && !community && len(object(req.Params["input"])) != 0 {
			return failure("plugin.account_delegation_denied")
		}
		if community {
			interval = 3000
		}
		if gacha {
			interval = 1000
		}
		if d.LastUsedAtMS > now-interval {
			return failure("plugin.account_delegation_throttled")
		}
		record.Delegations[i].LastUsedAtMS = now
		if err := a.Repo.Update(ctx, store, cipher, *record, record.Revision); err != nil {
			return err
		}
		record.Revision++
		return nil
	}
	return failure("plugin.account_delegation_denied")
}
