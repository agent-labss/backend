package config

import (
	"os"
	"strconv"
)

const (
	DefaultAppEnv              = "development"
	DefaultHTTPAddr            = ":8080"
	DefaultDatabaseURL         = "postgres://orderbuddy_ai:orderbuddy_ai@localhost:5432/orderbuddy_ai?sslmode=disable"
	DefaultOpenAIModel         = "gpt-5-mini"
	DefaultTrustedToolDir      = "./tools"
	DefaultAgentMaxSteps       = 8
	DefaultAgentTotalTimeoutMS = 60000
)

type Config struct {
	AppEnv                 string
	HTTPAddr               string
	DatabaseURL            string
	OpenAIAPIKey           string
	OpenAIModel            string
	TrustedToolDir         string
	InternalReportUsername string
	InternalReportPassword string
	AgentMaxSteps          int
	AgentTotalTimeoutMS    int
}

func Load() Config {
	return Config{
		AppEnv:                 getEnv("APP_ENV", DefaultAppEnv),
		HTTPAddr:               getEnv("HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL:            getEnv("DATABASE_URL", DefaultDatabaseURL),
		OpenAIAPIKey:           getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:            getEnv("OPENAI_MODEL", DefaultOpenAIModel),
		TrustedToolDir:         getEnv("TRUSTED_TOOL_DIR", DefaultTrustedToolDir),
		InternalReportUsername: getEnv("INTERNAL_REPORT_USERNAME", ""),
		InternalReportPassword: getEnv("INTERNAL_REPORT_PASSWORD", ""),
		AgentMaxSteps:          getPositiveIntEnv("AGENT_MAX_STEPS", DefaultAgentMaxSteps),
		AgentTotalTimeoutMS:    getPositiveIntEnv("AGENT_TOTAL_TIMEOUT_MS", DefaultAgentTotalTimeoutMS),
	}
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func getPositiveIntEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}
