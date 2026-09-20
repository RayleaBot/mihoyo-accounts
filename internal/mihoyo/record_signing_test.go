package mihoyo

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestRecordProfilesSignExactWireQueryAndBody(t *testing.T) {
	for _, tc := range []struct{ game, operation, version, clientType, referer string }{
		{"genshin", "genshin.note", "2.40.1", "5", "https://webstatic.mihoyo.com/"},
		{"starrail", "starrail.note", "2.40.1", "5", "https://webstatic.mihoyo.com/"},
		{"zzz", "zzz.note", "2.73.1", "2", "https://act.mihoyo.com/"},
		{"genshin", "genshin.characters", "2.40.1", "5", "https://webstatic.mihoyo.com/"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				ds := strings.Split(req.Header.Get("DS"), ",")
				if len(ds) != 3 {
					t.Fatal("missing DS")
				}
				nonce, _ := strconv.Atoi(ds[1])
				if nonce < 100000 || nonce > 999999 {
					t.Fatal("invalid record nonce")
				}
				// Mainland record salt from both Yunzai-genshin and ZZZ-Plugin.
				digest := md5.Sum([]byte("salt=xV8v4Qu54lUKrEYFZkJhB8cuOh9Asafs&t=" + ds[0] + "&r=" + ds[1] + "&b=" + string(body) + "&q=" + req.URL.RawQuery))
				if ds[2] != hex.EncodeToString(digest[:]) {
					t.Fatal("DS did not sign wire query/body with record salt")
				}
				if req.Header.Get("x-rpc-app_version") != tc.version || req.Header.Get("x-rpc-client_type") != tc.clientType || req.Header.Get("Referer") != tc.referer || !strings.Contains(req.Header.Get("User-Agent"), "miHoYoBBS/"+tc.version) {
					t.Fatal("record profile mismatch")
				}
				if req.Header.Get("x-rpc-device_fp") != "syntheticfingerprint" || strings.Contains(req.Header.Get("Cookie"), "stoken") {
					t.Fatal("wrong record authentication")
				}
				return reply(map[string]any{"retcode": 0, "data": map[string]any{"current_resin": 1, "current_stamina": 2, "energy": map[string]any{}, "list": []any{}}}), nil
			}), nil)
			_, err := client.Execute(t.Context(), accounts.Tokens{DeviceID: "test-device", DeviceFP: "syntheticfingerprint", CookieToken: "synthetic-cookie", SToken: "synthetic-stoken"}, accounts.Role{Game: tc.game, UID: "100000001", Region: "cn"}, tc.operation, nil)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
