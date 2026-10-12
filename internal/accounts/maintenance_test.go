package accounts

import (
	"fmt"
	"sync"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func TestInvalidCleanupAndRebindShareAtomicVersionCheck(t *testing.T) {
	for attempt := 0; attempt < 25; attempt++ {
		repo := &Repository{}
		store := &memorySecrets{values: map[string]string{}}
		cipher, _ := vault.NewCipher(vault.NewKey())
		owner := Subject{SourceProtocol: "onebot11", BotID: "bot", ActorID: fmt.Sprint(attempt)}
		tokens := Tokens{AccountID: "100001", LToken: "synthetic-ltoken", CookieToken: "synthetic-cookie"}
		r, err := repo.Bind(t.Context(), store, cipher, owner, tokens, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Status = "invalid"
		if err = repo.Update(t.Context(), store, cipher, r, r.Revision); err != nil {
			t.Fatal(err)
		}
		r, _ = repo.Get(t.Context(), store, cipher, r.Ref)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if _, err := repo.RemoveIfInvalid(t.Context(), store, cipher, r.Ref, r.Revision); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if _, err := repo.Bind(t.Context(), store, cipher, owner, tokens, nil, nil); err != nil {
				t.Error(err)
			}
		}()
		close(start)
		wg.Wait()
		items, err := repo.List(t.Context(), store, cipher, owner)
		if err != nil || len(items) != 1 || items[0].Status != "valid" {
			t.Fatal("cleanup removed renewed login", items, err)
		}
	}
}
func TestCheckMetadataRejectsUnrecognizedBackupFields(t *testing.T) {
	for _, check := range []*CheckResult{{Kind: "check", State: "available", Code: "synthetic-credential", CheckedAtMS: 1}, {Kind: "check", State: "unavailable", Code: "plugin.upstream_synthetic_secret", CheckedAtMS: 1}, {Kind: "synthetic-credential", State: "available", CheckedAtMS: 1}} {
		if check.Valid() {
			t.Fatal("unsafe metadata accepted")
		}
	}
	if !(*CheckResult)(nil).Valid() || !(&CheckResult{Kind: "check", State: "available", CheckedAtMS: 1}).Valid() {
		t.Fatal("legacy or valid metadata rejected")
	}
}
