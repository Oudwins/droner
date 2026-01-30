package env

import "testing"

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
