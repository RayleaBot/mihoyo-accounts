package accounts

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

// ListOwnedPage filters actual ownership before pagination. Unlike chat List,
// associations never project a different bot's account into this result.
func (r *Repository) ListOwnedPage(ctx context.Context, store SecretStore, cipher *vault.Cipher, filter Subject, page int) (Page, error) {
	if page < 0 {
		return Page{}, errors.New("invalid account page")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := Page{Items: []Summary{}}
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return result, err
	}
	var all catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &all); err != nil {
		return result, err
	}
	for p := 0; p < all.Pages; p++ {
		var refs index
		if _, err := readEncrypted(ctx, store, cipher, pageKey(p), &refs); err != nil {
			return result, err
		}
		for _, ref := range refs.Refs {
			record, err := r.getLocked(ctx, store, cipher, ref)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return result, err
			}
			owner := record.Owner
			if owner.SourceProtocol != filter.SourceProtocol || owner.BotID != filter.BotID || filter.ActorID != "" && owner.ActorID != filter.ActorID {
				continue
			}
			if result.Total/pageSize == page {
				result.Items = append(result.Items, record.Summary())
			}
			result.Total++
		}
	}
	if page < (result.Total-1)/pageSize {
		next := page + 1
		result.NextPage = &next
	}
	return result, nil
}

// AssignMissingBot only repairs an unresolved OneBot identity. All records of
// that subject and its bindings move together under the repository lock.
// Complete bot identities and QQ openids cannot be reassigned.
func (r *Repository) AssignMissingBot(ctx context.Context, store SecretStore, cipher *vault.Cipher, old Subject, botID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old.SourceProtocol != "onebot11" || old.BotID != "" || old.ActorID == "" || botID == "" {
		return ErrAssociation
	}
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return err
	}
	owner := Subject{SourceProtocol: "onebot11", BotID: botID, ActorID: old.ActorID}
	values := map[string]any{}
	var all catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &all); err != nil {
		return err
	}
	found := false
	var owned index
	if _, err := readEncrypted(ctx, store, cipher, owner.Key(), &owned); err != nil {
		return err
	}
	for page := 0; page < all.Pages; page++ {
		var refs index
		if _, err := readEncrypted(ctx, store, cipher, pageKey(page), &refs); err != nil {
			return err
		}
		for _, ref := range refs.Refs {
			record, err := r.getLocked(ctx, store, cipher, ref)
			if err != nil {
				return err
			}
			changed := false
			if record.Owner.Equal(old) && record.Owner.SourceAdapter == old.SourceAdapter {
				record.Owner = owner
				if !slices.Contains(owned.Refs, ref) {
					owned.Refs = append(owned.Refs, ref)
				}
				changed, found = true, true
			}
			for i := range record.Delegations {
				if record.Delegations[i].Owner == old {
					record.Delegations[i].Owner = owner
					changed, found = true, true
				}
			}
			if changed {
				record.Revision++
				record.UpdatedAtMS = time.Now().UnixMilli()
				values["account."+ref] = record
			}
		}
	}
	links, err := r.associationsLocked(ctx, store, cipher)
	if err != nil {
		return err
	}
	for i := range links {
		if links[i].Primary == old {
			links[i].Primary = owner
			found = true
		}
		if links[i].Secondary == old {
			links[i].Secondary = owner
			found = true
		}
	}
	for i, link := range links {
		if link.Primary.Equal(link.Secondary) {
			return ErrAssociation
		}
		for _, other := range links[i+1:] {
			if link.Secondary.Equal(other.Secondary) || link.Secondary.Equal(other.Primary) || link.Primary.Equal(other.Secondary) {
				return ErrAssociation
			}
		}
	}
	var storedOwner Subject
	var migrated bool
	if _, err := readEncrypted(ctx, store, cipher, legacySubjectKey(old)+".migrated", &migrated); err != nil {
		return err
	}
	if exists, err := readEncrypted(ctx, store, cipher, legacySubjectKey(old)+".owner", &storedOwner); err != nil {
		return err
	} else if exists && storedOwner == old && !migrated {
		found = true
	}
	if !found {
		return ErrNotFound
	}
	if len(owned.Refs) > maxSubjectAccounts {
		return ErrConflict
	}
	values[owner.Key()] = owned
	if err := migrateSubject(ctx, store, cipher, old, owner, values); err != nil {
		return err
	}
	values[legacySubjectKey(old)+".migrated"] = true
	values[associationsKey] = links
	values[old.Key()] = index{}
	values[owner.Key()+".owner"] = owner
	return writeEncrypted(ctx, store, cipher, values)
}
