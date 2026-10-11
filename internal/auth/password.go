package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// Parameter scrypt Node: crypto.scryptSync(password, salt, 64)
// dengan N=16384, r=8, p=1. saltHex dipakai sebagai byte UTF-8, bukan hasil decode hex.
const (
	scryptN      = 16384
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 64
)

// HashPassword menyimpan password sebagai scrypt$<saltHex>$<hashHex>.
func HashPassword(password string) (string, error) {
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(saltBytes)
	sum, err := derive(password, salt)
	if err != nil {
		return "", err
	}
	return "scrypt$" + salt + "$" + hex.EncodeToString(sum), nil
}

// VerifyPassword membandingkan password dengan hash tersimpan memakai waktu konstan.
func VerifyPassword(password, stored string) bool {
	scheme, salt, hashHex, ok := splitHash(stored)
	if !ok || scheme != "scrypt" || salt == "" || hashHex == "" {
		return false
	}
	expected, err := hex.DecodeString(hashHex)
	if err != nil || len(expected) != scryptKeyLen {
		return false
	}
	actual, err := derive(password, salt)
	if err != nil || len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func splitHash(stored string) (scheme, salt, hash string, ok bool) {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func derive(password, salt string) ([]byte, error) {
	return scrypt.Key([]byte(password), []byte(salt), scryptN, scryptR, scryptP, scryptKeyLen)
}
