package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsConfigSchemaDefaultsParentPaths(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	var parsed ProjectsConfig
	if err := ProjectsConfigSchema.Parse(map[string]any{}, &parsed); err != nil {
		t.Fatalf("parse defaults: %v", err)
	}

	want := []string{filepath.Join(homeDir, "projects"), filepath.Join(homeDir, "Documents")}
	if len(parsed.ParentPaths) != len(want) {
		t.Fatalf("parentPaths = %v, want %v", parsed.ParentPaths, want)
	}
	for i := range want {
		if parsed.ParentPaths[i] != want[i] {
			t.Fatalf("parentPaths[%d] = %q, want %q", i, parsed.ParentPaths[i], want[i])
		}
	}
}

func TestProjectsConfigSchemaNormalizesParentPaths(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	var parsed ProjectsConfig
	if err := ProjectsConfigSchema.Parse(map[string]any{"parentPaths": []any{"  ~/projects  ", "", " /tmp/work ", "\t"}}, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	want := []string{filepath.Join(homeDir, "projects"), "/tmp/work"}
	if len(parsed.ParentPaths) != len(want) {
		t.Fatalf("parentPaths = %v, want %v", parsed.ParentPaths, want)
	}
	for i := range want {
		if parsed.ParentPaths[i] != want[i] {
			t.Fatalf("parentPaths[%d] = %q, want %q", i, parsed.ParentPaths[i], want[i])
		}
	}
}

func TestConfigSchemaIncludesProjectsConfig(t *testing.T) {
	var parsed Config
	if err := ConfigSchema.Parse(map[string]any{"projects": map[string]any{"parentPaths": []any{"/tmp/projects"}}}, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if len(parsed.Projects.ParentPaths) != 1 || parsed.Projects.ParentPaths[0] != "/tmp/projects" {
		t.Fatalf("parentPaths = %v, want [/tmp/projects]", parsed.Projects.ParentPaths)
	}
}
