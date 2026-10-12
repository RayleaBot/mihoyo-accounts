package accounts

import (
	"context"
	"regexp"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

var subjectDataKey = regexp.MustCompile(`^subject\.[a-f0-9]{64}$`)

type SubjectData struct {
	Key      string                `json:"key"`
	Owner    *Subject              `json:"owner,omitempty"`
	Defaults map[string]Selection  `json:"default_roles"`
	Bindings map[string]UIDBinding `json:"uid_bindings"`
}

// The host's secret-status endpoint enumerates keys, never values. Old UID
// records contain only an irreversible subject hash; expose those for cleanup
// without guessing an identity. New writes retain encrypted owner metadata.
func (r *Repository) SubjectData(ctx context.Context, store SecretStore, cipher *vault.Cipher, keys []string) ([]SubjectData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(keys) > pageSize {
		return nil, ErrNotFound
	}
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return nil, err
	}
	items := []SubjectData{}
	for _, key := range keys {
		if !subjectDataKey.MatchString(key) {
			return nil, ErrNotFound
		}
		var migrated bool
		if _, err := readEncrypted(ctx, store, cipher, key+".migrated", &migrated); err != nil {
			return nil, err
		}
		if migrated {
			continue
		}
		item := SubjectData{Key: key, Defaults: map[string]Selection{}, Bindings: map[string]UIDBinding{}}
		if _, err := readEncrypted(ctx, store, cipher, key+".owner", &item.Owner); err != nil {
			return nil, err
		}
		if _, err := readEncrypted(ctx, store, cipher, key+".defaults", &item.Defaults); err != nil {
			return nil, err
		}
		if _, err := readEncrypted(ctx, store, cipher, key+".uids", &item.Bindings); err != nil {
			return nil, err
		}
		if len(item.Defaults) > 0 || len(item.Bindings) > 0 {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *Repository) RemoveSubjectData(ctx context.Context, store SecretStore, cipher *vault.Cipher, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !subjectDataKey.MatchString(key) {
		return ErrNotFound
	}
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return err
	}
	return writeEncrypted(ctx, store, cipher, map[string]any{key + ".defaults": map[string]Selection{}, key + ".uids": map[string]UIDBinding{}})
}
