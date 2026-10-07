package mihoyo

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

// Fingerprint registers the account's stable logical device with the official
// endpoint. The compatibility profile follows the local ZZZ reference; it does
// not collect hardware information from the host or send account credentials.
// An account with a device of its own registers that device, as ZZZ-Plugin's
// bound device; others register ZZZ-Plugin's default device.
func (c *Client) Fingerprint(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	if tokens.DeviceID == "" {
		return accounts.Tokens{}, invalidInput("device.fingerprint")
	}
	device := fingerprintDevice
	if tokens.Device != nil {
		device = *tokens.Device
	}
	ext, _ := json.Marshal(fingerprintFields(device, tokens.DeviceID))
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

// fingerprintDevice is ZZZ-Plugin's defSet device.yaml, the device it
// registers for users who bound none; Name stands for its productType.
var fingerprintDevice = accounts.Device{Name: "XQ-BC52", Board: "lahaina", Model: "XQ-BC52", AndroidVersion: "13", Fingerprint: "Sony/XQ-BC52_EEA/XQ-BC52:13/61.2.A.0.472A/061002A0000472A0046651803:user/release-keys", Product: "XQ-BC52_EEA"}

// fingerprintFields are ZZZ-Plugin's getFp ext_fields for a device: the
// model is both deviceName and model, the device's own name is deviceType,
// the advertising IDs are the device identifier and oaid is the device's
// own when it has one.
func fingerprintFields(device accounts.Device, deviceID string) map[string]any {
	oaid := device.OAID
	if oaid == "" {
		oaid = deviceID
	}
	return map[string]any{
		"proxyStatus": 1, "isRoot": 0, "romCapacity": "768", "deviceName": device.Model, "productName": device.Product, "romRemain": "727",
		"hostname": "BuildHost", "screenSize": "1096x2434", "isTablet": 0, "aaid": deviceID, "model": device.Model, "brand": device.Brand(),
		"hardware": "qcom", "deviceType": device.Name, "devId": "REL", "serialNumber": "unknown", "sdCapacity": 224845, "buildTime": "1692775759000",
		"buildUser": "BuildUser", "simState": 1, "ramRemain": "218344", "appUpdateTimeDiff": 1740498108042, "deviceInfo": device.Fingerprint, "vaid": deviceID,
		"buildType": "user", "sdkVersion": "33", "ui_mode": "UI_MODE_TYPE_NORMAL", "isMockLocation": 0, "cpuType": "arm64-v8a", "isAirMode": 0,
		"ringMode": 2, "chargeStatus": 1, "manufacturer": device.Brand(), "emulatorStatus": 0, "appMemory": "768", "osVersion": device.AndroidVersion,
		"vendor": "unknown", "accelerometer": "-1.588236x6.8404818x6.999604", "sdRemain": 218214, "buildTags": "release-keys", "packageName": "com.mihoyo.hyperion",
		"networkType": "WiFi", "oaid": oaid, "debugStatus": 1, "ramCapacity": "224845", "magnetometer": "-47.04375x51.3375x137.96251", "display": device.Display(),
		"appInstallTimeDiff": 1740498108042, "packageVersion": "2.35.0", "gyroscope": "-0.22601996x-0.09453133x0.09040799", "batteryStatus": 88, "hasKeyboard": 0,
		"board": device.Board,
	}
}
