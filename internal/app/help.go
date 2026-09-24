package app

import (
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/pluginmeta"
	"strings"
)

func (a *App) SetManifest(raw []byte) error {
	m, err := pluginmeta.Read(raw)
	if err != nil {
		return err
	}
	if m.ID != "raylea.mihoyo-accounts" {
		return failure("plugin.account_input_invalid")
	}
	a.Manifest = m
	return nil
}
func (a *App) helpCommand(event *rayleabot.EventContext, command string) error {
	m := a.Manifest
	if command == "账号版本" {
		return event.SendText(m.Name + " " + m.Version + "\n许可：" + m.License + "\n最低宿主：" + m.MinCoreVersion)
	}
	lines := []string{m.Name + " " + m.Version, "国服米游社扫码、海外加密授权导入。“我的CK”请私聊查看，其他命令私聊或群聊均可；前缀沿用宿主设置。"}
	query := strings.Join(event.Event.Args(), " ")
	prefix := commandPrefix(event)
	for _, c := range m.Commands {
		if query == "" || strings.Contains(c.Name+c.Description+c.Usage, query) {
			lines = append(lines, usageText(c.Usage, prefix)+" · "+c.Description)
		}
	}
	lines = append(lines, "密钥、插件授权、云授权与加密备份由机器人管理员在管理页处理。请勿在聊天发送CK原文。")
	return event.SendText(strings.Join(lines, "\n"))
}

// commandPrefix is this plugin's primary command prefix: the first of the
// prefixes the host applies to it, its own before the shared ones.
func commandPrefix(event *rayleabot.EventContext) string {
	if len(event.CommandPrefixes) > 0 {
		return event.CommandPrefixes[0]
	}
	return ""
}

// usageText writes a manifest usage with the given prefix. The manifest
// writes each command with "#" as an example prefix, which the host menu
// replaces the same way.
func usageText(usage, prefix string) string {
	parts := strings.Split(usage, " / ")
	for i, part := range parts {
		parts[i] = prefix + strings.TrimPrefix(part, "#")
	}
	return strings.Join(parts, " / ")
}
