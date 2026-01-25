package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

type GitHubAuth struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type authFile struct {
	GitHub *GitHubAuth `json:"github,omitempty"`
}

func ReadGitHubAuth() (*GitHubAuth, bool, error) {
	path, err := authFilePath()
	if err != nil {
		return nil, false, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var payload authFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false, err
	}

	if payload.GitHub == nil || payload.GitHub.AccessToken == "" {
		return nil, false, nil
	}

	return payload.GitHub, true, nil
}

func WriteGitHubAuth(auth GitHubAuth) error {
	path, err := authFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	payload := authFile{GitHub: &auth}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func authFilePath() (string, error) {
	config := conf.GetConfig()
	dataDir, err := expandPath(config.DATA_DIR)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(dataDir), "auth.json"), nil
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
