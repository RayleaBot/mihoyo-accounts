package app

import (
	"strings"
	"testing"
)

// actionsNamed are the actions of one name.
func actionsNamed(actions []hostAction, name string) []hostAction {
	var found []hostAction
	for _, action := range actions {
		if action.Name == name {
			found = append(found, action)
		}
	}
	return found
}

func TestAccountCommandsWorkInGroupsExceptMyCK(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	host := newChatHost(t, a, store)
	// As Yunzai's 我的CK, the list is only shown in private.
	reply, actions := host.message("group", "#我的CK", "我的CK")
	if sentText(reply) != "请私聊查看" || len(actionsNamed(actions, "secret.read")) != 0 {
		t.Fatalf("group 我的CK: %q, %d reads", sentText(reply), len(actionsNamed(actions, "secret.read")))
	}
	reply, _ = host.message("private", "#我的CK", "我的CK")
	if !strings.Contains(sentText(reply), record.Tokens.AccountID) {
		t.Fatalf("private 我的CK: %q", sentText(reply))
	}
	// The other commands act for the sender in the group.
	host.message("group", "#检查CK", "检查CK")
	cipher, _ := a.Vault.Cipher()
	saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || saved.LastCheck == nil || saved.LastCheck.Kind != "check" {
		t.Fatal("group 检查CK did not check the account", err)
	}
	host.message("group", "#刷新CK", "刷新CK")
	if api.refreshCalls.Load() != 1 {
		t.Fatal("group 刷新CK did not refresh the account")
	}
	reply, _ = host.message("group", "#绑定设备", "绑定设备")
	if ref, waiting := a.devices.waiting(subject()); !waiting || ref != record.Ref || object(reply["data"])["target_type"] != "group" {
		t.Fatalf("group 绑定设备 did not start: %q", sentText(reply))
	}
}
