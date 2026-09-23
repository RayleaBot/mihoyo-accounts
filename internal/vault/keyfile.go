package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrLocked = errors.New("credential key is locked")
var ErrMissingKey = errors.New("credential key file is missing")
var ErrPassword = errors.New("key passphrase must contain 12 to 128 bytes")
var ErrNativeUnavailable = errors.New("operating system key protection is not available")

const keyFileName = "master-key.json"
const passwordIterations = 600000

type keyFile struct {
	Version    int    `json:"version"`
	Mode       string `json:"mode"`
	Wrapped    string `json:"wrapped"`
	Salt       string `json:"salt,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
}

func KeyFileExists(directory string) bool {
	_, err := os.Stat(filepath.Join(directory, keyFileName))
	return err == nil
}

// CreateKeyFile never replaces an existing key, including unreadable or corrupt
// files. Callers check whether encrypted database records already exist first.
func CreateKeyFile(directory, password string) (Key, error) {
	key := NewKey()
	plain, err := json.Marshal(key)
	if err != nil {
		return Key{}, err
	}
	defer clear(plain)
	file := keyFile{Version: 1}
	var wrapped []byte
	if password == "" {
		wrapped, err = protectNative(plain)
		file.Mode = "dpapi"
	} else {
		if err = validPassword(password); err != nil {
			return Key{}, err
		}
		salt := make([]byte, 32)
		_, _ = rand.Read(salt)
		file.Mode, file.Salt, file.Iterations = "password", base64.StdEncoding.EncodeToString(salt), passwordIterations
		var aead cipher.AEAD
		aead, err = passwordCipher(password, salt, passwordIterations)
		if err == nil {
			wrapped = aead.Seal(nil, nil, plain, []byte(namespace+"/key/password-v1"))
		}
	}
	if err != nil {
		return Key{}, err
	}
	file.Wrapped = base64.StdEncoding.EncodeToString(wrapped)
	data, err := json.Marshal(file)
	if err != nil {
		return Key{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Key{}, errors.New("could not create key directory")
	}
	handle, err := os.OpenFile(filepath.Join(directory, keyFileName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Key{}, errors.New("could not create key file; existing key was preserved")
	}
	_, writeErr := handle.Write(data)
	if writeErr == nil {
		writeErr = handle.Sync()
	}
	closeErr := handle.Close()
	if writeErr != nil || closeErr != nil {
		return Key{}, errors.New("key file write failed; do not initialize encrypted records")
	}
	return key, nil
}

func loadKeyPayload(directory, password string) ([]byte, error) {
	data, err := readKeyFile(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissingKey
	}
	if err != nil || len(data) > 64*1024 {
		return nil, ErrLocked
	}
	var file keyFile
	if json.Unmarshal(data, &file) != nil || file.Version != 1 {
		return nil, ErrLocked
	}
	wrapped, err := base64.StdEncoding.DecodeString(file.Wrapped)
	if err != nil {
		return nil, ErrLocked
	}
	var plain []byte
	switch file.Mode {
	case "dpapi":
		plain, err = unprotectNative(wrapped)
	case "password":
		if validPassword(password) != nil || file.Iterations != passwordIterations {
			return nil, ErrLocked
		}
		salt, decodeErr := base64.StdEncoding.DecodeString(file.Salt)
		if decodeErr != nil || len(salt) != 32 {
			return nil, ErrLocked
		}
		var aead cipher.AEAD
		aead, err = passwordCipher(password, salt, file.Iterations)
		if err == nil {
			plain, err = aead.Open(nil, nil, wrapped, []byte(namespace+"/key/password-v1"))
		}
	default:
		return nil, ErrLocked
	}
	if err != nil {
		return nil, ErrLocked
	}
	return plain, nil
}

func KeyFileMode(directory string) string {
	data, err := readKeyFile(directory)
	if err != nil {
		return "unknown"
	}
	var file keyFile
	if json.Unmarshal(data, &file) != nil || file.Version != 1 || (file.Mode != "dpapi" && file.Mode != "password") {
		return "unknown"
	}
	return file.Mode
}

func readKeyFile(directory string) ([]byte, error) {
	file, err := os.Open(filepath.Join(directory, keyFileName))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return nil, ErrLocked
	}
	return data, nil
}

// validPassword applies one rule to creating, unlocking and switching, so a
// passphrase accepted once is accepted everywhere.
func validPassword(password string) error {
	if len(password) < 12 || len(password) > 128 {
		return ErrPassword
	}
	return nil
}

func passwordCipher(password string, salt []byte, iterations int) (cipher.AEAD, error) {
	derived, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return nil, err
	}
	defer clear(derived)
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}
