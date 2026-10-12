package app

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/mihoyo"
)

// actionText is the text of a message.send action.
func actionText(action hostAction) string {
	return sentText(map[string]any{"data": action.Data})
}

// quotes are the message.send actions quoting the 扫码登录 message.
func quotes(actions []hostAction) []hostAction {
	var found []hostAction
	for _, action := range actionsNamed(actions, "message.send") {
		if action.Data["reply_to_event_id"] != nil {
			found = append(found, action)
		}
	}
	return found
}

// resultSends are the message.send actions that are neither the code nor a
// quote.
func resultSends(actions []hostAction) []hostAction {
	var found []hostAction
	for _, action := range actionsNamed(actions, "message.send") {
		if action.Data["reply_to_event_id"] == nil && !strings.Contains(actionText(action), "请扫码以完成绑定") {
			found = append(found, action)
		}
	}
	return found
}

func position(actions []hostAction, want hostAction) int {
	return slices.IndexFunc(actions, func(action hostAction) bool {
		return action.Name == want.Name && reflect.DeepEqual(action.Data, want.Data)
	})
}

func chatLogin(t *testing.T, api *fakeAPI) (*App, *chatHost) {
	t.Helper()
	a, store, _ := setup(t, api)
	a.pollInterval, a.recallDelay = time.Millisecond, time.Hour
	return a, newChatHost(t, a, store)
}

func currentJob(a *App) *login {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	return a.jobs[a.owners[subject().Key()]]
}

// scannedThenConfirmed is scanned for 200ms after its first poll, then
// confirmed.
func scannedThenConfirmed() *fakeAPI {
	var once sync.Once
	var first time.Time
	return &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
		once.Do(func() { first = time.Now() })
		if time.Since(first) < 200*time.Millisecond {
			return mihoyo.Confirmation{State: "scanned"}, nil
		}
		return mihoyo.Confirmation{State: "confirmed", Tokens: syntheticTokens()}, nil
	}}
}

// waiting is a code nobody scans.
func waiting() *fakeAPI {
	return &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
		return mihoyo.Confirmation{State: "waiting"}, nil
	}}
}

func TestGroupLoginIsFollowedInTheBackground(t *testing.T) {
	a, host := chatLogin(t, scannedThenConfirmed())
	a.pollInterval, a.recallDelay = 5*time.Millisecond, 50*time.Millisecond
	reply, actions := host.message("group", "#扫码登录", "扫码登录")
	detaches, sends := actionsNamed(actions, "event.detach"), actionsNamed(actions, "message.send")
	// The event moves to the background before the code, and ends with a
	// plain result once it answered.
	if reply["type"] != "result" || len(detaches) != 1 || len(sends) == 0 || position(actions, detaches[0]) > position(actions, sends[0]) {
		t.Fatalf("the login was not followed in the background: %v %v", reply, actions)
	}
	code := object(sends[0].Data["message"])["segments"].([]any)
	if sends[0].Data["target_id"] != "group-1" || object(object(code[0])["data"])["user_id"] != subject().ActorID || object(object(code[1])["data"])["text"] != "\n请扫码以完成绑定\n" || object(code[2])["type"] != "image" {
		t.Fatalf("the code does not @ the sender in the group: %v", sends[0].Data)
	}
	notices := quotes(actions)
	if len(notices) != 1 || actionText(notices[0]) != "二维码已扫描，请确认登录" || notices[0].Data["reply_to_event_id"] != "chat-1" || notices[0].Data["target_id"] != "group-1" {
		t.Fatalf("the scan was not noticed once: %v", notices)
	}
	results, recalls := resultSends(actions), actionsNamed(actions, "message.delete")
	if len(results) != 1 || object(object(results[0].Data["message"])["segments"].([]any)[0])["type"] != "at" || !strings.Contains(actionText(results[0]), "登录成功") || results[0].Data["target_id"] != "group-1" {
		t.Fatalf("the result was not sent to the group: %v", actions)
	}
	// The code was recalled once when due, while the login was still open.
	if len(recalls) != 1 || recalls[0].Data["message_id"] != "sent-1" || recalls[0].At.Sub(sends[0].At) < a.recallDelay || position(actions, recalls[0]) > position(actions, results[0]) {
		t.Fatalf("the code was not recalled once when due: %v", actions)
	}
	for _, action := range append(sends, recalls...) {
		if action.Data["bot_id"] != subject().BotID || action.Data["source_adapter"] != nil {
			t.Fatalf("chat action still routes by connection: %s %v", action.Name, action.Data)
		}
		if action.Name == "message.send" && action.Data["source_protocol"] != subject().SourceProtocol {
			t.Fatalf("message lacks the account protocol: %v", action.Data)
		}
	}
}

func TestPrivateLoginIsFollowedInTheBackground(t *testing.T) {
	_, host := chatLogin(t, scannedThenConfirmed())
	reply, actions := host.message("private", "/扫码登录", "扫码登录")
	sends := actionsNamed(actions, "message.send")
	code := object(sends[0].Data["message"])["segments"].([]any)
	if reply["type"] != "result" || sends[0].Data["target_type"] != "private" || sends[0].Data["target_id"] != subject().ActorID || object(object(code[0])["data"])["text"] != "请扫码以完成绑定\n" || object(code[1])["type"] != "image" {
		t.Fatalf("private code: %v", sends[0].Data)
	}
	notices, results, recalls := quotes(actions), resultSends(actions), actionsNamed(actions, "message.delete")
	if len(notices) != 1 || actionText(notices[0]) != "二维码已扫描，请确认登录" || notices[0].Data["target_type"] != "private" {
		t.Fatalf("scanned notice: %v", notices)
	}
	if len(results) != 1 || results[0].Data["target_type"] != "private" || object(object(results[0].Data["message"])["segments"].([]any)[0])["type"] == "at" || !strings.Contains(actionText(results[0]), "登录成功") {
		t.Fatalf("result: %v", actions)
	}
	// The login ended before the code was due; the spent code goes first.
	if len(recalls) != 1 || recalls[0].Data["message_id"] != "sent-1" || position(actions, recalls[0]) > position(actions, results[0]) {
		t.Fatalf("the private code was not recalled with the result: %v", actions)
	}
}

func TestLoginNotConfirmedInTimeAnswersAsUpstream(t *testing.T) {
	timedOut := func(t *testing.T, actions []hostAction) {
		t.Helper()
		answers, recalls := quotes(actions), actionsNamed(actions, "message.delete")
		if len(answers) != 1 || actionText(answers[0]) != "验证超时" || answers[0].Data["reply_to_event_id"] != "chat-1" || len(resultSends(actions)) != 0 {
			t.Fatalf("the timeout was not answered as upstream: %v", actions)
		}
		if len(recalls) != 1 || position(actions, recalls[0]) > position(actions, answers[0]) {
			t.Fatalf("the code was not recalled: %v", actions)
		}
	}
	t.Run("code expired", func(t *testing.T) {
		_, host := chatLogin(t, &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
			return mihoyo.Confirmation{State: "expired"}, nil
		}})
		_, actions := host.message("group", "#扫码登录", "扫码登录")
		timedOut(t, actions)
	})
	t.Run("background deadline", func(t *testing.T) {
		a, host := chatLogin(t, waiting())
		host.detachFor = answerTime + 200*time.Millisecond
		_, actions := host.message("group", "#扫码登录", "扫码登录")
		timedOut(t, actions)
		// The event gave the login up answerTime before its deadline.
		detached, answered := actionsNamed(actions, "event.detach")[0].At, quotes(actions)[0].At
		if waited := answered.Sub(detached); waited < 150*time.Millisecond || waited >= host.detachFor {
			t.Fatalf("answered %v after moving to the background", waited)
		}
		job := currentJob(a)
		job.mu.Lock()
		defer job.mu.Unlock()
		if job.view.State != "expired" {
			t.Fatalf("login state %s", job.view.State)
		}
	})
}

func TestLoginCanceledElsewhereEndsItsEvent(t *testing.T) {
	a, host := chatLogin(t, waiting())
	// The login is canceled, as on the management page, once its code is out.
	host.seen = func(action hostAction) {
		if action.Name == "message.send" && strings.Contains(actionText(action), "请扫码以完成绑定") {
			job := currentJob(a)
			job.mu.Lock()
			ref := job.view.Ref
			job.mu.Unlock()
			_, _ = a.cancelLogin(ref)
		}
	}
	start := time.Now()
	reply, actions := host.message("group", "#扫码登录", "扫码登录")
	recalls := actionsNamed(actions, "message.delete")
	if reply["type"] != "result" || time.Since(start) > time.Second || len(recalls) != 1 || recalls[0].Data["message_id"] != "sent-1" {
		t.Fatalf("the canceled login did not end its event: %v", actions)
	}
	if len(resultSends(actions)) != 0 || len(quotes(actions)) != 0 {
		t.Fatalf("the canceled login was answered: %v", actions)
	}
}

func TestBusyPluginShowsNoCode(t *testing.T) {
	a, host := chatLogin(t, waiting())
	host.detachRefused = true
	reply, actions := host.message("group", "#扫码登录", "扫码登录")
	if reply["action"] != "message.send" || sentText(reply) != busyText || len(actionsNamed(actions, "message.send")) != 0 {
		t.Fatalf("the busy plugin did not refuse the login: %v %v", reply, actions)
	}
	job := currentJob(a)
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.view.State != "canceled" {
		t.Fatalf("the refused login is %s", job.view.State)
	}
}
