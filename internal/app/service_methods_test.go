package app

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// Every documented method must be both declared and registered, or the host
// and the SDK answer plugin.method_not_found.
func TestManifestDeclaresEveryRegisteredMethod(t *testing.T) {
	raw, err := os.ReadFile("../../info.json")
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Services []struct {
			Name    string   `json:"name"`
			Methods []string `json:"methods"`
		} `json:"services"`
	}
	if json.Unmarshal(raw, &info) != nil || len(info.Services) != 1 {
		t.Fatal("invalid manifest")
	}
	registered := []string{}
	for name := range (&App{}).Services()[0].Methods {
		registered = append(registered, name)
	}
	declared := slices.Clone(info.Services[0].Methods)
	slices.Sort(registered)
	slices.Sort(declared)
	if !slices.Equal(registered, declared) {
		t.Fatal(registered, declared)
	}
	for _, method := range []string{"uids.bind", "uids.remove", "uids.use", "ark.request"} {
		if !slices.Contains(declared, method) {
			t.Fatal("missing", method)
		}
	}
}
