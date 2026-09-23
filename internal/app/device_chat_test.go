package app

import (
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestChatDeviceBindingWaitsForThePrivateMessage(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	ask := func(method, eventType string) rayleabot.ServiceRequest {
		request := request("zzz", record.Ref)
		request.Method, request.Params = method, map[string]any{}
		request.Origin.EventType = eventType
		request.Origin.Target = &rayleabot.Target{Type: map[string]string{"message.private": "private", "message.group": "group"}[eventType], ID: "chat-1"}
		return request
	}
	_, err := a.Service(t.Context(), store, policy(), ask("device.bind", "message.group"))
	assertCode(t, err, "plugin.account_operation_denied")
	started := ask("device.bind", "message.private")
	started.CallerPluginID = "raylea.genshin"
	_, err = a.Service(t.Context(), store, policy(), started)
	assertCode(t, err, "plugin.account_operation_denied")
	result, err := a.Service(t.Context(), store, policy(), ask("device.bind", "message.private"))
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
	if _, err := a.Service(t.Context(), store, policy(), ask("device.unbind", "message.group")); err != nil {
		t.Fatal(err)
	}
	saved, _ = a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if saved.Tokens.DeviceID == "0123456789abcdef0123" || saved.Tokens.DeviceFP != "" || saved.Tokens.DeviceFPImported {
		t.Fatal("the device was kept", saved.Tokens.DeviceID)
	}
}
