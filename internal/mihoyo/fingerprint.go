package mihoyo

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

// Fingerprint registers the account's stable logical device with the official
// endpoint. The compatibility profile follows the local ZZZ reference; it does
// not collect hardware information from the host or send account credentials.
func (c *Client) Fingerprint(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	if tokens.DeviceID == "" {
		return accounts.Tokens{}, invalidInput("device.fingerprint")
	}
	ext, _ := json.Marshal(map[string]any{
		"deviceName": "XQ-BC52", "productName": "XQ-BC52_EEA", "model": "XQ-BC52",
		"brand": "Sony", "manufacturer": "Sony", "deviceType": "XQ-BC52",
		"deviceInfo": "Sony/XQ-BC52_EEA/XQ-BC52:13/61.2.A.0.472A/061002A0000472A0046651803:user/release-keys",
		"display":    "61.2.A.0.472A", "osVersion": "13", "sdkVersion": "33", "board": "lahaina",
		"hardware": "qcom", "cpuType": "arm64-v8a", "buildType": "user", "buildTags": "release-keys",
		"packageName": "com.mihoyo.hyperion", "packageVersion": "2.73.1",
		"aaid": tokens.DeviceID, "vaid": tokens.DeviceID, "oaid": tokens.DeviceID,
		"screenSize": "1096x2434", "networkType": "WiFi", "romCapacity": "768", "romRemain": "727",
		"ramCapacity": "224845", "ramRemain": "218344", "appMemory": "768",
		"isRoot": 0, "isTablet": 0, "emulatorStatus": 0, "proxyStatus": 1,
	})
	if device := tokens.Device; device != nil {
		// An imported device registers as itself, as ZZZ-Plugin's bound
		// device does.
		ext, _ = json.Marshal(map[string]any{
			"deviceName": device.Name, "productName": device.Product, "model": device.Model, "brand": device.Brand(), "manufacturer": device.Brand(),
			"deviceType": device.Name, "deviceInfo": device.Fingerprint, "display": device.Display(), "osVersion": device.AndroidVersion, "board": device.Board,
			"packageName": "com.mihoyo.hyperion", "packageVersion": "2.73.1", "aaid": device.OAID, "vaid": device.OAID, "oaid": device.OAID,
			"networkType": "WiFi", "isRoot": 0, "isTablet": 0, "emulatorStatus": 0, "proxyStatus": 0,
		})
	}
	body := map[string]any{
		"app_name": "bbs_cn", "platform": "2", "device_id": tokens.DeviceID,
		"bbs_device_id": tokens.DeviceID, "seed_id": strings.ToLower(RandomDeviceID()[:16]),
		"seed_time": strconv.FormatInt(c.now().UnixMilli(), 10), "ext_fields": string(ext),
		// Public bootstrap value used by the reference client, replaced by the
		// returned device-specific value before an authenticated record request.
		"device_fp": "38d805c20d53d",
	}
	host := fingerprintHost
	if tokens.Realm == "os" {
		host = "https://sg-public-data-api.hoyoverse.com"
		body["app_name"] = "bbs_oversea"
		body["hoyolab_device_id"] = tokens.DeviceID
		delete(body, "bbs_device_id")
		ext, _ := json.Marshal(map[string]any{"oaid": tokens.DeviceID, "board": "lahaina"})
		body["ext_fields"] = string(ext)
	}
	data, err := c.request(ctx, "device.fingerprint", host, "/device-fp/api/getFp", "fingerprint", nil, body, accounts.Tokens{DeviceID: tokens.DeviceID})
	if err != nil {
		return accounts.Tokens{}, err
	}
	// This endpoint has an additional status inside its successful envelope.
	if integer(data["code"]) != 200 {
		return accounts.Tokens{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "device.fingerprint", RetCode: integer(data["code"]), Reason: "fingerprint_rejected"}
	}
	fp := text(data["device_fp"])
	if len(fp) < 8 || len(fp) > 128 || strings.IndexFunc(fp, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') }) >= 0 {
		return accounts.Tokens{}, &Error{Code: "plugin.upstream_response_invalid", Operation: "device.fingerprint", Reason: "fingerprint_invalid"}
	}
	tokens.DeviceFP = fp
	tokens.DeviceFPExpiresAtMS = c.now().Add(72 * time.Hour).UnixMilli()
	return tokens, nil
}
