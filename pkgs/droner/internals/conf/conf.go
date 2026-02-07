package conf

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/backends"

	z "github.com/Oudwins/zog"
)

type Config struct {
	Version   string          `json:"-"`
	Providers ProvidersConfig `json:"providers"`
	Worktrees WorktreesConfig `json:"worktrees" `
	Server    ServerConfig    `json:"server"`
	Sessions  SessionsConfig  `json:"sessions"`
	Agent     AgentConfig     `json:"agent"`
}

type ProvidersConfig struct {
	Github GitHubConfig `json:"github"`
}

type GitHubConfig struct {
	PollInterval string `json:"poll_interval"`
}

type WorktreesConfig struct {
	Dir string `json:"dir"`
}

type ServerConfig struct {
	DataDir string `json:"data_dir"`
}

type SessionsConfig struct {
	DefaultBackend backends.BackendID `json:"default_backend"`
}

type AgentConfig struct {
	DefaultModel string `json:"default_model"`
}

var gitHubSchema = z.Struct(z.Shape{
	"PollInterval": z.String().Default("60"),
})

var providersSchema = z.Struct(z.Shape{
	"github": gitHubSchema,
})

var worktreesSchema = z.Struct(z.Shape{
	"Dir": z.String().Default("~/.droner/worktrees").Transform(expandPathTransform),
})

var serverSchema = z.Struct(z.Shape{
	"DataDir": z.String().Default("~/.droner").Transform(expandPathTransform),
})

var sessionsSchema = z.Struct(z.Shape{
	"DefaultBackend": z.StringLike[backends.BackendID]().Default(backends.BackendLocal).OneOf(backends.DefaultIDs()),
})

var agentSchema = z.Struct(z.Shape{
	"DefaultModel": z.String().Default("openai/gpt-5.2-codex"),
})

var ConfigSchema = z.Struct(z.Shape{
	"providers": providersSchema,
	"worktrees": worktreesSchema,
	"server":    serverSchema,
	"sessions":  sessionsSchema,
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

func expandPathTransform(ptr *string, c z.Ctx) error {
	expanded, err := expandPath(*ptr)
	*ptr = expanded
	return err
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
