package sessionids

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/naming"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
)

type CreateSessionIDOptions struct {
	RepoPath      string
	Naming        conf.SessionNamingConfig
	Message       *messages.Message
	MaxAttempts   int
	IsValid       func(id string) error
	OnNamingError func(err error)
}

func NewForCreateSession(ctx context.Context, opts CreateSessionIDOptions) (string, error) {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 100
	}
	if opts.IsValid == nil {
		return "", fmt.Errorf("missing IsValid")
	}

	promptText := strings.TrimSpace(messages.ToRawText(opts.Message))
	prefix := ""

	if opts.Naming.Strategy == conf.SessionNamingStrategyOpenCodePrompt && promptText != "" {
		rawPrefix, err := generateNamePrefixWithOpenCode(ctx, opts.RepoPath, opts.Naming.Model, promptText)
		if err != nil {
			if opts.OnNamingError != nil {
				opts.OnNamingError(err)
			}
		} else {
			prefix = naming.SanitizeSessionNamePrefix(rawPrefix)
		}
	}

	return NewWithPrefix(prefix, &GeneratorConfig{
		MaxAttempts: opts.MaxAttempts,
		IsValid:     opts.IsValid,
	})
}

const openCodeSessionNamePromptPrefix = "" +
	"Generate a short (1-3 words) git branch name for this coding session.\n" +
	"\n" +
	"Return ONLY the name, with no additional text.\n" +
	"Rules:\n" +
	"- lowercase ASCII\n" +
	"- only letters a-z, numbers 0-9, and hyphen (-)\n" +
	"- kebab-case\n" +
	"- no spaces\n" +
	"- no punctuation\n" +
	"- no quotes\n" +
	"- no markdown\n" +
	"\n" +
	"User prompt:\n"

func generateNamePrefixWithOpenCode(ctx context.Context, repoPath string, model string, promptText string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeouts.SecondLong)
	defer cancel()

	message := openCodeSessionNamePromptPrefix + promptText

	args := []string{"run", "--dir", repoPath}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	args = append(args, message)

	cmd := exec.CommandContext(ctx, "opencode", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("opencode run failed: %w: %s", err, errMsg)
		}
		return "", fmt.Errorf("opencode run failed: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
