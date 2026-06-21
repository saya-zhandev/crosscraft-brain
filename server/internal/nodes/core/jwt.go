// Package core — minimal RSA-SHA256 JWT assertion signer for FCM push.
// Uses only the standard library; no external JWT library needed.
package core

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

// base64urlEncode returns base64url without padding (RFC 7515).
func base64urlEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// signRSASHA256 signs data with a PEM-encoded RSA private key.
func signRSASHA256(pemKey, data []byte) ([]byte, error) {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM private key")
	}
	var key *rsa.PrivateKey
	var err error
	if block.Type == "RSA PRIVATE KEY" {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		k, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, e
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
	} else {
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	h := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
}
