package app

import (
	"context"
	"crypto/rand"
	"slices"
	"sync"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/accounts"
)

type batchItem struct {
	Ref      string `json:"account_ref"`
	Revision uint64 `json:"revision"`
}
type batchResult struct {
	Ref     string `json:"account_ref"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type accountStatistics struct {
	Accounts      int            `json:"accounts"`
	Subjects      int            `json:"subjects"`
	States        map[string]int `json:"states"`
	GameRoles     map[string]int `json:"game_roles"`
	Refreshable   int            `json:"refreshable"`
	PublicQueries int            `json:"public_queries"`
	Pages         int            `json:"pages"`
}
type batchView struct {
	Ref          string             `json:"ref"`
	Mode         string             `json:"mode"`
	State        string             `json:"state"`
	Sequence     int                `json:"sequence"`
	Total        int                `json:"total"`
	Processed    int                `json:"processed"`
	Results      []batchResult      `json:"results"`
	Statistics   *accountStatistics `json:"statistics,omitempty"`
	StartedAtMS  int64              `json:"started_at_ms"`
	FinishedAtMS int64              `json:"finished_at_ms,omitempty"`
}
type accountBatch struct {
	mu      sync.Mutex
	view    batchView
	items   []batchItem
	page    int
	seen    map[string]bool
	owners  map[string]bool
	expires time.Time
}
type accountBatches struct {
	mu   sync.Mutex
	jobs map[string]*accountBatch
}

func (b *accountBatch) snapshot() batchView {
	v := b.view
	v.Results = slices.Clone(v.Results)
	if v.Statistics != nil {
		s := *v.Statistics
		s.States = copyCounts(s.States)
		s.GameRoles = copyCounts(s.GameRoles)
		v.Statistics = &s
	}
	return v
}
func copyCounts(values map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
func (b *accountBatches) get(ref string) (*accountBatch, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	job := b.jobs[ref]
	if job == nil {
		return nil, failure("plugin.account_batch_missing")
	}
	return job, nil
}
func (b *accountBatches) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, job := range b.jobs {
		job.mu.Lock()
		job.view.State = "canceled"
		job.items = nil
		job.seen = nil
		job.owners = nil
		job.mu.Unlock()
	}
	b.jobs = nil
}
func (a *App) manageBatch(ctx context.Context, store accounts.SecretStore, policy Policy, action string, input map[string]any) (map[string]any, error) {
	if action == "accounts.batch.start" {
		var q struct {
			Mode    string      `json:"mode"`
			Items   []batchItem `json:"items"`
			Confirm bool        `json:"confirm"`
		}
		if decode(input, &q) != nil || !slices.Contains([]string{"check", "refresh", "remove_invalid", "statistics"}, q.Mode) || q.Mode == "statistics" && len(q.Items) > 0 || q.Mode != "statistics" && (len(q.Items) == 0 || len(q.Items) > 128) || (q.Mode == "refresh" || q.Mode == "remove_invalid") && !q.Confirm {
			return nil, failure("plugin.account_input_invalid")
		}
		seen := map[string]bool{}
		for _, item := range q.Items {
			if item.Ref == "" || len(item.Ref) > 64 || seen[item.Ref] || item.Revision == 0 {
				return nil, failure("plugin.account_input_invalid")
			}
			seen[item.Ref] = true
		}
		if _, err := a.Vault.Cipher(); err != nil {
			return nil, err
		}
		a.batches.mu.Lock()
		defer a.batches.mu.Unlock()
		if a.batches.jobs == nil {
			a.batches.jobs = map[string]*accountBatch{}
		}
		for ref, job := range a.batches.jobs {
			job.mu.Lock()
			expired := time.Now().After(job.expires) || job.view.State == "canceled"
			if expired {
				job.view.State = "canceled"
			}
			job.mu.Unlock()
			if expired {
				delete(a.batches.jobs, ref)
			}
		}
		if len(a.batches.jobs) >= 8 {
			return nil, failure("plugin.account_batch_limit")
		}
		v := batchView{Ref: rand.Text(), Mode: q.Mode, State: "running", Total: len(q.Items), Results: []batchResult{}, StartedAtMS: time.Now().UnixMilli()}
		if q.Mode == "statistics" {
			v.Statistics = &accountStatistics{States: map[string]int{}, GameRoles: map[string]int{}}
		}
		job := &accountBatch{view: v, items: q.Items, seen: map[string]bool{}, owners: map[string]bool{}, expires: time.Now().Add(15 * time.Minute)}
		a.batches.jobs[v.Ref] = job
		return map[string]any{"batch": job.snapshot()}, nil
	}
	job, err := a.batches.get(stringValue(input["ref"]))
	if err != nil {
		return nil, err
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if time.Now().After(job.expires) {
		job.view.State = "canceled"
		return nil, failure("plugin.account_batch_missing")
	}
	if action == "accounts.batch.cancel" {
		job.view.State = "canceled"
		job.items = nil
		job.seen = nil
		job.owners = nil
		return map[string]any{"batch": job.snapshot()}, nil
	}
	var q struct {
		Sequence *int `json:"sequence"`
	}
	if action != "accounts.batch.step" || decode(input, &q) != nil || q.Sequence == nil || *q.Sequence < 0 {
		return nil, failure("plugin.account_input_invalid")
	}
	if *q.Sequence == job.view.Sequence-1 && job.view.State != "canceled" {
		job.expires = time.Now().Add(15 * time.Minute)
		return map[string]any{"batch": job.snapshot()}, nil
	}
	if *q.Sequence != job.view.Sequence || job.view.State != "running" {
		return nil, failure("plugin.account_batch_sequence")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if job.view.Mode == "statistics" {
		if job.page >= 32768 {
			return nil, failure("plugin.account_batch_limit")
		}
		result, err := a.Manage(ctx, store, policy, "accounts.list", map[string]any{"page": job.page})
		if err != nil {
			return nil, err
		}
		items := result["items"].([]accounts.Summary)
		newCount := 0
		for _, item := range items {
			if !job.seen[item.Ref] {
				newCount++
			}
		}
		if len(job.seen)+newCount > 16384 {
			return nil, failure("plugin.account_batch_limit")
		}
		stats := job.view.Statistics
		for _, item := range items {
			if job.seen[item.Ref] {
				continue
			}
			job.seen[item.Ref] = true
			job.owners[item.Owner.Key()] = true
			stats.Accounts++
			stats.States[item.Status]++
			if item.CanRefresh {
				stats.Refreshable++
			}
			if item.PublicQuery != nil && item.PublicQuery.Enabled {
				stats.PublicQueries++
			}
			for _, role := range item.Roles {
				stats.GameRoles[role.Game]++
			}
		}
		stats.Subjects = len(job.owners)
		stats.Pages++
		job.view.Processed = stats.Accounts
		job.view.Total = result["total"].(int)
		if next, _ := result["next_page"].(*int); next != nil && *next > job.page {
			job.page = *next
		} else {
			job.view.State = "completed"
		}
	} else {
		item := job.items[job.view.Sequence]
		value := batchResult{Ref: item.Ref, Label: "所选账号", State: "success"}
		// Each call uses this step's live store; no expired event client is kept.
		result, err := a.Manage(ctx, store, policy, "accounts."+job.view.Mode, map[string]any{"account_ref": item.Ref, "revision": item.Revision, "confirm": true})
		if err != nil {
			failure := publicError(err)
			if slices.Contains([]string{"plugin.vault_locked", "plugin.vault_missing", "plugin.vault_ciphertext_invalid", "plugin.account_operation_canceled", "plugin.account_operation_failed"}, failure.Code) {
				return nil, err
			}
			value.Code = failure.Code
			value.Message = failure.Message
			value.State = "failed"
			if failure.Code == "plugin.account_not_found" || failure.Code == "plugin.account_refresh_unavailable" {
				value.State = "skipped"
			}
		} else {
			if summary, ok := result["account"].(accounts.Summary); ok {
				value.Label = summary.AccountLabel
			}
			if result["skipped"] == true {
				value.State = "skipped"
				value.Message = "账号已变化或并非失效，未移除。"
			}
		}
		job.view.Results = append(job.view.Results, value)
		job.view.Processed++
		if job.view.Processed == len(job.items) {
			job.view.State = "completed"
		}
	}
	job.view.Sequence++
	job.expires = time.Now().Add(15 * time.Minute)
	if job.view.State == "completed" {
		job.view.FinishedAtMS = time.Now().UnixMilli()
		job.items = nil
		job.seen = nil
		job.owners = nil
	}
	return map[string]any{"batch": job.snapshot()}, nil
}
