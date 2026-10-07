package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
	qrcode "github.com/skip2/go-qrcode"
)

type LoginView struct {
	Ref          string            `json:"ref"`
	State        string            `json:"state"`
	Phase        string            `json:"phase"`
	ExpiresAtMS  int64             `json:"expires_at_ms"`
	QRCode       string            `json:"qr_code,omitempty"`
	Account      *accounts.Summary `json:"account,omitempty"`
	RoleStates   map[string]string `json:"role_states"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	Failure      *LoginFailure     `json:"failure,omitempty"`
}
type LoginFailure struct {
	Stage      string `json:"stage"`
	Operation  string `json:"operation,omitempty"`
	RetCode    int    `json:"retcode,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// login is a login job. changed wakes the 扫码登录 event following the job
// when the worker moved it on; logins started on the management page are
// polled by the page instead.
type login struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	owner       accounts.Subject
	view        LoginView
	qr          mihoyo.QR
	tokens      accounts.Tokens
	roles       []accounts.Role
	ready       bool
	changed     chan struct{}
	retries     int
	nextAttempt time.Time
}

func (s *login) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func terminal(state string) bool {
	return state == "bound" || state == "failed" || state == "expired" || state == "canceled"
}

func (s *login) snapshot() LoginView {
	copy := s.view
	if s.view.Failure != nil {
		value := *s.view.Failure
		copy.Failure = &value
	}
	copy.RoleStates = map[string]string{}
	for game, state := range s.view.RoleStates {
		copy.RoleStates[game] = state
	}
	if s.view.Account != nil {
		value := *s.view.Account
		value.Roles = append([]accounts.Role{}, value.Roles...)
		value.AllowedCallers = append([]string{}, value.AllowedCallers...)
		copy.Account = &value
	}
	return copy
}
func (s *login) finish(state string) {
	s.view.State, s.view.Phase, s.view.QRCode = state, "done", ""
	s.qr, s.tokens, s.roles = mihoyo.QR{}, accounts.Tokens{}, nil
	s.cancel()
}

func (s *login) expire() {
	if terminal(s.view.State) {
		return
	}
	if s.view.ErrorCode != "" {
		s.finish("failed")
		return
	}
	if s.view.Phase != "scan" {
		s.view.ErrorCode, s.view.ErrorMessage = "plugin.login_processing_timeout", message("plugin.login_processing_timeout")
		s.view.Failure = &LoginFailure{Stage: s.view.Phase}
		s.finish("failed")
		return
	}
	s.finish("expired")
}

func (s *login) reject(err error, interval time.Duration) {
	failure := publicError(err)
	s.view.ErrorCode, s.view.ErrorMessage = failure.Code, failure.Message
	diagnostic := &LoginFailure{Stage: s.view.Phase}
	var upstream *mihoyo.Error
	if errors.As(err, &upstream) {
		diagnostic.Operation = upstream.Operation
		diagnostic.RetCode = upstream.RetCode
		diagnostic.HTTPStatus = upstream.HTTPStatus
		diagnostic.Reason = upstream.Reason
	}
	s.view.Failure = diagnostic
	slog.Warn("米游社登录阶段失败", "stage", diagnostic.Stage, "code", failure.Code, "operation", diagnostic.Operation, "retcode", diagnostic.RetCode, "http_status", diagnostic.HTTPStatus, "reason", diagnostic.Reason)
	if (failure.Code == "plugin.upstream_unavailable" || failure.Code == "plugin.upstream_rate_limited") && s.retries < 3 {
		s.retries++
		s.nextAttempt = time.Now().Add(time.Duration(min(1<<s.retries, 6)) * interval)
		return
	}
	s.finish("failed")
}

func (s *login) recovered() {
	s.view.ErrorCode = ""
	s.view.ErrorMessage = ""
	s.view.Failure = nil
	s.retries = 0
	s.nextAttempt = time.Time{}
}

func (a *App) startLogin(ctx context.Context, owner accounts.Subject) (LoginView, error) {
	a.jobsMu.Lock()
	for ref, item := range a.jobs {
		item.mu.Lock()
		if time.Now().UnixMilli() > item.view.ExpiresAtMS+60000 {
			if !terminal(item.view.State) {
				item.expire()
			}
			delete(a.jobs, ref)
			if a.owners[item.owner] == ref {
				delete(a.owners, item.owner)
			}
		}
		item.mu.Unlock()
	}
	if len(a.jobs) >= 128 {
		a.jobsMu.Unlock()
		return LoginView{}, failure("plugin.login_limit")
	}
	a.jobsMu.Unlock()
	qr, err := a.API.CreateQR(ctx)
	if err != nil {
		return LoginView{}, err
	}
	png, err := qrcode.Encode(qr.URL, qrcode.Medium, 256)
	if err != nil {
		return LoginView{}, failure("plugin.login_qr_invalid")
	}
	jobCtx, cancel := context.WithCancel(a.ctx)
	s := &login{ctx: jobCtx, cancel: cancel, owner: owner, qr: qr, changed: make(chan struct{}, 1), view: LoginView{Ref: rand.Text(), State: "waiting", Phase: "scan", ExpiresAtMS: time.Now().Add(3 * time.Minute).UnixMilli(), QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), RoleStates: map[string]string{}}}
	a.jobsMu.Lock()
	if len(a.jobs) >= 128 {
		a.jobsMu.Unlock()
		cancel()
		return LoginView{}, failure("plugin.login_limit")
	}
	if old := a.jobs[a.owners[owner]]; old != nil {
		old.mu.Lock()
		if !terminal(old.view.State) {
			old.finish("canceled")
		}
		old.mu.Unlock()
	}
	a.jobs[s.view.Ref], a.owners[owner] = s, s.view.Ref
	s.mu.Lock()
	view := s.snapshot()
	s.mu.Unlock()
	a.jobsMu.Unlock()
	go a.watchLogin(s)
	go a.expireLogin(s)
	return view, nil
}

func (a *App) expireLogin(s *login) {
	for {
		s.mu.Lock()
		remaining := time.Until(time.UnixMilli(s.view.ExpiresAtMS))
		s.mu.Unlock()
		timer := time.NewTimer(max(remaining, 0))
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.mu.Lock()
			if time.Now().UnixMilli() >= s.view.ExpiresAtMS {
				s.expire()
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
	}
}

// The worker only contacts official HTTPS endpoints and changes memory. It
// never reuses an ended EventContext. pollLogin commits with a fresh live event.
// The code is polled every interval, as upstream polls it; once it is
// confirmed, the exchange and each game's roles follow one another at once,
// and only a retry waits for its turn.
func (a *App) watchLogin(s *login) {
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	next := false
	for {
		if !next {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			}
		}
		next = false
		s.mu.Lock()
		if time.Now().UnixMilli() >= s.view.ExpiresAtMS {
			s.expire()
			s.mu.Unlock()
			return
		}
		phase, qr, tokens := s.view.Phase, s.qr, s.tokens
		if time.Now().Before(s.nextAttempt) {
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		switch phase {
		case "scan":
			confirmation, err := a.API.PollQR(ctx, qr)
			cancel()
			s.mu.Lock()
			if s.ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			if err != nil {
				s.reject(err, a.pollInterval)
			} else {
				s.recovered()
				switch confirmation.State {
				case "confirmed":
					s.tokens = confirmation.Tokens
					s.view.State, s.view.Phase, s.view.QRCode = "scanned", "exchange", ""
					s.view.ExpiresAtMS = time.Now().Add(3 * time.Minute).UnixMilli()
					next = true
				case "waiting", "scanned":
					s.view.State = confirmation.State
				case "expired", "canceled":
					s.finish(confirmation.State)
				default:
					s.reject(failure("plugin.upstream_response_invalid"), a.pollInterval)
				}
			}
			s.mu.Unlock()
		case "exchange":
			refreshed, err := a.API.Refresh(ctx, tokens)
			cancel()
			s.mu.Lock()
			if s.ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			if err != nil {
				s.reject(err, a.pollInterval)
			} else {
				s.tokens = refreshed
				s.view.Phase = "roles"
				s.recovered()
				next = true
			}
			s.mu.Unlock()
		case "roles":
			s.mu.Lock()
			game := ""
			for _, candidate := range []string{"genshin", "starrail", "zzz"} {
				if s.view.RoleStates[candidate] == "" {
					game = candidate
					break
				}
			}
			s.mu.Unlock()
			if game == "" {
				cancel()
				s.mu.Lock()
				if s.ctx.Err() == nil {
					s.ready = true
					s.view.Phase = "saving"
				}
				s.mu.Unlock()
				s.signal()
				return
			}
			roles, err := a.API.RolesForGame(ctx, tokens, game)
			cancel()
			s.mu.Lock()
			if s.ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			if err != nil {
				s.view.RoleStates[game] = publicError(err).Code
			} else {
				s.view.RoleStates[game] = "empty"
				if len(roles) > 0 {
					s.view.RoleStates[game] = "ready"
					s.roles = append(s.roles, roles...)
				}
			}
			s.mu.Unlock()
			next = true
		default:
			cancel()
			return
		}
		s.signal()
	}
}

func (a *App) getLogin(ref string) (*login, error) {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	s := a.jobs[ref]
	if s == nil {
		return nil, failure("plugin.login_not_found")
	}
	return s, nil
}

func (a *App) pollLogin(ctx context.Context, store accounts.SecretStore, cipher *vault.Cipher, policy Policy, ref string) (LoginView, error) {
	s, err := a.getLogin(ref)
	if err != nil {
		return LoginView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !terminal(s.view.State) && time.Now().UnixMilli() >= s.view.ExpiresAtMS {
		s.expire()
	}
	if !terminal(s.view.State) && s.ready && s.ctx.Err() == nil {
		record, err := a.Repo.Bind(ctx, store, cipher, s.owner, s.tokens, s.roles, policy.IDs())
		if err != nil {
			return s.snapshot(), err
		}
		summary := record.Summary()
		s.view.Account = &summary
		s.finish("bound")
	}
	return s.snapshot(), nil
}

func (a *App) cancelLogin(ref string) (LoginView, error) {
	s, err := a.getLogin(ref)
	if err != nil {
		return LoginView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !terminal(s.view.State) {
		s.finish("canceled")
	}
	return s.snapshot(), nil
}

func (a *App) cancelLogins() {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	for _, item := range a.jobs {
		item.mu.Lock()
		if !terminal(item.view.State) {
			item.finish("canceled")
		}
		item.mu.Unlock()
	}
}

func (a *App) cancelOwnerLogin(owner accounts.Subject) {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	if s := a.jobs[a.owners[owner]]; s != nil {
		s.mu.Lock()
		if !terminal(s.view.State) {
			s.finish("canceled")
		}
		s.mu.Unlock()
	}
}
