package accounts

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func TestOwnedPaginationFiltersBeforePagingAndKeepsProtocolDistinct(t *testing.T) {
	store := &memorySecrets{values: map[string]string{}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	repo := &Repository{}
	tokens := Tokens{AccountID: "account", LToken: "synthetic-ltoken", CookieToken: "synthetic-cookie"}
	for i := 0; i < 260; i++ {
		owner := Subject{SourceProtocol: "onebot11", BotID: "same-id", ActorID: fmt.Sprint(i)}
		if i%2 == 1 {
			owner.SourceProtocol = "qqofficial"
		}
		if _, err := repo.Bind(t.Context(), store, cipher, owner, tokens, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	filter := Subject{SourceProtocol: "onebot11", BotID: "same-id"}
	first, err := repo.ListOwnedPage(t.Context(), store, cipher, filter, 0)
	if err != nil || len(first.Items) != 128 || first.Total != 130 || first.NextPage == nil || *first.NextPage != 1 {
		t.Fatal(first, err)
	}
	second, err := repo.ListOwnedPage(t.Context(), store, cipher, filter, 1)
	if err != nil || len(second.Items) != 2 || second.Total != 130 || second.NextPage != nil {
		t.Fatal(second, err)
	}
	for _, page := range []Page{first, second} {
		for _, item := range page.Items {
			if item.Owner.SourceProtocol != filter.SourceProtocol {
				t.Fatal("another protocol leaked")
			}
		}
	}
}

func TestHistoryAssignmentMergesBindingsAndRejectsCompleteOrQQIdentity(t *testing.T) {
	ctx := t.Context()
	store := &memorySecrets{values: map[string]string{}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	old := Subject{SourceProtocol: "onebot11", SourceAdapter: "deleted", ActorID: "user"}
	owner := Subject{SourceProtocol: "onebot11", BotID: "target", ActorID: old.ActorID}
	other := owner
	other.BotID = "other"
	record := Record{Ref: "old", Owner: old, Revision: 3, Tokens: Tokens{AccountID: "old"}}
	current := Record{Ref: "current", Owner: owner, Revision: 5, Tokens: Tokens{AccountID: "current"}}
	selection := Selection{AccountRef: record.Ref, RoleRef: "role"}
	if err := writeEncrypted(ctx, store, cipher, map[string]any{
		catalogKey: catalog{Pages: 1, Active: 2}, pageKey(0): index{Refs: []string{record.Ref, current.Ref}},
		"account.old": oldRecord(t, record), "account.current": current,
		oldSubjectKey(old): index{Refs: []string{record.Ref}}, owner.Key(): index{Refs: []string{current.Ref}},
		oldSubjectKey(old) + ".uids":     map[string]UIDBinding{"genshin": {UIDs: []string{"100000001"}, Current: "100000001"}},
		oldSubjectKey(old) + ".defaults": map[string]Selection{"genshin": selection},
		owner.Key() + ".uids":            map[string]UIDBinding{"genshin": {UIDs: []string{"100000002"}, Current: "100000002"}},
		other.Key() + ".uids":            map[string]UIDBinding{"genshin": {UIDs: []string{"100000003"}, Current: "100000003"}},
		"setting.ark.token":              "synthetic-global",
	}); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{}
	for _, subject := range []Subject{owner, {SourceProtocol: "qqofficial", SourceAdapter: "old", ActorID: "openid"}} {
		if err := repo.AssignMissingBot(ctx, store, cipher, subject, "target"); err != ErrAssociation {
			t.Fatal("forbidden assignment", err)
		}
	}
	if err := repo.AssignMissingBot(ctx, store, cipher, old, owner.BotID); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List(ctx, store, cipher, owner)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	bindings, err := repo.UIDBindings(ctx, store, cipher, owner)
	if err != nil || !reflect.DeepEqual(bindings["genshin"], UIDBinding{UIDs: []string{"100000002", "100000001"}, Current: "100000002"}) {
		t.Fatal(bindings, err)
	}
	defaults, err := repo.Defaults(ctx, store, cipher, owner)
	if err != nil || defaults["genshin"] != selection {
		t.Fatal(defaults, err)
	}
	bindings, err = repo.UIDBindings(ctx, store, cipher, other)
	if err != nil || bindings["genshin"].Current != "100000003" {
		t.Fatal("other bot bindings changed", bindings, err)
	}
	var setting string
	_, err = repo.ReadSetting(ctx, store, cipher, "ark.token", &setting)
	if err != nil || setting != "synthetic-global" {
		t.Fatal(setting, err)
	}
	if err := repo.AssignMissingBot(ctx, store, cipher, old, "another-target"); err == nil {
		t.Fatal("assigned identity was assigned again")
	}
}

func TestAnonymousHistoricalBindingsCanBeInspectedAndRemoved(t *testing.T) {
	store := &memorySecrets{values: map[string]string{}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	repo := &Repository{}
	key := oldSubjectKey(Subject{SourceProtocol: "qqofficial", ActorID: "lost"})
	values := map[string]UIDBinding{"zzz": {UIDs: []string{"100000001"}}}
	if err := writeEncrypted(t.Context(), store, cipher, map[string]any{key + ".uids": values, "setting.ark.token": "synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	items, err := repo.SubjectData(t.Context(), store, cipher, []string{key})
	if err != nil || len(items) != 1 || items[0].Owner != nil || !reflect.DeepEqual(items[0].Bindings, values) {
		t.Fatal(items, err)
	}
	if _, err := repo.SubjectData(t.Context(), store, cipher, []string{"setting.ark.token"}); err == nil {
		t.Fatal("arbitrary secret exposed")
	}
	if err := repo.RemoveSubjectData(t.Context(), store, cipher, key); err != nil {
		t.Fatal(err)
	}
	items, err = repo.SubjectData(t.Context(), store, cipher, []string{key})
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
}

func TestUnresolvedQQAssociationCanBeRemovedWithoutAssigningIt(t *testing.T) {
	store := &memorySecrets{values: map[string]string{}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	primary := Subject{SourceProtocol: "onebot11", BotID: "bot", ActorID: "user"}
	secondary := Subject{SourceProtocol: "qqofficial", SourceAdapter: "old", ActorID: "openid"}
	if err := writeEncrypted(t.Context(), store, cipher, map[string]any{
		associationsKey: []Association{{Ref: "missing-bot", Primary: primary, Secondary: secondary}},
		legacySubjectKey(secondary) + ".defaults": map[string]Selection{"genshin": {AccountRef: "shared", RoleRef: "role"}},
	}); err != nil {
		t.Fatal(err)
	}
	repo := &Repository{}
	if err := repo.Dissociate(t.Context(), store, cipher, "missing-bot", nil); err != nil {
		t.Fatal(err)
	}
	links, err := repo.Associations(t.Context(), store, cipher)
	if err != nil || len(links) != 0 {
		t.Fatal(links, err)
	}
	var defaults map[string]Selection
	if _, err := readEncrypted(t.Context(), store, cipher, legacySubjectKey(secondary)+".defaults", &defaults); err != nil || len(defaults) != 0 {
		t.Fatal(defaults, err)
	}
}
