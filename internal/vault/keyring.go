package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// keyRing is the version 2 key file payload. Version 0 is a single bare Key,
// written by CreateKeyFile. Earlier releases could add key generations, so a
// ring is still read in full: records sealed with any listed key stay readable.
type keyRing struct {
	Version int   `json:"version"`
	Keys    []Key `json:"keys"`
}

func LoadKeyRing(directory, password string) ([]Key, error) {
	plain, err := loadKeyPayload(directory, password)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	var ring keyRing
	if json.Unmarshal(plain, &ring) != nil {
		return nil, ErrLocked
	}
	if ring.Version == 0 {
		var key Key
		if json.Unmarshal(plain, &key) != nil {
			return nil, ErrLocked
		}
		ring.Keys = []Key{key}
	} else if ring.Version != 2 {
		return nil, ErrLocked
	}
	if _, err = NewRingCipher(ring.Keys); err != nil {
		clearKeys(ring.Keys)
		return nil, ErrLocked
	}
	return ring.Keys, nil
}

// rewrapKeyFile protects the same keys under another mode and replaces the key
// file atomically. The keys themselves do not change, so no record needs to be
// re-encrypted, and a failed write leaves the previous file in place.
func rewrapKeyFile(directory, password, mode string, keys []Key) error {
	plain, err := json.Marshal(keyRing{Version: 2, Keys: keys})
	if err != nil {
		return ErrLocked
	}
	defer clear(plain)
	file := keyFile{Version: 1, Mode: mode}
	var wrapped []byte
	switch mode {
	case "dpapi":
		wrapped, err = protectNative(plain)
	case "password":
		if err = validPassword(password); err != nil {
			return err
		}
		salt := make([]byte, 32)
		if _, err = rand.Read(salt); err != nil {
			return err
		}
		file.Salt = base64.StdEncoding.EncodeToString(salt)
		file.Iterations = passwordIterations
		aead, cipherErr := passwordCipher(password, salt, passwordIterations)
		err = cipherErr
		if err == nil {
			wrapped = aead.Seal(nil, nil, plain, []byte(namespace+"/key/password-v1"))
		}
	default:
		return ErrLocked
	}
	if err != nil {
		return ErrLocked
	}
	file.Wrapped = base64.StdEncoding.EncodeToString(wrapped)
	raw, err := json.Marshal(file)
	if err != nil || len(raw) > 64*1024 {
		return ErrLocked
	}
	temporary, err := os.CreateTemp(directory, ".key-rewrap-")
	if err != nil {
		return errors.New("could not stage the key file")
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(raw)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil || closeErr != nil {
		return errors.New("could not write the key file")
	}
	if os.Rename(name, filepath.Join(directory, keyFileName)) != nil {
		return errors.New("could not replace the key file")
	}
	return nil
}

func clearKeys(keys []Key) {
	for _, key := range keys {
		clear(key.Value)
	}
}
