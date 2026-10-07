package app

import (
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

func TestUIDBindingsBelongToTheCallerAndItsGames(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	call := func(method, game, uid string) (map[string]any, error) {
		r := request(game, record.Ref)
		r.Method, r.Params = method, map[string]any{"game": game, "uid": uid}
		return a.Service(t.Context(), store, policy(), r)
	}
	if _, err := call("uids.bind", "genshin", "100000001"); err != nil {
		t.Fatal(err)
	}
	out, err := call("uids.bind", "genshin", "100000002")
	if err != nil || out["binding"].(accounts.UIDBinding).Current != "100000002" || len(out["binding"].(accounts.UIDBinding).UIDs) != 2 {
		t.Fatal(out, err)
	}
	// A plugin binds UIDs of its own games only, and UIDs have upstream's shape.
	r := request("starrail", record.Ref)
	r.Method, r.Params = "uids.bind", map[string]any{"game": "genshin", "uid": "100000003"}
	if _, err := a.Service(t.Context(), store, policy(), r); err == nil {
		t.Fatal("another game's UID was bound")
	}
	_, err = call("uids.bind", "genshin", "1234")
	assertCode(t, err, "plugin.account_input_invalid")
	if _, err = call("uids.remove", "genshin", "100000002"); err != nil {
		t.Fatal(err)
	}
	listed := request("genshin", record.Ref)
	listed.Method, listed.Params = "list", map[string]any{}
	result, err := a.Service(t.Context(), store, policy(), listed)
	if err != nil {
		t.Fatal(err)
	}
	binding := result["uid_bindings"].(map[string]accounts.UIDBinding)["genshin"]
	if len(binding.UIDs) != 1 || binding.UIDs[0] != "100000001" || binding.Current != "" {
		t.Fatalf("binding = %+v", binding)
	}
	if _, ok := result["uid_bindings"].(map[string]accounts.UIDBinding)["starrail"]; ok {
		t.Fatal("list gave another game's binding")
	}
}
