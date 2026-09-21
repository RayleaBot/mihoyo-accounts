package main

import (
	"context"
	"os"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	plugin "github.com/RayleaBot/plugin-mihoyo-accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/app"
)

func main() {
	application, err := app.New(os.Getenv("RAYLEABOT_PLUGIN_DATA_DIR"), nil)
	if err != nil {
		_, _ = os.Stderr.WriteString("账号插件无法初始化，请检查数据目录。\n")
		os.Exit(1)
	}
	defer application.Close()
	if err = application.SetManifest(plugin.Info); err != nil {
		os.Exit(1)
	}
	if err := rayleabot.Run(context.Background(), rayleabot.Options{Services: application.Services()}, application); err != nil {
		_, _ = os.Stderr.WriteString("账号插件协议会话已结束。\n")
		os.Exit(1)
	}
}
