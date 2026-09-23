package app

import (
	"context"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

func TestGachaLinkOnlyAnswersAPrivateChat(t *testing.T) {
	api := &fakeAPI{execute: func(_ context.Context, _ accounts.Tokens, role accounts.Role, name string, _ map[string]any) (map[string]any, error) {
		if name != mihoyo.ZZZGachaLink || role.Game != "zzz" {
			t.Fatal("unexpected operation", name, role.Game)
		}
		return map[string]any{"url": "https://public-operation-common.mihoyo.com/common/gacha_record/api/getGachaLog?authkey=synthetic"}, nil
	}}
	a, store, record := setup(t, api)
	link := func(game, eventType, target string) rayleabot.ServiceRequest {
		r := request(game, record.Ref)
		r.Method = "gacha.link"
		r.Params = map[string]any{"account_ref": record.Ref, "role_ref": role(game).Ref}
		r.Origin.EventType = eventType
		r.Origin.Target = &rayleabot.Target{Type: target, ID: "user-1"}
		return r
	}
	result, err := a.Service(t.Context(), store, policy(), link("zzz", "message.private", "private"))
	if err != nil || result["url"] == "" {
		t.Fatal(result, err)
	}
	_, err = a.Service(t.Context(), store, policy(), link("zzz", "message.group", "group"))
	assertCode(t, err, "plugin.account_operation_denied")
	_, err = a.Service(t.Context(), store, policy(), link("genshin", "message.private", "private"))
	assertCode(t, err, "plugin.account_operation_denied")
	// execute never reaches the link, whatever operation name a caller sends.
	r := request("zzz", record.Ref)
	r.Params["operation"] = mihoyo.ZZZGachaLink
	_, err = a.Service(t.Context(), store, policy(), r)
	assertCode(t, err, "plugin.account_operation_denied")
}
