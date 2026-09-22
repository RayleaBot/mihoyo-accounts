package app

import "regexp"

// pastedCredential finds miHoYo credentials pasted into chat: cookie fields,
// SToken, login tickets and gacha-link authkeys. Upstream reads them from
// chat; here they are only accepted encrypted from the management page, so a
// pasted one is answered with how to withdraw it instead.
var pastedCredential = regexp.MustCompile(`(?i)\b(?:ltoken(?:_v2)?|cookie_token(?:_v2)?|stoken(?:_v2)?|login_ticket|ltuid(?:_v2)?|_MHYUUID)\s*=|\bauthkey=`)

// credentialWarning asks to withdraw a pasted credential, as upstream asks to
// send cookies privately and to recall gacha links; one posted in a group is
// public and has to be made invalid.
func credentialWarning(group bool) string {
	text := "检测到米游社凭据（Cookie、SToken 或抽卡链接）。机器人不从聊天读取凭据，请撤回这条消息。绑定账号请私聊发送“扫码登录”，或在米游社账号管理页的加密表单中导入；抽卡链接同样在管理页导入。"
	if group {
		text = "这条消息把米游社凭据公开在了群里，请立即撤回，并在米游社退出登录或修改密码，让这份凭据失效。机器人不从聊天读取凭据：绑定账号请私聊发送“扫码登录”，或在米游社账号管理页的加密表单中导入；抽卡链接同样在管理页导入。"
	}
	return text
}
