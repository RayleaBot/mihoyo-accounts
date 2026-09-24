package app

import "regexp"

// pastedCredential finds miHoYo account credentials pasted into chat: cookie
// fields, SToken and login tickets. Upstream reads them from chat; here they
// are only accepted encrypted, so a pasted one is answered with how to
// withdraw it instead. Gacha links are not account credentials: the game
// plugins read them from chat as upstream does.
var pastedCredential = regexp.MustCompile(`(?i)\b(?:ltoken(?:_v2)?|cookie_token(?:_v2)?|stoken(?:_v2)?|login_ticket|ltuid(?:_v2)?|_MHYUUID)\s*=`)

// credentialWarning asks to withdraw a pasted credential: CK is the one thing
// kept out of chats, as upstream asks to send cookies privately. One posted in
// a group is public and has to be made invalid.
func credentialWarning(group bool) string {
	text := "检测到米游社凭据（Cookie 或 SToken）。机器人不从聊天读取账号凭据，请撤回这条消息。绑定账号请发送“扫码登录”，或请机器人管理员在管理页的加密表单中导入。"
	if group {
		text = "这条消息把米游社凭据公开在了群里，请立即撤回，并在米游社退出登录或修改密码，让这份凭据失效。机器人不从聊天读取账号凭据：绑定账号请发送“扫码登录”，或请机器人管理员在管理页的加密表单中导入。"
	}
	return text
}
