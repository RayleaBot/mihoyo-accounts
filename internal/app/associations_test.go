package app

import (
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"strings"
	"testing"
	"time"
)

func TestAssociationRequiresBothSubjectsAndFinalActorConfirmation(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	secondary := record.Owner
	secondary.ActorID = "other-user"
	text, err := a.associationFlow(t.Context(), store, record.Owner, "关联账号", nil)
	if err != nil {
		t.Fatal(err)
	}
	a.associations.mu.Lock()
	ref := ""
	for id := range a.associations.items {
		ref = id
	}
	a.associations.mu.Unlock()
	if ref == "" || !strings.Contains(text, ref) {
		t.Fatal("no invitation")
	}
	if _, err = a.associationFlow(t.Context(), store, secondary, "申请关联", []string{ref}); err != nil {
		t.Fatal(err)
	}
	cipher, _ := a.Vault.Cipher()
	if yes, _ := a.Repo.CanAccess(t.Context(), store, cipher, record.Owner, secondary); yes {
		t.Fatal("registration granted access")
	}
	if _, err = a.associationFlow(t.Context(), store, record.Owner, "完成关联", []string{ref, "wrong"}); err == nil {
		t.Fatal("wrong target accepted")
	}
	if _, err = a.associationFlow(t.Context(), store, secondary, "完成关联", []string{ref, secondary.ActorID}); err == nil {
		t.Fatal("secondary self-confirmed")
	}
	if _, err = a.associationFlow(t.Context(), store, record.Owner, "完成关联", []string{ref, secondary.ActorID}); err != nil {
		t.Fatal(err)
	}
	if yes, _ := a.Repo.CanAccess(t.Context(), store, cipher, record.Owner, secondary); !yes {
		t.Fatal("confirmed link missing")
	}
	if _, err = a.associationFlow(t.Context(), store, record.Owner, "完成关联", []string{ref, secondary.ActorID}); err == nil {
		t.Fatal("invitation replayed")
	}
	if _, err = a.Manage(t.Context(), store, policy(), "accounts.remove", map[string]any{"account_ref": record.Ref, "subject": secondary}); err == nil {
		t.Fatal("alias deleted primary record")
	}
}
func TestUnlinkRevokesScheduledAliasWithoutDuplicatingCredentials(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	secondary := record.Owner
	secondary.ActorID = "linked"
	link, err := a.Repo.Associate(t.Context(), store, cipher, record.Owner, secondary)
	if err != nil {
		t.Fatal(err)
	}
	req := request("genshin", record.Ref)
	req.Origin.SourceAdapter = "second"
	req.Origin.Actor.ID = secondary.ActorID
	req.Method = "list"
	req.Params = map[string]any{}
	out, err := a.Service(t.Context(), store, policy(), req)
	if err != nil {
		t.Fatal(err)
	}
	summaries := out["items"].([]accounts.Summary)
	if len(summaries) != 1 || summaries[0].Owner != secondary {
		t.Fatal("wrong notification owner", out)
	}
	req = request("genshin", record.Ref)
	req.Origin.SourceAdapter = "second"
	req.Origin.Actor.ID = secondary.ActorID
	req.Method = "delegation.create"
	req.Params["task_id"] = "linked-task"
	req.Params["days"] = 1
	out, err = a.Service(t.Context(), store, policy(), req)
	if err != nil {
		t.Fatal(err)
	}
	grant := out["delegation"].(accounts.Delegation)
	if grant.Owner != secondary {
		t.Fatal(grant)
	}
	if _, err = a.Manage(t.Context(), store, policy(), "accounts.associations.remove", map[string]any{"ref": link.Ref, "confirm": true}); err != nil {
		t.Fatal(err)
	}
	req.Method = "execute"
	req.Params["delegation_ref"] = grant.Ref
	delete(req.Params, "task_id")
	delete(req.Params, "days")
	req.Origin = rayleabot.ServiceOrigin{EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: grant.TaskID}
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_delegation_denied")
	if api.executeCalls.Load() != 0 {
		t.Fatal("unlinked delegation reached upstream")
	}
}
func TestAssociationExpiryLockAndDeviceReset(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	_, err := a.associationFlow(t.Context(), store, record.Owner, "关联账号", nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for id, v := range a.associations.items {
		ref = id
		v.Expires = time.Now().Add(-time.Second)
		a.associations.items[id] = v
	}
	_, err = a.associationFlow(t.Context(), store, record.Owner, "关联进度", []string{ref})
	assertCode(t, err, "plugin.account_association_missing")
	state, err := a.Manage(t.Context(), store, policy(), "accounts.device.status", map[string]any{"account_ref": record.Ref})
	if err != nil {
		t.Fatal(err)
	}
	if state["device_label"] != record.Tokens.DeviceID {
		t.Fatalf("device label = %v, want the whole device ID", state["device_label"])
	}
	changed, err := a.Manage(t.Context(), store, policy(), "accounts.device.reset", map[string]any{"account_ref": record.Ref, "revision": record.Revision, "confirm": true})
	if err != nil || changed["has_fingerprint"] != false {
		t.Fatal(changed, err)
	}
	cipher, _ := a.Vault.Cipher()
	saved, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	if saved.Tokens.DeviceID == record.Tokens.DeviceID || saved.Tokens.CookieToken != record.Tokens.CookieToken || saved.Tokens.DeviceFP != "" {
		t.Fatal("device reset changed credentials")
	}
	_, err = a.Manage(t.Context(), store, policy(), "accounts.device.reset", map[string]any{"account_ref": record.Ref, "revision": record.Revision, "confirm": true})
	assertCode(t, err, "plugin.account_changed")
}
