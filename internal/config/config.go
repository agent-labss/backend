package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultAppEnv                 = "development"
	DefaultHTTPAddr               = ":8080"
	DefaultDatabaseDriver         = "sqlite"
	DefaultDatabaseURL            = "sqlite.db"
	DefaultOpenAIBaseURL          = ""
	DefaultOpenAIModel            = "gpt-5-mini"
	DefaultTrustedToolDir         = "./tools"
	DefaultAgentMaxSteps          = 8
	DefaultAgentTotalTimeoutMS    = 60000
	DefaultAgentMaxFilesPerRun    = 5
	DefaultAgentMaxFileBytes      = 10 * 1024 * 1024
	DefaultAgentMaxTotalFileBytes = 25 * 1024 * 1024
)

type Config struct {
	AppEnv                 string
	HTTPAddr               string
	DatabaseDriver         string
	DatabaseURL            string
	OpenAIAPIKey           string
	OpenAIBaseURL          string
	OpenAIModel            string
	TrustedToolDir         string
	AgentMaxSteps          int
	AgentTotalTimeoutMS    int
	AgentMaxFilesPerRun    int
	AgentMaxFileBytes      int
	AgentMaxTotalFileBytes int
}

func Load() Config {
	dotEnv := loadDotEnv(".env")

	return Config{
		AppEnv:                 getEnv(dotEnv, "APP_ENV", DefaultAppEnv),
		HTTPAddr:               getEnv(dotEnv, "HTTP_ADDR", DefaultHTTPAddr),
		DatabaseDriver:         getEnv(dotEnv, "DATABASE_DRIVER", DefaultDatabaseDriver),
		DatabaseURL:            getEnv(dotEnv, "DATABASE_URL", DefaultDatabaseURL),
		OpenAIAPIKey:           getEnv(dotEnv, "OPENAI_API_KEY", ""),
		OpenAIBaseURL:          getEnv(dotEnv, "OPENAI_BASE_URL", DefaultOpenAIBaseURL),
		OpenAIModel:            getEnv(dotEnv, "OPENAI_MODEL", DefaultOpenAIModel),
		TrustedToolDir:         getEnv(dotEnv, "TRUSTED_TOOL_DIR", DefaultTrustedToolDir),
		AgentMaxSteps:          getPositiveIntEnv(dotEnv, "AGENT_MAX_STEPS", DefaultAgentMaxSteps),
		AgentTotalTimeoutMS:    getPositiveIntEnv(dotEnv, "AGENT_TOTAL_TIMEOUT_MS", DefaultAgentTotalTimeoutMS),
		AgentMaxFilesPerRun:    getPositiveIntEnv(dotEnv, "AGENT_MAX_FILES_PER_RUN", DefaultAgentMaxFilesPerRun),
		AgentMaxFileBytes:      getPositiveIntEnv(dotEnv, "AGENT_MAX_FILE_BYTES", DefaultAgentMaxFileBytes),
		AgentMaxTotalFileBytes: getPositiveIntEnv(dotEnv, "AGENT_MAX_TOTAL_FILE_BYTES", DefaultAgentMaxTotalFileBytes),
	}
}

func getEnv(dotEnv map[string]string, key string, fallback string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}
	if value, ok := dotEnv[key]; ok && value != "" {
		return value
	}

	return fallback
}

func getPositiveIntEnv(dotEnv map[string]string, key string, fallback int) int {
	value := getEnv(dotEnv, key, "")
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func loadDotEnv(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer closeDotEnv(file)

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if ok {
			values[key] = value
		}
	}

	return values
}

func closeDotEnv(file *os.File) {
	if err := file.Close(); err != nil {
		return
	}
}

func parseDotEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	return key, unquoteDotEnvValue(strings.TrimSpace(value)), true
}

func unquoteDotEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return unquoted
		}
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
	}
	return value
}
