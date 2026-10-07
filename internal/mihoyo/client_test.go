package mihoyo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func reply(value any) *http.Response {
	data, _ := json.Marshal(value)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}
}

func TestQRFlowExchangesOnlyNamedTokens(t *testing.T) {
	client := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" {
			t.Fatal("missing encrypted transport")
		}
		if strings.Contains(r.URL.Path, "QRLogin") && r.Header.Get("DS") == "" {
			t.Fatal("missing QR signature")
		}
		switch r.URL.Path {
		case "/account/ma-cn-passport/app/createQRLogin":
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"url": "https://user.mihoyo.com/qr?ticket=synthetic-ticket"}}), nil
		case "/account/ma-cn-passport/app/queryQRLoginStatus":
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"status": "Confirmed", "user_info": map[string]any{"aid": "123456789", "mid": "synthetic-mid"}, "tokens": []any{map[string]any{"name": "unrelated", "token": "must-not-use"}, map[string]any{"name": "stoken_v2", "token": "synthetic-stoken"}, map[string]any{"name": "ltoken", "token": "synthetic-ltoken"}}}}), nil
		case "/account/auth/api/getCookieAccountInfoBySToken":
			if !strings.Contains(r.Header.Get("Cookie"), "synthetic-stoken") {
				t.Fatal("missing session authentication")
			}
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"cookie_token": "synthetic-cookie"}}), nil
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
			return nil, nil
		}
	}), nil)
	qr, err := client.CreateQR(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.PollQR(t.Context(), qr)
	if err != nil || result.State != "confirmed" {
		t.Fatalf("confirmation: %#v %v", result, err)
	}
	tokens, err := client.Refresh(t.Context(), result.Tokens)
	if err != nil || tokens.CookieToken != "synthetic-cookie" {
		t.Fatalf("exchange: %v", err)
	}
}

func TestUnknownTokensAreNotTreatedAsCredentials(t *testing.T) {
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"status": "Confirmed", "user_info": map[string]any{"aid": "1234"}, "tokens": []any{map[string]any{"name": "unknown", "token": "synthetic-secret"}}}}), nil
	}), nil)
	if _, err := client.PollQR(t.Context(), QR{Ticket: "synthetic-ticket", DeviceID: "synthetic-device"}); err == nil {
		t.Fatal("unknown token type accepted")
	}
}

func TestFailuresDoNotExposeURLsOrCredentialBodies(t *testing.T) {
	marker := "synthetic-sensitive-value"
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request failed: stoken=" + marker)
	}), nil)
	_, err := client.Refresh(context.Background(), accounts.Tokens{AccountID: "1234", SToken: marker, LToken: "synthetic-ltoken"})
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatal("network error exposed credentials")
	}
	for code, want := range map[int]string{10001: "plugin.upstream_auth_invalid", 10041: "plugin.upstream_device_required", 1034: "plugin.upstream_challenge_required"} {
		client = New(doerFunc(func(*http.Request) (*http.Response, error) {
			return reply(map[string]any{"retcode": code, "message": marker, "data": nil}), nil
		}), nil)
		_, err = client.Refresh(t.Context(), accounts.Tokens{AccountID: "1234", SToken: marker, LToken: "synthetic-ltoken"})
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != want || strings.Contains(err.Error(), marker) {
			t.Fatalf("wrong failure: %v", err)
		}
	}
}
