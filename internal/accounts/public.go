package accounts

import (
	"context"
	"errors"
	"github.com/RayleaBot/plugin-mihoyo-accounts/internal/vault"
	"slices"
	"time"
)

const publicKey = "public.pool"

var ErrPublicUnavailable = errors.New("public query budget unavailable")

type PublicPolicy struct {
	Enabled    bool     `json:"enabled"`
	Games      []string `json:"games"`
	DailyLimit int      `json:"daily_limit"`
	Day        string   `json:"day"`
	Used       int      `json:"used"`
	NextUseMS  int64    `json:"next_use_ms"`
}

func (r *Repository) PublicRefs(ctx context.Context, store SecretStore, cipher *vault.Cipher) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pool index
	_, err := readEncrypted(ctx, store, cipher, publicKey, &pool)
	return pool.Refs, err
}
func (r *Repository) SetPublic(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref string, owner Subject, enabled bool, limit int, games []string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.Get(ctx, store, cipher, ref)
	if err != nil {
		return record, err
	}
	if record.Owner != owner {
		return record, ErrNotFound
	}
	var pool index
	if _, err = readEncrypted(ctx, store, cipher, publicKey, &pool); err != nil {
		return record, err
	}
	policy := PublicPolicy{}
	if record.PublicQuery != nil {
		policy = *record.PublicQuery
	}
	policy.Enabled = enabled
	if enabled {
		if limit < 1 || limit > 1000 || len(games) == 0 || len(games) > 3 {
			return record, ErrPublicUnavailable
		}
		for _, game := range games {
			if !slices.ContainsFunc(record.Roles, func(role Role) bool { return role.Game == game }) {
				return record, ErrPublicUnavailable
			}
		}
		policy.DailyLimit = limit
		policy.Games = slices.Compact(slices.Sorted(slices.Values(games)))
		if !slices.Contains(pool.Refs, ref) {
			if len(pool.Refs) >= 128 {
				return record, ErrPublicUnavailable
			}
			pool.Refs = append(pool.Refs, ref)
		}
	} else {
		pool.Refs = slices.DeleteFunc(pool.Refs, func(item string) bool { return item == ref })
	}
	record.PublicQuery = &policy
	record.Revision++
	record.UpdatedAtMS = time.Now().UnixMilli()
	return record, writeEncrypted(ctx, store, cipher, map[string]any{"account." + ref: record, publicKey: pool})
}
func (r *Repository) ReservePublic(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref, caller, game string, now time.Time) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.Get(ctx, store, cipher, ref)
	if err != nil {
		return record, err
	}
	p := record.PublicQuery
	if p == nil || !p.Enabled || record.Status == "invalid" || !slices.Contains(p.Games, game) || !slices.Contains(record.AllowedCallers, caller) || !slices.ContainsFunc(record.Roles, func(role Role) bool { return role.Game == game }) {
		return record, ErrPublicUnavailable
	}
	day := now.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02")
	if p.Day != day {
		p.Day = day
		p.Used = 0
	}
	if p.Used >= p.DailyLimit || p.NextUseMS > now.UnixMilli() {
		return record, ErrPublicUnavailable
	}
	p.Used++
	p.NextUseMS = now.Add(5 * time.Second).UnixMilli()
	record.Revision++
	record.UpdatedAtMS = now.UnixMilli()
	if err = writeEncrypted(ctx, store, cipher, map[string]any{"account." + ref: record}); err != nil {
		return record, err
	}
	return record, nil
}
