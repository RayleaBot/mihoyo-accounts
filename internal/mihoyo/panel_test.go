package mihoyo

import (
	"net/http"
	"testing"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
)

func TestStarrailCharacterFilterKeepsPropertyMetadata(t *testing.T) {
	client := New(doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Has("character_ids[]") {
			t.Fatal("local selection leaked into undocumented upstream parameters")
		}
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"avatar_list": []any{map[string]any{"id": 1001}, map[string]any{"id": 1002}}, "property_info": map[string]any{"5": map[string]any{"name": "暴击率"}}}}), nil
	}), nil)
	result, err := client.Execute(t.Context(), accounts.Tokens{}, accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}, "starrail.character", map[string]any{"character_ids": []any{"1002"}})
	if err != nil {
		t.Fatal(err)
	}
	list := result["avatar_list"].([]any)
	if len(list) != 1 || text(object(list[0])["id"]) != "1002" || result["property_info"] == nil {
		t.Fatal("wrong selection or missing metadata")
	}
}
func TestMalformedCharacterListReturnsTypedFailure(t *testing.T) {
	client := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return reply(map[string]any{"retcode": 0, "data": map[string]any{"avatar_list": "unexpected"}}), nil
	}), nil)
	_, err := client.Execute(t.Context(), accounts.Tokens{}, accounts.Role{Game: "starrail", UID: "100000001", Region: "prod_gf_cn"}, "starrail.character", map[string]any{"character_ids": []any{"1001"}})
	if failure, ok := err.(*Error); !ok || failure.Code != "plugin.upstream_response_invalid" {
		t.Fatal("malformed list did not produce a safe business error")
	}
}
