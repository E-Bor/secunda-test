package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr      string
	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	JWTSecret     string
	JWTTTL        time.Duration
	RateLimit     int
	AuthRateLimit int
	LogLevel      slog.Level
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:      value("HTTP_ADDR", ":8080"),
		MySQLDSN:      os.Getenv("MYSQL_DSN"),
		RedisAddr:     value("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
	}
	if cfg.MySQLDSN == "" || len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("MYSQL_DSN and JWT_SECRET (at least 32 bytes) are required")
	}

	var err error
	if cfg.JWTTTL, err = time.ParseDuration(value("JWT_TTL", "24h")); err != nil || cfg.JWTTTL <= 0 {
		return Config{}, fmt.Errorf("JWT_TTL must be a positive duration")
	}
	if cfg.RedisDB, err = integer("REDIS_DB", 0, 0, 15); err != nil {
		return Config{}, err
	}
	if cfg.RateLimit, err = integer("RATE_LIMIT_RPM", 120, 1, 100000); err != nil {
		return Config{}, err
	}
	if cfg.AuthRateLimit, err = integer("AUTH_RATE_LIMIT_RPM", 10, 1, 100000); err != nil {
		return Config{}, err
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(value("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}

	return cfg, nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func integer(key string, fallback, minValue, maxValue int) (int, error) {
	v, err := strconv.Atoi(value(key, strconv.Itoa(fallback)))
	if err != nil || v < minValue || v > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minValue, maxValue)
	}

	return v, nil
}
