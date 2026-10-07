package mihoyo

import (
	"net/http"
	"testing"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

func TestQRTokenTypeMustPassOfficialExchange(t *testing.T) {
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/account/ma-cn-passport/app/queryQRLoginStatus":
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"status": "Confirmed", "user_info": map[string]any{"aid": "123456789", "mid": "synthetic-mid"}, "tokens": []any{map[string]any{"token_type": 1, "token": "synthetic-typed-stoken"}}}}), nil
		case "/account/auth/api/getLTokenBySToken":
			return reply(map[string]any{"retcode": 0, "data": map[string]any{"ltoken": "synthetic-ltoken"}}), nil
		case "/account/auth/api/getCookieAccountInfoBySToken":
			response := reply(map[string]any{"retcode": 0, "data": map[string]any{"uid": "123456789"}})
			response.Header.Add("Set-Cookie", "cookie_token=synthetic-cookie; Secure; HttpOnly")
			response.Header.Add("Set-Cookie", "account_id=123456789; Secure; HttpOnly")
			return response, nil
		default:
			t.Fatal("unexpected endpoint")
			return nil, nil
		}
	}), nil)
	confirmation, err := client.PollQR(t.Context(), QR{Ticket: "synthetic-ticket", DeviceID: "synthetic-device"})
	if err != nil || confirmation.Tokens.SToken != "synthetic-typed-stoken" {
		t.Fatal("the app token of type 1 was not read as the SToken")
	}
	verified, err := client.Refresh(t.Context(), confirmation.Tokens)
	if err != nil || verified.CookieToken != "synthetic-cookie" || verified.LToken != "synthetic-ltoken" {
		t.Fatal("header credentials were not consumed internally")
	}
}

func TestUntypedUnnamedQRTokensAreRejected(t *testing.T) {
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"status": "Confirmed", "user_info": map[string]any{"aid": "123456789"}, "tokens": []any{map[string]any{"token": "synthetic-one"}, map[string]any{"token": "synthetic-two"}}}}), nil
	}), nil)
	if _, err := client.PollQR(t.Context(), QR{}); err == nil {
		t.Fatal("ambiguous credentials accepted")
	}
}

func TestCookieExchangeCannotSwitchAccount(t *testing.T) {
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"uid": "987654321", "cookie_token": "synthetic-cookie"}}), nil
	}), nil)
	if _, err := client.Refresh(t.Context(), accounts.Tokens{AccountID: "123456789", SToken: "synthetic-stoken", LToken: "synthetic-ltoken"}); err == nil {
		t.Fatal("exchange changed account identity")
	}
}
