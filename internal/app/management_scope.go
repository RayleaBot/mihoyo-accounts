package app

import (
	"context"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

type managementBotKey struct{}

func scopedBot(ctx context.Context) (rayleabot.BotAccount, bool) {
	bot, ok := ctx.Value(managementBotKey{}).(rayleabot.BotAccount)
	return bot, ok
}

func ownsBot(bot rayleabot.BotAccount, owner accounts.Subject) bool {
	return owner.SourceProtocol == bot.SourceProtocol && owner.BotID == bot.BotID
}

func checkManagementOwner(ctx context.Context, owner accounts.Subject) error {
	if bot, ok := scopedBot(ctx); ok && !ownsBot(bot, owner) {
		return failure("plugin.account_subject_denied")
	}
	return nil
}

// ManageEvent applies global actions directly and scopes all account actions
// to the bot account supplied by the host. Manage is also used by chat commands.
func (a *App) ManageEvent(ctx context.Context, store accounts.SecretStore, policy Policy, event rayleabot.Event) (map[string]any, error) {
	action, input := stringValue(event.Payload["action"]), object(event.Payload["payload"])
	if event.SourceProtocol != "management" || event.EventType != "management.action" {
		return nil, failure("platform.invalid_request")
	}
	switch action {
	case "status", "vault.public_key", "vault.initialize", "vault.unlock", "vault.lock", "vault.mode", "ark.status", "ark.save", "ark.clear":
		return a.Manage(ctx, store, policy, action, input)
	}
	bot, scoped := event.ManagementBot()
	if !scoped {
		return nil, failure("platform.invalid_request")
	}
	ctx = context.WithValue(ctx, managementBotKey{}, bot)
	if input["all"] == true {
		return nil, failure("plugin.account_subject_denied")
	}
	for _, field := range []string{"subject", "owner", "bot"} {
		if raw, exists := input[field]; exists {
			var owner accounts.Subject
			if decode(raw, &owner) != nil || !ownsBot(bot, owner) {
				return nil, failure("plugin.account_subject_denied")
			}
		}
	}
	if id, exists := input["bot_id"]; exists && id != bot.BotID {
		return nil, failure("plugin.account_subject_denied")
	}
	if protocol, exists := input["source_protocol"]; exists && protocol != bot.SourceProtocol {
		return nil, failure("plugin.account_subject_denied")
	}
	switch action {
	case "accounts.list", "accounts.check", "accounts.refresh", "accounts.remove", "accounts.remove_invalid", "accounts.roles", "accounts.permissions", "accounts.public.disable", "accounts.cloud.save", "accounts.cloud.clear", "accounts.device.status", "accounts.device.import", "accounts.device.reset", "accounts.associations.list", "accounts.associations.remove", "accounts.import.start", "accounts.import.step", "accounts.import.cancel", "accounts.batch.start", "accounts.batch.step", "accounts.batch.cancel", "login.start", "login.poll", "login.cancel", "subjects.get", "subjects.select", "subjects.uids.bind", "subjects.uids.remove", "subjects.uids.use":
	default:
		return nil, failure("plugin.account_operation_denied")
	}
	if action == "login.start" || action == "accounts.import.start" || strings.HasPrefix(action, "subjects.") {
		owner := accounts.Subject{SourceProtocol: bot.SourceProtocol, BotID: bot.BotID, ActorID: strings.TrimSpace(stringValue(input["actor_id"]))}
		if raw, exists := input["subject"]; exists {
			if decode(raw, &owner) != nil {
				return nil, failure("plugin.account_input_invalid")
			}
		}
		if owner.Validate() != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		input["subject"] = owner
	}
	return a.Manage(ctx, store, policy, action, input)
}
