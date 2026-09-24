package app

import (
	"context"
	"errors"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

// The host gives an event 60 seconds by default
// (runtime.plugin_event_timeout_seconds) and does not tell a plugin the value
// it uses; the SDK sets a deadline only on service requests. A chat event
// starts new work for eventTime of that, and a login followed in its event
// leaves resultTime of it for saving, drawing and sending the result.
const (
	eventTime  = 40 * time.Second
	resultTime = 20 * time.Second
	actionTime = 4 * time.Second
)

// maxFollowers is how many 扫码登录 events wait for their logins at once.
// Each holds one of the plugin's event slots; beyond it, the per-minute task
// answers the login.
const maxFollowers = 2

// loginTaskRequest creates the per-minute login task; creating it again keeps
// the one task.
func loginTaskRequest() rayleabot.SchedulerCreateRequest {
	return rayleabot.SchedulerCreateRequest{TaskID: loginTask, Cron: "* * * * *", LogLabel: "米游社扫码结果", Payload: map[string]any{"kind": "login_notifications"}}
}

// followLogin waits for a group login inside its 扫码登录 event, as
// xiaoyao-cvs-plugin's GetQrCode polls it: it says once that the code was
// scanned, recalls the code once it is due and sends the result as soon as
// the login ends, recalling a code still shown. It follows until
// followWindow after the event started, or resultTime before the context's
// deadline when that is sooner; a login still open then is left to the
// per-minute task. A login canceled elsewhere ends it at once.
func (a *App) followLogin(parent context.Context, started time.Time, event *rayleabot.EventContext, store accounts.SecretStore, policy Policy, s *login) {
	select {
	case a.followers <- struct{}{}:
		defer func() { <-a.followers }()
	default:
		return
	}
	until := started.Add(a.followWindow)
	if deadline, ok := parent.Deadline(); ok && deadline.Add(-resultTime).Before(until) {
		until = deadline.Add(-resultTime)
	}
	end := time.NewTimer(time.Until(until))
	defer end.Stop()
	for {
		s.mu.Lock()
		done := terminal(s.view.State) || s.ready || s.ctx.Err() != nil
		scanned := !s.scanNoticed && s.view.State == "scanned" && s.view.Phase == "scan"
		s.scanNoticed = s.scanNoticed || scanned
		code, recallAt := s.qrMessage, s.recallAt
		s.mu.Unlock()
		if done {
			a.finishFollow(parent, event, store, policy, s)
			return
		}
		if scanned {
			// As upstream, a reply to the 扫码登录 message.
			ctx, cancel := context.WithTimeout(parent, actionTime)
			_, _ = event.Actions().MessageSend(ctx, rayleabot.MessageSendRequest{SourceAdapter: event.Event.SourceAdapter, SourceProtocol: event.Event.SourceProtocol, TargetType: event.Event.Target.Type, TargetID: event.Event.Target.ID, ReplyToEventID: event.Event.EventID, FallbackToSendIfMissing: true, Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text("二维码已扫描，请确认登录")}}})
			cancel()
		}
		recall := time.NewTimer(time.Until(recallAt))
		due := recall.C
		if code == "" {
			due = nil
		}
		select {
		case <-parent.Done():
			recall.Stop()
			return
		case <-end.C:
			recall.Stop()
			a.handOver(parent, event)
			return
		case <-s.ctx.Done():
		case <-s.changed:
		case <-due:
			a.recallCode(parent, event, s, true)
		}
		recall.Stop()
	}
}

// finishFollow recalls a code still shown, saves the ended login and sends
// its result; what it cannot finish is left to the per-minute task.
func (a *App) finishFollow(parent context.Context, event *rayleabot.EventContext, store accounts.SecretStore, policy Policy, s *login) {
	a.recallCode(parent, event, s, false)
	ctx, cancel := context.WithTimeout(parent, resultTime)
	defer cancel()
	s.mu.Lock()
	ref := s.view.Ref
	s.mu.Unlock()
	result, err := a.Manage(ctx, store, policy, "login.poll", map[string]any{"login_ref": ref})
	if err == nil {
		view := result["login"].(LoginView)
		if terminal(view.State) && a.sendLoginResult(ctx, event, store, s, view) == nil {
			return
		}
	}
	a.handOver(parent, event)
}

// handOver leaves a login to the per-minute task, creating the task again in
// case it ended meanwhile.
func (a *App) handOver(parent context.Context, event *rayleabot.EventContext) {
	ctx, cancel := context.WithTimeout(parent, actionTime)
	defer cancel()
	if _, err := event.Actions().SchedulerCreate(ctx, loginTaskRequest()); err != nil {
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "扫码结果任务未能创建，可发送“登录状态”查看结果。"})
	}
}

// recallCode recalls the code a login sent to a group, once: with due set
// only when its time came, otherwise at once. As upstream, a failed recall is
// logged, not retried.
func (a *App) recallCode(parent context.Context, event *rayleabot.EventContext, s *login, due bool) {
	s.mu.Lock()
	id, adapter := "", s.owner.SourceAdapter
	if s.qrMessage != "" && (!due || !time.Now().Before(s.recallAt)) {
		id, s.qrMessage = s.qrMessage, ""
	}
	s.mu.Unlock()
	if id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, actionTime)
	defer cancel()
	if _, err := event.Actions().ForOneBotAdapter(adapter).MessageDelete(ctx, id); err != nil {
		fields := map[string]any{}
		var failure *rayleabot.ActionError
		if errors.As(err, &failure) {
			fields["code"] = failure.Code
		}
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "群聊中的扫码二维码未能撤回；二维码在发出三分钟后失效。", Fields: fields})
	}
}

// claimResult marks an ended login's result as sent before it is sent, so
// it is sent once; given a chat, only when the login came from that chat. It
// is false when the result was already sent or has no chat to go to.
func (s *login) claimResult(chat *rayleabot.Target) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notified || s.chat.Type == "" || !terminal(s.view.State) || chat != nil && (s.chat.Type != chat.Type || s.chat.ID != chat.ID) {
		return false
	}
	s.notified = true
	return true
}

// sendLoginResult sends an ended login's result to the chat it came from,
// once, @ing the sender in a group. When the chat does not take the card, the
// text is sent instead; when nothing was sent, the claim is dropped for the
// per-minute task to retry. A send that may have gone through is not repeated.
func (a *App) sendLoginResult(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, s *login, view LoginView) error {
	if !s.claimResult(nil) {
		return nil
	}
	return a.postLoginResult(ctx, event, store, s, view)
}

// postLoginResult sends a result claimResult marked, as sendLoginResult does.
func (a *App) postLoginResult(ctx context.Context, event *rayleabot.EventContext, store accounts.SecretStore, s *login, view LoginView) error {
	s.mu.Lock()
	owner, chat := s.owner, s.chat
	s.mu.Unlock()
	if view.State == "failed" && view.Failure != nil {
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: loginText(view), Fields: map[string]any{"code": view.ErrorCode, "login_stage": view.Failure.Stage, "operation": view.Failure.Operation, "retcode": view.Failure.RetCode, "http_status": view.Failure.HTTPStatus, "reason": view.Failure.Reason}})
	}
	send := func(segments []rayleabot.Segment) error {
		request := rayleabot.MessageSendRequest{SourceAdapter: owner.SourceAdapter, SourceProtocol: owner.SourceProtocol, TargetType: "private", TargetID: owner.ActorID, Message: rayleabot.MessageOut{Segments: segments}}
		if chat.Type != "private" {
			request.TargetType, request.TargetID = chat.Type, chat.ID
			first := segments[0]
			first.Data = map[string]any{"text": "\n" + stringValue(first.Data["text"])}
			request.Message.Segments = append([]rayleabot.Segment{rayleabot.At(owner.ActorID), first}, segments[1:]...)
		}
		_, err := event.Actions().MessageSend(ctx, request)
		return err
	}
	segments, image := a.loginResult(ctx, event, store, view)
	err := send(segments)
	if err != nil && image && !unconfirmed(err) {
		err = send([]rayleabot.Segment{rayleabot.Text(loginText(view))})
	}
	if err != nil && !unconfirmed(err) {
		s.mu.Lock()
		s.notified = false
		s.mu.Unlock()
		return err
	}
	return nil
}

// unconfirmed is a send that may still arrive, which is not repeated.
func unconfirmed(err error) bool {
	var failure *rayleabot.ActionError
	return errors.As(err, &failure) && failure.Code == "adapter.send_unconfirmed"
}
