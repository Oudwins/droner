package env

import (
	"path/filepath"
	"testing"
)

func TestEnvDefaults(t *testing.T) {
	env = nil
	t.Cleanup(func() { env = nil })

	got := Get()
	if got.PORT != 57876 {
		t.Fatalf("expected default port 57876, got %d", got.PORT)
	}
	if got.LISTEN_ADDR != "localhost:57876" {
		t.Fatalf("expected listen addr localhost:57876, got %s", got.LISTEN_ADDR)
	}
	if got.BASE_URL != "http://localhost:57876" {
		t.Fatalf("expected base url http://localhost:57876, got %s", got.BASE_URL)
	}
	if got.GITHUB_TOKEN != "" {
		t.Fatalf("expected empty github token, got %q", got.GITHUB_TOKEN)
	}
	if got.DATA_DIR != filepath.Join(got.HOME, ".droner") {
		t.Fatalf("expected default data dir %q, got %q", filepath.Join(got.HOME, ".droner"), got.DATA_DIR)
	}
	if got.LOG_OUTPUT != LogOutputFile {
		t.Fatalf("expected default log output %q, got %q", LogOutputFile, got.LOG_OUTPUT)
	}
	if got.LOG_LEVEL != LogLevelDebug {
		t.Fatalf("expected default log level %q, got %q", LogLevelDebug, got.LOG_LEVEL)
	}
}

func TestEnvOverridesPort(t *testing.T) {
	t.Setenv("DRONER_ENV_PORT", "1234")
	env = nil
	t.Cleanup(func() { env = nil })

	got := Get()
	if got.PORT != 1234 {
		t.Fatalf("expected port 1234, got %d", got.PORT)
	}
	if got.LISTEN_ADDR != "localhost:1234" {
		t.Fatalf("expected listen addr localhost:1234, got %s", got.LISTEN_ADDR)
	}
	if got.BASE_URL != "http://localhost:1234" {
		t.Fatalf("expected base url http://localhost:1234, got %s", got.BASE_URL)
	}
}

func TestEnvOverridesLogOutput(t *testing.T) {
	t.Setenv("DRONERD_LOG_OUTPUT", string(LogOutputBoth))
	env = nil
	t.Cleanup(func() { env = nil })

	got := Get()
	if got.LOG_OUTPUT != LogOutputBoth {
		t.Fatalf("expected log output %q, got %q", LogOutputBoth, got.LOG_OUTPUT)
	}
}

func TestEnvOverridesLogLevel(t *testing.T) {
	t.Setenv("DRONERD_LOG_LEVEL", string(LogLevelWarn))
	env = nil
	t.Cleanup(func() { env = nil })

	got := Get()
	if got.LOG_LEVEL != LogLevelWarn {
		t.Fatalf("expected log level %q, got %q", LogLevelWarn, got.LOG_LEVEL)
	}
}

func TestEnvOverridesDataDir(t *testing.T) {
	t.Setenv("DRONERD_DATA_DIR", "~/custom-droner")
	env = nil
	t.Cleanup(func() { env = nil })

	got := Get()
	if got.DATA_DIR != filepath.Join(got.HOME, "custom-droner") {
		t.Fatalf("expected data dir %q, got %q", filepath.Join(got.HOME, "custom-droner"), got.DATA_DIR)
	}
}
