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
	MinConns        int32
	MaxConns        int32
	MaxConnLifetime time.Duration
	RequestTimeout  time.Duration
	TokenSecret     string
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL wajib diisi")
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
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
	tokenSecret := os.Getenv("TOKEN_SECRET")
	if tokenSecret == "" {
		tokenSecret = "halfpos-dev-secret"
	}
	return Config{
		DatabaseURL:     databaseURL,
		HTTPAddr:        addr,
		MinConns:        minConns,
		MaxConns:        maxConns,
		MaxConnLifetime: lifetime,
		RequestTimeout:  15 * time.Second,
		TokenSecret:     tokenSecret,
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
