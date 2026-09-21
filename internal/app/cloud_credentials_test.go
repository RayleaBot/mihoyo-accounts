package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
	"math/big"
	"strings"
	"testing"
)

func cloudBox(t *testing.T, a *App, value string, purposes ...string) vault.InputBox {
	t.Helper()
	jwk := a.Vault.PublicKey()
	n, _ := base64.RawURLEncoding.DecodeString(jwk["n"].(string))
	e, _ := base64.RawURLEncoding.DecodeString(jwk["e"].(string))
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	key := make([]byte, 32)
	rand.Read(key)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	rand.Read(nonce)
	purpose := "account.cloud.save"
	if len(purposes) > 0 {
		purpose = purposes[0]
	}
	label := []byte("raylea/credential-input/v1/" + purpose)
	sealed := aead.Seal(nil, nonce, []byte(value), label)
	wrapped, _ := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, key, label)
	return vault.InputBox{Key: base64.StdEncoding.EncodeToString(wrapped), Nonce: base64.StdEncoding.EncodeToString(nonce), Payload: base64.StdEncoding.EncodeToString(sealed)}
}
func TestCloudAuthorizationEncryptedAndCommunityWorksWithoutGameRole(t *testing.T) {
	api := &fakeAPI{execute: func(_ context.Context, _ accounts.Tokens, role accounts.Role, name string, _ map[string]any) (map[string]any, error) {
		if role.UID != "" || role.Ref != "" {
			t.Fatal("account operation pretended to have a role")
		}
		return map[string]any{"total_points": 100}, nil
	}}
	a, store, record := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	record.Roles = nil
	if err := a.Repo.Update(t.Context(), store, cipher, record, record.Revision); err != nil {
		t.Fatal(err)
	}
	record, _ = a.Repo.Get(t.Context(), store, cipher, record.Ref)
	saved, err := a.Manage(t.Context(), store, policy(), "accounts.cloud.save", map[string]any{"account_ref": record.Ref, "revision": record.Revision, "game": "genshin", "box": cloudBox(t, a, "synthetic-cloud-combo")})
	if err != nil {
		t.Fatal(err)
	}
	summary := saved["account"].(accounts.Summary)
	if len(summary.CloudConfigured) != 1 {
		t.Fatal(summary)
	}
	for _, value := range store.values {
		if strings.Contains(value, "synthetic-cloud-combo") {
			t.Fatal("cloud token stored plaintext")
		}
	}
	response, _ := json.Marshal(summary)
	if strings.Contains(string(response), "synthetic-cloud-combo") {
		t.Fatal("cloud token exposed in summary")
	}
	req := request("genshin", record.Ref)
	req.Params["role_ref"] = ""
	req.Params["operation"] = "genshin.community_status"
	if _, err = a.Service(t.Context(), store, policy(), req); err != nil {
		t.Fatal(err)
	}
	req.Params["operation"] = "genshin.community_sign"
	_, err = a.Service(t.Context(), store, policy(), req)
	assertCode(t, err, "plugin.account_write_confirmation_required")
	before := api.executeCalls.Load()
	req.Params["operation"] = "genshin.cloud_status"
	if _, err = a.Service(t.Context(), store, policy(), req); err != nil || api.executeCalls.Load() != before {
		t.Fatal("status query accessed cloud wallet", err)
	}
	scheduled := req
	scheduled.Origin = rayleabot.ServiceOrigin{EventType: "scheduler.trigger", SourceProtocol: "scheduler", SourceAdapter: "scheduler.internal", TaskID: "ungranted"}
	_, err = a.Service(t.Context(), store, policy(), scheduled)
	assertCode(t, err, "plugin.account_delegation_denied")
	current, _ := a.Repo.Get(t.Context(), store, cipher, record.Ref)
	again, err := a.Repo.Bind(t.Context(), store, cipher, current.Owner, syntheticTokens(), nil, current.AllowedCallers)
	if err != nil || again.Tokens.CloudGenshin != "synthetic-cloud-combo" {
		t.Fatal("relogin dropped cloud authorization", err)
	}
	_, err = a.Manage(t.Context(), store, policy(), "accounts.cloud.clear", map[string]any{"account_ref": record.Ref, "revision": summary.Revision, "game": "genshin"})
	assertCode(t, err, "plugin.account_changed")
}
