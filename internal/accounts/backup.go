package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/RayleaBot/game-plugin-kit/vault"
)

var ErrBackupInvalid = errors.New("encrypted account backup is invalid")
var ErrRestoreNonempty = errors.New("account restore requires an empty target")

const maxBackupAccounts = 4096

type Snapshot struct {
	Version  int                             `json:"version"`
	Accounts []Record                        `json:"accounts"`
	Defaults map[string]map[string]Selection `json:"defaults"`
}

func (r *Repository) Snapshot(ctx context.Context, store SecretStore, cipher *vault.Cipher) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Snapshot{Version: 1, Accounts: []Record{}, Defaults: map[string]map[string]Selection{}}
	var all catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &all); err != nil {
		return out, err
	}
	if all.Active < 0 || all.Active > maxBackupAccounts || all.Pages < 0 || all.Pages > 32768 {
		return out, ErrBackupInvalid
	}
	seen := map[string]bool{}
	for page := 0; page < all.Pages; page++ {
		var refs index
		if _, err := readEncrypted(ctx, store, cipher, pageKey(page), &refs); err != nil {
			return out, err
		}
		for _, ref := range refs.Refs {
			if seen[ref] {
				return out, ErrBackupInvalid
			}
			seen[ref] = true
			record, err := r.Get(ctx, store, cipher, ref)
			if err != nil {
				return out, err
			}
			out.Accounts = append(out.Accounts, record)
			key := record.Owner.Key()
			if _, ok := out.Defaults[key]; !ok {
				values, err := r.Defaults(ctx, store, cipher, record.Owner)
				if err != nil {
					return out, err
				}
				out.Defaults[key] = values
			}
		}
	}
	if len(out.Accounts) != all.Active {
		return out, ErrBackupInvalid
	}
	active := map[string]Record{}
	for _, record := range out.Accounts {
		active[record.Ref] = record
	}
	for owner, defaults := range out.Defaults {
		for game, selection := range defaults {
			record, ok := active[selection.AccountRef]
			valid := ok && record.Owner.Key() == owner
			if valid {
				valid = false
				for _, role := range record.Roles {
					if role.Game == game && role.Ref == selection.RoleRef {
						valid = true
					}
				}
			}
			if !valid {
				delete(defaults, game)
			}
		}
	}
	// Every exported snapshot must fit the same atomic restore operation.
	if _, err := snapshotValues(cipher, out); err != nil {
		return out, err
	}
	return out, nil
}
func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Version != 1 || len(snapshot.Accounts) > maxBackupAccounts || len(snapshot.Defaults) > maxBackupAccounts {
		return ErrBackupInvalid
	}
	refs := map[string]Record{}
	owners := map[string]int{}
	identities := map[string]bool{}
	for _, record := range snapshot.Accounts {
		for _, token := range []string{record.Tokens.CloudGenshin, record.Tokens.CloudZZZ} {
			if len(token) > 8192 {
				return ErrBackupInvalid
			}
			for _, ch := range []byte(token) {
				if ch < 32 || ch > 126 {
					return ErrBackupInvalid
				}
			}
		}
		if !record.LastCheck.Valid() {
			return ErrBackupInvalid
		}
		if record.Ref == "" || len(record.Ref) > 64 || strings.ContainsAny(record.Ref, "/\\.") || record.Deleted || record.Owner.Validate() != nil || !record.Tokens.ValidBinding() || record.Revision == 0 {
			return ErrBackupInvalid
		}
		if _, exists := refs[record.Ref]; exists {
			return ErrBackupInvalid
		}
		refs[record.Ref] = record
		key := record.Owner.Key()
		owners[key]++
		if owners[key] > maxSubjectAccounts {
			return ErrBackupInvalid
		}
		identity := key + "/" + record.Tokens.RealmName() + "/" + record.Tokens.AccountID
		if identities[identity] {
			return ErrBackupInvalid
		}
		identities[identity] = true
		raw, _ := json.Marshal(record)
		if len(raw) > 64*1024 || len(record.Roles) > 256 || len(record.AllowedCallers) > 128 {
			return ErrBackupInvalid
		}
		if !slices.Contains([]string{"valid", "unknown", "invalid"}, record.Status) {
			return ErrBackupInvalid
		}
		roleRefs := map[string]bool{}
		for _, role := range record.Roles {
			if role.Ref == "" || len(role.Ref) > 256 || roleRefs[role.Ref] || !slices.Contains([]string{"genshin", "starrail", "zzz"}, role.Game) || role.UID == "" || role.Region == "" {
				return ErrBackupInvalid
			}
			roleRefs[role.Ref] = true
		}
		for _, caller := range record.AllowedCallers {
			if caller == "" || len(caller) > 128 {
				return ErrBackupInvalid
			}
		}
	}
	for owner, defaults := range snapshot.Defaults {
		if owners[owner] == 0 || len(defaults) > 3 {
			return ErrBackupInvalid
		}
		for game, selected := range defaults {
			record, ok := refs[selected.AccountRef]
			if !ok || record.Owner.Key() != owner {
				return ErrBackupInvalid
			}
			found := false
			for _, role := range record.Roles {
				if role.Game == game && role.Ref == selected.RoleRef {
					found = true
				}
			}
			if !found {
				return ErrBackupInvalid
			}
		}
	}
	return nil
}

func (r *Repository) Restore(ctx context.Context, store SecretStore, cipher *vault.Cipher, snapshot Snapshot) error {
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var current catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &current); err != nil {
		return err
	}
	if current.Active != 0 {
		return ErrRestoreNonempty
	}
	values, err := snapshotValues(cipher, snapshot)
	if err != nil {
		return err
	}
	return store.Write(ctx, values)
}

func snapshotValues(cipher *vault.Cipher, snapshot Snapshot) (map[string]string, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	pages := (len(snapshot.Accounts) + pageSize - 1) / pageSize
	records := map[string]any{catalogKey: catalog{Pages: pages, Active: len(snapshot.Accounts)}, publicKey: index{Refs: []string{}}, associationsKey: []Association{}}
	owners := map[string]index{}
	buckets := make([]index, pages)
	for i, record := range snapshot.Accounts {
		record.Delegations = nil
		record.PublicQuery = nil
		record.CatalogPage = i / pageSize
		record.Revision++
		records["account."+record.Ref] = record
		key := record.Owner.Key()
		owned := owners[key]
		owned.Refs = append(owned.Refs, record.Ref)
		owners[key] = owned
		buckets[i/pageSize].Refs = append(buckets[i/pageSize].Refs, record.Ref)
	}
	for key, owned := range owners {
		records[key] = owned
		defaults := snapshot.Defaults[key]
		if defaults == nil {
			defaults = map[string]Selection{}
		}
		records[key+".defaults"] = defaults
	}
	for page, bucket := range buckets {
		records[pageKey(page)] = bucket
	}
	// The host applies one secret.write batch atomically. Check the complete
	// ciphertext payload before submitting so size failures leave old state intact.
	values := map[string]string{}
	for key, value := range records {
		plain, err := json.Marshal(value)
		if err != nil {
			return nil, ErrBackupInvalid
		}
		sealed, err := cipher.Seal(key, plain)
		clear(plain)
		if err != nil {
			return nil, err
		}
		values[key] = sealed
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > 6*1024*1024 {
		return nil, ErrBackupInvalid
	}
	return values, nil
}

func (r *Repository) ReencryptPage(ctx context.Context, store SecretStore, cipher *vault.Cipher, page int) (*int, error) {
	if page < 0 {
		return nil, ErrBackupInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var all catalog
	if _, err := readEncrypted(ctx, store, cipher, catalogKey, &all); err != nil {
		return nil, err
	}
	if all.Pages < 0 || all.Pages > 32768 {
		return nil, ErrBackupInvalid
	}
	keys := []string{catalogKey}
	if page == 0 {
		keys = append(keys, publicKey)
		keys = append(keys, associationsKey)
		links, err := r.Associations(ctx, store, cipher)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			keys = append(keys, link.Secondary.Key()+".defaults")
		}
	}
	if page < all.Pages {
		keys = append(keys, pageKey(page))
		var refs index
		if _, err := readEncrypted(ctx, store, cipher, pageKey(page), &refs); err != nil {
			return nil, err
		}
		for _, ref := range refs.Refs {
			record, err := r.Get(ctx, store, cipher, ref)
			if err != nil {
				return nil, err
			}
			keys = append(keys, "account."+ref, record.Owner.Key(), record.Owner.Key()+".defaults")
		}
	}
	values := map[string]string{}
	for _, key := range keys {
		sealed, exists, err := store.Read(ctx, key)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		plain, err := cipher.Open(key, sealed)
		if err != nil {
			return nil, err
		}
		next, err := cipher.Seal(key, plain)
		clear(plain)
		if err != nil {
			return nil, err
		}
		values[key] = next
	}
	if err := store.Write(ctx, values); err != nil {
		return nil, fmt.Errorf("re-encryption write failed: %w", err)
	}
	if page+1 < all.Pages {
		next := page + 1
		return &next, nil
	}
	return nil, nil
}
