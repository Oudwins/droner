package conf

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	z "github.com/Oudwins/zog"
)

type Config struct {
	Version   string          `json:"-"`
	Providers ProvidersConfig `json:"providers" zog:"providers"`
	Worktrees WorktreesConfig `json:"worktrees" zog:"worktrees"`
	Server    ServerConfig    `json:"server" zog:"server"`
	Agent     AgentConfig     `json:"agent" zog:"agent"`
}

type ProvidersConfig struct {
	GitHub GitHubConfig `json:"github" zog:"github"`
}

type GitHubConfig struct {
	PollInterval string `json:"poll_interval" zog:"poll_interval"`
}

type WorktreesConfig struct {
	Dir string `json:"dir" zog:"dir"`
}

type ServerConfig struct {
	DataDir string `json:"data_dir" zog:"data_dir"`
}

type AgentConfig struct {
	DefaultModel string `json:"default_model" zog:"default_model"`
}

var gitHubSchema = z.Struct(z.Shape{
	"poll_interval": z.String().Default("1h"),
})

var providersSchema = z.Struct(z.Shape{
	"github": gitHubSchema,
})

var worktreesSchema = z.Struct(z.Shape{
	"dir": z.String().Default("~/.local/share/droner/worktrees"),
})

var serverSchema = z.Struct(z.Shape{
	"data_dir": z.String().Default("~/.local/share/droner"),
})

var agentSchema = z.Struct(z.Shape{
	"default_model": z.String().Default("openai/gpt-5.2-codex"),
})

var ConfigSchema = z.Struct(z.Shape{
	"providers": providersSchema,
	"worktrees": worktreesSchema,
	"server":    serverSchema,
	"agent":     agentSchema,
})
var config *Config

func GetConfig() *Config {

	if config == nil {
		defaults := &Config{}
		if err := ConfigSchema.Parse(map[string]any{}, defaults); err != nil {
			log.Fatal("[Droner] Failed to parse config", err)
		}
		defaults.Version = "0.0.1"

		dataDir, err := expandPath(defaults.Server.DataDir)
		if err != nil {
			log.Fatal("[Droner] Failed to expand config data dir", err)
		}

		configPath := filepath.Join(filepath.Clean(dataDir), "droner.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				config = defaults
				return config
			}
			log.Fatal("[Droner] Failed to read config file", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			config = defaults
			return config
		}

		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			log.Fatal("[Droner] Failed to parse config file", err)
		}
		parsed := &Config{}
		if err := ConfigSchema.Parse(payload, parsed); err != nil {
			log.Fatal("[Droner] Failed to parse config", err)
		}
		parsed.Version = defaults.Version
		config = parsed
	}

	return config
}

func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}
