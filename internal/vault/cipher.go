package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const namespace = "raylea.mihoyo-accounts"

var ErrCiphertext = errors.New("credential ciphertext cannot be opened")

type Key struct {
	ID    string `json:"id"`
	Value []byte `json:"value"`
}

type Cipher struct {
	id      string
	aead    cipher.AEAD
	readers map[string]cipher.AEAD
}

type envelope struct {
	Version int    `json:"version"`
	KeyID   string `json:"key_id"`
	Sealed  string `json:"sealed"`
}

func NewKey() Key {
	value := make([]byte, 32)
	_, _ = rand.Read(value)
	return Key{ID: rand.Text(), Value: value}
}

func NewCipher(key Key) (*Cipher, error) {
	if len(key.Value) != 32 || key.ID == "" {
		return nil, errors.New("invalid credential encryption key")
	}
	block, err := aes.NewCipher(key.Value)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{id: key.ID, aead: aead, readers: map[string]cipher.AEAD{key.ID: aead}}, nil
}
func NewRingCipher(keys []Key) (*Cipher, error) {
	if len(keys) < 1 || len(keys) > 128 {
		return nil, ErrLocked
	}
	current, err := NewCipher(keys[0])
	if err != nil {
		return nil, err
	}
	for _, key := range keys[1:] {
		if _, exists := current.readers[key.ID]; exists {
			return nil, ErrLocked
		}
		old, err := NewCipher(key)
		if err != nil {
			return nil, err
		}
		current.readers[key.ID] = old.aead
	}
	return current, nil
}
func (c *Cipher) KeyID() string { return c.id }

func (c *Cipher) Seal(record string, plaintext []byte) (string, error) {
	if record == "" {
		return "", errors.New("encrypted record identity is required")
	}
	sealed := c.aead.Seal(nil, nil, plaintext, []byte(namespace+"/v1/"+record))
	data, err := json.Marshal(envelope{Version: 1, KeyID: c.id, Sealed: base64.StdEncoding.EncodeToString(sealed)})
	return string(data), err
}

func (c *Cipher) Open(record, value string) ([]byte, error) {
	var input envelope
	if json.Unmarshal([]byte(value), &input) != nil || input.Version != 1 || record == "" {
		return nil, ErrCiphertext
	}
	reader := c.readers[input.KeyID]
	if reader == nil {
		return nil, ErrCiphertext
	}
	sealed, err := base64.StdEncoding.DecodeString(input.Sealed)
	if err != nil {
		return nil, ErrCiphertext
	}
	plaintext, err := reader.Open(nil, nil, sealed, []byte(namespace+"/v1/"+record))
	if err != nil {
		return nil, ErrCiphertext
	}
	return plaintext, nil
}
