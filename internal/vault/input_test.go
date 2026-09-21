package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestInputBoxLongTokenPurposeAndTamper(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	rand.Read(key)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	purpose := "cloud.save"
	label := []byte("raylea/credential-input/v1/" + purpose)
	plain := bytes.Repeat([]byte("synthetic-token-"), 250)
	sealed := aead.Seal(nil, nonce, plain, label)
	wrapped, _ := rsa.EncryptOAEP(sha256.New(), rand.Reader, &m.transport.PublicKey, key, label)
	box := InputBox{base64.StdEncoding.EncodeToString(wrapped), base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(sealed)}
	got, err := m.DecodeInput(box, purpose)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("hybrid input failed", err)
	}
	if _, err = m.DecodeInput(box, "cloud.initialize"); err == nil {
		t.Fatal("purpose swap accepted")
	}
	second, _ := NewManager(t.TempDir())
	if _, err = second.DecodeInput(box, purpose); err == nil {
		t.Fatal("old process box accepted")
	}
	sealed[0] ^= 1
	box.Payload = base64.StdEncoding.EncodeToString(sealed)
	if _, err = m.DecodeInput(box, purpose); err == nil {
		t.Fatal("tamper accepted")
	}
}
