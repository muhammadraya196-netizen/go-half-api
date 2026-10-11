package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrToken = errors.New("token tidak valid")

type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	Exp    int64  `json:"exp"`
}

// Sign membuat token HMAC-SHA256 yang berisi id user, role, dan waktu kedaluwarsa.
func Sign(secret []byte, userID, role string, expiresAt time.Time) (string, error) {
	body, err := json.Marshal(Claims{
		UserID: userID,
		Role:   role,
		Exp:    expiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

// Parse memverifikasi tanda tangan dan masa berlaku token.
func Parse(secret []byte, token string, now time.Time) (Claims, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || sig == "" || strings.Contains(sig, ".") {
		return Claims{}, ErrToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Claims{}, ErrToken
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return Claims{}, ErrToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, ErrToken
	}
	var claims Claims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.UserID == "" || claims.Role == "" {
		return Claims{}, ErrToken
	}
	if now.Unix() >= claims.Exp {
		return Claims{}, ErrToken
	}
	return claims, nil
}
