package accounts

import (
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
	"strings"
	"testing"
)

func TestAssociationsShareOneRecordSeparateDefaultsAndRestoreClears(t *testing.T) {
	repo := Repository{}
	store := &memorySecrets{values: map[string]string{}}
	cipher, _ := vault.NewCipher(vault.NewKey())
	primary := Subject{"onebot11", "a", "b", "primary"}
	secondary := Subject{"test", "other", "bot", "secondary"}
	tokens := Tokens{AccountID: "12345678", CookieToken: "synthetic-cookie", LToken: "synthetic-ltoken"}
	record, err := repo.Bind(t.Context(), store, cipher, primary, tokens, []Role{{Ref: "r", Game: "genshin", UID: "100000001", Region: "cn_gf01"}}, []string{"raylea.genshin"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := repo.Associate(t.Context(), store, cipher, primary, secondary)
	if err != nil {
		t.Fatal(err)
	}
	items, err := repo.List(t.Context(), store, cipher, secondary)
	if err != nil || len(items) != 1 || items[0].Ref != record.Ref || items[0].Owner != secondary || !items[0].Shared {
		t.Fatal(items, err)
	}
	if _, err = repo.Select(t.Context(), store, cipher, secondary, record.Ref, "r"); err != nil {
		t.Fatal(err)
	}
	values, _ := repo.Defaults(t.Context(), store, cipher, primary)
	if len(values) != 0 {
		t.Fatal("secondary changed primary default")
	}
	if _, err = repo.Associate(t.Context(), store, cipher, secondary, primary); err == nil {
		t.Fatal("cycle allowed")
	}
	third := Subject{"test", "a", "bot", "third"}
	if _, err = repo.Associate(t.Context(), store, cipher, secondary, third); err == nil {
		t.Fatal("chain allowed")
	}
	if strings.Contains(store.values[associationsKey], secondary.ActorID) {
		t.Fatal("association stored plaintext")
	}
	if err = repo.Dissociate(t.Context(), store, cipher, link.Ref, &secondary); err != nil {
		t.Fatal(err)
	}
	items, _ = repo.List(t.Context(), store, cipher, secondary)
	if len(items) != 0 {
		t.Fatal("unlinked alias still visible")
	}
	values, _ = repo.Defaults(t.Context(), store, cipher, secondary)
	if len(values) != 0 {
		t.Fatal("linked defaults survived removal")
	}
}
