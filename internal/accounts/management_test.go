package accounts

import (
	"fmt"
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
