package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type arkDoer func(*http.Request) (*http.Response, error)

func (f arkDoer) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestArkTokenStaysEncryptedAndOnlyThisPluginSendsIt(t *testing.T) {
	const token = "synthetic-ark-token-value"
	a, store, _ := setup(t, &fakeAPI{})
	calls := 0
	a.ArkHTTP = arkDoer(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != "https://ark.ivny.cn/auth/usage" || req.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("wrong ark request %s", req.URL)
		}
		body, _ := io.ReadAll(req.Body)
		echo, _ := json.Marshal(map[string]any{"data": map[string]any{"auth": map[string]any{"permission": 1}, "echo": "Bearer " + token, token: "key"}, "request": json.RawMessage(body)})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(echo))}, nil
	})
	arkRequest := func(caller, route string) (map[string]any, error) {
		req := request("genshin", "")
		req.CallerPluginID, req.Method = caller, "ark.request"
		req.Params = map[string]any{"route": route, "body": map[string]any{"version": "0.1.0"}}
		return a.Service(t.Context(), store, policy(), req)
	}
	_, err := arkRequest("raylea.genshin", "auth/usage")
	assertCode(t, err, "plugin.account_ark_unconfigured")

	if _, err = a.Manage(t.Context(), store, policy(), "ark.save", map[string]any{"token": token}); err == nil {
		t.Fatal("plaintext token accepted")
	}
	saved, err := a.Manage(t.Context(), store, policy(), "ark.save", map[string]any{"box": cloudBox(t, a, token, "ark.token.save")})
	if err != nil || saved["configured"] != true {
		t.Fatal(saved, err)
	}
	for _, value := range store.values {
		if strings.Contains(value, token) {
			t.Fatal("ark token stored in plaintext")
		}
	}
	result, err := arkRequest("raylea.starrail", "auth/usage")
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(result)
	if strings.Contains(string(wire), token) || !strings.Contains(string(wire), "permission") {
		t.Fatalf("token echoed back or response lost: %s", wire)
	}
	_, err = arkRequest("raylea.zzz", "auth/usage")
	assertCode(t, err, "plugin.account_caller_denied")
	_, err = arkRequest("raylea.genshin", "../admin")
	assertCode(t, err, "plugin.account_input_invalid")
	if calls != 1 {
		t.Fatalf("ark reached %d times", calls)
	}

	cleared, err := a.Manage(t.Context(), store, policy(), "ark.clear", map[string]any{})
	if err != nil || cleared["configured"] != false {
		t.Fatal(cleared, err)
	}
	status, err := a.Manage(t.Context(), store, policy(), "ark.status", map[string]any{})
	if err != nil || status["configured"] != false {
		t.Fatal(status, err)
	}
}
