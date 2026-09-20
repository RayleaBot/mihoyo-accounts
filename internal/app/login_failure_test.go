package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

func TestExchangeRejectionIsNotReportedAsExpiredQR(t *testing.T) {
	api := &fakeAPI{refresh: func(context.Context, accounts.Tokens) (accounts.Tokens, error) {
		return accounts.Tokens{}, &mihoyo.Error{Code: "plugin.upstream_rejected", Operation: "tokens.cookie", RetCode: -999, HTTPStatus: 200}
	}}
	a, store, _ := setup(t, api)
	a.pollInterval = time.Millisecond
	started, err := a.Manage(t.Context(), store, policy(), "login.start", map[string]any{"subject": subject()})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := a.getLogin(started["login"].(LoginView).Ref)
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return terminal(job.view.State) })
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.view.State != "failed" || job.view.ErrorCode != "plugin.upstream_rejected" || job.view.Failure.RetCode != -999 || api.refreshCalls.Load() != 1 {
		t.Fatal("permanent exchange error was retried or hidden")
	}
	job.expire()
	if job.view.State != "failed" || !strings.Contains(loginText(job.snapshot()), "-999") {
		t.Fatal("expiry lost the actual failure")
	}
	if job.tokens.SToken != "" {
		t.Fatal("failed login retained credentials")
	}
}

func TestCompletedLoginCannotBeOverwrittenByExpiry(t *testing.T) {
	job := &login{view: LoginView{State: "bound", Phase: "done"}, cancel: func() {}}
	job.expire()
	if job.view.State != "bound" {
		t.Fatal("expiration overwrote committed login")
	}
	job.view = LoginView{State: "scanned", Phase: "exchange"}
	job.expire()
	if job.view.State != "failed" || job.view.ErrorCode != "plugin.login_processing_timeout" {
		t.Fatal("confirmed login was reported as unscanned QR expiry")
	}
}
