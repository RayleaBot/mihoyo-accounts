//go:build manual_smoke

package mihoyo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/accounts"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
)

// Explicit local acceptance check. The database is opened read-only; encrypted
// records travel through a private child-process pipe and are opened only inside
// this plugin. Credentials/device material are never printed or saved. Game
// records stay in memory unless MIHOYO_SMOKE_SAVE_PANELS explicitly requests
// credential-free business DTOs in the ignored local diagnostics directory.
// Python is a manual test dependency, not a plugin runtime dependency.
func TestLiveStoredAccountNotes(t *testing.T) {
	database, directory, actor := os.Getenv("MIHOYO_SMOKE_DB"), os.Getenv("MIHOYO_SMOKE_DATA"), os.Getenv("MIHOYO_SMOKE_ACTOR")
	if database == "" || directory == "" || actor == "" {
		t.Skip("explicit database, plugin data directory and actor required")
	}
	timeout := 60 * time.Second
	if os.Getenv("MIHOYO_SMOKE_ALL_PAGES") == "1" {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	loader := `import json,pathlib,sqlite3,sys
c=sqlite3.connect(pathlib.Path(sys.argv[1]).resolve().as_uri()+'?mode=ro',uri=True)
p='plugin:raylea.mihoyo-accounts:secret:'
print(json.dumps({k[len(p):]:v.decode() if isinstance(v,bytes) else v for k,v in c.execute('select key,value from secret_store where key like ?', (p+'account.%',))}))`
	raw, err := exec.CommandContext(ctx, "python", "-c", loader, database).Output()
	if err != nil {
		t.Fatal("could not read encrypted account rows")
	}
	defer clear(raw)
	store := liveReadOnlyStore{}
	if json.Unmarshal(raw, &store) != nil {
		t.Fatal("invalid encrypted store result")
	}
	manager, err := vault.NewManager(directory)
	if err != nil {
		t.Fatal("could not open plugin key manager")
	}
	defer manager.Lock()
	cipher, err := manager.Cipher()
	if err != nil {
		t.Fatal("account vault must already support local automatic unlock")
	}
	repository := accounts.Repository{}
	var selected accounts.Record
	for key := range store {
		record, err := repository.Get(ctx, store, cipher, strings.TrimPrefix(key, "account."))
		if err != nil {
			t.Fatal("could not open encrypted account")
		}
		if record.Owner.ActorID == actor {
			if selected.Ref != "" {
				t.Fatal("ambiguous actor; select a single account before this check")
			}
			selected = record
		}
	}
	if selected.Ref == "" {
		t.Fatal("no account for selected actor")
	}
	client := New(nil, nil)
	if os.Getenv("MIHOYO_SMOKE_RECORDS") == "1" {
		base := client.http
		shapes := map[string]any{}
		client.http = doerFunc(func(req *http.Request) (*http.Response, error) {
			response, err := base.Do(req)
			if err != nil {
				return response, err
			}
			if strings.Contains(req.URL.Path, "/badge/") {
				names := []string{}
				for _, cookie := range response.Cookies() {
					if shapeName.MatchString(cookie.Name) {
						names = append(names, cookie.Name)
					}
				}
				t.Logf("activity cookie field names: %v", names)
			}
			raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
			response.Body.Close()
			if err != nil {
				return nil, err
			}
			response.Body = io.NopCloser(bytes.NewReader(raw))
			var envelope struct {
				RetCode int `json:"retcode"`
				Data    any `json:"data"`
			}
			if json.Unmarshal(raw, &envelope) == nil && (strings.Contains(req.URL.Path, "game_record") || strings.Contains(req.URL.Path, "ledger")) {
				shapes[req.URL.Path] = map[string]any{"retcode": envelope.RetCode, "shape": businessShape(envelope.Data, 0)}
			}
			return response, nil
		})
		t.Cleanup(func() {
			data, _ := json.MarshalIndent(shapes, "", "  ")
			_ = os.MkdirAll("../../.rayleabot", 0700)
			if os.WriteFile(filepath.Join("../../.rayleabot", "live-operation-shapes.json"), data, 0600) != nil {
				t.Error("could not save field-shape diagnostics")
			}
		})
	}
	if os.Getenv("MIHOYO_SMOKE_DIAGNOSTICS") == "1" {
		base := client.http
		client.http = doerFunc(func(req *http.Request) (*http.Response, error) {
			response, err := base.Do(req)
			if err != nil || !(strings.Contains(req.URL.Path, "GachaLog") || strings.HasSuffix(req.URL.Path, "genAuthKey")) {
				return response, err
			}
			raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
			response.Body.Close()
			if err != nil {
				return nil, err
			}
			response.Body = io.NopCloser(bytes.NewReader(raw))
			var envelope struct {
				RetCode int `json:"retcode"`
				Data    struct {
					Version json.Number `json:"authkey_ver"`
					Sign    json.Number `json:"sign_type"`
					Zone    json.Number `json:"region_time_zone"`
					List    []any       `json:"list"`
				} `json:"data"`
			}
			if json.Unmarshal(raw, &envelope) == nil {
				t.Logf("gacha shape: retcode=%d timezone=%s records=%d authkey_ver=%s sign_type=%s", envelope.RetCode, envelope.Data.Zone, len(envelope.Data.List), envelope.Data.Version, envelope.Data.Sign)
			}
			return response, nil
		})
	}
	tokens := selected.Tokens
	if tokens.DeviceFP == "" || tokens.DeviceFPExpiresAtMS <= time.Now().UnixMilli() {
		tokens, err = client.Fingerprint(ctx, tokens)
		if err != nil {
			reportLiveError(t, err)
		}
	}
	checked := map[string]bool{}
	for _, role := range selected.Roles {
		if checked[role.Game] {
			continue
		}
		checked[role.Game] = true
		t.Run(role.Game, func(t *testing.T) {
			if os.Getenv("MIHOYO_SMOKE_SIGN_INFO") == "1" && role.Game != "zzz" {
				for _, suffix := range []string{"sign_info", "sign_rewards"} {
					value, err := client.Execute(ctx, tokens, role, role.Game+"."+suffix, nil)
					if err != nil {
						reportLiveError(t, err)
					}
					if len(value) == 0 {
						t.Fatal("missing read-only sign data")
					}
					t.Logf("read-only %s accepted", suffix)
				}
				return
			}
			if os.Getenv("MIHOYO_SMOKE_GROWTH") == "1" && role.Game != "zzz" {
				roster, err := client.Execute(ctx, tokens, role, role.Game+".characters", nil)
				if err != nil {
					reportLiveError(t, err)
				}
				var list []any
				for _, key := range []string{"list", "avatars", "avatar_list"} {
					if items, ok := roster[key].([]any); ok && len(items) > 0 {
						list = items
						break
					}
				}
				if len(list) == 0 {
					t.Fatal("no growth character available")
				}
				entry := object(list[0])
				if base := object(entry["base"]); base != nil {
					entry = base
				}
				id := text(entry["id"])
				detail, err := client.Execute(ctx, tokens, role, role.Game+".growth_detail", map[string]any{"character_id": id})
				if err != nil {
					reportLiveError(t, err)
				}
				shape, _ := json.Marshal(businessShape(detail, 0))
				t.Logf("growth detail shape: %s", shape)
				computed, err := client.Execute(ctx, tokens, role, role.Game+".growth_compute", map[string]any{"character_id": id, "current_level": 1, "target_level": 2})
				if err != nil {
					reportLiveError(t, err)
				}
				shape, _ = json.Marshal(businessShape(computed, 0))
				t.Logf("growth compute shape: %s", shape)
				if os.Getenv("MIHOYO_SMOKE_SAVE_GROWTH") == "1" {
					raw, _ := json.Marshal(map[string]any{"character_id": id, "detail": detail, "computed": computed})
					if err := os.WriteFile(filepath.Join("../../.rayleabot", role.Game+"-growth-business.json"), raw, 0600); err != nil {
						t.Fatal("could not save credential-free growth DTO")
					}
				}
				return
			}
			if os.Getenv("MIHOYO_SMOKE_BILLING") == "1" && role.Game == "starrail" {
				// Logs the self-help list shape per category: list length, item
				// field names, action names with counts and per-day amounts of
				// the time-recovered power. No token or account value is printed.
				base := client.http
				client.http = doerFunc(func(req *http.Request) (*http.Response, error) {
					response, err := base.Do(req)
					if err != nil || !strings.Contains(req.URL.Path, "self_help_inquiry") {
						return response, err
					}
					raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
					response.Body.Close()
					if err != nil {
						return nil, err
					}
					response.Body = io.NopCloser(bytes.NewReader(raw))
					var envelope struct {
						RetCode int            `json:"retcode"`
						Message string         `json:"message"`
						Data    map[string]any `json:"data"`
					}
					if json.Unmarshal(raw, &envelope) == nil {
						keys := []string{}
						for key := range envelope.Data {
							keys = append(keys, key)
						}
						sort.Strings(keys)
						list, _ := envelope.Data["list"].([]any)
						fields := []string{}
						if len(list) > 0 {
							for key := range object(list[0]) {
								fields = append(fields, key)
							}
							sort.Strings(fields)
						}
						t.Logf("%s page=%s size=%s retcode=%d message=%q data keys=%v list=%d item fields=%v", req.URL.Path, req.URL.Query().Get("page"), req.URL.Query().Get("page_size"), envelope.RetCode, envelope.Message, keys, len(list), fields)
					}
					return response, nil
				})
				for _, category := range []string{"stellar", "dreams", "relic", "power"} {
					t.Run(category, func(t *testing.T) {
						size := 10
						if category == "power" {
							size = 50
						}
						actions, days, ids, firstIDs := map[string]int{}, map[string][2]int{}, map[string]bool{}, []string{}
						cursor := "0"
						for page := 1; page <= 200; page++ {
							input := map[string]any{"category": category, "page": page, "end_id": cursor}
							if size != 10 {
								input["page_size"] = size
							}
							result, err := client.Execute(ctx, tokens, role, "starrail.billing", input)
							if err != nil {
								reportLiveError(t, err)
								return
							}
							items, _ := result["items"].([]any)
							if len(items) > 0 {
								firstIDs = append(firstIDs, text(object(items[0])["id"])[max(0, len(text(object(items[0])["id"]))-4):]+"@"+text(object(items[0])["time"]))
							}
							for _, raw := range items {
								item := object(raw)
								if ids[text(item["id"])] {
									continue
								}
								ids[text(item["id"])] = true
								action := text(item["action"])
								actions[action]++
								if category == "power" && action == "随时间回复开拓力" {
									day := text(item["time"])
									if len(day) >= 10 {
										day = day[:10]
									}
									count := days[day]
									if text(item["add_num"]) == "1" {
										count[0]++
									} else {
										count[1]++
									}
									days[day] = count
								}
							}
							cursor = text(result["next_end_id"])
							if category != "power" || result["has_more"] != true {
								t.Logf("%s pages=%d", category, page)
								break
							}
							time.Sleep(500 * time.Millisecond)
						}
						t.Logf("%s distinct=%d actions=%v first item per page (id tail@time)=%v", category, len(ids), actions, firstIDs)
						if category == "power" {
							t.Logf("time-recovered power per day [single, bulk]=%v", days)
						}
					})
				}
				return
			}
			if os.Getenv("MIHOYO_SMOKE_PANEL") == "1" {
				roster, err := client.Execute(ctx, tokens, role, role.Game+".characters", nil)
				if err != nil {
					reportLiveError(t, err)
				}
				var entries []any
				for _, key := range []string{"list", "avatars", "avatar_list"} {
					if list, ok := roster[key].([]any); ok && len(list) > 0 {
						entries = list
						break
					}
				}
				if len(entries) == 0 {
					t.Skip("no owned character available")
				}
				item := object(entries[0])
				id := text(item["id"])
				if id == "" {
					id = text(object(item["base"])["id"])
				}
				input := map[string]any{}
				if role.Game == "genshin" {
					input["character_ids"] = []any{id}
				}
				if role.Game == "zzz" {
					input["id_list"] = []any{id}
				}
				result, err := client.Execute(ctx, tokens, role, role.Game+".character", input)
				if err != nil {
					reportLiveError(t, err)
				}
				if len(result) == 0 {
					t.Fatal("missing panel fields")
				}
				if os.Getenv("MIHOYO_SMOKE_SAVE_PANELS") == "1" {
					raw, _ := json.Marshal(result)
					_ = os.MkdirAll("../../.rayleabot", 0700)
					if os.WriteFile(filepath.Join("../../.rayleabot", role.Game+"-panel-business.json"), raw, 0600) != nil {
						t.Fatal("could not save redacted business DTO")
					}
				}
				metadata := map[string]any{}
				for _, field := range []string{"property_map", "property_info"} {
					if value, exists := result[field]; exists {
						metadata[field] = value
					}
				}
				if len(metadata) > 0 {
					raw, _ := json.MarshalIndent(metadata, "", "  ")
					_ = os.MkdirAll("../../.rayleabot", 0700)
					_ = os.WriteFile(filepath.Join("../../.rayleabot", role.Game+"-property-metadata.json"), raw, 0600)
				}
				return
			}
			if os.Getenv("MIHOYO_SMOKE_RECORDS") == "1" {
				for _, name := range OperationNames() {
					if OperationGame(name) != role.Game || strings.HasSuffix(name, ".gacha") || name == role.Game+".character" {
						continue
					}
					t.Run(strings.TrimPrefix(name, role.Game+"."), func(t *testing.T) {
						defer func() {
							select {
							case <-ctx.Done():
							case <-time.After(500 * time.Millisecond):
							}
						}()
						data, err := client.Execute(ctx, tokens, role, name, nil)
						if err != nil {
							reportLiveError(t, err)
						}
						if len(data) == 0 {
							t.Fatal("no selected business fields")
						}
						if os.Getenv("MIHOYO_SMOKE_SELECTORS") == "1" {
							input := map[string]any{}
							for _, field := range operations[name].inputs {
								if field == "schedule_type" {
									input[field] = 2
								}
								if field == "month" {
									now := time.Now().In(time.FixedZone("UTC+8", 8*3600))
									month := int(now.Month())
									if role.Game != "genshin" {
										month += now.Year() * 100
									}
									input[field] = month
								}
							}
							if len(input) > 0 {
								if _, err := client.Execute(ctx, tokens, role, name, input); err != nil {
									reportLiveError(t, err)
								}
							}
						}
					})
				}
				return
			}
			if os.Getenv("MIHOYO_SMOKE_GACHA") == "1" {
				for _, pool := range gachaProfiles[role.Game].pools {
					t.Run(pool, func(t *testing.T) {
						endID := "0"
						for page := 1; page <= 10000; page++ {
							result, err := client.Execute(ctx, tokens, role, role.Game+".gacha", map[string]any{"gacha_type": pool, "end_id": endID, "page": page})
							if err != nil {
								reportLiveError(t, err)
							}
							if _, ok := result["list"].([]any); !ok {
								t.Fatal("missing gacha list")
							}
							select {
							case <-ctx.Done():
								t.Fatal("acceptance deadline")
							case <-time.After(time.Second):
							}
							if os.Getenv("MIHOYO_SMOKE_ALL_PAGES") != "1" || result["has_more"] != true {
								return
							}
							endID = result["next_end_id"].(string)
						}
						t.Fatal("page limit")
					})
				}
				return
			}
			result, err := client.Execute(ctx, tokens, role, role.Game+".note", nil)
			if err != nil {
				reportLiveError(t, err)
			}
			if len(result) == 0 {
				t.Fatal("missing note response")
			}
		})
	}
	if len(checked) != 3 {
		t.Fatal("expected bound roles for all three games")
	}
}

var shapeName = regexp.MustCompile(`^[a-z_][a-z_0-9]{0,47}$`)

func businessShape(value any, depth int) any {
	if depth > 5 {
		return "nested"
	}
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		keys := make([]string, 0, len(v))
		for key := range v {
			if shapeName.MatchString(key) && !strings.Contains(key, "token") && !strings.Contains(key, "cookie") {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			out[key] = businessShape(v[key], depth+1)
		}
		return out
	case []any:
		if len(v) == 0 {
			return []any{}
		}
		return []any{businessShape(v[0], depth+1)}
	case bool:
		return "bool"
	case float64, json.Number:
		return "number"
	case string:
		return "string"
	default:
		return "null"
	}
}

type liveReadOnlyStore map[string]string

func (s liveReadOnlyStore) Read(_ context.Context, key string) (string, bool, error) {
	value, exists := s[key]
	return value, exists, nil
}
func (liveReadOnlyStore) Write(context.Context, map[string]string) error {
	return errors.New("read-only acceptance store")
}
