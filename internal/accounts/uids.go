package accounts

import (
	"context"
	"errors"
	"slices"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

// UIDBinding is a game's UIDs a user bound without an account, as upstream's
// 绑定uid keeps them, and the UID in use, which may also be one of the user's
// account roles.
type UIDBinding struct {
	UIDs    []string `json:"uids"`
	Current string   `json:"current,omitempty"`
}

// maxBoundUIDs keeps a user's bound UIDs per game to a list upstream's
// numbered 删除uid can reach.
const maxBoundUIDs = 20

var ErrBindingLimit = errors.New("too many bound UIDs")

func (r *Repository) UIDBindings(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject) (map[string]UIDBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uidBindingsLocked(ctx, store, cipher, owner)
}

func (r *Repository) uidBindingsLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject) (map[string]UIDBinding, error) {
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	values := map[string]UIDBinding{}
	if err := r.migrateOwnerLocked(ctx, store, cipher, owner); err != nil {
		return nil, err
	}
	_, err := readEncrypted(ctx, store, cipher, owner.Key()+".uids", &values)
	return values, err
}

// ChangeUIDBinding changes a game's bound UIDs: bind adds a UID and uses it,
// remove drops one, and use picks the UID in use.
func (r *Repository) ChangeUIDBinding(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject, game, action, uid string) (UIDBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values, err := r.uidBindingsLocked(ctx, store, cipher, owner)
	if err != nil {
		return UIDBinding{}, err
	}
	binding := values[game]
	switch action {
	case "bind":
		if !slices.Contains(binding.UIDs, uid) {
			if len(binding.UIDs) >= maxBoundUIDs {
				return binding, ErrBindingLimit
			}
			binding.UIDs = append(binding.UIDs, uid)
		}
		binding.Current = uid
	case "remove":
		binding.UIDs = slices.DeleteFunc(binding.UIDs, func(value string) bool { return value == uid })
		if binding.Current == uid {
			binding.Current = ""
		}
	case "use":
		binding.Current = uid
	}
	values[game] = binding
	return binding, writeEncrypted(ctx, store, cipher, map[string]any{owner.Key() + ".uids": values, owner.Key() + ".owner": owner})
}
