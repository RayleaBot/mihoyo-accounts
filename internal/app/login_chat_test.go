package app

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/mihoyo"
)

// actionText is the text of a message.send action.
func actionText(action hostAction) string {
	return sentText(map[string]any{"data": action.Data})
}

// resultSends are the message.send actions that are not the code or the
// scanned notice.
func resultSends(actions []hostAction) []hostAction {
	var found []hostAction
	for _, action := range actionsNamed(actions, "message.send") {
		segments := object(action.Data["message"])["segments"].([]any)
		if action.Data["reply_to_event_id"] == nil && object(segments[len(segments)-1])["type"] != "image" {
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
	a.pollInterval, a.followWindow, a.recallDelay = time.Millisecond, 2*time.Second, time.Hour
	return a, newChatHost(t, a, store)
}

func currentJob(a *App) *login {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	return a.jobs[a.owners[subject()]]
}

func TestGroupLoginResultIsSentInItsEvent(t *testing.T) {
	_, host := chatLogin(t, &fakeAPI{})
	reply, actions := host.message("group", "#扫码登录", "扫码登录")
	sends, recalls := actionsNamed(actions, "message.send"), actionsNamed(actions, "message.delete")
	if reply["type"] != "result" || len(actionsNamed(actions, "scheduler.create")) != 1 || len(sends) != 2 {
		t.Fatalf("group 扫码登录 did not answer in its event: %v", actions)
	}
	code := object(sends[0].Data["message"])["segments"].([]any)
	if sends[0].Data["target_id"] != "group-1" || object(object(code[0])["data"])["user_id"] != subject().ActorID || object(code[2])["type"] != "image" {
		t.Fatalf("the code does not @ the sender in the group: %v", sends[0].Data)
	}
	result := object(sends[1].Data["message"])["segments"].([]any)
	if sends[1].Data["target_id"] != "group-1" || object(result[0])["type"] != "at" || !strings.Contains(actionText(sends[1]), "登录成功") {
		t.Fatalf("the result was not sent to the group: %v", sends[1].Data)
	}
	// The login ended before the code was due; the spent code goes first.
	if len(recalls) != 1 || recalls[0].Data["message_id"] != "sent-1" || position(actions, recalls[0]) > position(actions, sends[1]) {
		t.Fatalf("the code was not recalled with the result: %v", actions)
	}
	_, actions = host.loginTrigger()
	if len(actionsNamed(actions, "message.send")) != 0 || len(actionsNamed(actions, "message.delete")) != 0 || len(actionsNamed(actions, "scheduler.delete")) != 1 {
		t.Fatalf("the task repeated the event's work: %v", actions)
	}
}

func TestGroupLoginSaysScannedOnceAndRecallsTheCodeWhenDue(t *testing.T) {
	var once sync.Once
	var first time.Time
	api := &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
		once.Do(func() { first = time.Now() })
		if time.Since(first) < 200*time.Millisecond {
			return mihoyo.Confirmation{State: "scanned"}, nil
		}
		return mihoyo.Confirmation{State: "confirmed", Tokens: syntheticTokens()}, nil
	}}
	a, host := chatLogin(t, api)
	a.pollInterval, a.recallDelay = 5*time.Millisecond, 50*time.Millisecond
	_, actions := host.message("group", "#扫码登录", "扫码登录")
	var notices []hostAction
	for _, send := range actionsNamed(actions, "message.send") {
		if send.Data["reply_to_event_id"] != nil {
			notices = append(notices, send)
		}
	}
	if len(notices) != 1 || actionText(notices[0]) != "二维码已扫描，请确认登录" || notices[0].Data["reply_to_event_id"] != "chat-1" || notices[0].Data["target_id"] != "group-1" {
		t.Fatalf("scanned notice: %v", notices)
	}
	results, recalls := resultSends(actions), actionsNamed(actions, "message.delete")
	if len(results) != 1 || !strings.Contains(actionText(results[0]), "登录成功") {
		t.Fatalf("result: %v", actions)
	}
	// The code was recalled when due, while the login was still open.
	if len(recalls) != 1 || recalls[0].Data["message_id"] != "sent-1" || position(actions, recalls[0]) > position(actions, results[0]) {
		t.Fatalf("the code was not recalled once when due: %v", actions)
	}
	_, actions = host.loginTrigger()
	if len(actionsNamed(actions, "message.send")) != 0 || len(actionsNamed(actions, "message.delete")) != 0 {
		t.Fatalf("the task repeated the event's work: %v", actions)
	}
}

func TestGroupLoginStillOpenIsLeftToTheTask(t *testing.T) {
	var confirmed atomic.Bool
	api := &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
		if !confirmed.Load() {
			return mihoyo.Confirmation{State: "waiting"}, nil
		}
		return mihoyo.Confirmation{State: "confirmed", Tokens: syntheticTokens()}, nil
	}}
	a, host := chatLogin(t, api)
	a.followWindow, a.recallDelay = 200*time.Millisecond, 50*time.Millisecond
	reply, actions := host.message("group", "#扫码登录", "扫码登录")
	// The event recalled its code when due and handed the login over,
	// creating the task again.
	if reply["type"] != "result" || len(actionsNamed(actions, "message.send")) != 1 || len(actionsNamed(actions, "message.delete")) != 1 || len(actionsNamed(actions, "scheduler.create")) != 2 {
		t.Fatalf("the open login was not handed over: %v", actions)
	}
	confirmed.Store(true)
	job := currentJob(a)
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.ready })
	_, actions = host.loginTrigger()
	results := actionsNamed(actions, "message.send")
	if len(results) != 1 || results[0].Data["target_id"] != "group-1" || !strings.Contains(actionText(results[0]), "登录成功") || len(actionsNamed(actions, "message.delete")) != 0 || len(actionsNamed(actions, "scheduler.delete")) != 1 {
		t.Fatalf("the task did not answer the login once: %v", actions)
	}
	_, actions = host.loginTrigger()
	if len(actionsNamed(actions, "message.send")) != 0 || len(actionsNamed(actions, "message.delete")) != 0 {
		t.Fatalf("the result was sent again: %v", actions)
	}
}

func TestFollowedLoginEndsWhenCanceledElsewhere(t *testing.T) {
	api := &fakeAPI{poll: func(context.Context, mihoyo.QR) (mihoyo.Confirmation, error) {
		return mihoyo.Confirmation{State: "waiting"}, nil
	}}
	a, host := chatLogin(t, api)
	canceled := make(chan struct{})
	go func() {
		defer close(canceled)
		for {
			if job := currentJob(a); job != nil {
				job.mu.Lock()
				sent := job.qrMessage != ""
				ref := job.view.Ref
				job.mu.Unlock()
				if sent {
					_, _ = a.cancelLogin(ref)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	start := time.Now()
	_, actions := host.message("group", "#扫码登录", "扫码登录")
	<-canceled
	results := resultSends(actions)
	if time.Since(start) > time.Second || len(results) != 1 || !strings.Contains(actionText(results[0]), "登录已取消") || len(actionsNamed(actions, "message.delete")) != 1 {
		t.Fatalf("the canceled login did not end its event: %v", actions)
	}
}

func TestPrivateLoginIsAnsweredOnceByTheTaskOrLoginStatus(t *testing.T) {
	a, host := chatLogin(t, &fakeAPI{})
	reply, actions := host.message("private", "#扫码登录", "扫码登录")
	if !strings.Contains(sentText(reply), "登录结果会在一分钟内发送") || len(actionsNamed(actions, "message.send")) != 0 {
		t.Fatalf("private 扫码登录 changed: %q, %v", sentText(reply), actions)
	}
	job := currentJob(a)
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.ready })
	reply, _ = host.message("private", "#登录状态", "登录状态")
	if !strings.Contains(sentText(reply), "登录成功") {
		t.Fatalf("登录状态: %q", sentText(reply))
	}
	// 登录状态 gave the result in the login's chat; the task does not send it
	// again.
	_, actions = host.loginTrigger()
	if len(actionsNamed(actions, "message.send")) != 0 || len(actionsNamed(actions, "scheduler.delete")) != 1 {
		t.Fatalf("the result was sent twice: %v", actions)
	}
}
