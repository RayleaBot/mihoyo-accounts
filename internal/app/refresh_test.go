package app

import (
	"context"
	"strings"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
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
	// The refresh and the role reads can outlast the event deadline, so the
	// command moves to the background and answers with an ordinary send.
	reply, actions := host.message("group", "/刷新CK", "刷新CK")
	sends := actionsNamed(actions, "message.send")
	if reply["type"] != "result" || len(actionsNamed(actions, "event.detach")) != 1 || len(sends) != 1 {
		t.Fatalf("刷新CK was not answered in the background: %v %v", reply, actions)
	}
	if text := actionText(sends[0]); !strings.Contains(text, "账号凭据已刷新") || !strings.Contains(text, "绝区零角色未能读取") || strings.Contains(text, "原神") {
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

func TestBusyPluginDoesNotRefresh(t *testing.T) {
	api := &fakeAPI{}
	a, store, _ := setup(t, api)
	host := newChatHost(t, a, store)
	host.detachRefused = true
	if reply, _ := host.message("group", "/刷新CK", "刷新CK"); sentText(reply) != busyText || api.refreshCalls.Load() != 0 {
		t.Fatalf("the busy plugin refreshed: %v", reply)
	}
}
