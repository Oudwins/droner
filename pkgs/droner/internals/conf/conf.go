package conf

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/version"

	z "github.com/Oudwins/zog"
)

type Config struct {
	Version   string          `json:"-"`
	Providers ProvidersConfig `json:"providers"`
	Sessions  SessionsConfig  `json:"sessions"`
	TUI       TUIConfig       `json:"tui" zog:"tui"`
}

type ProvidersConfig struct {
	Github GitHubConfig `json:"github"`
}

type GitHubConfig struct {
	// Seconds
	PollInterval int `json:"pollInterval"`
}

var gitHubSchema = z.Struct(z.Shape{
	"PollInterval": z.Int().DefaultFunc(func() int {
		return int(10 * time.Second)
	}),
})

var providersSchema = z.Struct(z.Shape{
	"github": gitHubSchema,
})

var ConfigSchema = z.Struct(z.Shape{
	"Providers": providersSchema,
	"Sessions":  SessionsConfigSchema,
	"TUI":       TUIConfigSchema,
})
var config *Config

func GetConfig() *Config {

	if config == nil {
		defaults := &Config{}
		if err := ConfigSchema.Parse(map[string]any{}, defaults); err != nil {
			log.Fatal("[Droner] Failed to parse config", err)
		}
		defaults.Version = version.Version()

		configPath := filepath.Join(filepath.Clean(env.Get().DATA_DIR), "droner.json")
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
