package app

import (
	"context"
	"crypto/rand"
	"fmt"
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"slices"
	"strconv"
	"sync"
	"time"
)

type pendingAssociation struct {
	Ref       string
	Primary   accounts.Subject
	Secondary accounts.Subject
	Expires   time.Time
}
type associationRequests struct {
	mu    sync.Mutex
	items map[string]pendingAssociation
}

func (s *associationRequests) clear() { s.mu.Lock(); defer s.mu.Unlock(); s.items = nil }
func subjectLabel(s accounts.Subject) string {
	return fmt.Sprintf("%s / 机器人%s / 用户%s", s.SourceProtocol, s.BotID, s.ActorID)
}
func (a *App) associationFlow(ctx context.Context, store accounts.SecretStore, owner accounts.Subject, command string, args []string) (string, error) {
	if owner.Validate() != nil {
		return "", failure("plugin.account_subject_denied")
	}
	a.vaultGate.Lock()
	defer a.vaultGate.Unlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return "", err
	}
	if command == "关联列表" || command == "解除关联" {
		links, err := a.Repo.Associations(ctx, store, cipher)
		if err != nil {
			return "", err
		}
		links = slices.DeleteFunc(links, func(link accounts.Association) bool {
			return !link.Primary.Equal(owner) && !link.Secondary.Equal(owner)
		})
		if command == "关联列表" {
			if len(links) == 0 {
				return "当前聊天身份没有账号关联。", nil
			}
			text := "本人账号关联："
			for i, l := range links {
				text += fmt.Sprintf("\n%d. %s → %s", i+1, subjectLabel(l.Primary), subjectLabel(l.Secondary))
			}
			return text + "\n使用“解除关联 序号”停止共享。", nil
		}
		if len(args) != 1 {
			return "使用“解除关联 序号”，序号可通过“关联列表”查看。", nil
		}
		index, err := strconv.Atoi(args[0])
		if err != nil || index < 1 || index > len(links) {
			return "", failure("plugin.account_input_invalid")
		}
		if err = a.Repo.Dissociate(ctx, store, cipher, links[index-1].Ref, &owner); err != nil {
			return "", err
		}
		return "关联已解除；该身份对共享账号的后续查询及定时委托将被拒绝，双方原有账号保留。", nil
	}
	a.associations.mu.Lock()
	defer a.associations.mu.Unlock()
	now := time.Now()
	if a.associations.items == nil {
		a.associations.items = map[string]pendingAssociation{}
	}
	for ref, v := range a.associations.items {
		if !now.Before(v.Expires) {
			delete(a.associations.items, ref)
		}
	}
	if command == "关联账号" {
		if len(args) != 0 {
			return "使用“关联账号”在已登录的主聊天身份发起。", nil
		}
		list, err := a.Repo.List(ctx, store, cipher, owner)
		if err != nil {
			return "", err
		}
		hasOwn := false
		for _, v := range list {
			record, e := a.Repo.Get(ctx, store, cipher, v.Ref)
			if e == nil && record.Owner.Equal(owner) {
				hasOwn = true
				break
			}
		}
		if !hasOwn {
			return "请先在此聊天身份完成一次账号登录。", nil
		}
		for ref, v := range a.associations.items {
			if v.Primary.Equal(owner) {
				delete(a.associations.items, ref)
			}
		}
		if len(a.associations.items) >= 32 {
			return "", failure("plugin.account_association_limit")
		}
		ref := rand.Text()
		a.associations.items[ref] = pendingAssociation{Ref: ref, Primary: owner, Expires: now.Add(5 * time.Minute)}
		return "请在你要关联的另一聊天身份向机器人发送：\n申请关联 " + ref + "\n随后回到这里发送“关联进度 " + ref + "”核对目标，再按提示完成确认。编号5分钟有效。", nil
	}
	if len(args) < 1 || len(args) > 2 {
		return "请提供关联编号；完成确认时还需目标用户ID。", nil
	}
	v, ok := a.associations.items[args[0]]
	if !ok {
		return "", failure("plugin.account_association_missing")
	}
	switch command {
	case "申请关联":
		if len(args) != 1 || owner.Equal(v.Primary) || v.Secondary.Validate() == nil && !v.Secondary.Equal(owner) {
			return "", failure("plugin.account_subject_denied")
		}
		v.Secondary = owner
		a.associations.items[v.Ref] = v
		return "此身份已登记，尚未共享账号。请回到发起聊天查看关联进度并完成确认。", nil
	case "关联进度":
		if !owner.Equal(v.Primary) {
			return "", failure("plugin.account_subject_denied")
		}
		if v.Secondary.Validate() != nil {
			return "等待另一聊天身份发送“申请关联 " + v.Ref + "”。", nil
		}
		return "将共享此主身份账号给：\n" + subjectLabel(v.Secondary) + "\n确认是你本人后，发送：\n完成关联 " + v.Ref + " " + v.Secondary.ActorID, nil
	case "完成关联":
		if !owner.Equal(v.Primary) || len(args) != 2 || v.Secondary.Validate() != nil || args[1] != v.Secondary.ActorID {
			return "", failure("plugin.account_subject_denied")
		}
		if _, err = a.Repo.Associate(ctx, store, cipher, v.Primary, v.Secondary); err != nil {
			return "", err
		}
		delete(a.associations.items, v.Ref)
		return "本人账号关联已完成。附属身份现在可使用主身份已授权给插件的账号；不需要重新获取CK。", nil
	case "取消关联":
		if !owner.Equal(v.Primary) && !owner.Equal(v.Secondary) {
			return "", failure("plugin.account_subject_denied")
		}
		delete(a.associations.items, v.Ref)
		return "本次关联申请已取消。", nil
	}
	return "", failure("plugin.account_input_invalid")
}
func (a *App) manageAssociations(ctx context.Context, store accounts.SecretStore, action string, input map[string]any) (map[string]any, error) {
	a.vaultGate.Lock()
	defer a.vaultGate.Unlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	if action == "accounts.associations.list" {
		items, err := a.Repo.Associations(ctx, store, cipher)
		if bot, ok := scopedBot(ctx); ok {
			items = slices.DeleteFunc(items, func(link accounts.Association) bool {
				return !ownsBot(bot, link.Primary) && !ownsBot(bot, link.Secondary)
			})
		}
		return map[string]any{"items": items}, err
	}
	if action != "accounts.associations.remove" || input["confirm"] != true {
		return nil, failure("plugin.account_write_confirmation_required")
	}
	var actor *accounts.Subject
	if bot, ok := scopedBot(ctx); ok {
		items, err := a.Repo.Associations(ctx, store, cipher)
		if err != nil {
			return nil, err
		}
		for _, link := range items {
			if link.Ref == stringValue(input["ref"]) {
				if ownsBot(bot, link.Primary) {
					actor = &link.Primary
				} else if ownsBot(bot, link.Secondary) {
					actor = &link.Secondary
				}
			}
		}
		if actor == nil {
			return nil, failure("plugin.account_subject_denied")
		}
	}
	err = a.Repo.Dissociate(ctx, store, cipher, stringValue(input["ref"]), actor)
	return map[string]any{"removed": err == nil}, err
}
func (a *App) associationCommand(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, owner accounts.Subject, command string) error {
	text, err := a.associationFlow(ctx, store, owner, command, event.Event.Args())
	if err != nil {
		text = publicError(err).Message
	}
	return event.SendText(text)
}
