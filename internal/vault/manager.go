package vault

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/big"
	"sync"
)

type Manager struct {
	mu        sync.RWMutex
	directory string
	current   *Cipher
	unlockErr error
	transport *rsa.PrivateKey
}

func NewManager(directory string) (*Manager, error) {
	if directory == "" {
		return nil, errors.New("plugin data directory is required")
	}
	transport, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errors.New("could not initialize key input encryption")
	}
	m := &Manager{directory: directory, transport: transport}
	// A DPAPI key unlocks itself at start; a passphrase key waits for the admin.
	keys, err := LoadKeyRing(directory, "")
	if err == nil {
		m.current, err = NewRingCipher(keys)
		clearKeys(keys)
	}
	m.unlockErr = err
	return m, nil
}

func (m *Manager) State() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current != nil {
		return "unlocked"
	}
	if KeyFileExists(m.directory) {
		return "locked"
	}
	return "uninitialized"
}

func (m *Manager) Cipher() (*Cipher, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		if errors.Is(m.unlockErr, ErrNativeKeyUnavailable) {
			return nil, ErrNativeKeyUnavailable
		}
		return nil, ErrLocked
	}
	return m.current, nil
}
func (m *Manager) Lock() { m.mu.Lock(); m.current = nil; m.mu.Unlock() }

func (m *Manager) Mode() string { return KeyFileMode(m.directory) }

func (m *Manager) Initialize(password string, hasEncryptedRecords bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		return nil
	}
	if !KeyFileExists(m.directory) && hasEncryptedRecords {
		return ErrMissingKey
	}
	var keys []Key
	var err error
	if KeyFileExists(m.directory) {
		keys, err = LoadKeyRing(m.directory, password)
	} else {
		var key Key
		key, err = CreateKeyFile(m.directory, password)
		keys = []Key{key}
	}
	if err != nil {
		m.unlockErr = err
		return err
	}
	defer clearKeys(keys)
	m.current, err = NewRingCipher(keys)
	m.unlockErr = err
	return err
}

func (m *Manager) Unlock(password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys, err := LoadKeyRing(m.directory, password)
	if err != nil {
		if m.current == nil {
			m.unlockErr = err
		}
		return err
	}
	defer clearKeys(keys)
	m.current, err = NewRingCipher(keys)
	m.unlockErr = err
	return err
}

// SwitchMode protects the unlocked key under target mode: "dpapi" for the
// current Windows account, or "password" with newPassword. Switching from a
// passphrase requires it again, which also makes password-to-password a
// passphrase change. The keys are unchanged, so stored records stay as they are.
func (m *Manager) SwitchMode(target, currentPassword, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return ErrLocked
	}
	switch target {
	case "dpapi":
		if !NativeAvailable() {
			return ErrNativeUnavailable
		}
		if KeyFileMode(m.directory) == "dpapi" {
			return nil
		}
	case "password":
		if err := validPassword(newPassword); err != nil {
			return err
		}
	default:
		return ErrLocked
	}
	if KeyFileMode(m.directory) != "password" {
		currentPassword = ""
	}
	keys, err := LoadKeyRing(m.directory, currentPassword)
	if err != nil {
		return err
	}
	defer clearKeys(keys)
	if keys[0].ID != m.current.id {
		return ErrLocked
	}
	return rewrapKeyFile(m.directory, newPassword, target, keys)
}

// The UI encrypts passphrase input with this per-process public key before it
// enters management HTTP or JSONL. It is not a plugin authentication mechanism.
func (m *Manager) PublicKey() map[string]any {
	return map[string]any{"kty": "RSA", "alg": "RSA-OAEP-256", "ext": true, "key_ops": []string{"encrypt"}, "n": base64.RawURLEncoding.EncodeToString(m.transport.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(m.transport.E)).Bytes())}
}

func (m *Manager) DecodePassword(value string) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(sealed) != m.transport.Size() {
		return "", ErrLocked
	}
	plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, m.transport, sealed, []byte(namespace+"/key-input-v1"))
	if err != nil {
		return "", ErrLocked
	}
	defer clear(plain)
	if len(plain) > 128 {
		return "", ErrLocked
	}
	return string(plain), nil
}
