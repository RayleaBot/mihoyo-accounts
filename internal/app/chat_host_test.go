package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

// chatHost runs the plugin through the SDK as the host does, answering its
// actions, so tests can send chat and scheduler events.
type chatHost struct {
	t      *testing.T
	store  *memoryStore
	writer *io.PipeWriter
	lines  chan []byte
	// output is every frame the plugin wrote.
	output bytes.Buffer
	next   int
	sent   int
}

// hostAction is an action the plugin asked the host for during one event.
type hostAction struct {
	Name string
	Data map[string]any
}

func newChatHost(t *testing.T, a *App, store *memoryStore) *chatHost {
	t.Helper()
	inReader, hostWriter := io.Pipe()
	hostReader, outWriter := io.Pipe()
	finished := make(chan error, 1)
	go func() {
		finished <- rayleabot.Run(context.Background(), rayleabot.Options{Stdin: inReader, Stdout: outWriter, Stderr: io.Discard, Services: a.Services()}, a)
		outWriter.Close()
	}()
	h := &chatHost{t: t, store: store, writer: hostWriter, lines: make(chan []byte, 64)}
	t.Cleanup(func() {
		hostWriter.Close()
		a.Close()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("SDK did not stop")
		}
		hostReader.Close()
	})
	go func() {
		scanner := bufio.NewScanner(hostReader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			h.lines <- bytes.Clone(scanner.Bytes())
		}
		close(h.lines)
	}()
	s := subject()
	h.exchange("init", map[string]any{"type": "init", "request_id": "init", "protocol_version": "4", "plugin_id": "raylea.mihoyo-accounts", "bots": []any{map[string]any{"source_adapter": s.SourceAdapter, "source_protocol": s.SourceProtocol, "id": s.BotID}}, "config": policy(), "super_admins": []string{}, "command_prefixes": []string{"#"}, "timezone": "Asia/Shanghai", "concurrency": 4})
	return h
}

func (h *chatHost) write(frame map[string]any) {
	h.t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.writer.Write(append(raw, '\n')); err != nil {
		h.t.Fatal(err)
	}
}

// exchange sends a frame and answers the plugin's actions until it ends the
// request id; it returns the ending frame and the actions asked for.
func (h *chatHost) exchange(id string, frame map[string]any) (map[string]any, []hostAction) {
	h.t.Helper()
	h.write(frame)
	var actions []hostAction
	for {
		var raw []byte
		select {
		case raw = <-h.lines:
			if raw == nil {
				h.t.Fatal("SDK closed unexpectedly")
			}
		case <-time.After(3 * time.Second):
			h.t.Fatal("SDK response timed out")
		}
		h.output.Write(raw)
		var response map[string]any
		if json.Unmarshal(raw, &response) != nil {
			h.t.Fatal("invalid SDK frame")
		}
		if response["request_id"] == id {
			return response, actions
		}
		if response["type"] != "action" {
			continue
		}
		data := object(response["data"])
		actions = append(actions, hostAction{Name: stringValue(response["action"]), Data: data})
		result := map[string]any{}
		switch response["action"] {
		case "secret.read":
			value, exists, err := h.store.Read(h.t.Context(), stringValue(data["key"]))
			if err != nil {
				h.t.Fatal(err)
			}
			result = map[string]any{"key": data["key"], "exists": exists, "value": value}
		case "secret.write":
			values := map[string]string{}
			if decode(data["values"], &values) != nil {
				h.t.Fatal("invalid secret batch")
			}
			if err := h.store.Write(h.t.Context(), values); err != nil {
				h.t.Fatal(err)
			}
		case "message.send":
			h.sent++
			result = map[string]any{"message_id": fmt.Sprintf("sent-%d", h.sent), "delivery_kind": "send", "target_type": data["target_type"], "target_id": data["target_id"]}
		case "message.delete", "scheduler.create", "scheduler.delete", "logger.write":
		default:
			h.t.Fatalf("unexpected action %s", response["action"])
		}
		h.write(map[string]any{"type": "result", "request_id": response["request_id"], "status": "success", "data": result})
	}
}

// message sends a chat message from the test subject; targetType is private
// or group. command is the host's parsed command, empty for plain text.
func (h *chatHost) message(targetType, text, command string, args ...string) (map[string]any, []hostAction) {
	h.t.Helper()
	h.next++
	id := fmt.Sprintf("chat-%d", h.next)
	s := subject()
	targetID := "group-1"
	if targetType == "private" {
		targetID = s.ActorID
	}
	payload := map[string]any{}
	if command != "" {
		payload = map[string]any{"command": command, "args": args}
	}
	return h.exchange(id, map[string]any{"type": "event", "request_id": id, "event": map[string]any{"event_id": id, "event_type": "message." + targetType, "source_protocol": s.SourceProtocol, "source_adapter": s.SourceAdapter, "timestamp": time.Now().UnixMilli(), "actor": map[string]any{"id": s.ActorID}, "target": map[string]any{"type": targetType, "id": targetID}, "message": map[string]any{"plain_text": text, "segments": []any{map[string]any{"type": "text", "data": map[string]any{"text": text}}}}, "payload": payload}})
}

// loginTrigger sends the per-minute login task's scheduler event.
func (h *chatHost) loginTrigger() (map[string]any, []hostAction) {
	h.t.Helper()
	h.next++
	id := fmt.Sprintf("scheduler-%d", h.next)
	return h.exchange(id, map[string]any{"type": "event", "request_id": id, "event": map[string]any{"event_id": id, "event_type": "scheduler.trigger", "source_protocol": "scheduler", "source_adapter": "scheduler.internal", "timestamp": time.Now().UnixMilli(), "payload": map[string]any{"task_id": loginTask, "payload": map[string]any{"kind": "login_notifications"}}}})
}

// sentText is the text of a terminal message.send frame.
func sentText(frame map[string]any) string {
	var text string
	for _, segment := range object(object(frame["data"])["message"])["segments"].([]any) {
		if value, ok := object(object(segment)["data"])["text"].(string); ok {
			text += value
		}
	}
	return text
}
