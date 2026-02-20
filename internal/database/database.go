// Package database provides PostgreSQL connection management using pgx.
package database

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds PostgreSQL connection settings read from environment variables.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// configFromEnv reads database config from well-known environment variables,
// falling back to sensible local-development defaults.
func configFromEnv() Config {
	return Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     getEnv("DB_PORT", "5432"),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		DBName:   getEnv("DB_NAME", "eventbooking"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// DSN builds a libpq-compatible connection string.
func (c Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

// NewPool creates and validates a pgxpool connection pool.
// It retries up to 5 times to accommodate containers starting up.
func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	cfg := configFromEnv()

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	// Sensible pool defaults for a small service.
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	var pool *pgxpool.Pool
	for attempt := 1; attempt <= 5; attempt++ {
		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				break
			}
			pool.Close()
		}
		fmt.Printf("db connect attempt %d/5 failed: %v – retrying in 2s\n", attempt, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	return pool, nil
}
