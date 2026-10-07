//go:build manual_smoke

package mihoyo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

// This check creates an unauthenticated, expiring login QR and polls it once.
// It never prints or saves its URL/ticket and does not confirm an account login.
func TestLiveQRCreationAndWaitingState(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	client := New(nil, nil)
	qr, err := client.CreateQR(ctx)
	if err != nil {
		reportLiveError(t, err)
	}
	confirmation, err := client.PollQR(ctx, qr)
	if err != nil {
		reportLiveError(t, err)
	}
	if confirmation.State != "waiting" {
		t.Fatalf("unconfirmed QR state = %s", confirmation.State)
	}
}

func TestLiveAnonymousFingerprint(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tokens, err := New(nil, nil).Fingerprint(ctx, accounts.Tokens{DeviceID: RandomDeviceID()})
	if err != nil {
		reportLiveError(t, err)
	}
	if tokens.DeviceFP == "" || tokens.DeviceFPExpiresAtMS <= time.Now().UnixMilli() {
		t.Fatal("fingerprint not usable")
	}
}

func reportLiveError(t *testing.T, err error) {
	t.Helper()
	var failure *Error
	if errors.As(err, &failure) {
		t.Fatalf("%s: code=%s http_status=%d retcode=%d reason=%s", failure.Operation, failure.Code, failure.HTTPStatus, failure.RetCode, failure.Reason)
	}
	t.Fatal("live request failed")
}
