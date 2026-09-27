package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestSDKTranscriptSharesLoginWithoutPlaintextCredentials(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	a.pollInterval = time.Millisecond
	inReader, hostWriter := io.Pipe()
	hostReader, outWriter := io.Pipe()
	finishedRun := make(chan error, 1)
	go func() {
		finishedRun <- rayleabot.Run(context.Background(), rayleabot.Options{Stdin: inReader, Stdout: outWriter, Stderr: io.Discard, Services: a.Services()}, a)
		outWriter.Close()
	}()
	t.Cleanup(func() {
		hostWriter.Close()
		a.Close()
		select {
		case <-finishedRun:
		case <-time.After(3 * time.Second):
			t.Error("SDK did not stop")
		}
		hostReader.Close()
	})
	lines := make(chan []byte, 64)
	go func() {
		scanner := bufio.NewScanner(hostReader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			lines <- bytes.Clone(scanner.Bytes())
		}
		close(lines)
	}()
	var transcript bytes.Buffer
	write := func(frame map[string]any) {
		t.Helper()
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		transcript.Write(raw)
		if _, err := hostWriter.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	transaction := func(id string, frame map[string]any) map[string]any {
		t.Helper()
		write(frame)
		for {
			var raw []byte
			select {
			case raw = <-lines:
				if raw == nil {
					t.Fatal("SDK closed unexpectedly")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SDK response timed out")
			}
			transcript.Write(raw)
			var response map[string]any
			if json.Unmarshal(raw, &response) != nil {
				t.Fatal("invalid SDK frame")
			}
			if response["type"] == "action" {
				data := object(response["data"])
				result := map[string]any{}
				switch response["action"] {
				case "secret.read":
					value, exists, err := store.Read(t.Context(), stringValue(data["key"]))
					if err != nil {
						t.Fatal(err)
					}
					result = map[string]any{"key": data["key"], "exists": exists, "value": value}
				case "secret.write":
					values := map[string]string{}
					if decode(data["values"], &values) != nil {
						t.Fatal("invalid secret batch")
					}
					if err := store.Write(t.Context(), values); err != nil {
						t.Fatal(err)
					}
				default:
					t.Fatalf("unexpected action %s", response["action"])
				}
				write(map[string]any{"type": "result", "request_id": response["request_id"], "status": "success", "data": result})
				continue
			}
			if response["request_id"] == id {
				if response["type"] == "error" {
					t.Fatalf("SDK error: %v", response["code"])
				}
				return response
			}
		}
	}
	transaction("init", map[string]any{"type": "init", "request_id": "init", "protocol_version": "4", "plugin_id": "raylea.mihoyo-accounts", "bots": []any{}, "config": policy(), "super_admins": []string{}, "command_prefixes": []string{"#"}, "timezone": "Asia/Shanghai", "concurrency": 4})
	manage := func(id, action string, payload map[string]any) map[string]any {
		return transaction(id, eventFrame(id, time.Now().Add(eventTimeout), map[string]any{"event_id": id, "event_type": "management.action", "source_protocol": "management", "source_adapter": "management.ui", "timestamp": time.Now().UnixMilli(), "payload": map[string]any{"action": action, "payload": payload}}))
	}
	started := manage("start", "login.start", map[string]any{"subject": subject()})
	ref := stringValue(object(object(started["data"])["login"])["ref"])
	job, err := a.getLogin(ref)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.ready })
	bound := object(object(manage("poll", "login.poll", map[string]any{"login_ref": ref})["data"])["login"])
	if bound["state"] != "bound" {
		t.Fatal("login was not bound")
	}
	accountRef := stringValue(object(bound["account"])["ref"])
	for index, game := range []string{"genshin", "starrail", "zzz"} {
		r := request(game, accountRef)
		r.DeadlineAtMs = time.Now().Add(time.Second).UnixMilli()
		id := fmt.Sprintf("service-%d", index)
		response := transaction(id, eventFrame(id, time.UnixMilli(r.DeadlineAtMs), map[string]any{"event_id": id, "event_type": "plugin.request", "source_protocol": "platform", "source_adapter": "plugins.internal", "timestamp": time.Now().UnixMilli(), "payload": map[string]any{"service_request": r}}))
		if object(response["data"])["operation"] != game+".note" {
			t.Fatal("service response lost operation")
		}
	}
	manage("refresh", "accounts.refresh", map[string]any{"account_ref": accountRef})
	manage("delete", "accounts.remove", map[string]any{"account_ref": accountRef})
	for _, secret := range []string{"synthetic-stoken", "synthetic-ltoken", "synthetic-cookie", "synthetic-refreshed-cookie", "synthetic-refreshed-ltoken", "synthetic-mid", "synthetic-device", "synthetic-qr"} {
		if strings.Contains(transcript.String(), secret) {
			t.Fatal("credential material appeared in JSONL")
		}
	}
}

func TestCKCommandWordsFollowUpstream(t *testing.T) {
	for word, want := range map[string]string{"我的ck": "我的CK", "原神我的Cookie": "我的CK", "删除cookie": "删除CK", "刷新Ck": "刷新CK", "检查ck": "检查CK", "ck": "检查CK", "我的ck状态": "检查CK", "扫码登录": "扫码登录"} {
		if got := ckCommand(word); got != want {
			t.Errorf("%s = %s, want %s", word, got, want)
		}
	}
}

func TestTriggerOfAnUnknownTaskDeletesIt(t *testing.T) {
	a, store, _ := setup(t, &fakeAPI{})
	host := newChatHost(t, a, store)
	reply, actions := host.trigger("mihoyo.accounts.login", map[string]any{"kind": "login_notifications"})
	deletes := actionsNamed(actions, "scheduler.delete")
	if reply["type"] != "result" || len(deletes) != 1 || deletes[0].Data["task_id"] != "mihoyo.accounts.login" {
		t.Fatalf("the unknown task was not deleted: %v %v", reply, actions)
	}
}
