package conf

import "testing"

func TestTUIConfigSchemaDefaultsAgentNames(t *testing.T) {
	var parsed TUIConfig
	if err := TUIConfigSchema.Parse(map[string]any{}, &parsed); err != nil {
		t.Fatalf("parse defaults: %v", err)
	}

	assertAgentNames(t, parsed.AgentNames, []string{"build", "plan"})
}

func TestTUIConfigSchemaNormalizesAgentNames(t *testing.T) {
	var parsed TUIConfig
	if err := TUIConfigSchema.Parse(map[string]any{"agentNames": []any{"  build  ", "", " plan", "\t"}}, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	assertAgentNames(t, parsed.AgentNames, []string{"build", "plan"})
}

func TestTUIConfigSchemaFallsBackWhenConfiguredAgentNamesNormalizeEmpty(t *testing.T) {
	var parsed TUIConfig
	if err := TUIConfigSchema.Parse(map[string]any{"agentNames": []any{" ", "\t", "\n"}}, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	assertAgentNames(t, parsed.AgentNames, []string{"build", "plan"})
}

func TestConfigSchemaIncludesTUIConfig(t *testing.T) {
	var parsed Config
	if err := ConfigSchema.Parse(map[string]any{"tui": map[string]any{"agentNames": []any{"review"}}}, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	assertAgentNames(t, parsed.TUI.AgentNames, []string{"review"})
}

func assertAgentNames(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("agent name count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agentNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
