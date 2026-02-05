package cliutil

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/desktop"
	"github.com/Oudwins/droner/pkgs/droner/internals/term"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

func RunGitHubAuthFlow(client *sdk.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start, err := client.StartGitHubOAuth(ctx)
	if err != nil {
		return err
	}

	url := start.VerificationURIComplete
	if url == "" {
		url = start.VerificationURI
	}

	if url != "" {
		_ = desktop.OpenURL(url)
		fmt.Printf("Authenticate with GitHub to continue:\nURL: %s\n", term.ClickableLink(url, url))
	} else {
		fmt.Println("Authenticate with GitHub to continue:")
	}
	if start.UserCode != "" {
		fmt.Printf("User code: %s\n", start.UserCode)
	}

	deadline := time.Now().Add(2 * time.Minute)
	if start.ExpiresIn > 0 {
		expiry := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
		if expiry.Before(deadline) {
			deadline = expiry
		}
	}
	pollInterval := 2 * time.Second
	if start.Interval > 0 {
		pollInterval = time.Duration(start.Interval) * time.Second
		if pollInterval < 2*time.Second {
			pollInterval = 2 * time.Second
		}
	}
	for time.Now().Before(deadline) {
		pollCtx, pollCancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := client.GitHubOAuthStatus(pollCtx, start.State)
		pollCancel()
		if err != nil {
			return err
		}
		switch status.Status {
		case "complete":
			fmt.Println("GitHub auth complete.")
			return nil
		case "failed":
			if status.Error != "" {
				return fmt.Errorf("github auth failed: %s", status.Error)
			}
			return errors.New("github auth failed")
		default:
			time.Sleep(pollInterval)
		}
	}

	return errors.New("timed out waiting for github auth")
}
