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
	// The device is read once; a new one replaces it at once.
	for key := range store.values {
		if strings.Contains(key, defaultDeviceSetting) {
			delete(store.values, key)
		}
	}
	if _, err := a.Service(t.Context(), store, policy(), request("zzz", record.Ref)); err != nil || used == nil || used.Model != "XQ-BC52" {
		t.Fatalf("cached device not used: %+v %v", used, err)
	}
	if err := a.saveDefaultDevice(t.Context(), store, []byte(strings.Replace(device, `"deviceModel":"XQ-BC52"`, `"deviceModel":"Replaced"`, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Service(t.Context(), store, policy(), request("zzz", record.Ref)); err != nil || used == nil || used.Model != "Replaced" {
		t.Fatalf("replaced device not used: %+v %v", used, err)
	}
	a.defaultDev.forget()
	for key := range store.values {
		if strings.Contains(key, defaultDeviceSetting) {
			delete(store.values, key)
		}
	}
	if _, err := a.Service(t.Context(), store, policy(), request("zzz", record.Ref)); err != nil || used != nil {
		t.Fatalf("a forgotten device was still lent: %+v %v", used, err)
	}
	// An account's own device wins over the default one.
	if err := a.saveDevice(t.Context(), store, record.Ref, []byte(strings.Replace(device, `"deviceModel":"XQ-BC52"`, `"deviceModel":"Own","oaid":"own-oaid"`, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Service(t.Context(), store, policy(), request("zzz", record.Ref)); err != nil || used == nil || used.Model != "Own" {
		t.Fatalf("own device not used: %+v %v", used, err)
	}
}

func TestPublicQueryBorrowsTheDefaultDeviceWithoutKeepingIt(t *testing.T) {
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
	cipher, _ := a.Vault.Cipher()
	if _, err := a.Repo.SetPublic(t.Context(), store, cipher, record.Ref, record.Owner, true, 5, []string{"zzz"}); err != nil {
		t.Fatal(err)
	}
	device := `{"deviceName":"XQ-BC52","deviceBoard":"lahaina","deviceModel":"XQ-BC52","androidVersion":"13","deviceFingerprint":"Sony/XQ-BC52_EEA/XQ-BC52:13/61.2.A.0.472A/061002A0000472A0046651803:user/release-keys","deviceProduct":"XQ-BC52_EEA"}`
	if err := a.saveDefaultDevice(t.Context(), store, []byte(device)); err != nil {
		t.Fatal(err)
	}
	req := request("zzz", record.Ref)
	req.Method = "public.execute"
	req.Origin.Actor.ID = "another-user"
	req.Params = map[string]any{"game": "zzz", "uid": "10000002", "region": "prod_gf_cn"}
	if _, err := a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
	if registered == nil || used == nil || used.Model != "XQ-BC52" {
		t.Fatalf("default device not lent: %+v %+v", registered, used)
	}
	saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil || saved.Tokens.Device != nil || saved.Tokens.DeviceFP != "syntheticfingerprint" {
		t.Fatalf("lending account kept the default device: %+v %v", saved.Tokens.Device, err)
	}
}
