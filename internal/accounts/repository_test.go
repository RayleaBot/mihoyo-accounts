package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
	fail   bool
}

func (s *memorySecrets) Read(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok, nil
}
func (s *memorySecrets) Write(_ context.Context, values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("synthetic write failure")
	}
	for k, v := range values {
		s.values[k] = v
	}
	return nil
}

func TestEncryptedAccountsDeduplicateAndDoNotExposeTokens(t *testing.T) {
	ctx := t.Context()
	store := &memorySecrets{values: map[string]string{}}
	repo := &Repository{}
	cipher, _ := vault.NewCipher(vault.NewKey())
	owner := Subject{SourceProtocol: "onebot11", SourceAdapter: "fixture-adapter", BotID: "fixture-bot", ActorID: "fixture-user"}
	tokens := Tokens{AccountID: "123456789", CookieToken: "synthetic-cookie-marker", LToken: "synthetic-ltoken-marker", SToken: "synthetic-stoken-marker"}
	first, err := repo.Bind(ctx, store, cipher, owner, tokens, []Role{{Ref: "role-a", Game: "genshin", UID: "100000001"}}, []string{"fixture-game"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Bind(ctx, store, cipher, owner, tokens, nil, nil)
	if err != nil || first.Ref != second.Ref || second.Revision <= first.Revision {
		t.Fatalf("rebind lost identity: %v", err)
	}
	items, err := repo.List(ctx, store, cipher, owner)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v", err)
	}
	if items[0].AccountLabel != tokens.AccountID {
		t.Fatalf("account label = %q, want the whole account ID", items[0].AccountLabel)
	}
	public, _ := json.Marshal(items)
	for _, marker := range []string{tokens.CookieToken, tokens.LToken, tokens.SToken} {
		if strings.Contains(string(public), marker) {
			t.Fatal("summary leaked credentials")
		}
		for _, value := range store.values {
			if strings.Contains(value, marker) {
				t.Fatal("database value contains plaintext")
			}
		}
	}
	other := owner
	other.SourceAdapter = "other-adapter"
	visible, err := repo.List(ctx, store, cipher, other)
	if err != nil || len(visible) != 0 {
		t.Fatal("account leaked across adapter identity")
	}
}

func TestRemovedAccountCannotBeRevivedByLateRefresh(t *testing.T) {
	ctx := t.Context()
	store := &memorySecrets{values: map[string]string{}}
	repo := &Repository{}
	cipher, _ := vault.NewCipher(vault.NewKey())
	owner := Subject{SourceProtocol: "onebot11", SourceAdapter: "adapter", BotID: "bot", ActorID: "user"}
	record, err := repo.Bind(ctx, store, cipher, owner, Tokens{AccountID: "1234", CookieToken: "synthetic-cookie", LToken: "synthetic-ltoken"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Remove(ctx, store, cipher, record.Ref); err != nil {
		t.Fatal(err)
	}
	record.Tokens.CookieToken = "synthetic-refreshed-cookie"
	if err := repo.Update(ctx, store, cipher, record, record.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late refresh revived record: %v", err)
	}
	page, err := repo.ListPage(ctx, store, cipher, 0)
	if err != nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("removed account still listed: %#v %v", page, err)
	}
}

func TestFailedBatchDoesNotCreatePartialBinding(t *testing.T) {
	ctx := t.Context()
	store := &memorySecrets{values: map[string]string{}, fail: true}
	repo := &Repository{}
	cipher, _ := vault.NewCipher(vault.NewKey())
	_, err := repo.Bind(ctx, store, cipher, Subject{SourceProtocol: "onebot11", SourceAdapter: "adapter", BotID: "bot", ActorID: "user"}, Tokens{AccountID: "1234", CookieToken: "synthetic-cookie", LToken: "synthetic-ltoken"}, nil, nil)
	if err == nil || len(store.values) != 0 {
		t.Fatal("failed batch left partial account records")
	}
}
