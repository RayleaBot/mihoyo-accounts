package app

import (
	"strings"
	"testing"

	plugin "github.com/RayleaBot/mihoyo-accounts"
)

func TestHelpWritesCommandsWithThePluginPrefix(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	if err := a.SetManifest(plugin.Info); err != nil {
		t.Fatal(err)
	}
	host := newChatHost(t, a, store)
	reply, _ := host.message("private", "/账号帮助", "账号帮助")
	text := sentText(reply)
	// The host here uses / * ~; the manifest's "#" is only an example.
	if strings.Contains(text, "#") || !strings.Contains(text, "\n/扫码登录 · ") || !strings.Contains(text, "\n/CK帮助 / /体力帮助 · ") {
		t.Fatalf("help: %q", text)
	}
}
