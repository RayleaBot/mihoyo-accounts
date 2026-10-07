package app

import (
	"context"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

func deviceRequest(ref, method, eventType string) rayleabot.ServiceRequest {
	request := request("zzz", ref)
	request.Method, request.Params = method, map[string]any{}
	request.Origin.EventType = eventType
	request.Origin.Target = &rayleabot.Target{Type: map[string]string{"message.private": "private", "message.group": "group"}[eventType], ID: "chat-1"}
	return request
}

func TestChatDeviceBindingWaitsForTheDeviceMessage(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	started := deviceRequest(record.Ref, "device.bind", "message.group")
	started.CallerPluginID = "raylea.genshin"
	_, err := a.Service(t.Context(), store, policy(), started)
	assertCode(t, err, "plugin.account_operation_denied")
	// A group starts the wait as a private chat does, as upstream's.
	result, err := a.Service(t.Context(), store, policy(), deviceRequest(record.Ref, "device.bind", "message.group"))
	if err != nil || result["uid"] != role("zzz").UID {
		t.Fatal(result, err)
	}
	ref, waiting := a.devices.waiting(subject())
	if !waiting || ref != record.Ref {
		t.Fatal("no wait for the device information", ref, waiting)
	}
	if err := a.saveDevice(t.Context(), store, ref, []byte(`{"device_id":"0123456789abcdef0123","device_fp":"38d7faa51d2e1"}`)); err != nil {
		t.Fatal(err)
	}
	cipher, _ := a.Vault.Cipher()
	saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || saved.Tokens.DeviceID != "0123456789abcdef0123" || saved.Tokens.DeviceFP != "38d7faa51d2e1" || !saved.Tokens.DeviceFPImported {
		t.Fatal(saved.Tokens.DeviceID, err)
	}
	assertCode(t, a.saveDevice(t.Context(), store, ref, []byte(`{"device_id":"short"}`)), "plugin.account_input_invalid")
	// 解绑设备 works in a group too, as upstream's.
	if _, err := a.Service(t.Context(), store, policy(), deviceRequest(record.Ref, "device.unbind", "message.group")); err != nil {
		t.Fatal(err)
	}
	saved, _ = a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if saved.Tokens.DeviceID == "0123456789abcdef0123" || saved.Tokens.DeviceFP != "" || saved.Tokens.DeviceFPImported {
		t.Fatal("the device was kept", saved.Tokens.DeviceID)
	}
}

func TestDeviceBindingNeedsAZenlessRole(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	other := subject()
	other.ActorID = "user-2"
	tokens := syntheticTokens()
	tokens.AccountID = "987654321"
	cipher, _ := a.Vault.Cipher()
	record, err := a.Repo.Bind(t.Context(), store, cipher, other, tokens, []accounts.Role{role("genshin")}, policy().IDs())
	if err != nil {
		t.Fatal(err)
	}
	// As upstream stops without a Zenless UID, no wait starts.
	request := deviceRequest(record.Ref, "device.bind", "message.private")
	request.Origin.Actor = &rayleabot.Actor{ID: other.ActorID}
	_, err = a.Service(t.Context(), store, policy(), request)
	assertCode(t, err, "plugin.account_not_found")
	if _, waiting := a.devices.waiting(other); waiting {
		t.Fatal("waiting for a device without a Zenless role")
	}
}

func TestDeviceInformationIsTakenFromTheGroupWithoutEcho(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	host := newChatHost(t, a, store)
	ids := map[string]string{"group": "0123456789abcdef0123", "private": "fedcba9876543210fedc"}
	for _, chat := range []string{"group", "private"} {
		// The wait is the user's, as upstream's: started in the group, it
		// takes the information from either chat.
		if _, err := a.Service(t.Context(), store, policy(), deviceRequest(record.Ref, "device.bind", "message.group")); err != nil {
			t.Fatal(err)
		}
		reply, _ := host.message(chat, `{"device_id":"`+ids[chat]+`","device_fp":"38d7faa51d2e1"}`, "")
		if reply["action"] != "message.send" || object(reply["data"])["target_type"] != chat {
			t.Fatalf("%s: not answered in the chat: %v", chat, reply)
		}
		// Only a group, which sees the information, is asked to recall it.
		if recall := strings.Contains(sentText(reply), "请撤回设备信息"); recall != (chat == "group") {
			t.Fatalf("%s: reply %q", chat, sentText(reply))
		}
		cipher, _ := a.Vault.Cipher()
		saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
		if err != nil || saved.Tokens.DeviceID != ids[chat] || !saved.Tokens.DeviceFPImported {
			t.Fatalf("%s: device not saved: %v", chat, err)
		}
	}
	// The default device is answered without the hint, as upstream's.
	admins := policy()
	admins.SuperAdmins = []string{subject().ActorID}
	if _, err := a.Service(t.Context(), store, admins, deviceRequest(record.Ref, "device.default", "message.group")); err != nil {
		t.Fatal(err)
	}
	reply, _ := host.message("group", `{"deviceName":"XQ-BC52","deviceBoard":"lahaina","deviceModel":"XQ-BC52","androidVersion":"13","deviceFingerprint":"Sony/XQ-BC52_EEA/XQ-BC52:13/61.2.A.0.472A/061002A0000472A0046651803:user/release-keys","deviceProduct":"XQ-BC52_EEA"}`, "")
	if sentText(reply) != "默认设备信息设置成功" {
		t.Fatalf("default device reply %q", sentText(reply))
	}
	for _, value := range []string{ids["group"], ids["private"], "38d7faa51d2e1", "lahaina"} {
		if strings.Contains(host.output.String(), value) {
			t.Fatal("device information was echoed or logged")
		}
	}
}

func TestDeviceWaitAnswersAsUpstream(t *testing.T) {
	api := &fakeAPI{fingerprint: func(context.Context, accounts.Tokens) (accounts.Tokens, error) {
		return accounts.Tokens{}, &mihoyo.Error{Code: "plugin.upstream_unavailable", Operation: "device.fingerprint"}
	}}
	a, store, record := setup(t, api)
	host := newChatHost(t, a, store)
	bind := func() {
		t.Helper()
		if _, err := a.Service(t.Context(), store, policy(), deviceRequest(record.Ref, "device.bind", "message.group")); err != nil {
			t.Fatal(err)
		}
	}
	bind()
	// A message without text is asked again; other text is left alone.
	if reply, _ := host.message("group", "", ""); sentText(reply) != "请发送设备信息" {
		t.Fatalf("empty message: %v", reply)
	}
	if reply, _ := host.message("group", "今天的体力", ""); reply["type"] != "result" {
		t.Fatalf("ordinary message was taken: %v", reply)
	}
	if _, waiting := a.devices.waiting(subject()); !waiting {
		t.Fatal("the wait ended early")
	}
	// Any message with 取消 ends the wait.
	if reply, _ := host.message("group", "不绑了，取消吧", ""); sentText(reply) != "已取消" {
		t.Fatalf("cancel: %q", sentText(reply))
	}
	if _, waiting := a.devices.waiting(subject()); waiting {
		t.Fatal("取消 did not end the wait")
	}
	// A device that gets no fingerprint is not bound.
	bind()
	reply, _ := host.message("private", `{"deviceName":"XQ-BC52","deviceBoard":"lahaina","deviceModel":"XQ-BC52","oaid":"synthetic-oaid","androidVersion":"13","deviceFingerprint":"Sony/XQ-BC52_EEA/XQ-BC52:13/61.2.A.0.472A/061002A0000472A0046651803:user/release-keys","deviceProduct":"XQ-BC52_EEA"}`, "")
	if sentText(reply) != "绑定设备失败" {
		t.Fatalf("failed binding: %q", sentText(reply))
	}
	cipher, _ := a.Vault.Cipher()
	if saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref); err != nil || saved.Tokens.Device != nil {
		t.Fatal("a device without a fingerprint was saved", err)
	}
}
