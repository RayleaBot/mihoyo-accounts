package app

import (
	"context"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestDefaultDeviceServesAccountsWithoutTheirOwn(t *testing.T) {
	var registered, used *accounts.Device
	api := &fakeAPI{
		fingerprint: func(_ context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
			registered = tokens.Device
			tokens.DeviceFP = "syntheticfingerprint"
			return tokens, nil
		},
		execute: func(_ context.Context, tokens accounts.Tokens, _ accounts.Role, _ string, _ map[string]any) (map[string]any, error) {
			used = tokens.Device
			return map[string]any{}, nil
		},
	}
	a, store, record := setup(t, api)
	ask := func(eventType string) rayleabot.ServiceRequest {
		request := request("zzz", record.Ref)
		request.Method, request.Params = "device.default", map[string]any{}
		request.Origin.EventType = eventType
		request.Origin.Target = &rayleabot.Target{Type: map[string]string{"message.private": "private", "message.group": "group"}[eventType], ID: "chat-1"}
		return request
	}
	admins := policy()
	admins.SuperAdmins = []string{subject().ActorID}
	// Only a super administrator starts it, and only in private.
	_, err := a.Service(t.Context(), store, policy(), ask("message.private"))
	assertCode(t, err, "plugin.account_subject_denied")
	_, err = a.Service(t.Context(), store, admins, ask("message.group"))
	assertCode(t, err, "plugin.account_operation_denied")
	if _, err = a.Service(t.Context(), store, admins, ask("message.private")); err != nil {
		t.Fatal(err)
	}
	if ref, waiting := a.devices.waiting(subject()); !waiting || ref != defaultDeviceWait {
		t.Fatal("no wait for the default device", ref, waiting)
	}
	// Upstream's six fields are required.
	assertCode(t, a.saveDefaultDevice(t.Context(), store, []byte(`{"deviceName":"XQ-BC52","deviceModel":"XQ-BC52"}`)), "plugin.account_input_invalid")
	device := `{"deviceName":"XQ-BC52","deviceBoard":"lahaina","deviceModel":"XQ-BC52","androidVersion":"13","deviceFingerprint":"Sony/XQ-BC52_EEA/XQ-BC52:13/61.2.A.0.472A/061002A0000472A0046651803:user/release-keys","deviceProduct":"XQ-BC52_EEA"}`
	if err := a.saveDefaultDevice(t.Context(), store, []byte(device)); err != nil {
		t.Fatal(err)
	}
	for _, encrypted := range store.values {
		if strings.Contains(encrypted, "lahaina") {
			t.Fatal("default device stored in plaintext")
		}
	}
	if _, err := a.Service(t.Context(), store, policy(), request("zzz", record.Ref)); err != nil {
		t.Fatal(err)
	}
	if registered == nil || used == nil || used.Model != "XQ-BC52" || used.OAID != syntheticTokens().DeviceID {
		t.Fatalf("default device not lent: %+v %+v", registered, used)
	}
	cipher, _ := a.Vault.Cipher()
	saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || saved.Tokens.Device != nil || saved.Tokens.DeviceFP != "syntheticfingerprint" {
		t.Fatalf("account kept the lent device: %+v %v", saved.Tokens.Device, err)
	}
	// An account's own device wins over the default one.
	if err := a.saveDevice(t.Context(), store, record.Ref, []byte(strings.Replace(device, `"deviceModel":"XQ-BC52"`, `"deviceModel":"Own","oaid":"own-oaid"`, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Service(t.Context(), store, policy(), request("zzz", record.Ref)); err != nil || used == nil || used.Model != "Own" {
		t.Fatalf("own device not used: %+v %v", used, err)
	}
}
