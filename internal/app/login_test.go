package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

func TestConfirmedLoginIsProcessedWithoutWaitingForPolls(t *testing.T) {
	var mu sync.Mutex
	var confirmed time.Time
	api := &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
		mu.Lock()
		confirmed = time.Now()
		mu.Unlock()
		return mihoyo.Confirmation{State: "confirmed", Tokens: syntheticTokens()}, nil
	}}
	a, store, _ := setup(t, api)
	a.pollInterval = 200 * time.Millisecond
	started, err := a.Manage(t.Context(), store, policy(), "login.start", map[string]any{"subject": subject()})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := a.getLogin(started["login"].(LoginView).Ref)
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.ready })
	// The exchange and three role lookups used to wait a poll each, four
	// intervals in all.
	mu.Lock()
	elapsed := time.Since(confirmed)
	mu.Unlock()
	if elapsed > a.pollInterval {
		t.Fatalf("roles were ready %v after the confirmation", elapsed)
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if len(job.roles) != 3 || api.refreshCalls.Load() != 1 {
		t.Fatalf("confirmed login was not processed: %d roles", len(job.roles))
	}
}
