package app

import (
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"testing"
	"time"
)

func TestDelegatedReadIsBoundToCallerTaskRoleAndExpiry(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	ctx := t.Context()
	create := request("genshin", record.Ref)
	create.Method = "delegation.create"
	create.Params["task_id"] = "game.reminder.test"
	create.Params["days"] = 30
	result, err := a.Service(ctx, store, policy(), create)
	if err != nil {
		t.Fatal(err)
	}
	grant := result["delegation"].(accounts.Delegation)
	scheduled := func() rayleabot.ServiceRequest {
		req := request("genshin", record.Ref)
		req.Params["delegation_ref"] = grant.Ref
		req.Origin = rayleabot.ServiceOrigin{EventID: "tick", EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: grant.TaskID}
		return req
	}
	for _, change := range []func(*rayleabot.ServiceRequest){
		func(r *rayleabot.ServiceRequest) { r.Origin.TaskID = "other" }, func(r *rayleabot.ServiceRequest) { r.Params["delegation_ref"] = "missing" }, func(r *rayleabot.ServiceRequest) { r.Params["operation"] = "genshin.profile" }, func(r *rayleabot.ServiceRequest) { r.Params["input"] = map[string]any{"uid": "other"} },
	} {
		req := scheduled()
		change(&req)
		_, err := a.Service(ctx, store, policy(), req)
		assertCode(t, err, "plugin.account_delegation_denied")
	}
	bad := scheduled()
	bad.Method = "delegation.create"
	_, err = a.Service(ctx, store, policy(), bad)
	assertCode(t, err, "plugin.account_subject_denied")
	bad = scheduled()
	bad.CallerPluginID = "raylea.starrail"
	bad.Params["operation"] = "starrail.note"
	bad.Params["role_ref"] = role("starrail").Ref
	_, err = a.Service(ctx, store, policy(), bad)
	assertCode(t, err, "plugin.account_delegation_denied")
	bad = request("genshin", record.Ref)
	bad.Params["delegation_ref"] = grant.Ref
	bad.Origin.Actor.ID = "other-owner"
	_, err = a.Service(ctx, store, policy(), bad)
	assertCode(t, err, "plugin.account_subject_denied")
	if api.executeCalls.Load() != 0 {
		t.Fatal("rejected delegation reached upstream")
	}
	store.fail = true
	_, err = a.Service(ctx, store, policy(), scheduled())
	if err == nil || api.executeCalls.Load() != 0 {
		t.Fatal("uncommitted admission reached upstream")
	}
	store.fail = false
	if _, err = a.Service(ctx, store, policy(), scheduled()); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(ctx, store, policy(), scheduled())
	assertCode(t, err, "plugin.account_delegation_throttled")
	// A new repository reader sees the persisted cooldown, with no in-memory grant registry.
	cipher, _ := a.Vault.Cipher()
	saved, err := a.Repo.Get(ctx, store, cipher, record.Ref)
	if err != nil || saved.Delegations[0].LastUsedAtMS == 0 {
		t.Fatal("cooldown not durable", err)
	}
	saved.Delegations[0].ExpiresAtMS = time.Now().Add(-time.Minute).UnixMilli()
	if err = a.Repo.Update(ctx, store, cipher, saved, saved.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(ctx, store, policy(), scheduled())
	assertCode(t, err, "plugin.account_delegation_denied")
	result, err = a.Service(ctx, store, policy(), create)
	if err != nil {
		t.Fatal(err)
	}
	grant = result["delegation"].(accounts.Delegation)
	revoke := request("genshin", record.Ref)
	revoke.Method = "delegation.revoke"
	revoke.Params["delegation_ref"] = grant.Ref
	if _, err = a.Service(ctx, store, policy(), revoke); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(ctx, store, policy(), scheduled())
	assertCode(t, err, "plugin.account_delegation_denied")
	if api.executeCalls.Load() != 1 {
		t.Fatal("unexpected authenticated calls")
	}
}

func TestChallengeDelegationAllowsOnlyFixedReadAndEmptyInput(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	create := request("genshin", record.Ref)
	create.Method = "delegation.create"
	create.Params["operation"] = "genshin.abyss"
	create.Params["task_id"] = "game.challenge.test"
	create.Params["days"] = 30
	result, err := a.Service(t.Context(), store, policy(), create)
	if err != nil {
		t.Fatal(err)
	}
	grant := result["delegation"].(accounts.Delegation)
	req := request("genshin", record.Ref)
	req.Params["operation"] = "genshin.abyss"
	req.Params["delegation_ref"] = grant.Ref
	req.Origin = rayleabot.ServiceOrigin{EventID: "tick", EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: grant.TaskID}
	req.Params["input"] = map[string]any{"schedule_type": 2}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_denied")
	req.Params["input"] = map[string]any{}
	if _, err = a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_throttled")
	create.Params["operation"] = "genshin.profile"
	_, err = a.Service(t.Context(), store, policy(), create)
	assertCode(t, err, "plugin.account_operation_denied")
}

func TestMonthlyDelegationAllowsOnlyDefaultMonth(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	create := request("genshin", record.Ref)
	create.Method = "delegation.create"
	create.Params["operation"] = "genshin.monthly"
	create.Params["task_id"] = "game.monthly.test"
	create.Params["days"] = 30
	result, err := a.Service(t.Context(), store, policy(), create)
	if err != nil {
		t.Fatal(err)
	}
	grant := result["delegation"].(accounts.Delegation)
	req := request("genshin", record.Ref)
	req.Params["operation"] = "genshin.monthly"
	req.Params["delegation_ref"] = grant.Ref
	req.Origin = rayleabot.ServiceOrigin{EventID: "tick", EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: grant.TaskID}
	req.Params["input"] = map[string]any{"month": 202608}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_denied")
	req.Params["input"] = map[string]any{}
	if _, err = a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_throttled")
	create.Params["operation"] = "genshin.profile"
	_, err = a.Service(t.Context(), store, policy(), create)
	assertCode(t, err, "plugin.account_operation_denied")
}

func TestAccountCommunityDelegationKeepsTaskScopeAndWriteConfirmation(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	create := request("genshin", record.Ref)
	create.Method = "delegation.create"
	create.Params["operation"] = "genshin.community_run"
	create.Params["role_ref"] = ""
	create.Params["task_id"] = "game.community.test"
	create.Params["days"] = 1
	_, err := a.Service(t.Context(), store, policy(), create)
	assertCode(t, err, "plugin.account_write_confirmation_required")
	create.Params["write_confirmed"] = true
	out, err := a.Service(t.Context(), store, policy(), create)
	if err != nil {
		t.Fatal(err)
	}
	grant := out["delegation"].(accounts.Delegation)
	req := request("genshin", record.Ref)
	req.Params["operation"] = "genshin.community_run"
	req.Params["role_ref"] = ""
	req.Params["delegation_ref"] = grant.Ref
	req.Params["write_confirmed"] = true
	req.Origin = rayleabot.ServiceOrigin{EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: grant.TaskID}
	req.Params["input"] = map[string]any{"action": "like", "post_id": "123", "url": "https://other"}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_denied")
	req.Params["input"] = map[string]any{"action": "status"}
	if _, err = a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_throttled")
}

func TestOneTaskHoldsADelegationPerOperation(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	grants := map[string]accounts.Delegation{}
	for _, operation := range []string{"zzz.challenge", "zzz.deadly", "zzz.challenge"} {
		create := request("zzz", record.Ref)
		create.Method = "delegation.create"
		create.Params["operation"] = operation
		create.Params["task_id"] = "game.challenge.pair"
		create.Params["days"] = 30
		result, err := a.Service(t.Context(), store, policy(), create)
		if err != nil {
			t.Fatal(err)
		}
		grant := result["delegation"].(accounts.Delegation)
		// Creating again for the same operation keeps its delegation.
		if previous, ok := grants[operation]; ok && previous.Ref != grant.Ref {
			t.Fatal("the same operation got a second delegation")
		}
		grants[operation] = grant
	}
	cipher, _ := a.Vault.Cipher()
	saved, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if len(saved.Delegations) != 2 {
		t.Fatalf("delegations = %+v", saved.Delegations)
	}
	for operation, grant := range grants {
		req := request("zzz", record.Ref)
		req.Params["operation"] = operation
		req.Params["delegation_ref"] = grant.Ref
		req.Origin = rayleabot.ServiceOrigin{EventID: "tick", EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: "game.challenge.pair"}
		if _, err := a.Service(t.Context(), store, policy(), req); err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		// A delegation admits only its own operation.
		req.Params["operation"] = map[string]string{"zzz.challenge": "zzz.deadly", "zzz.deadly": "zzz.challenge"}[operation]
		req.Origin.EventID = "tick-2"
		_, err := a.Service(t.Context(), store, policy(), req)
		assertCode(t, err, "plugin.account_delegation_denied")
	}
}

// Paging reads a game plugin follows in one background event are not
// delegated to its scheduled tasks.
func TestPagingReadsAreNotDelegated(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	for game, operations := range map[string][]string{"genshin": {"genshin.gacha", "genshin.billing"}, "zzz": {"zzz.gacha", "zzz.character"}} {
		for _, operation := range operations {
			create := request(game, record.Ref)
			create.Method = "delegation.create"
			create.Params["operation"] = operation
			create.Params["task_id"] = "game.paging.test"
			create.Params["days"] = 1
			_, err := a.Service(t.Context(), store, policy(), create)
			assertCode(t, err, "plugin.account_operation_denied")
		}
	}
}
