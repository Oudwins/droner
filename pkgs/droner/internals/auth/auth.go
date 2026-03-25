package auth

import (
	"os"
	"os/exec"
	"strings"
)

type GitHubAuth struct {
	AccessToken string `json:"access_token" zog:"access_token"`
}

type Store struct{}

var lookPath = exec.LookPath
var execCommand = exec.Command

func Default() (*Store, error) {
	return &Store{}, nil
}

func New(_ string) (*Store, error) {
	return &Store{}, nil
}

func (s *Store) GitHub() (*GitHubAuth, bool) {
	_ = s

	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return &GitHubAuth{AccessToken: token}, true
	}

	if token, ok := ghAuthToken(); ok {
		return &GitHubAuth{AccessToken: token}, true
	}

	return nil, false
}

func ghAuthToken() (string, bool) {
	if _, err := lookPath("gh"); err != nil {
		return "", false
	}

	output, err := execCommand("gh", "auth", "token").Output()
	if err != nil {
		return "", false
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", false
	}

	return token, true
}
