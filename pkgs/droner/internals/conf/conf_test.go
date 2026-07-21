package conf

import (
	"encoding/json"
	"testing"
)

func TestConfigSchemaParsesDefaultModel(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(`{
		"sessions": {
			"harness": {
				"providers": {
					"openCode": {
						"defaultModel": "openai/gpt-5.6-sol"
					}
				}
			}
		}
	}`), &payload); err != nil {
		t.Fatal(err)
	}

	var parsed Config
	if err := ConfigSchema.Parse(payload, &parsed); err != nil {
		t.Fatal(err)
	}
	if got := parsed.Sessions.Harness.DefaultModel(); got != "openai/gpt-5.6-sol" {
		t.Fatalf("default model = %q, want %q", got, "openai/gpt-5.6-sol")
	}
}
