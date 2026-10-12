package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
	"slices"
	"time"
)

const associationsKey = "associations.root"

var ErrAssociation = errors.New("account association is invalid")

type Association struct {
	Ref       string  `json:"ref"`
	Primary   Subject `json:"primary"`
	Secondary Subject `json:"secondary"`
	CreatedMS int64   `json:"created_ms"`
}

func (r *Repository) Associations(ctx context.Context, store SecretStore, cipher *vault.Cipher) ([]Association, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.associationsLocked(ctx, store, cipher)
}

func (r *Repository) associationsLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher) ([]Association, error) {
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return nil, err
	}
	items := []Association{}
	_, err := readEncrypted(ctx, store, cipher, associationsKey, &items)
	return items, err
}
func (r *Repository) CanAccess(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner, actor Subject) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canAccessLocked(ctx, store, cipher, owner, actor)
}

func (r *Repository) canAccessLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner, actor Subject) (bool, error) {
	if owner.Validate() != nil || actor.Validate() != nil {
		return false, nil
	}
	if owner.Equal(actor) {
		return true, nil
	}
	items, err := r.associationsLocked(ctx, store, cipher)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(items, func(a Association) bool { return a.Primary.Equal(owner) && a.Secondary.Equal(actor) }), nil
}
func (r *Repository) Associate(ctx context.Context, store SecretStore, cipher *vault.Cipher, primary, secondary Subject) (Association, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if primary.Validate() != nil || secondary.Validate() != nil || primary.Equal(secondary) {
		return Association{}, ErrAssociation
	}
	items, err := r.associationsLocked(ctx, store, cipher)
	if err != nil {
		return Association{}, err
	}
	if len(items) >= 128 {
		return Association{}, ErrAssociation
	}
	for _, a := range items {
		if a.Secondary.Equal(secondary) || a.Secondary.Equal(primary) || a.Primary.Equal(secondary) {
			return Association{}, ErrAssociation
		}
	}
	var owned index
	if err := r.migrateOwnerLocked(ctx, store, cipher, primary); err != nil {
		return Association{}, err
	}
	if err := r.migrateOwnerLocked(ctx, store, cipher, secondary); err != nil {
		return Association{}, err
	}
	if _, err = readEncrypted(ctx, store, cipher, primary.Key(), &owned); err != nil {
		return Association{}, err
	}
	has := false
	for _, ref := range owned.Refs {
		record, e := r.getLocked(ctx, store, cipher, ref)
		if e == nil && record.Owner.Equal(primary) {
			has = true
			break
		}
	}
	if !has {
		return Association{}, ErrNotFound
	}
	next := Association{Ref: rand.Text(), Primary: primary, Secondary: secondary, CreatedMS: time.Now().UnixMilli()}
	items = append(items, next)
	if err = writeEncrypted(ctx, store, cipher, map[string]any{associationsKey: items}); err != nil {
		return Association{}, err
	}
	return next, nil
}
func (r *Repository) Dissociate(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref string, actor *Subject) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	items, err := r.associationsLocked(ctx, store, cipher)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(items, func(a Association) bool { return a.Ref == ref })
	if i < 0 {
		return ErrNotFound
	}
	link := items[i]
	if actor != nil && !actor.Equal(link.Primary) && !actor.Equal(link.Secondary) {
		return ErrAssociation
	}
	defaults, err := r.defaultsLocked(ctx, store, cipher, link.Secondary)
	if err != nil {
		return err
	}
	for game, choice := range defaults {
		record, e := r.getLocked(ctx, store, cipher, choice.AccountRef)
		if e != nil || !record.Owner.Equal(link.Secondary) {
			delete(defaults, game)
		}
	}
	items = slices.Delete(items, i, i+1)
	return writeEncrypted(ctx, store, cipher, map[string]any{associationsKey: items, link.Secondary.Key() + ".defaults": defaults})
}
