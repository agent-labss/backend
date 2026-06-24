package config

import (
	"os"
	"testing"
)

const (
	testHTTPAddr      = ":9090"
	testDotEnvAPIKey  = "sk-dotenv"
	testOpenAIBaseURL = "https://third-party.example/v1"
)

func TestLoadUsesDefaults(t *testing.T) {
	clearEnv(t)

	cfg := Load()
	want := defaultConfig()

	assertConfig(t, cfg, want)
}

func TestLoadUsesAgentDefaults(t *testing.T) {
	clearEnv(t)

	cfg := Load()
	want := defaultConfig()

	assertConfig(t, cfg, want)
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("HTTP_ADDR", testHTTPAddr)
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_URL", "custom.db")

	cfg := Load()
	want := defaultConfig()
	want.AppEnv = "test"
	want.HTTPAddr = testHTTPAddr
	want.DatabaseDriver = "sqlite"
	want.DatabaseURL = "custom.db"

	assertConfig(t, cfg, want)
}

func TestLoadUsesAgentEnvironmentOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_BASE_URL", testOpenAIBaseURL)
	t.Setenv("OPENAI_MODEL", "gpt-5-mini")
	t.Setenv("TRUSTED_TOOL_DIR", "/opt/ai-tools")
	t.Setenv("AGENT_MAX_STEPS", "12")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "90000")

	cfg := Load()
	want := defaultConfig()
	want.OpenAIAPIKey = "sk-test"
	want.OpenAIBaseURL = testOpenAIBaseURL
	want.OpenAIModel = "gpt-5-mini"
	want.TrustedToolDir = "/opt/ai-tools"
	want.AgentMaxSteps = 12
	want.AgentTotalTimeoutMS = 90000

	assertConfig(t, cfg, want)
}

func TestLoadUsesUploadLimitEnvironmentOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_MAX_FILES_PER_RUN", "4")
	t.Setenv("AGENT_MAX_FILE_BYTES", "1048576")
	t.Setenv("AGENT_MAX_TOTAL_FILE_BYTES", "2097152")

	cfg := Load()
	want := defaultConfig()
	want.AgentMaxFilesPerRun = 4
	want.AgentMaxFileBytes = 1048576
	want.AgentMaxTotalFileBytes = 2097152

	assertConfig(t, cfg, want)
}

func TestLoadUsesDotEnvOverrides(t *testing.T) {
	clearEnv(t)
	chdirTemp(t)
	writeDotEnv(t, `# local development
APP_ENV=local
HTTP_ADDR=:7070
DATABASE_DRIVER=sqlite
DATABASE_URL=sqlite.db
OPENAI_API_KEY=`+testDotEnvAPIKey+`
OPENAI_BASE_URL=`+testOpenAIBaseURL+`
OPENAI_MODEL=gpt-5-mini
TRUSTED_TOOL_DIR=./tools
AGENT_MAX_STEPS=5
AGENT_TOTAL_TIMEOUT_MS=30000
AGENT_MAX_FILES_PER_RUN=3
AGENT_MAX_FILE_BYTES=512000
AGENT_MAX_TOTAL_FILE_BYTES=1024000
`)

	cfg := Load()
	want := defaultConfig()
	want.AppEnv = "local"
	want.HTTPAddr = ":7070"
	want.DatabaseURL = "sqlite.db"
	want.OpenAIAPIKey = testDotEnvAPIKey
	want.OpenAIBaseURL = testOpenAIBaseURL
	want.AgentMaxSteps = 5
	want.AgentTotalTimeoutMS = 30000
	want.AgentMaxFilesPerRun = 3
	want.AgentMaxFileBytes = 512000
	want.AgentMaxTotalFileBytes = 1024000

	assertConfig(t, cfg, want)
}

func TestLoadEnvironmentOverridesDotEnv(t *testing.T) {
	clearEnv(t)
	chdirTemp(t)
	writeDotEnv(t, "HTTP_ADDR=:7070\nOPENAI_API_KEY="+testDotEnvAPIKey+"\n")
	t.Setenv("HTTP_ADDR", testHTTPAddr)

	cfg := Load()
	want := defaultConfig()
	want.HTTPAddr = testHTTPAddr
	want.OpenAIAPIKey = testDotEnvAPIKey

	assertConfig(t, cfg, want)
}

func TestLoadFallsBackForInvalidAgentNumbers(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_MAX_STEPS", "invalid")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "-1")
	t.Setenv("AGENT_MAX_FILES_PER_RUN", "0")
	t.Setenv("AGENT_MAX_FILE_BYTES", "-1")
	t.Setenv("AGENT_MAX_TOTAL_FILE_BYTES", "invalid")

	cfg := Load()
	want := defaultConfig()

	assertConfig(t, cfg, want)
}

func clearEnv(t *testing.T) {
	t.Helper()

	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("TRUSTED_TOOL_DIR", "")
	t.Setenv("AGENT_MAX_STEPS", "")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "")
	t.Setenv("AGENT_MAX_FILES_PER_RUN", "")
	t.Setenv("AGENT_MAX_FILE_BYTES", "")
	t.Setenv("AGENT_MAX_TOTAL_FILE_BYTES", "")
}

func defaultConfig() Config {
	return Config{
		AppEnv:                 DefaultAppEnv,
		HTTPAddr:               DefaultHTTPAddr,
		DatabaseDriver:         DefaultDatabaseDriver,
		DatabaseURL:            DefaultDatabaseURL,
		OpenAIAPIKey:           "",
		OpenAIBaseURL:          DefaultOpenAIBaseURL,
		OpenAIModel:            DefaultOpenAIModel,
		TrustedToolDir:         DefaultTrustedToolDir,
		AgentMaxSteps:          DefaultAgentMaxSteps,
		AgentTotalTimeoutMS:    DefaultAgentTotalTimeoutMS,
		AgentMaxFilesPerRun:    DefaultAgentMaxFilesPerRun,
		AgentMaxFileBytes:      DefaultAgentMaxFileBytes,
		AgentMaxTotalFileBytes: DefaultAgentMaxTotalFileBytes,
	}
}

func chdirTemp(t *testing.T) {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatal(err)
		}
	})
}

func writeDotEnv(t *testing.T, content string) {
	t.Helper()

	if err := os.WriteFile(".env", []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(.env) error = %v", err)
	}
}

func assertConfig(t *testing.T, got Config, want Config) {
	t.Helper()

	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}
