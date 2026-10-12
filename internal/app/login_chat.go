package app

import (
	"context"
	"errors"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

// answerTime is kept before the background deadline for the last answer: a
// chat login still open then ends as upstream's 验证超时.
const answerTime = 10 * time.Second

// chatLogin answers 扫码登录 in a private chat or a group as
// xiaoyao-cvs-plugin's qrCodeLogin, in one background event: it sends the
// code, @ing the sender in a group, and follows the login until it ends.
func (a *App) chatLogin(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, policy Policy, owner accounts.Subject) error {
	// Only an administrator initializes the account store and chooses how its
	// key is protected; a chat user must not decide that by logging in first.
	if a.Vault.State() == "uninitialized" {
		return event.SendText(message("plugin.vault_uninitialized"))
	}
	result, err := a.Manage(ctx, store, policy, "login.start", map[string]any{"subject": owner})
	if err != nil {
		return event.SendText(publicError(err).Message)
	}
	view := result["login"].(LoginView)
	s, err := a.getLogin(view.Ref)
	if err != nil {
		return event.SendText(publicError(err).Message)
	}
	// The code is sent once the event is in the background, so a plugin too
	// busy to follow the login shows no code.
	if detached, err := background(ctx, event); !detached {
		_, _ = a.cancelLogin(view.Ref)
		if err != nil {
			return err
		}
		return event.SendText(busyText)
	}
	code := rayleabot.Image("base64://" + strings.TrimPrefix(view.QRCode, "data:image/png;base64,"))
	segments := []rayleabot.Segment{rayleabot.Text("请扫码以完成绑定\n"), code}
	if event.Event.Target.Type != "private" {
		segments = []rayleabot.Segment{rayleabot.At(owner.ActorID), rayleabot.Text("\n请扫码以完成绑定\n"), code}
	}
	address, _ := event.Address()
	sent, err := event.Actions().SendTo(ctx, address, segments...)
	if err != nil && !unconfirmed(err) {
		_, _ = a.cancelLogin(view.Ref)
		return err
	}
	shown := ""
	if owner.SourceProtocol == "onebot11" {
		shown = stringValue(sent["message_id"])
	}
	return a.followLogin(ctx, event, store, policy, owner, s, shown)
}

// followLogin follows a chat login as xiaoyao-cvs-plugin's GetQrCode polls
// it: the worker polls the code every five seconds and wakes the event on
// each change. The event says once that the code was scanned, recalls the
// shown code recallDelay after sending it and, when the login ends, recalls
// a code still shown and answers. The login expires by itself three minutes
// after the code or the confirmation; one still open answerTime before the
// background deadline is expired then.
func (a *App) followLogin(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, policy Policy, owner accounts.Subject, s *login, shown string) error {
	recall := time.NewTimer(a.recallDelay)
	defer recall.Stop()
	due := recall.C
	if shown == "" {
		due = nil
	}
	expire := time.NewTimer(time.Until(event.Deadline().Add(-answerTime)))
	defer expire.Stop()
	noticed := false
	for {
		s.mu.Lock()
		ended := terminal(s.view.State) || s.ready || s.ctx.Err() != nil
		scanned := s.view.State == "scanned" && s.view.Phase == "scan"
		s.mu.Unlock()
		if ended {
			break
		}
		if scanned && !noticed {
			noticed = true
			// As upstream, a reply to the 扫码登录 message.
			_, _ = event.Actions().MessageSend(ctx, quote(event, "二维码已扫描，请确认登录"))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-expire.C:
			s.mu.Lock()
			s.expire()
			s.mu.Unlock()
		case <-s.changed:
		case <-s.ctx.Done():
		case <-due:
			recallCode(ctx, event, owner.BotID, shown)
			shown, due = "", nil
		}
	}
	recallCode(ctx, event, owner.BotID, shown)
	s.mu.Lock()
	ref := s.view.Ref
	s.mu.Unlock()
	result, err := a.Manage(ctx, store, policy, "login.poll", map[string]any{"login_ref": ref})
	if err != nil {
		return event.SendText(publicError(err).Message)
	}
	view := result["login"].(LoginView)
	switch view.State {
	case "expired":
		// As upstream, a code not confirmed in time.
		if _, err := event.Actions().MessageSend(ctx, quote(event, "验证超时")); err != nil {
			return err
		}
	case "bound", "failed":
		if err := a.sendLoginResult(ctx, event, store, owner, view); err != nil {
			return err
		}
	}
	// A login canceled by 取消登录, a newer 扫码登录 or the management page
	// ends without an answer here; each of those answers for itself.
	return event.Result(map[string]any{"handled": true})
}

// recallCode recalls the code sent to the chat. As upstream, a failed recall
// is logged, not retried.
func recallCode(ctx context.Context, event *rayleabot.EventContext, botID, id string) {
	if id == "" {
		return
	}
	if _, err := event.Actions().ForOneBotAccount(botID).MessageDelete(ctx, id); err != nil {
		fields := map[string]any{}
		var failure *rayleabot.ActionError
		if errors.As(err, &failure) {
			fields["code"] = failure.Code
		}
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "扫码二维码未能撤回；二维码在发出三分钟后失效。", Fields: fields})
	}
}

// sendLoginResult answers an ended login in the chat 扫码登录 came from,
// @ing the sender in a group. When the chat does not take the card, the text
// is sent instead; a send that may have gone through is not repeated.
func (a *App) sendLoginResult(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, owner accounts.Subject, view LoginView) error {
	if view.State == "failed" && view.Failure != nil {
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: loginText(view), Fields: map[string]any{"code": view.ErrorCode, "login_stage": view.Failure.Stage, "operation": view.Failure.Operation, "retcode": view.Failure.RetCode, "http_status": view.Failure.HTTPStatus, "reason": view.Failure.Reason}})
	}
	send := func(segments []rayleabot.Segment) error {
		if event.Event.Target.Type != "private" {
			first := segments[0]
			first.Data = map[string]any{"text": "\n" + stringValue(first.Data["text"])}
			segments = append([]rayleabot.Segment{rayleabot.At(owner.ActorID), first}, segments[1:]...)
		}
		address, _ := event.Address()
		_, err := event.Actions().SendTo(ctx, address, segments...)
		return err
	}
	segments, image := a.loginResult(ctx, event, store, view)
	err := send(segments)
	if err != nil && image && !unconfirmed(err) {
		err = send([]rayleabot.Segment{rayleabot.Text(loginText(view))})
	}
	if err != nil && !unconfirmed(err) {
		return err
	}
	return nil
}

// quote is a reply quoting the event's message, as upstream's reply(msg, true).
func quote(event *rayleabot.EventContext, text string) rayleabot.MessageSendRequest {
	return rayleabot.MessageSendRequest{SourceProtocol: event.Event.SourceProtocol, BotID: event.Event.BotID, TargetType: event.Event.Target.Type, TargetID: event.Event.Target.ID, ReplyToEventID: event.Event.EventID, FallbackToSendIfMissing: true, Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(text)}}}
}

// unconfirmed is a send that may still arrive, which is not repeated.
func unconfirmed(err error) bool {
	var failure *rayleabot.ActionError
	return errors.As(err, &failure) && failure.Code == "adapter.send_unconfirmed"
}
