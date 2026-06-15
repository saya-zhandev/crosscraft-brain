// Package crypto implements AES-256-GCM credential encryption, byte-compatible
// with packages/engine/src/crypto.ts: the on-disk blob is
// "hex(iv):hex(tag):hex(ciphertext)" and the key is a 64-char hex string.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Cipher holds the 32-byte AES key.
type Cipher struct{ key []byte }

// New parses the 64-char hex secret into a Cipher.
func New(secretHex string) (*Cipher, error) {
	if len(secretHex) != 64 {
		return nil, fmt.Errorf("CREDENTIALS_SECRET must be a 64-char hex string (32 bytes)")
	}
	k, err := hex.DecodeString(secretHex)
	if err != nil {
		return nil, err
	}
	return &Cipher{key: k}, nil
}

// Encrypt returns "hex(iv):hex(tag):hex(ciphertext)".
func (c *Cipher) Encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block) // 12-byte nonce, 16-byte tag (matches Node)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(plain), nil) // ciphertext || tag
	enc := sealed[:len(sealed)-16]
	tag := sealed[len(sealed)-16:]
	return strings.Join([]string{
		hex.EncodeToString(iv),
		hex.EncodeToString(tag),
		hex.EncodeToString(enc),
	}, ":"), nil
}

// Decrypt reverses Encrypt (and Node's encrypt()).
func (c *Cipher) Decrypt(blob string) (string, error) {
	parts := strings.Split(blob, ":")
	if len(parts) != 3 {
		return "", errors.New("malformed encrypted blob")
	}
	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	tag, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	enc, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, iv, append(enc, tag...), nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
