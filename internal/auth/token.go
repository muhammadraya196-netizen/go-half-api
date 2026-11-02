package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// IssueToken membuat token sesi sederhana: base64url(userID|exp|mac).
func IssueToken(secret, userID string, ttl time.Duration) string {
	if secret == "" {
		secret = "halfpos-dev-secret"
	}
	exp := time.Now().Add(ttl).Unix()
	payload := userID + "|" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

// ParseToken memverifikasi token dan mengembalikan userID.
func ParseToken(secret, token string) (string, error) {
	if secret == "" {
		secret = "halfpos-dev-secret"
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return "", fmt.Errorf("token tidak valid")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", fmt.Errorf("token tidak valid")
	}
	userID, expRaw, sig := parts[0], parts[1], parts[2]
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", fmt.Errorf("sesi berakhir")
	}
	payload := userID + "|" + expRaw
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", fmt.Errorf("token tidak valid")
	}
	if userID == "" {
		return "", fmt.Errorf("token tidak valid")
	}
	return userID, nil
}
