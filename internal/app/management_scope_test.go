package app

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

func managementEvent(owner *accounts.Subject, action string, input map[string]any) rayleabot.Event {
	payload := map[string]any{"action": action, "payload": input}
	if owner != nil {
		payload["page_id"] = "accounts"
		payload["bot"] = map[string]any{"source_protocol": owner.SourceProtocol, "bot_id": owner.BotID}
	}
	return rayleabot.Event{EventType: "management.action", SourceProtocol: "management", SourceAdapter: "management.ui", Payload: payload}
}

func managementOrigin(owner *accounts.Subject) rayleabot.ServiceOrigin {
	fields := map[string]any{"event_type": "management.action", "source_protocol": "management", "source_adapter": "management.ui"}
	if owner != nil {
		fields["bot"] = map[string]any{"source_protocol": owner.SourceProtocol, "bot_id": owner.BotID}
	}
	raw, _ := json.Marshal(fields)
	var origin rayleabot.ServiceOrigin
	_ = json.Unmarshal(raw, &origin)
	return origin
}

func TestManagementAccountBoundary(t *testing.T) {
	api := &fakeAPI{}
	a, store, first := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	other := first.Owner
	other.BotID = "bot-2"
	second, err := a.Repo.Bind(t.Context(), store, cipher, other, syntheticTokens(), []accounts.Role{role("genshin")}, policy().IDs())
	if err != nil {
		t.Fatal(err)
	}
	second, err = a.Repo.Get(t.Context(), store, cipher, second.Ref)
	if err != nil {
		t.Fatal(err)
	}
	call := func(owner *accounts.Subject, action string, input map[string]any) (map[string]any, error) {
		return a.ManageEvent(t.Context(), store, policy(), managementEvent(owner, action, input))
	}
	listed, err := call(&first.Owner, "accounts.list", nil)
	if err != nil || listed["total"] != 1 || len(listed["items"].([]accounts.Summary)) != 1 || listed["items"].([]accounts.Summary)[0].Ref != first.Ref {
		t.Fatal(listed, err)
	}
	for _, action := range []string{"accounts.list", "accounts.check", "accounts.remove", "login.start", "login.poll", "login.cancel", "accounts.import.start", "accounts.import.step", "accounts.import.cancel", "accounts.associations.list", "subjects.get", "subjects.uids.bind"} {
		_, err := call(nil, action, map[string]any{"subject": first.Owner, "account_ref": first.Ref})
		assertCode(t, err, "platform.invalid_request")
	}
	for _, action := range []string{"accounts.check", "accounts.refresh", "accounts.remove", "accounts.remove_invalid", "accounts.roles", "accounts.permissions", "accounts.public.disable", "accounts.device.status", "accounts.device.import", "accounts.device.reset", "accounts.cloud.save", "accounts.cloud.clear", "subjects.select"} {
		_, err := call(&first.Owner, action, map[string]any{"actor_id": first.Owner.ActorID, "account_ref": second.Ref, "game": "genshin", "role_ref": role("genshin").Ref})
		assertCode(t, err, "plugin.account_subject_denied")
	}
	for _, input := range []map[string]any{{"subject": other}, {"owner": other}, {"bot_id": other.BotID}, {"all": true}} {
		_, err := call(&first.Owner, "accounts.list", input)
		assertCode(t, err, "plugin.account_subject_denied")
	}
	_, err = call(&first.Owner, "accounts.batch.start", map[string]any{"mode": "check", "items": []batchItem{{first.Ref, first.Revision}, {second.Ref, second.Revision}}})
	assertCode(t, err, "plugin.account_operation_denied")
	_, err = call(&first.Owner, "maintenance.list", nil)
	assertCode(t, err, "plugin.account_operation_denied")
	global, err := call(nil, "maintenance.list", nil)
	if err != nil || global["total"] != 2 {
		t.Fatal(global, err)
	}
	if api.executeCalls.Load() != 0 || api.refreshCalls.Load() != 0 {
		t.Fatal("foreign operation reached upstream")
	}
	saved, _ := a.Repo.Get(t.Context(), store, cipher, second.Ref)
	if !reflect.DeepEqual(saved, second) {
		t.Fatal("foreign record changed")
	}
}

func TestManagementSessionsAndAssociationOwnership(t *testing.T) {
	a, store, first := setup(t, &fakeAPI{})
	cipher, _ := a.Vault.Cipher()
	other := first.Owner
	other.BotID = "other"
	third := other
	third.BotID = "third"
	_, err := a.Repo.Bind(t.Context(), store, cipher, other, syntheticTokens(), nil, policy().IDs())
	if err != nil {
		t.Fatal(err)
	}
	link, err := a.Repo.Associate(t.Context(), store, cipher, other, third)
	if err != nil {
		t.Fatal(err)
	}
	call := func(owner accounts.Subject, action string, input map[string]any) (map[string]any, error) {
		return a.ManageEvent(t.Context(), store, policy(), managementEvent(&owner, action, input))
	}
	links, err := call(first.Owner, "accounts.associations.list", nil)
	if err != nil || len(links["items"].([]accounts.Association)) != 0 {
		t.Fatal(links, err)
	}
	_, err = call(first.Owner, "accounts.associations.remove", map[string]any{"ref": link.Ref, "confirm": true})
	assertCode(t, err, "plugin.account_subject_denied")
	links, err = call(other, "accounts.associations.list", nil)
	if err != nil || len(links["items"].([]accounts.Association)) != 1 {
		t.Fatal(links, err)
	}
	login, err := call(other, "login.start", map[string]any{"actor_id": "new-user"})
	if err != nil {
		t.Fatal(err)
	}
	loginRef := login["login"].(LoginView).Ref
	for _, action := range []string{"login.poll", "login.cancel"} {
		_, err := call(first.Owner, action, map[string]any{"login_ref": loginRef})
		assertCode(t, err, "plugin.account_subject_denied")
	}
	job, _ := a.getLogin(loginRef)
	if job.owner.BotID != other.BotID || job.owner.ActorID != "new-user" {
		t.Fatal("login owner was not host derived")
	}
	start, err := call(other, "accounts.import.start", map[string]any{"actor_id": "new-user", "realm": "os", "box": cloudBox(t, a, "ltuid_v2=123; ltoken_v2=synthetic-overseas", "account.cookie.import")})
	if err != nil {
		t.Fatal(err)
	}
	importRef := start["import"].(cookieImport).Ref
	for _, action := range []string{"accounts.import.step", "accounts.import.cancel"} {
		_, err := call(first.Owner, action, map[string]any{"ref": importRef, "sequence": 0})
		assertCode(t, err, "plugin.account_subject_denied")
	}
	for i := 0; i < 3; i++ {
		_, err = call(other, "accounts.import.step", map[string]any{"ref": importRef, "sequence": i})
		if err != nil {
			t.Fatal(err)
		}
	}
	owner := other
	owner.ActorID = "new-user"
	items, err := a.Repo.List(t.Context(), store, cipher, owner)
	if err != nil || len(items) != 1 || !items[0].Owner.Equal(owner) {
		t.Fatal(items, err)
	}
}

func TestManagementServiceOrigins(t *testing.T) {
	a, store, first := setup(t, &fakeAPI{})
	cipher, _ := a.Vault.Cipher()
	other := first.Owner
	other.BotID = "second"
	second, err := a.Repo.Bind(t.Context(), store, cipher, other, syntheticTokens(), []accounts.Role{role("genshin")}, policy().IDs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Repo.Associate(t.Context(), store, cipher, first.Owner, other); err != nil {
		t.Fatal(err)
	}
	for _, method := range serviceMethods {
		req := request("genshin", first.Ref)
		req.Method = method
		req.Origin = managementOrigin(nil)
		_, err := a.Service(t.Context(), store, policy(), req)
		if method != "ark.status" && method != "ark.request" && method != "public.execute" {
			assertCode(t, err, "platform.invalid_request")
		}
	}
	req := request("genshin", second.Ref)
	req.Origin = managementOrigin(&other)
	req.Method = "list"
	req.Params = map[string]any{"subject": other}
	result, err := a.Service(t.Context(), store, policy(), req)
	if err != nil || len(result["items"].([]accounts.Summary)) != 1 || result["items"].([]accounts.Summary)[0].Ref != second.Ref {
		t.Fatal("shared account crossed bot boundary", result, err)
	}
	delete(req.Params, "subject")
	result, err = a.Service(t.Context(), store, policy(), req)
	if err != nil || len(result["items"].([]accounts.Summary)) != 1 {
		t.Fatal(result, err)
	}
	for _, method := range []string{"list", "roles", "select", "execute", "delegation.create", "delegation.revoke", "uids.bind", "uids.remove", "uids.use"} {
		req.Method = method
		req.Params = map[string]any{"subject": first.Owner, "account_ref": first.Ref}
		_, err := a.Service(t.Context(), store, policy(), req)
		assertCode(t, err, "plugin.account_subject_denied")
	}
	for _, method := range []string{"roles", "select", "execute", "delegation.create", "delegation.revoke"} {
		req.Method = method
		req.Params = map[string]any{"account_ref": first.Ref}
		_, err := a.Service(t.Context(), store, policy(), req)
		assertCode(t, err, "plugin.account_subject_denied")
	}
	req.Method = "list"
	req.Params = map[string]any{"all": true}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_subject_denied")
	req.Method = "roles"
	req.Params = map[string]any{"account_ref": second.Ref}
	if _, err := a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
	alias := first.Owner
	alias.ActorID = "same-bot-alias"
	if _, err := a.Repo.Associate(t.Context(), store, cipher, first.Owner, alias); err != nil {
		t.Fatal(err)
	}
	req.Origin = managementOrigin(&alias)
	req.Method = "list"
	req.Params = map[string]any{"subject": alias}
	result, err = a.Service(t.Context(), store, policy(), req)
	if err != nil || len(result["items"].([]accounts.Summary)) != 1 || result["items"].([]accounts.Summary)[0].Ref != first.Ref {
		t.Fatal("same-bot sharing was lost", result, err)
	}
	req.Origin = managementOrigin(nil)
	req.Method = "ark.status"
	req.Params = map[string]any{}
	result, err = a.Service(t.Context(), store, policy(), req)
	if err != nil || result["configured"] != false {
		t.Fatal("global ark status was denied", result, err)
	}
	req.Origin = managementOrigin(&other)
	req.Method = "uids.bind"
	req.Params = map[string]any{"subject": other, "game": "genshin", "uid": "100000001"}
	if _, err := a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
}

func TestScopedConcurrentWritesPreserveOtherBotsAndSettings(t *testing.T) {
	a, store, first := setup(t, &fakeAPI{})
	cipher, _ := a.Vault.Cipher()
	if err := a.Repo.WriteSetting(t.Context(), store, cipher, "ark.token", "synthetic-setting"); err != nil {
		t.Fatal(err)
	}
	owners := []accounts.Subject{first.Owner, first.Owner}
	owners[1].SourceProtocol = "qqofficial"
	var wg sync.WaitGroup
	for _, owner := range owners {
		wg.Go(func() {
			for _, uid := range []string{"100000001", "100000002"} {
				_, err := a.ManageEvent(t.Context(), store, policy(), managementEvent(&owner, "subjects.uids.bind", map[string]any{"actor_id": owner.ActorID, "game": "genshin", "uid": uid}))
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	for _, owner := range owners {
		bindings, err := a.Repo.UIDBindings(t.Context(), store, cipher, owner)
		if err != nil || len(bindings["genshin"].UIDs) != 2 {
			t.Fatal(bindings, err)
		}
	}
	var setting string
	_, err := a.Repo.ReadSetting(t.Context(), store, cipher, "ark.token", &setting)
	if err != nil || setting != "synthetic-setting" {
		t.Fatal("global setting lost", setting, err)
	}
	if _, err := a.Repo.Get(t.Context(), store, cipher, first.Ref); err != nil {
		t.Fatal("account lost", err)
	}
}
