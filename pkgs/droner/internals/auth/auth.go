package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"

	z "github.com/Oudwins/zog"
)

type GitHubAuth struct {
	AccessToken string    `json:"access_token" zog:"access_token"`
	TokenType   string    `json:"token_type,omitempty" zog:"token_type"`
	Scope       string    `json:"scope,omitempty" zog:"scope"`
	UpdatedAt   time.Time `json:"updated_at,omitempty" zog:"updated_at"`
}

type File struct {
	GitHub *GitHubAuth `json:"github,omitempty" zog:"github"`
}

type fileDocument struct {
	GitHub GitHubAuth `json:"github" zog:"github"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data File
}

var GitHubAuthSchema = z.Struct(z.Shape{
	"AccessToken": z.String().Trim().Required(),
	"TokenType":   z.String().Trim(),
	"Scope":       z.String().Trim(),
	"UpdatedAt":   z.Time(z.Time.Format(time.RFC3339Nano)),
})

var FileSchema = z.Struct(z.Shape{
	"GitHub": GitHubAuthSchema,
})

func DefaultPath() (string, error) {
	config := conf.GetConfig()
	dataDir, err := expandPath(config.Server.DataDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(dataDir), "auth.json"), nil
}

func Default() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return New(path)
}

func New(path string) (*Store, error) {
	expandedPath, err := expandPath(path)
	if err != nil {
		return nil, err
	}

	store := &Store{path: filepath.Clean(expandedPath)}
	if err := store.Reload(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func (s *Store) GitHub() (*GitHubAuth, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data.GitHub == nil || strings.TrimSpace(s.data.GitHub.AccessToken) == "" {
		return nil, false
	}

	copy := *s.data.GitHub
	return &copy, true
}

func (s *Store) SetGitHub(auth GitHubAuth) error {
	auth.AccessToken = strings.TrimSpace(auth.AccessToken)
	auth.TokenType = strings.TrimSpace(auth.TokenType)
	auth.Scope = strings.TrimSpace(auth.Scope)

	next := File{GitHub: &auth}
	if err := validateFile(next); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writeLocked(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	loaded, err := loadFile(s.path)
	if err != nil {
		return err
	}
	s.data = loaded
	return nil
}

func (s *Store) writeLocked(file File) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o600)
}

func loadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{}, nil
		}
		return File{}, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return File{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return File{}, fmt.Errorf("failed to parse auth file JSON: %w", err)
	}

	var parsed File
	var document fileDocument
	if issues := FileSchema.Parse(payload, &document); issues != nil {
		return File{}, fmt.Errorf("failed to parse auth file: %s", z.Issues.Prettify(issues))
	}
	if _, hasGitHub := payload["github"]; hasGitHub && strings.TrimSpace(document.GitHub.AccessToken) == "" {
		return File{}, errors.New("failed to parse auth file: github access_token is required")
	}
	if strings.TrimSpace(document.GitHub.AccessToken) != "" {
		parsed.GitHub = &document.GitHub
	}

	return parsed, nil
}

func validateFile(file File) error {
	if file.GitHub != nil && strings.TrimSpace(file.GitHub.AccessToken) == "" {
		return errors.New("failed to validate auth file: github access_token is required")
	}

	data, err := json.Marshal(file)
	if err != nil {
		return err
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	var parsed fileDocument
	if issues := FileSchema.Parse(payload, &parsed); issues != nil {
		return fmt.Errorf("failed to validate auth file: %s", z.Issues.Prettify(issues))
	}

	return nil
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
