package app

import (
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

func TestDeviceImportTakesZZZPluginsTwoForms(t *testing.T) {
	tokens, err := importDevice([]byte(`{"device_id":"0123456789abcdef0123","device_fp":"38d7faa51d2e1"}`), accounts.Tokens{DeviceID: "old-device-id-000000", DeviceFP: "old"})
	if err != nil || tokens.DeviceID != "0123456789abcdef0123" || tokens.DeviceFP != "38d7faa51d2e1" || !tokens.DeviceFPImported || tokens.Device != nil {
		t.Fatal(tokens, err)
	}
	info := `{"deviceName":"Pixel 7","deviceBoard":"panther","deviceModel":"Pixel 7","oaid":"synthetic-oaid","androidVersion":"14","deviceFingerprint":"google/panther/panther:14/UQ1A.240205.004/11269751:user/release-keys","deviceProduct":"panther"}`
	tokens, err = importDevice([]byte(info), tokens)
	if err != nil || tokens.Device == nil || tokens.Device.Brand() != "google" || tokens.Device.Display() != "UQ1A.240205.004" || tokens.DeviceFPImported || tokens.DeviceFP != "" {
		t.Fatal(tokens, err)
	}
	for _, bad := range []string{`{"device_id":"short","device_fp":"38d7faa51d2e1"}`, `{"deviceName":"Pixel 7"}`, `not json`} {
		if _, err := importDevice([]byte(bad), accounts.Tokens{}); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}

func TestImportedDeviceIsKeptThroughTheManagementPage(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	status, err := a.Manage(t.Context(), store, policy(), "accounts.device.status", map[string]any{"account_ref": record.Ref})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.device.import", map[string]any{"account_ref": record.Ref, "device_id": "plain"}); err == nil {
		t.Fatal("plaintext device accepted")
	}
	out, err := a.Manage(t.Context(), store, policy(), "accounts.device.import", map[string]any{"account_ref": record.Ref, "revision": status["revision"], "box": cloudBox(t, a, `{"device_id":"0123456789abcdef0123","device_fp":"38d7faa51d2e1"}`, "account.device.import")})
	if err != nil || out["fingerprint_imported"] != true || out["has_fingerprint"] != true {
		t.Fatal(out, err)
	}
}
