// Package id generates short, URL-safe, nanoid-style identifiers.
package id

import "crypto/rand"

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_-"

// New returns a 21-character random id (same length/alphabet class as nanoid).
func New() string {
	b := make([]byte, 21)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail; panic is acceptable here.
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])&63]
	}
	return string(b)
}
