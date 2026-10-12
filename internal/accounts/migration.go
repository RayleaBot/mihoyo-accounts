package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

func (r *Repository) ResetMigration() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.migrated = false
}

func legacySubjectKey(s Subject) string {
	// Preserve the old field order and the non-omitempty adapter, including
	// the empty bot ID on older records.
	data, _ := json.Marshal(struct {
		SourceProtocol string `json:"source_protocol"`
		SourceAdapter  string `json:"source_adapter"`
		BotID          string `json:"bot_id"`
		ActorID        string `json:"actor_id"`
	}{s.SourceProtocol, s.SourceAdapter, s.BotID, s.ActorID})
	digest := sha256.Sum256(data)
	return "subject." + hex.EncodeToString(digest[:])
}

// migrateCatalogLocked runs on the first repository access after unlocking.
// Accounts and associations enumerate their subjects; standalone UID bindings
// have no directory and are recovered separately from an event's adapter hint.
// Missing bot identities are left intact for explicit global maintenance.
func (r *Repository) migrateCatalogLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher) error {
	if r.migrated {
		return nil
	}
	values := map[string]any{}
	commit := func() error {
		if len(values) == 0 {
			return nil
		}
		if err := writeEncrypted(ctx, store, cipher, values); err != nil {
			return err
		}
		clear(values)
		return nil
	}
	migrate := func(s *Subject) error {
		// Empty delegation owners mean the account owner.
		if s.ActorID == "" {
			return nil
		}
		if s.SourceAdapter == "" {
			values[s.Key()+".owner"] = *s
			return nil
		}
		old := *s
		if s.BotID == "" {
			// A connection may now log in another bot. Missing identities need
			// explicit OneBot assignment; QQ openids cannot be recovered this way.
			values[legacySubjectKey(*s)+".owner"] = *s
			return nil
		}
		if err := migrateSubject(ctx, store, cipher, old, *s, values); err != nil {
			return err
		}
		s.SourceAdapter = ""
		values[s.Key()+".owner"] = *s
		return nil
	}
	var all catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &all); err != nil {
		return err
	}
	for page := 0; page < all.Pages; page++ {
		var refs index
		if _, err := readEncrypted(ctx, store, cipher, pageKey(page), &refs); err != nil {
			return err
		}
		for _, ref := range refs.Refs {
			var record Record
			exists, err := readEncrypted(ctx, store, cipher, "account."+ref, &record)
			if err != nil {
				return err
			}
			if !exists || record.Deleted {
				continue
			}
			old := record.Owner
			if err := migrate(&record.Owner); err != nil {
				return err
			}
			changed := old != record.Owner
			identityChanged := !old.Equal(record.Owner)
			for i := range record.Delegations {
				old := record.Delegations[i].Owner
				if err := migrate(&record.Delegations[i].Owner); err != nil {
					return err
				}
				changed = changed || old != record.Delegations[i].Owner
				identityChanged = identityChanged || !old.Equal(record.Delegations[i].Owner)
			}
			// A repaired delegation owner must not be overwritten by a record
			// fetched before the bot identity became available.
			if identityChanged {
				record.Revision++
			}
			if changed {
				values["account."+ref] = record
			}
			// Commit each record with its subject data and markers, keeping the
			// host action bounded even for a large catalog. A later failure can
			// safely resume from the remaining legacy records.
			if err := commit(); err != nil {
				return err
			}
		}
	}
	var links []Association
	if _, err := readEncrypted(ctx, store, cipher, associationsKey, &links); err != nil {
		return err
	}
	changed := false
	for i := range links {
		old := links[i]
		if err := migrate(&links[i].Primary); err != nil {
			return err
		}
		if err := migrate(&links[i].Secondary); err != nil {
			return err
		}
		changed = changed || old != links[i]
	}
	// Associations share one encrypted root: migrate all its subjects before
	// serializing it, so no unprocessed adapter hint is dropped on a retry.
	if changed {
		values[associationsKey] = links
	}
	if err := commit(); err != nil {
		return err
	}
	r.migrated = true
	return nil
}

// migrateOwnerLocked recovers adapter-based hashes with a complete bot identity.
// A marker prevents removed UIDs from reappearing.
func (r *Repository) migrateOwnerLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject) error {
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return err
	}
	if owner.SourceAdapter == "" || owner.Validate() != nil {
		return nil
	}
	values := map[string]any{}
	if err := migrateSubject(ctx, store, cipher, owner, owner, values); err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return writeEncrypted(ctx, store, cipher, values)
}

func migrationValue[T any](ctx context.Context, store SecretStore, cipher *vault.Cipher, values map[string]any, key string, out *T) (bool, error) {
	if value, ok := values[key]; ok {
		*out = value.(T)
		return true, nil
	}
	return readEncrypted(ctx, store, cipher, key, out)
}

// All destination values and markers are staged for one atomic SecretWrite
// while the repository lock is held. The original ciphertext stays as a
// recovery copy and is never consulted again after its marker is committed.
func migrateSubject(ctx context.Context, store SecretStore, cipher *vault.Cipher, old, owner Subject, values map[string]any) error {
	from, to := legacySubjectKey(old), owner.Key()
	var done bool
	if _, err := migrationValue(ctx, store, cipher, values, from+".migrated", &done); err != nil || done {
		return err
	}
	found := false
	var refs index
	if exists, err := readEncrypted(ctx, store, cipher, from, &refs); err != nil {
		return err
	} else if exists {
		var current index
		if _, err := migrationValue(ctx, store, cipher, values, to, &current); err != nil {
			return err
		}
		for _, ref := range refs.Refs {
			if !slices.Contains(current.Refs, ref) {
				current.Refs = append(current.Refs, ref)
			}
		}
		values[to], found = current, true
	}
	var defaults map[string]Selection
	if exists, err := readEncrypted(ctx, store, cipher, from+".defaults", &defaults); err != nil {
		return err
	} else if exists {
		current := map[string]Selection{}
		if _, err := migrationValue(ctx, store, cipher, values, to+".defaults", &current); err != nil {
			return err
		}
		for game, selection := range defaults {
			if _, ok := current[game]; !ok {
				current[game] = selection
			}
		}
		values[to+".defaults"], found = current, true
	}
	var bindings map[string]UIDBinding
	if exists, err := readEncrypted(ctx, store, cipher, from+".uids", &bindings); err != nil {
		return err
	} else if exists {
		current := map[string]UIDBinding{}
		if _, err := migrationValue(ctx, store, cipher, values, to+".uids", &current); err != nil {
			return err
		}
		for game, binding := range bindings {
			merged := current[game]
			for _, uid := range binding.UIDs {
				if !slices.Contains(merged.UIDs, uid) {
					merged.UIDs = append(merged.UIDs, uid)
				}
			}
			if merged.Current == "" {
				merged.Current = binding.Current
			}
			current[game] = merged
		}
		values[to+".uids"], found = current, true
	}
	if found {
		values[from+".migrated"] = true
		values[to+".owner"] = owner
	}
	return nil
}
