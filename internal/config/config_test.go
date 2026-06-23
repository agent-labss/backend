package config

import "testing"

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
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_URL", "custom.db")

	cfg := Load()
	want := defaultConfig()
	want.AppEnv = "test"
	want.HTTPAddr = ":9090"
	want.DatabaseDriver = "sqlite"
	want.DatabaseURL = "custom.db"

	assertConfig(t, cfg, want)
}

func TestLoadUsesAgentEnvironmentOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_MODEL", "gpt-5-mini")
	t.Setenv("TRUSTED_TOOL_DIR", "/opt/orderbuddy-tools")
	t.Setenv("INTERNAL_REPORT_USERNAME", "svc-user")
	t.Setenv("INTERNAL_REPORT_PASSWORD", "svc-pass")
	t.Setenv("AGENT_MAX_STEPS", "12")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "90000")

	cfg := Load()
	want := defaultConfig()
	want.OpenAIAPIKey = "sk-test"
	want.OpenAIModel = "gpt-5-mini"
	want.TrustedToolDir = "/opt/orderbuddy-tools"
	want.InternalReportUsername = "svc-user"
	want.InternalReportPassword = "svc-pass"
	want.AgentMaxSteps = 12
	want.AgentTotalTimeoutMS = 90000

	assertConfig(t, cfg, want)
}

func TestLoadFallsBackForInvalidAgentNumbers(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_MAX_STEPS", "invalid")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "-1")

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
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("TRUSTED_TOOL_DIR", "")
	t.Setenv("INTERNAL_REPORT_USERNAME", "")
	t.Setenv("INTERNAL_REPORT_PASSWORD", "")
	t.Setenv("AGENT_MAX_STEPS", "")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "")
}

func defaultConfig() Config {
	return Config{
		AppEnv:                 DefaultAppEnv,
		HTTPAddr:               DefaultHTTPAddr,
		DatabaseDriver:         DefaultDatabaseDriver,
		DatabaseURL:            DefaultDatabaseURL,
		OpenAIAPIKey:           "",
		OpenAIModel:            DefaultOpenAIModel,
		TrustedToolDir:         DefaultTrustedToolDir,
		InternalReportUsername: "",
		InternalReportPassword: "",
		AgentMaxSteps:          DefaultAgentMaxSteps,
		AgentTotalTimeoutMS:    DefaultAgentTotalTimeoutMS,
	}
}

func assertConfig(t *testing.T, got Config, want Config) {
	t.Helper()

	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}
