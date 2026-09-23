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
	lines := []string{m.Name + " " + m.Version, "国服米游社扫码、海外加密授权导入。账号命令除帮助外请在本人私聊使用；前缀沿用宿主设置。"}
	query := strings.Join(event.Event.Args(), " ")
	for _, c := range m.Commands {
		if query == "" || strings.Contains(c.Name+c.Description+c.Usage, query) {
			lines = append(lines, c.Usage+" · "+c.Description)
		}
	}
	lines = append(lines, "密钥、授权插件、云授权、关联和加密备份在账号管理页。请勿在聊天发送CK原文。")
	return event.SendText(strings.Join(lines, "\n"))
}
