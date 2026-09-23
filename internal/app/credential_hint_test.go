package app

import "testing"

func TestPastedCredentialsAreRecognized(t *testing.T) {
	for _, text := range []string{
		"ltuid=100000001; ltoken=abcdef; account_id=100000001",
		"cookie_token_v2=v2_abc; account_mid_v2=abc",
		"stoken=v2_abc;stuid=100000001",
		"login_ticket=abc",
		"_MHYUUID=abc-def",
	} {
		if !pastedCredential.MatchString(text) {
			t.Errorf("not recognized: %s", text)
		}
	}
	// Gacha links are left to the game plugins.
	for _, text := range []string{"#刷新CK", "我的ltoken过期了吗", "authkey_ver=1", "token=abc", "扫码登录", "https://public-operation-hk4e.mihoyo.com/gacha_info/api/getGachaLog?authkey_ver=1&authkey=abc%2B&page=1"} {
		if pastedCredential.MatchString(text) {
			t.Errorf("recognized ordinary text: %s", text)
		}
	}
}
