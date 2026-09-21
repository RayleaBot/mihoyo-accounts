package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
)

// The ark cloud token authorizes the Genshin Impact and Honkai: Star Rail
// cloud ranking and panel queries. It is kept encrypted with the account
// records, and game plugins ask this plugin to send the requests that need it.
const arkTokenSetting = "ark.token"

var arkRoutes = []string{"auth/usage", "panel/data", "panel/download", "panel/upload", "panel/refresh", "rank/data", "rank/specific", "rank/custom", "rank/custom/specific", "rank/self", "rank/group", "rank/stygian", "pos", "verify", "verify/code", "verify/user", "ocr/profilechange/gs", "ocr/profilechange/sr"}

// HTTPDoer sends ark requests; tests replace it.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func (a *App) arkClient() HTTPDoer {
	if a.ArkHTTP != nil {
		return a.ArkHTTP
	}
	return &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (a *App) arkToken(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher) (string, error) {
	var token string
	exists, err := a.Repo.ReadSetting(ctx, store, cipher, arkTokenSetting, &token)
	if err != nil {
		return "", err
	}
	if !exists || token == "" {
		return "", failure("plugin.account_ark_unconfigured")
	}
	return token, nil
}

func (a *App) arkRequest(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, request rayleabot.ServiceRequest, games []string) (map[string]any, error) {
	if !slices.Contains(games, "genshin") && !slices.Contains(games, "starrail") {
		return nil, failure("plugin.account_caller_denied")
	}
	var q struct {
		Route string         `json:"route"`
		Body  map[string]any `json:"body"`
	}
	if decode(request.Params, &q) != nil || !slices.Contains(arkRoutes, q.Route) || q.Body == nil {
		return nil, failure("plugin.account_input_invalid")
	}
	body, err := json.Marshal(q.Body)
	if err != nil || len(body) > 256*1024 {
		return nil, failure("plugin.account_input_invalid")
	}
	token, err := a.arkToken(ctx, store, cipher)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ark.ivny.cn/"+q.Route, bytes.NewReader(body))
	if err != nil {
		return nil, failure("plugin.account_ark_unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "RayleaBot-mihoyo-accounts/0.1.0")
	response, err := a.arkClient().Do(req)
	if err != nil {
		return nil, failure("plugin.account_ark_unavailable")
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == 401 || response.StatusCode == 403:
		return nil, failure("plugin.account_ark_rejected")
	case response.StatusCode == 429:
		return nil, failure("plugin.account_ark_rate_limited")
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, failure("plugin.account_ark_unavailable")
	}
	limit := 2 * 1024 * 1024
	if q.Route == "panel/data" || q.Route == "panel/download" {
		limit = 8 * 1024 * 1024
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, failure("plugin.account_ark_unavailable")
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, failure("plugin.account_ark_unavailable")
	}
	return map[string]any{"data": redactArkEcho(decoded, token, 0)}, nil
}

// redactArkEcho removes every string value and field name that contains the
// token, in case the service echoes the request back.
func redactArkEcho(value any, token string, depth int) any {
	if depth > 32 {
		return nil
	}
	switch v := value.(type) {
	case string:
		if strings.Contains(v, token) {
			return ""
		}
		return v
	case []any:
		for i := range v {
			v[i] = redactArkEcho(v[i], token, depth+1)
		}
	case map[string]any:
		for key, item := range v {
			if strings.Contains(key, token) {
				delete(v, key)
			} else {
				v[key] = redactArkEcho(item, token, depth+1)
			}
		}
	}
	return value
}

func (a *App) manageArk(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, action string, input map[string]any) (map[string]any, error) {
	for _, key := range []string{"token", "authorization", "value"} {
		if _, exists := input[key]; exists {
			return nil, failure("plugin.account_input_invalid")
		}
	}
	value := ""
	switch action {
	case "ark.status":
		_, err := a.arkToken(ctx, store, cipher)
		if err != nil && !isFailure(err, "plugin.account_ark_unconfigured") {
			return nil, err
		}
		return map[string]any{"configured": err == nil}, nil
	case "ark.save":
		var q struct {
			Box vault.InputBox `json:"box"`
		}
		if decode(input, &q) != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		plain, err := a.Vault.DecodeInput(q.Box, "ark.token.save")
		if err != nil {
			return nil, err
		}
		defer clear(plain)
		value = strings.TrimSpace(string(plain))
		if len(value) < 8 || len(value) > 4096 {
			return nil, failure("plugin.account_input_invalid")
		}
		for _, ch := range value {
			if ch < 33 || ch > 126 {
				return nil, failure("plugin.account_input_invalid")
			}
		}
	case "ark.clear":
	default:
		return nil, failure("plugin.account_operation_denied")
	}
	// Clearing stores an empty encrypted value, which reads as unconfigured.
	if err := a.Repo.WriteSetting(ctx, store, cipher, arkTokenSetting, value); err != nil {
		return nil, err
	}
	return map[string]any{"configured": value != ""}, nil
}
