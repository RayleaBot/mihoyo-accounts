package app

import (
	"context"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

func TestRefreshCKReadsTheRolesAgain(t *testing.T) {
	api := &fakeAPI{roles: func(_ context.Context, _ accounts.Tokens, game string) ([]accounts.Role, error) {
		switch game {
		case "genshin":
			found := role("genshin")
			found.Ref, found.UID, found.Level = "genshin:cn:5678", "5678", 60
			return []accounts.Role{found}, nil
		case "starrail":
			return []accounts.Role{}, nil
		}
		return nil, &mihoyo.Error{Code: "plugin.upstream_unavailable", Operation: "accounts.roles"}
	}}
	a, store, record := setup(t, api)
	host := newChatHost(t, a, store)
	reply, _ := host.message("group", "/刷新CK", "刷新CK")
	if text := sentText(reply); !strings.Contains(text, "账号凭据已刷新") || !strings.Contains(text, "绝区零角色未能读取") || strings.Contains(text, "原神") {
		t.Fatalf("刷新CK: %q", text)
	}
	cipher, _ := a.Vault.Cipher()
	saved, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if err != nil {
		t.Fatal(err)
	}
	// 原神 was read again, 星铁 has no role now, and 绝区零 keeps its role.
	uids := map[string]string{}
	for _, found := range saved.Roles {
		uids[found.Game] = found.UID
	}
	if api.refreshCalls.Load() != 1 || len(saved.Roles) != 2 || uids["genshin"] != "5678" || uids["zzz"] != role("zzz").UID {
		t.Fatalf("roles after 刷新CK: %+v", saved.Roles)
	}
}
