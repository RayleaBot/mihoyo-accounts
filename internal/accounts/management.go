package accounts

import (
	"context"
	"errors"

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
			record, err := r.Get(ctx, store, cipher, ref)
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
