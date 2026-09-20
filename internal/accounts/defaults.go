package accounts

import (
	"context"

	"github.com/RayleaBot/game-plugin-kit/vault"
)

type Selection struct {
	AccountRef string `json:"account_ref"`
	RoleRef    string `json:"role_ref"`
}

func (r *Repository) Defaults(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject) (map[string]Selection, error) {
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	values := map[string]Selection{}
	_, err := readEncrypted(ctx, store, cipher, owner.Key()+".defaults", &values)
	return values, err
}

func (r *Repository) Select(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject, ref, roleRef string) (Selection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.Get(ctx, store, cipher, ref)
	if err != nil {
		return Selection{}, err
	}
	allowed, err := r.CanAccess(ctx, store, cipher, record.Owner, owner)
	if err != nil {
		return Selection{}, err
	}
	if !allowed {
		return Selection{}, ErrNotFound
	}
	for _, role := range record.Roles {
		if role.Ref != roleRef {
			continue
		}
		values, err := r.Defaults(ctx, store, cipher, owner)
		if err != nil {
			return Selection{}, err
		}
		selection := Selection{AccountRef: ref, RoleRef: roleRef}
		values[role.Game] = selection
		return selection, writeEncrypted(ctx, store, cipher, map[string]any{owner.Key() + ".defaults": values})
	}
	return Selection{}, ErrNotFound
}
