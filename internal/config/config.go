package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL     string
	HTTPAddr        string
	TokenSecret     []byte
	TokenTTL        time.Duration
	MinConns        int32
	MaxConns        int32
	MaxConnLifetime time.Duration
	RequestTimeout  time.Duration
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL wajib diisi")
	}
	secret := os.Getenv("TOKEN_SECRET")
	if secret == "" {
		return Config{}, fmt.Errorf("TOKEN_SECRET wajib diisi")
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	ttl := 24 * time.Hour
	if raw := os.Getenv("TOKEN_TTL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("TOKEN_TTL tidak valid")
		}
		ttl = parsed
	}
	minConns := int32(envInt("DB_MIN_CONNS", 2))
	maxConns := int32(envInt("DB_MAX_CONNS", 20))
	if minConns < 0 || maxConns < 1 || minConns > maxConns {
		return Config{}, fmt.Errorf("DB_MIN_CONNS dan DB_MAX_CONNS tidak valid")
	}
	lifetime := 30 * time.Minute
	if raw := os.Getenv("DB_MAX_CONN_LIFETIME"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("DB_MAX_CONN_LIFETIME tidak valid")
		}
		lifetime = parsed
	}
	return Config{
		DatabaseURL:     databaseURL,
		HTTPAddr:        addr,
		TokenSecret:     []byte(secret),
		TokenTTL:        ttl,
		MinConns:        minConns,
		MaxConns:        maxConns,
		MaxConnLifetime: lifetime,
		RequestTimeout:  15 * time.Second,
	}, nil
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
