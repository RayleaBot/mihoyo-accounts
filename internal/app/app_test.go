package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	fail   bool
}

func (s *memoryStore) Read(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return value, ok, nil
}
func (s *memoryStore) Write(ctx context.Context, values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.fail {
		return errors.New("synthetic storage failure")
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

type fakeAPI struct {
	roles        func(context.Context, accounts.Tokens, string) ([]accounts.Role, error)
	fingerprint  func(context.Context, accounts.Tokens) (accounts.Tokens, error)
	refresh      func(context.Context, accounts.Tokens) (accounts.Tokens, error)
	execute      func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error)
	poll         func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error)
	roleError    string
	refreshCalls atomic.Int32
	executeCalls atomic.Int32
}

func (f *fakeAPI) Fingerprint(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	if f.fingerprint != nil {
		return f.fingerprint(ctx, tokens)
	}
	tokens.DeviceFP = "syntheticfingerprint"
	tokens.DeviceFPExpiresAtMS = time.Now().Add(time.Hour).UnixMilli()
	return tokens, nil
}

func (f *fakeAPI) CreateQR(context.Context) (mihoyo.QR, error) {
	return mihoyo.QR{URL: "https://user.mihoyo.com/qr?ticket=synthetic-qr", Ticket: "synthetic-qr", DeviceID: "synthetic-device"}, nil
}
func (f *fakeAPI) PollQR(ctx context.Context, qr mihoyo.QR) (mihoyo.Confirmation, error) {
	if f.poll != nil {
		return f.poll(ctx, qr)
	}
	return mihoyo.Confirmation{State: "confirmed", Tokens: syntheticTokens()}, nil
}
func (f *fakeAPI) Refresh(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
	f.refreshCalls.Add(1)
	if f.refresh != nil {
		return f.refresh(ctx, tokens)
	}
	tokens.CookieToken = "synthetic-refreshed-cookie"
	tokens.LToken = "synthetic-refreshed-ltoken"
	return tokens, nil
}
func (f *fakeAPI) RolesForGame(ctx context.Context, tokens accounts.Tokens, game string) ([]accounts.Role, error) {
	if f.roles != nil {
		return f.roles(ctx, tokens, game)
	}
	if f.roleError == game {
		return nil, &mihoyo.Error{Code: "plugin.upstream_unavailable", Operation: "accounts.roles"}
	}
	return []accounts.Role{role(game)}, nil
}
func (f *fakeAPI) Execute(ctx context.Context, tokens accounts.Tokens, role accounts.Role, name string, input map[string]any) (map[string]any, error) {
	f.executeCalls.Add(1)
	if f.execute != nil {
		return f.execute(ctx, tokens, role, name, input)
	}
	return map[string]any{"current": 10}, nil
}
func syntheticTokens() accounts.Tokens {
	return accounts.Tokens{AccountID: "123456789", MID: "synthetic-mid", SToken: "synthetic-stoken", LToken: "synthetic-ltoken", CookieToken: "synthetic-cookie", DeviceID: "synthetic-device"}
}
func subject() accounts.Subject {
	return accounts.Subject{SourceProtocol: "onebot11", SourceAdapter: "primary", BotID: "bot-1", ActorID: "user-1"}
}
func role(game string) accounts.Role {
	return accounts.Role{Ref: game + ":cn:1234", Game: game, GameBiz: game + "_cn", UID: "1234", Region: "cn", Nickname: "测试角色", Level: 50}
}
func policy() Policy {
	return Policy{AllowedPlugins: []Caller{{PluginID: "raylea.genshin", Games: []string{"genshin"}}, {PluginID: "raylea.starrail", Games: []string{"starrail"}}, {PluginID: "raylea.zzz", Games: []string{"zzz"}}}}
}
func setup(t *testing.T, api *fakeAPI) (*App, *memoryStore, accounts.Record) {
	t.Helper()
	a, err := New(t.TempDir(), api)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if err := a.Vault.Initialize("synthetic-passphrase-only", false); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{values: map[string]string{}}
	cipher, _ := a.Vault.Cipher()
	record, err := a.Repo.Bind(t.Context(), store, cipher, subject(), syntheticTokens(), []accounts.Role{role("genshin"), role("starrail"), role("zzz")}, policy().IDs())
	if err != nil {
		t.Fatal(err)
	}
	return a, store, record
}
func request(game, ref string) rayleabot.ServiceRequest {
	s := subject()
	return rayleabot.ServiceRequest{CallerPluginID: "raylea." + game, Service: "accounts", ServiceVersion: 1, Method: "execute", Params: map[string]any{"account_ref": ref, "role_ref": role(game).Ref, "operation": game + ".note", "input": map[string]any{}}, Origin: rayleabot.ServiceOrigin{EventID: "event-1", EventType: "message.private", SourceProtocol: s.SourceProtocol, SourceAdapter: s.SourceAdapter, BotID: s.BotID, Actor: &rayleabot.Actor{ID: s.ActorID}}}
}
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var actual *rayleabot.ActionError
	if !errors.As(err, &actual) || actual.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestOneAccountServesThreeGamesWithoutCredentials(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		result, err := a.Service(t.Context(), store, policy(), request(game, record.Ref))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "synthetic-") {
			t.Fatal("credential in service result")
		}
	}
	if api.executeCalls.Load() != 3 {
		t.Fatal("games did not share the account")
	}
	for _, stored := range store.values {
		for _, secret := range []string{"synthetic-stoken", "synthetic-cookie", "synthetic-ltoken", "user-1", "123456789"} {
			if strings.Contains(stored, secret) {
				t.Fatal("plaintext in database value")
			}
		}
	}
	for name, mutate := range map[string]func(*rayleabot.ServiceRequest){
		"caller":           func(r *rayleabot.ServiceRequest) { r.CallerPluginID = "another-plugin" },
		"owner":            func(r *rayleabot.ServiceRequest) { r.Origin.Actor.ID = "user-2" },
		"adapter":          func(r *rayleabot.ServiceRequest) { r.Origin.SourceAdapter = "secondary" },
		"role":             func(r *rayleabot.ServiceRequest) { r.Params["role_ref"] = role("zzz").Ref },
		"operation":        func(r *rayleabot.ServiceRequest) { r.Params["operation"] = "zzz.note" },
		"subject override": func(r *rayleabot.ServiceRequest) { r.Params["subject"] = subject() },
	} {
		t.Run(name, func(t *testing.T) {
			r := request("genshin", record.Ref)
			mutate(&r)
			if _, err := a.Service(t.Context(), store, policy(), r); err == nil {
				t.Fatal("denied request succeeded")
			}
		})
	}
	if api.executeCalls.Load() != 3 {
		t.Fatal("denied request reached upstream")
	}
}

func TestPermissionRevocationAndDeletionPreventNewRequests(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.permissions", map[string]any{"account_ref": record.Ref, "revision": record.Revision, "callers": []string{"raylea.starrail"}}); err != nil {
		t.Fatal(err)
	}
	_, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
	assertCode(t, err, "plugin.account_caller_denied")
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.remove", map[string]any{"account_ref": record.Ref}); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(t.Context(), store, policy(), request("starrail", record.Ref))
	assertCode(t, err, "plugin.account_not_found")
	if api.executeCalls.Load() != 0 {
		t.Fatal("revoked account reached upstream")
	}
}

func TestDefaultRoleIsOwnedByAccountServiceAndHiddenAfterRevocation(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	selected := request("genshin", record.Ref)
	selected.Method = "select"
	if _, err := a.Service(t.Context(), store, policy(), selected); err != nil {
		t.Fatal(err)
	}
	listed := request("genshin", record.Ref)
	listed.Method, listed.Params = "list", map[string]any{}
	result, err := a.Service(t.Context(), store, policy(), listed)
	if err != nil {
		t.Fatal(err)
	}
	defaults := result["default_roles"].(map[string]accounts.Selection)
	if defaults["genshin"].RoleRef != role("genshin").Ref {
		t.Fatal("default role was not retained")
	}
	if _, err := a.Manage(t.Context(), store, policy(), "accounts.permissions", map[string]any{"account_ref": record.Ref, "revision": record.Revision, "callers": []string{}}); err != nil {
		t.Fatal(err)
	}
	result, err = a.Service(t.Context(), store, policy(), listed)
	if err != nil {
		t.Fatal(err)
	}
	if len(result["default_roles"].(map[string]accounts.Selection)) != 0 {
		t.Fatal("revoked default role remained available")
	}
}

func TestTransientFailuresPreserveAccountAndAuthenticationFailureMarksIt(t *testing.T) {
	api := &fakeAPI{}
	a, store, record := setup(t, api)
	cipher, _ := a.Vault.Cipher()
	for _, code := range []string{"plugin.upstream_unavailable", "plugin.upstream_challenge_required", "plugin.upstream_rate_limited", "plugin.upstream_auth_invalid"} {
		api.execute = func(context.Context, accounts.Tokens, accounts.Role, string, map[string]any) (map[string]any, error) {
			return nil, &mihoyo.Error{Code: code}
		}
		_, err := a.Service(t.Context(), store, policy(), request("genshin", record.Ref))
		assertCode(t, err, code)
		current, err := a.Repo.Get(t.Context(), store, cipher, record.Ref)
		if err != nil {
			t.Fatal(err)
		}
		want := "valid"
		if code == "plugin.upstream_auth_invalid" {
			want = "invalid"
		}
		if current.Status != want || current.Tokens.SToken == "" {
			t.Fatal("incorrect credential transition")
		}
	}
}

func TestConcurrentRefreshWaiterCanCancelWithoutAnotherExchange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	api := &fakeAPI{refresh: func(ctx context.Context, tokens accounts.Tokens) (accounts.Tokens, error) {
		close(entered)
		select {
		case <-release:
			return tokens, nil
		case <-ctx.Done():
			return accounts.Tokens{}, ctx.Err()
		}
	}}
	a, store, record := setup(t, api)
	first := make(chan error, 1)
	go func() {
		_, err := a.Manage(t.Context(), store, policy(), "accounts.refresh", map[string]any{"account_ref": record.Ref})
		first <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := a.Manage(ctx, store, policy(), "accounts.refresh", map[string]any{"account_ref": record.Ref})
	assertCode(t, err, "plugin.account_operation_canceled")
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if api.refreshCalls.Load() != 1 {
		t.Fatal("concurrent refresh exchanged twice")
	}
}

func TestLoginOnlyBindsAfterEncryptedCommitAndRetainsPartialRoles(t *testing.T) {
	api := &fakeAPI{roleError: "zzz"}
	a, store, _ := setup(t, api)
	a.pollInterval = time.Millisecond
	started, err := a.Manage(t.Context(), store, policy(), "login.start", map[string]any{"subject": subject()})
	if err != nil {
		t.Fatal(err)
	}
	view := started["login"].(LoginView)
	if !strings.HasPrefix(view.QRCode, "data:image/png;base64,") {
		t.Fatal("QR was not rendered locally")
	}
	job, _ := a.getLogin(view.Ref)
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.ready })
	store.fail = true
	if _, err := a.Manage(t.Context(), store, policy(), "login.poll", map[string]any{"login_ref": view.Ref}); err == nil {
		t.Fatal("failed storage returned bound")
	}
	job.mu.Lock()
	bound := job.view.State == "bound"
	job.mu.Unlock()
	if bound {
		t.Fatal("bound before persistence")
	}
	store.fail = false
	polled, err := a.Manage(t.Context(), store, policy(), "login.poll", map[string]any{"login_ref": view.Ref})
	if err != nil {
		t.Fatal(err)
	}
	view = polled["login"].(LoginView)
	if view.State != "bound" || len(view.Account.Roles) != 2 || view.RoleStates["zzz"] != "plugin.upstream_unavailable" || view.QRCode != "" {
		t.Fatalf("incorrect login state: %#v", view)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "synthetic-") {
		t.Fatal("login result leaked credentials")
	}
}

func TestCanceledLoginCannotBindAndUnknownSessionDoesNotRestart(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	a.pollInterval = time.Millisecond
	started, err := a.Manage(t.Context(), store, policy(), "login.start", map[string]any{"subject": subject()})
	if err != nil {
		t.Fatal(err)
	}
	ref := started["login"].(LoginView).Ref
	if _, err := a.Manage(t.Context(), store, policy(), "login.cancel", map[string]any{"login_ref": ref}); err != nil {
		t.Fatal(err)
	}
	result, err := a.Manage(t.Context(), store, policy(), "login.poll", map[string]any{"login_ref": ref})
	if err != nil {
		t.Fatal(err)
	}
	if result["login"].(LoginView).State != "canceled" {
		t.Fatal("canceled login changed state")
	}
	_, err = a.Manage(t.Context(), store, policy(), "login.poll", map[string]any{"login_ref": "unknown"})
	assertCode(t, err, "plugin.login_not_found")
	if _, err := a.Manage(t.Context(), store, policy(), "vault.lock", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	_, err = a.Service(t.Context(), store, policy(), request("genshin", "any"))
	assertCode(t, err, "plugin.vault_locked")
}

func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
