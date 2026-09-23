package app

import (
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestCurrentUIDAnswersAMentionWithOnlyTheUID(t *testing.T) {
	a, store, record := setup(t, &fakeAPI{})
	s := subject()
	ask := func(eventType, userID string) rayleabot.ServiceRequest {
		return rayleabot.ServiceRequest{CallerPluginID: "raylea.zzz", Service: "accounts", ServiceVersion: 1, Method: "uids.current", Params: map[string]any{"game": "zzz", "user_id": userID}, Origin: rayleabot.ServiceOrigin{EventID: "event-2", EventType: eventType, SourceProtocol: s.SourceProtocol, SourceAdapter: s.SourceAdapter, BotID: s.BotID, Actor: &rayleabot.Actor{ID: "user-2"}, Target: &rayleabot.Target{Type: "group", ID: "group-1"}}}
	}
	result, err := a.Service(t.Context(), store, policy(), ask("message.group", s.ActorID))
	if err != nil || result["uid"] != role("zzz").UID || len(result) != 1 {
		t.Fatal(result, err)
	}
	bind := request("zzz", record.Ref)
	bind.Method, bind.Params = "uids.bind", map[string]any{"game": "zzz", "uid": "12345678"}
	if _, err := a.Service(t.Context(), store, policy(), bind); err != nil {
		t.Fatal(err)
	}
	if result, err = a.Service(t.Context(), store, policy(), ask("message.group", s.ActorID)); err != nil || result["uid"] != "12345678" {
		t.Fatal("the bound current UID does not come first", result, err)
	}
	if result, err = a.Service(t.Context(), store, policy(), ask("message.group", "nobody")); err != nil || result["uid"] != "" {
		t.Fatal(result, err)
	}
	_, err = a.Service(t.Context(), store, policy(), ask("management.action", s.ActorID))
	assertCode(t, err, "plugin.account_subject_denied")
}
