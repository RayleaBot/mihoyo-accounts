package accounts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func oldSubjectJSON(s Subject) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"source_protocol":%q,"source_adapter":%q,"bot_id":%q,"actor_id":%q}`, s.SourceProtocol, s.SourceAdapter, s.BotID, s.ActorID))
}

func oldSubjectKey(s Subject) string {
	return fmt.Sprintf("subject.%x", sha256.Sum256(oldSubjectJSON(s)))
}

func oldRecord(t *testing.T, record Record) map[string]any {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["owner"] = oldSubjectJSON(record.Owner)
	if grants, ok := value["delegations"].([]any); ok {
		for i, grant := range grants {
			grant.(map[string]any)["owner"] = oldSubjectJSON(record.Delegations[i].Owner)
		}
	}
	return value
}

func TestSubjectIdentityIgnoresAdapter(t *testing.T) {
	old := Subject{SourceProtocol: "onebot11", SourceAdapter: "deleted", BotID: "10001", ActorID: "20002"}
	current := old
	current.SourceAdapter = ""
	if current.Validate() != nil || !old.Equal(current) || old.Key() != current.Key() {
		t.Fatal("adapter still participates in identity")
	}
	raw, err := json.Marshal(old)
	if err != nil || strings.Contains(string(raw), "source_adapter") {
		t.Fatal("new subjects persist a connection", string(raw), err)
	}
	var loaded Subject
	if err := json.Unmarshal(oldSubjectJSON(old), &loaded); err != nil || loaded.SourceAdapter != old.SourceAdapter || !loaded.Equal(current) {
		t.Fatal("legacy recovery hint was lost", loaded, err)
	}
	for _, other := range []Subject{
		{SourceProtocol: "qqofficial", BotID: current.BotID, ActorID: current.ActorID},
		{SourceProtocol: current.SourceProtocol, BotID: "other", ActorID: current.ActorID},
		{SourceProtocol: current.SourceProtocol, BotID: current.BotID, ActorID: "other"},
	} {
		if current.Equal(other) || current.Key() == other.Key() {
			t.Fatal("different protocol, bot or user was merged")
		}
	}
}

func TestCatalogMigrationPreservesAccountsAndMergesSubjectData(t *testing.T) {
	ctx := t.Context()
	store := &memorySecrets{values: map[string]string{}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	first := Subject{"onebot11", "removed-a", "bot", "user"}
	second := first
	second.SourceAdapter = "removed-b"
	linked := Subject{"qqofficial", "removed-c", "official", "linked"}
	otherLink := linked
	otherLink.ActorID = "other-linked"
	current := first
	current.SourceAdapter = ""
	record := Record{Ref: "first", Owner: first, Revision: 7, Status: "valid", UpdatedAtMS: 1234,
		Tokens: Tokens{AccountID: "account", LToken: "synthetic-ltoken", CookieToken: "synthetic-cookie", DeviceID: "device", DeviceFP: "fingerprint", Device: &Device{Model: "imported"}},
		Roles:  []Role{{Ref: "role", Game: "zzz", UID: "100000001"}}, AllowedCallers: []string{"raylea.zzz"},
		PublicQuery: &PublicPolicy{Enabled: true, Games: []string{"zzz"}, Day: "2026-10-11", Used: 4, DailyLimit: 9, NextUseMS: 5678},
		Delegations: []Delegation{{Owner: linked, Ref: "grant", TaskID: "task", Caller: "raylea.zzz", Operation: "zzz.note", RoleRef: "role", ExpiresAtMS: 99999, LastUsedAtMS: 7777}},
	}
	otherRecord := Record{Ref: "second", Owner: second, Revision: 3, Tokens: Tokens{AccountID: "other"}, CatalogPage: 1}
	choice := Selection{AccountRef: record.Ref, RoleRef: "role"}
	existing := Selection{AccountRef: otherRecord.Ref, RoleRef: "existing-role"}
	values := map[string]any{
		catalogKey: catalog{Pages: 2, Active: 2}, pageKey(0): index{Refs: []string{record.Ref}}, pageKey(1): index{Refs: []string{otherRecord.Ref}},
		"account.first": oldRecord(t, record), "account.second": oldRecord(t, otherRecord),
		oldSubjectKey(first): index{Refs: []string{record.Ref}}, oldSubjectKey(second): index{Refs: []string{otherRecord.Ref}},
		current.Key() + ".defaults":         map[string]Selection{"zzz": existing},
		oldSubjectKey(first) + ".defaults":  map[string]Selection{"zzz": choice, "genshin": choice},
		oldSubjectKey(second) + ".defaults": map[string]Selection{"starrail": choice},
		oldSubjectKey(first) + ".uids":      map[string]UIDBinding{"zzz": {UIDs: []string{"100000001"}, Current: "100000001"}},
		oldSubjectKey(second) + ".uids":     map[string]UIDBinding{"zzz": {UIDs: []string{"100000001", "100000002"}, Current: "100000002"}},
		oldSubjectKey(linked) + ".defaults": map[string]Selection{"zzz": choice},
		oldSubjectKey(otherLink) + ".uids":  map[string]UIDBinding{"zzz": {UIDs: []string{"100000003"}}},
		publicKey:                           index{Refs: []string{record.Ref}},
		associationsKey: []any{
			map[string]any{"ref": "link", "primary": oldSubjectJSON(first), "secondary": oldSubjectJSON(linked), "created_ms": 12},
			map[string]any{"ref": "other-link", "primary": oldSubjectJSON(second), "secondary": oldSubjectJSON(otherLink), "created_ms": 13},
		},
	}
	if err := writeEncrypted(ctx, store, cipher, values); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{}
	items, err := repo.List(ctx, store, cipher, current)
	if err != nil || len(items) != 2 {
		t.Fatal("catalog could not recover both removed connections", items, err)
	}
	for _, original := range []Record{record, otherRecord} {
		plain, err := cipher.Open("account."+original.Ref, store.values["account."+original.Ref])
		want, _ := json.Marshal(original)
		if err != nil || string(plain) != string(want) {
			t.Fatal("migration changed account, device, public quota or delegation state", original.Ref, err)
		}
	}
	defaults, err := repo.Defaults(ctx, store, cipher, current)
	if err != nil || len(defaults) != 3 || defaults["zzz"] != existing {
		t.Fatal("default role merge", defaults, err)
	}
	bindings, err := repo.UIDBindings(ctx, store, cipher, current)
	if err != nil || !reflect.DeepEqual(bindings["zzz"], UIDBinding{UIDs: []string{"100000001", "100000002"}, Current: "100000001"}) {
		t.Fatal("UID merge", bindings, err)
	}
	linked.SourceAdapter, otherLink.SourceAdapter = "", ""
	shared, err := repo.List(ctx, store, cipher, linked)
	if err != nil || len(shared) != 2 || !shared[0].Shared {
		t.Fatal("association lost shared accounts", shared, err)
	}
	defaults, err = repo.Defaults(ctx, store, cipher, linked)
	if err != nil || defaults["zzz"] != choice {
		t.Fatal("linked subject default lost", defaults, err)
	}
	bindings, err = repo.UIDBindings(ctx, store, cipher, otherLink)
	if err != nil || !slices.Contains(bindings["zzz"].UIDs, "100000003") {
		t.Fatal("second association's recovery hint was discarded", bindings, err)
	}
	refs, err := repo.PublicRefs(ctx, store, cipher)
	if err != nil || !reflect.DeepEqual(refs, []string{record.Ref}) {
		t.Fatal("public pool changed", refs, err)
	}
}

func TestHashOnlyMigrationRetriesAndSerializesUIDChanges(t *testing.T) {
	ctx := t.Context()
	cipher, _ := vault.NewCipher(vault.NewKey())
	owner := Subject{"onebot11", "current-connection", "bot", "no-account"}
	store := botSecrets{memorySecrets: &memorySecrets{values: map[string]string{}}, bot: owner}
	old := owner
	old.BotID = ""
	oldKey := oldSubjectKey(old)
	choice := Selection{AccountRef: "saved-default", RoleRef: "role"}
	if err := writeEncrypted(ctx, store, cipher, map[string]any{
		oldKey + ".uids":     map[string]UIDBinding{"zzz": {UIDs: []string{"100000001"}, Current: "100000001"}},
		oldKey + ".defaults": map[string]Selection{"zzz": choice},
	}); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{}
	store.fail = true
	if _, err := repo.UIDBindings(ctx, store, cipher, owner); err == nil {
		t.Fatal("migration write failure was hidden")
	}
	if _, ok := store.values[owner.Key()+".uids"]; ok || store.values[oldKey+".migrated"] != "" {
		t.Fatal("failed migration left a partial destination or completion marker")
	}
	store.fail = false
	var wg sync.WaitGroup
	for i := 2; i <= 8; i++ {
		wg.Go(func() {
			if _, err := repo.ChangeUIDBinding(ctx, store, cipher, owner, "zzz", "bind", fmt.Sprint(100000000+i)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	bindings, err := repo.UIDBindings(ctx, store, cipher, owner)
	if err != nil || len(bindings["zzz"].UIDs) != 8 {
		t.Fatal("concurrent migration lost a UID", bindings, err)
	}
	if _, err := repo.ChangeUIDBinding(ctx, store, cipher, owner, "zzz", "remove", "100000001"); err != nil {
		t.Fatal(err)
	}
	restarted := &Repository{}
	bindings, err = restarted.UIDBindings(ctx, store, cipher, owner)
	if err != nil || slices.Contains(bindings["zzz"].UIDs, "100000001") {
		t.Fatal("old data resurrected a deleted UID", bindings, err)
	}
	defaults, err := restarted.Defaults(ctx, store, cipher, owner)
	if err != nil || defaults["zzz"] != choice {
		t.Fatal("hash-only default was not migrated", defaults, err)
	}
}

type botSecrets struct {
	*memorySecrets
	bot Subject
}

func (s botSecrets) ResolveBotID(protocol, adapter string) string {
	if s.bot.SourceProtocol == protocol && s.bot.SourceAdapter == adapter {
		return s.bot.BotID
	}
	return ""
}

func TestMissingBotRemainsVisibleUntilMatchingIdentityReturns(t *testing.T) {
	ctx := t.Context()
	store := botSecrets{memorySecrets: &memorySecrets{values: map[string]string{}}, bot: Subject{"qqofficial", "old", "wrong-protocol", ""}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	owner := Subject{"onebot11", "old", "", "user"}
	record := Record{Ref: "unresolved", Owner: owner, Revision: 1}
	if err := writeEncrypted(ctx, store, cipher, map[string]any{
		catalogKey: catalog{Pages: 1, Active: 1}, pageKey(0): index{Refs: []string{record.Ref}},
		"account.unresolved": oldRecord(t, record), oldSubjectKey(owner): index{Refs: []string{record.Ref}},
	}); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{}
	page, err := repo.ListPage(ctx, store, cipher, 0)
	if err != nil || len(page.Items) != 1 || page.Items[0].Owner.BotID != "" || page.Items[0].Owner.SourceAdapter != "old" || !page.Items[0].SubjectPending {
		t.Fatal("unresolved record was hidden or guessed", page, err)
	}
	store.bot.SourceProtocol, store.bot.BotID = "onebot11", "recovered"
	store.fail = true
	if _, err := repo.Get(ctx, store, cipher, record.Ref); err == nil {
		t.Fatal("failed catalog migration did not fail the access")
	}
	var saved Record
	if _, err := readEncrypted(ctx, store, cipher, "account.unresolved", &saved); err != nil || saved.Owner != owner {
		t.Fatal("failed migration removed its recovery hint", saved.Owner, err)
	}
	store.fail = false
	owner.BotID, owner.SourceAdapter = "recovered", ""
	items, err := repo.List(ctx, store, cipher, owner)
	if err != nil || len(items) != 1 || items[0].Ref != record.Ref || items[0].SubjectPending || items[0].Revision != record.Revision+1 {
		t.Fatal("recovered bot did not acquire its existing account", items, err)
	}
	if err := repo.Update(ctx, store, cipher, record, record.Revision); err != ErrConflict {
		t.Fatal("stale snapshot overwrote a repaired identity", err)
	}
}

func TestDelegationBotRepairRejectsAnEarlierRecordSnapshot(t *testing.T) {
	ctx := t.Context()
	store := botSecrets{memorySecrets: &memorySecrets{values: map[string]string{}}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	grantOwner := Subject{"qqofficial", "old", "", "alias"}
	record := Record{Ref: "delegated", Owner: Subject{SourceProtocol: "onebot11", BotID: "bot", ActorID: "owner"}, Revision: 5,
		Delegations: []Delegation{{Owner: grantOwner, Ref: "grant", LastUsedAtMS: 1234}},
	}
	if err := writeEncrypted(ctx, store, cipher, map[string]any{
		catalogKey: catalog{Pages: 1, Active: 1}, pageKey(0): index{Refs: []string{record.Ref}}, "account.delegated": oldRecord(t, record),
	}); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{}
	before, err := repo.Get(ctx, store, cipher, record.Ref)
	if err != nil || !before.Summary().SubjectPending {
		t.Fatal("unresolved delegation was not visible", err)
	}
	store.bot = grantOwner
	store.bot.BotID = "recovered"
	after, err := repo.Get(ctx, store, cipher, record.Ref)
	if err != nil || after.Summary().SubjectPending || after.Delegations[0].Owner.BotID != "recovered" || after.Delegations[0].LastUsedAtMS != 1234 {
		t.Fatal("delegation repair lost identity or throttle state", after, err)
	}
	if err := repo.Update(ctx, store, cipher, before, before.Revision); err != ErrConflict {
		t.Fatal("stale snapshot could discard the repaired delegation", err)
	}
}
