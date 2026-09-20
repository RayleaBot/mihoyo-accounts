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
