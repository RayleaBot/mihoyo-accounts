package mihoyo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestFingerprintRegistrationNeverSendsAccountCredentials(t *testing.T) {
	now := time.Unix(1700000000, 0)
	tokens := accounts.Tokens{DeviceID: "test-device", SToken: "synthetic-stoken", LToken: "synthetic-ltoken", CookieToken: "synthetic-cookie", AccountID: "private-account", MID: "private-mid"}
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		if req.Method != "POST" || req.URL.String() != "https://public-data-api.mihoyo.com/device-fp/api/getFp" || req.Header.Get("Cookie") != "" || req.Header.Get("DS") != "" {
			t.Fatal("fingerprint endpoint or anonymous profile mismatch")
		}
		wire := string(body) + req.URL.String()
		for _, value := range []string{tokens.SToken, tokens.LToken, tokens.CookieToken, tokens.AccountID, tokens.MID} {
			if strings.Contains(wire, value) {
				t.Fatal("registration leaked account credentials")
			}
		}
		var data map[string]any
		if json.Unmarshal(body, &data) != nil || data["device_id"] != tokens.DeviceID || data["bbs_device_id"] != tokens.DeviceID || data["platform"] != "2" {
			t.Fatal("device identity mismatch")
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"code": 200, "device_fp": "syntheticfingerprint"}}), nil
	}), func() time.Time { return now })
	updated, err := client.Fingerprint(t.Context(), tokens)
	if err != nil || updated.DeviceFP != "syntheticfingerprint" || updated.DeviceFPExpiresAtMS != now.Add(72*time.Hour).UnixMilli() || updated.CookieToken != tokens.CookieToken {
		t.Fatal("fingerprint update lost credentials or expiry")
	}
}

func TestFingerprintRejectsInnerFailureAndMalformedValue(t *testing.T) {
	for _, data := range []map[string]any{{"code": 500, "device_fp": "syntheticfingerprint"}, {"code": 200}, {"code": 200, "device_fp": "unsafe;value"}} {
		client := New(doerFunc(func(*http.Request) (*http.Response, error) {
			return reply(map[string]any{"retcode": 0, "data": data}), nil
		}), nil)
		if _, err := client.Fingerprint(t.Context(), accounts.Tokens{DeviceID: "test-device"}); err == nil {
			t.Fatal("invalid fingerprint accepted")
		}
	}
}

func TestFingerprintRegistersDevicesAsZZZPlugin(t *testing.T) {
	sent := map[string]any{}
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)
		sent = map[string]any{}
		_ = json.Unmarshal([]byte(body["ext_fields"].(string)), &sent)
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"code": 200, "device_fp": "syntheticfingerprint"}}), nil
	}), nil)
	device := &accounts.Device{Name: "panther", Board: "panther", Model: "Pixel 7", OAID: "own-oaid", AndroidVersion: "14", Fingerprint: "google/panther/panther:14/UQ1A.240205.004/11269751:user/release-keys", Product: "panther"}
	if _, err := client.Fingerprint(t.Context(), accounts.Tokens{DeviceID: "test-device", Device: device}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"deviceName": "Pixel 7", "model": "Pixel 7", "deviceType": "panther", "brand": "google", "display": "UQ1A.240205.004", "osVersion": "14", "aaid": "test-device", "vaid": "test-device", "oaid": "own-oaid", "proxyStatus": float64(1)}
	for key, value := range want {
		if sent[key] != value {
			t.Errorf("%s = %v, want %v", key, sent[key], value)
		}
	}
	// Without a device of its own, ZZZ-Plugin's default device registers
	// under the account's identifier.
	if _, err := client.Fingerprint(t.Context(), accounts.Tokens{DeviceID: "test-device"}); err != nil {
		t.Fatal(err)
	}
	if sent["deviceName"] != "XQ-BC52" || sent["productName"] != "XQ-BC52_EEA" || sent["oaid"] != "test-device" || sent["board"] != "lahaina" {
		t.Errorf("default device = %v", sent)
	}
}
