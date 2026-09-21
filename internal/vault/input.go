package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
)

// InputBox keeps arbitrary short credential input out of management HTTP and
// JSONL. It authenticates the purpose, but is not a plugin identity boundary.
type InputBox struct {
	Key     string `json:"key"`
	Nonce   string `json:"nonce"`
	Payload string `json:"payload"`
}

func (m *Manager) DecodeInput(box InputBox, purpose string) ([]byte, error) {
	if len(purpose) == 0 || len(purpose) > 128 || len(box.Payload) > 16384 || len(box.Key) > 512 || len(box.Nonce) > 32 {
		return nil, ErrCiphertext
	}
	keyBytes, err := base64.StdEncoding.DecodeString(box.Key)
	if err != nil || len(keyBytes) != m.transport.Size() {
		return nil, ErrCiphertext
	}
	label := []byte("raylea/credential-input/v1/" + purpose)
	key, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, m.transport, keyBytes, label)
	if err != nil || len(key) != 32 {
		return nil, ErrCiphertext
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCiphertext
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCiphertext
	}
	nonce, err := base64.StdEncoding.DecodeString(box.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrCiphertext
	}
	sealed, err := base64.StdEncoding.DecodeString(box.Payload)
	if err != nil || len(sealed) > 8192+aead.Overhead() {
		return nil, ErrCiphertext
	}
	plain, err := aead.Open(nil, nonce, sealed, label)
	if err != nil {
		return nil, ErrCiphertext
	}
	return plain, nil
}
