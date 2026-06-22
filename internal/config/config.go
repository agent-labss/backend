package config

import "os"

const (
	DefaultAppEnv      = "development"
	DefaultHTTPAddr    = ":8080"
	DefaultDatabaseURL = "postgres://orderbuddy_ai:orderbuddy_ai@localhost:5432/orderbuddy_ai?sslmode=disable"
)

type Config struct {
	AppEnv      string
	HTTPAddr    string
	DatabaseURL string
}

func Load() Config {
	return Config{
		AppEnv:      getEnv("APP_ENV", DefaultAppEnv),
		HTTPAddr:    getEnv("HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL: getEnv("DATABASE_URL", DefaultDatabaseURL),
	}
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
