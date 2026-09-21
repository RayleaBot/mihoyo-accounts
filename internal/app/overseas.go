package app

import (
	"context"
	"crypto/rand"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
	"slices"
	"sync"
	"time"
)

type overseasImport struct {
	CompletedGames int               `json:"completed_games"`
	Ref            string            `json:"ref"`
	State          string            `json:"state"`
	Sequence       int               `json:"sequence"`
	RoleStates     map[string]string `json:"role_states"`
	ExpiresMS      int64             `json:"expires_ms"`
	Account        *accounts.Summary `json:"account,omitempty"`
	owner          accounts.Subject
	tokens         accounts.Tokens
	roles          []accounts.Role
	last           *overseasImport
}
type overseasImports struct {
	mu    sync.Mutex
	items map[string]*overseasImport
}

func (s *overseasImports) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.items {
		v.tokens = accounts.Tokens{}
	}
	s.items = nil
}
func (v *overseasImport) view() overseasImport {
	copy := *v
	copy.tokens = accounts.Tokens{}
	copy.roles = nil
	copy.last = nil
	copy.RoleStates = map[string]string{}
	for k, v := range v.RoleStates {
		copy.RoleStates[k] = v
	}
	return copy
}
func (a *App) manageOverseas(ctx context.Context, store accounts.SecretStore, policy Policy, action string, input map[string]any) (map[string]any, error) {
	for _, key := range []string{"cookie", "ltoken", "stoken", "cookie_token", "password"} {
		if _, ok := input[key]; ok {
			return nil, failure("plugin.account_input_invalid")
		}
	}
	a.vaultGate.RLock()
	defer a.vaultGate.RUnlock()
	cipher, err := a.Vault.Cipher()
	if err != nil {
		return nil, err
	}
	a.overseas.mu.Lock()
	defer a.overseas.mu.Unlock()
	if a.overseas.items == nil {
		a.overseas.items = map[string]*overseasImport{}
	}
	now := time.Now()
	for ref, v := range a.overseas.items {
		if v.ExpiresMS <= now.UnixMilli() {
			v.tokens = accounts.Tokens{}
			delete(a.overseas.items, ref)
		}
	}
	if action == "accounts.overseas.start" {
		var q struct {
			Subject accounts.Subject `json:"subject"`
			Box     vault.InputBox   `json:"box"`
		}
		if decode(input, &q) != nil || q.Subject.Validate() != nil {
			return nil, failure("plugin.account_input_invalid")
		}
		for ref, v := range a.overseas.items {
			if v.State == "bound" || v.State == "failed" {
				delete(a.overseas.items, ref)
			}
		}
		if len(a.overseas.items) >= 8 {
			return nil, failure("plugin.account_association_limit")
		}
		raw, err := a.Vault.DecodeInput(q.Box, "account.overseas.import")
		if err != nil {
			return nil, err
		}
		tokens, err := mihoyo.ParseOverseasCookie(string(raw))
		clear(raw)
		if err != nil {
			return nil, err
		}
		job := &overseasImport{Ref: rand.Text(), State: "discovering", RoleStates: map[string]string{}, ExpiresMS: now.Add(5 * time.Minute).UnixMilli(), owner: q.Subject, tokens: tokens, roles: []accounts.Role{}}
		a.overseas.items[job.Ref] = job
		return map[string]any{"import": job.view()}, nil
	}
	ref := stringValue(input["ref"])
	job := a.overseas.items[ref]
	if job == nil {
		return nil, failure("plugin.account_overseas_missing")
	}
	if action == "accounts.overseas.cancel" {
		job.tokens = accounts.Tokens{}
		delete(a.overseas.items, ref)
		return map[string]any{"canceled": true}, nil
	}
	var q struct {
		Sequence int `json:"sequence"`
	}
	if decode(input, &q) != nil || action != "accounts.overseas.step" {
		return nil, failure("plugin.account_input_invalid")
	}
	if q.Sequence == job.Sequence-1 && job.last != nil {
		return map[string]any{"import": *job.last}, nil
	}
	if q.Sequence != job.Sequence || job.State != "discovering" || job.CompletedGames < 0 || job.CompletedGames > 2 {
		return nil, failure("plugin.account_overseas_sequence")
	}
	if job.tokens.LToken == "" {
		tokens, err := a.API.Refresh(ctx, job.tokens)
		if err != nil {
			if publicError(err).Code == "plugin.upstream_auth_invalid" {
				job.State = "failed"
				job.tokens = accounts.Tokens{}
			}
			return nil, err
		}
		if tokens.Realm != "os" || tokens.AccountID != job.tokens.AccountID || !tokens.ValidBinding() {
			return nil, failure("plugin.account_input_invalid")
		}
		job.tokens = tokens
		job.Sequence++
		copy := job.view()
		job.last = &copy
		return map[string]any{"import": copy}, nil
	}
	game := []string{"genshin", "starrail", "zzz"}[job.CompletedGames]
	roles, err := a.API.RolesForGame(ctx, job.tokens, game)
	if err != nil {
		if publicError(err).Code == "plugin.upstream_auth_invalid" {
			job.State = "failed"
			job.tokens = accounts.Tokens{}
		}
		return nil, err
	}
	candidate := append(slices.Clone(job.roles), roles...)
	if job.CompletedGames == 2 {
		record, err := a.Repo.Bind(ctx, store, cipher, job.owner, job.tokens, candidate, policy.IDs())
		if err != nil {
			return nil, err
		}
		summary := record.Summary()
		job.Account = &summary
		job.State = "bound"
		job.tokens = accounts.Tokens{}
	}
	job.roles = candidate
	job.RoleStates[game] = "empty"
	if len(roles) > 0 {
		job.RoleStates[game] = "ready"
	}
	job.Sequence++
	job.CompletedGames++
	copy := job.view()
	job.last = &copy
	return map[string]any{"import": copy}, nil
}
