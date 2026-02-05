package cliutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

func EnsureDaemonRunning(client *sdk.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if version, err := client.Version(ctx); err == nil {
		localVersion := conf.GetConfig().Version
		if strings.TrimSpace(version) == strings.TrimSpace(localVersion) {
			return nil
		}
		return replaceDaemon(client, version, localVersion)
	}

	if err := StartDaemon(); err != nil {
		return err
	}

	return waitForDaemon(client)
}

func StartDaemon() error {
	path, err := findServeBinary()
	if err != nil {
		return err
	}

	cmd := exec.Command(path, "serve")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

func waitForDaemon(client *sdk.Client) error {
	var lastErr error
	for i := 0; i < 8; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := client.Version(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * 150 * time.Millisecond)
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("failed to reach droner server")
}

func replaceDaemon(client *sdk.Client, remoteVersion string, localVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Shutdown(ctx); err != nil {
		if errors.Is(err, sdk.ErrShutdownUnsupported) {
			return fmt.Errorf("dronerd %s is running; please stop it and retry", strings.TrimSpace(remoteVersion))
		}
		return fmt.Errorf("failed to shutdown dronerd %s: %w", strings.TrimSpace(remoteVersion), err)
	}

	if err := waitForDaemonStop(client); err != nil {
		return fmt.Errorf("dronerd %s did not stop: %w", strings.TrimSpace(remoteVersion), err)
	}

	if err := StartDaemon(); err != nil {
		return err
	}

	return waitForDaemon(client)
}

func waitForDaemonStop(client *sdk.Client) error {
	for i := 0; i < 8; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := client.Version(ctx)
		cancel()
		if err != nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 150 * time.Millisecond)
	}

	return errors.New("failed to stop dronerd")
}

func findServeBinary() (string, error) {
	executable, err := os.Executable()
	if err == nil && executable != "" {
		return executable, nil
	}

	path, err := exec.LookPath("droner")
	if err != nil {
		return "", fmt.Errorf("droner not found in PATH")
	}
	return path, nil
}
