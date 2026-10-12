package accounts

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/RayleaBot/mihoyo-accounts/internal/vault"
)

var ErrNotFound = errors.New("account is not available")
var ErrConflict = errors.New("account changed while the operation was running")

const catalogKey = "catalog.root"
const pageSize = 128
const maxSubjectAccounts = 32

type SecretStore interface {
	Read(context.Context, string) (string, bool, error)
	Write(context.Context, map[string]string) error
}

type Repository struct {
	mu       sync.Mutex
	migrated bool
}
type catalog struct {
	Pages  int `json:"pages"`
	Active int `json:"active"`
}
type index struct {
	Refs []string `json:"refs"`
}

type Page struct {
	Items    []Summary `json:"items"`
	NextPage *int      `json:"next_page,omitempty"`
	Total    int       `json:"total"`
}

func (r *Repository) HasRecords(ctx context.Context, store SecretStore) (bool, error) {
	_, exists, err := store.Read(ctx, catalogKey)
	return exists, err
}

func readEncrypted(ctx context.Context, store SecretStore, cipher *vault.Cipher, key string, out any) (bool, error) {
	sealed, exists, err := store.Read(ctx, key)
	if err != nil || !exists {
		return exists, err
	}
	plain, err := cipher.Open(key, sealed)
	if err != nil {
		return true, err
	}
	defer clear(plain)
	if json.Unmarshal(plain, out) != nil {
		return true, errors.New("encrypted account record has invalid content")
	}
	return true, nil
}

// ReadSetting and WriteSetting keep plugin-level secrets, such as the ark
// token, encrypted in the same way as account records.
func (r *Repository) ReadSetting(ctx context.Context, store SecretStore, cipher *vault.Cipher, key string, out any) (bool, error) {
	return readEncrypted(ctx, store, cipher, "setting."+key, out)
}

func (r *Repository) WriteSetting(ctx context.Context, store SecretStore, cipher *vault.Cipher, key string, value any) error {
	return writeEncrypted(ctx, store, cipher, map[string]any{"setting." + key: value})
}

func writeEncrypted(ctx context.Context, store SecretStore, cipher *vault.Cipher, records map[string]any) error {
	values := make(map[string]string, len(records))
	for key, record := range records {
		plain, err := json.Marshal(record)
		if err != nil {
			return errors.New("account record could not be encoded")
		}
		sealed, err := cipher.Seal(key, plain)
		clear(plain)
		if err != nil {
			return err
		}
		values[key] = sealed
	}
	return store.Write(ctx, values)
}

func (r *Repository) Get(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getLocked(ctx, store, cipher, ref)
}

func (r *Repository) getLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref string) (Record, error) {
	if err := r.migrateCatalogLocked(ctx, store, cipher); err != nil {
		return Record{}, err
	}
	if ref == "" || len(ref) > 64 || strings.ContainsAny(ref, "/\\.") {
		return Record{}, ErrNotFound
	}
	var record Record
	exists, err := readEncrypted(ctx, store, cipher, "account."+ref, &record)
	if err != nil {
		return Record{}, err
	}
	if !exists || record.Deleted || record.Ref != ref {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (r *Repository) List(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject) ([]Summary, error) {
	return r.list(ctx, store, cipher, owner, false)
}

// ListForBotSubject retains sharing within the selected bot, but checks the
// stored owner before List projects a shared summary to the requesting subject.
func (r *Repository) ListForBotSubject(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject) ([]Summary, error) {
	return r.list(ctx, store, cipher, owner, true)
}

func (r *Repository) list(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject, withinBot bool) ([]Summary, error) {
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.migrateOwnerLocked(ctx, store, cipher, owner); err != nil {
		return nil, err
	}
	var refs index
	if _, err := readEncrypted(ctx, store, cipher, owner.Key(), &refs); err != nil {
		return nil, err
	}
	links, err := r.associationsLocked(ctx, store, cipher)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if link.Secondary.Equal(owner) {
			var shared index
			if _, err = readEncrypted(ctx, store, cipher, link.Primary.Key(), &shared); err != nil {
				return nil, err
			}
			for _, ref := range shared.Refs {
				if !slices.Contains(refs.Refs, ref) {
					refs.Refs = append(refs.Refs, ref)
				}
			}
		}
	}
	items := make([]Summary, 0, len(refs.Refs))
	for _, ref := range refs.Refs {
		record, err := r.getLocked(ctx, store, cipher, ref)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if withinBot && (record.Owner.SourceProtocol != owner.SourceProtocol || record.Owner.BotID != owner.BotID) {
			continue
		}
		if record.Owner.Equal(owner) || slices.ContainsFunc(links, func(a Association) bool { return a.Primary.Equal(record.Owner) && a.Secondary.Equal(owner) }) {
			summary := record.Summary()
			summary.Shared = !record.Owner.Equal(owner)
			summary.Owner = owner
			items = append(items, summary)
		}
	}
	return items, nil
}

func (r *Repository) ListPage(ctx context.Context, store SecretStore, cipher *vault.Cipher, page int) (Page, error) {
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
	result.Total = all.Active
	if page >= all.Pages {
		return result, nil
	}
	var refs index
	if _, err := readEncrypted(ctx, store, cipher, pageKey(page), &refs); err != nil {
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
		result.Items = append(result.Items, record.Summary())
	}
	if page+1 < all.Pages {
		next := page + 1
		result.NextPage = &next
	}
	return result, nil
}

// Bind deduplicates an account within its owner and atomically writes its
// encrypted record, owner index and paginated management catalog.
func (r *Repository) Bind(ctx context.Context, store SecretStore, cipher *vault.Cipher, owner Subject, tokens Tokens, roles []Role, callers []string) (Record, error) {
	if err := owner.Validate(); err != nil {
		return Record{}, err
	}
	if !tokens.ValidBinding() {
		return Record{}, errors.New("login credentials are incomplete")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var owned index
	if err := r.migrateOwnerLocked(ctx, store, cipher, owner); err != nil {
		return Record{}, err
	}
	if _, err := readEncrypted(ctx, store, cipher, owner.Key(), &owned); err != nil {
		return Record{}, err
	}
	for _, ref := range owned.Refs {
		record, err := r.getLocked(ctx, store, cipher, ref)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return Record{}, err
		}
		if record.Owner.Equal(owner) && record.Tokens.AccountID == tokens.AccountID && record.Tokens.RealmName() == tokens.RealmName() {
			record.LastCheck = nil
			if tokens.CloudGenshin == "" {
				tokens.CloudGenshin = record.Tokens.CloudGenshin
			}
			if tokens.CloudZZZ == "" {
				tokens.CloudZZZ = record.Tokens.CloudZZZ
			}
			record.Tokens, record.Roles = tokens, append([]Role{}, roles...)
			record.Status, record.UpdatedAtMS = "valid", time.Now().UnixMilli()
			record.Revision++
			return record, writeEncrypted(ctx, store, cipher, map[string]any{"account." + ref: record})
		}
	}
	if len(owned.Refs) >= maxSubjectAccounts {
		return Record{}, errors.New("account limit reached for this subject")
	}
	var all catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &all); err != nil {
		return Record{}, err
	}
	page := max(all.Pages-1, 0)
	var refs index
	if all.Pages > 0 {
		if _, err := readEncrypted(ctx, store, cipher, pageKey(page), &refs); err != nil {
			return Record{}, err
		}
	}
	if len(refs.Refs) >= pageSize {
		page++
		refs.Refs = nil
	}
	if page >= all.Pages {
		all.Pages = page + 1
	}
	ref := strings.ToLower(rand.Text())
	record := Record{Ref: ref, Owner: owner, Revision: 1, Tokens: tokens, Roles: append([]Role{}, roles...), AllowedCallers: append([]string{}, callers...), Status: "valid", UpdatedAtMS: time.Now().UnixMilli(), CatalogPage: page}
	owned.Refs = append(owned.Refs, ref)
	refs.Refs = append(refs.Refs, ref)
	all.Active++
	err := writeEncrypted(ctx, store, cipher, map[string]any{catalogKey: all, pageKey(page): refs, owner.Key(): owned, owner.Key() + ".owner": owner, "account." + ref: record})
	return record, err
}

func (r *Repository) Update(ctx context.Context, store SecretStore, cipher *vault.Cipher, record Record, expected uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, err := r.getLocked(ctx, store, cipher, record.Ref)
	if err != nil {
		return err
	}
	if current.Revision != expected || !current.Owner.Equal(record.Owner) {
		return ErrConflict
	}
	record.Revision = current.Revision + 1
	record.UpdatedAtMS = time.Now().UnixMilli()
	record.CatalogPage = current.CatalogPage
	return writeEncrypted(ctx, store, cipher, map[string]any{"account." + record.Ref: record})
}

func (r *Repository) Remove(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getLocked(ctx, store, cipher, ref)
	if err != nil {
		return err
	}
	return r.removeLocked(ctx, store, cipher, record)
}

// Status and revision are checked under the same lock as Bind and the final
// deletion, so a new login can never be removed by an old cleanup selection.
func (r *Repository) RemoveIfInvalid(ctx context.Context, store SecretStore, cipher *vault.Cipher, ref string, revision uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getLocked(ctx, store, cipher, ref)
	if err != nil {
		return false, err
	}
	if record.Status != "invalid" || record.Revision != revision {
		return false, nil
	}
	err = r.removeLocked(ctx, store, cipher, record)
	return err == nil, err
}
func (r *Repository) removeLocked(ctx context.Context, store SecretStore, cipher *vault.Cipher, record Record) error {
	ref := record.Ref
	var owned, refs index
	var all catalog
	for key, target := range map[string]any{record.Owner.Key(): &owned, pageKey(record.CatalogPage): &refs, catalogKey: &all} {
		if _, err := readEncrypted(ctx, store, cipher, key, target); err != nil {
			return err
		}
	}
	owned.Refs = slices.DeleteFunc(owned.Refs, func(value string) bool { return value == ref })
	refs.Refs = slices.DeleteFunc(refs.Refs, func(value string) bool { return value == ref })
	record.Deleted = true
	record.Tokens = Tokens{}
	record.Roles = nil
	record.Revision++
	record.Status = "invalid"
	record.UpdatedAtMS = time.Now().UnixMilli()
	all.Active = max(all.Active-1, 0)
	values := map[string]any{"account." + ref: record, record.Owner.Key(): owned, pageKey(record.CatalogPage): refs, catalogKey: all}
	var pool index
	if exists, err := readEncrypted(ctx, store, cipher, publicKey, &pool); err != nil {
		return err
	} else if exists {
		pool.Refs = slices.DeleteFunc(pool.Refs, func(item string) bool { return item == ref })
		values[publicKey] = pool
	}
	return writeEncrypted(ctx, store, cipher, values)
}

func pageKey(page int) string { return fmt.Sprintf("catalog.page.%06d", page) }
