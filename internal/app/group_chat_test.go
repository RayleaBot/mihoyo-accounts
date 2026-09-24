package app

import (
	"strings"
	"testing"
	"time"
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

func TestGroupQRLoginAtsTheSenderAndRecallsTheCode(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	a.pollInterval = time.Millisecond
	host := newChatHost(t, a, store)
	reply, actions := host.message("group", "#扫码登录", "扫码登录")
	sends := actionsNamed(actions, "message.send")
	if reply["type"] != "result" || len(actionsNamed(actions, "scheduler.create")) != 1 || len(sends) != 1 {
		t.Fatalf("group 扫码登录 was not accepted: %v", reply)
	}
	code := sends[0].Data
	segments := object(code["message"])["segments"].([]any)
	if code["target_type"] != "group" || code["target_id"] != "group-1" || len(segments) != 3 || object(segments[0])["type"] != "at" || object(object(segments[0])["data"])["user_id"] != subject().ActorID || object(segments[2])["type"] != "image" {
		t.Fatalf("the code does not @ the sender in the group: %v", code)
	}
	a.jobsMu.Lock()
	job := a.jobs[a.owners[subject()]]
	a.jobsMu.Unlock()
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.ready })
	// The result goes back to the group, @ing the sender; the code is not
	// due for recall yet, so the task stays.
	_, actions = host.loginTrigger()
	sends = actionsNamed(actions, "message.send")
	if len(sends) != 1 || sends[0].Data["target_type"] != "group" || object(object(sends[0].Data["message"])["segments"].([]any)[0])["type"] != "at" {
		t.Fatalf("result not sent to the group: %v", actions)
	}
	if len(actionsNamed(actions, "message.delete")) != 0 || len(actionsNamed(actions, "scheduler.delete")) != 0 {
		t.Fatal("the code was recalled early or the task ended before its recall")
	}
	job.mu.Lock()
	job.recallAt = time.Now()
	job.mu.Unlock()
	_, actions = host.loginTrigger()
	recalls := actionsNamed(actions, "message.delete")
	if len(recalls) != 1 || recalls[0].Data["message_id"] != "sent-1" || recalls[0].Data["source_adapter"] != subject().SourceAdapter {
		t.Fatalf("the code was not recalled: %v", actions)
	}
	if len(actionsNamed(actions, "scheduler.delete")) != 1 {
		t.Fatal("the task was kept after the recall")
	}
}
