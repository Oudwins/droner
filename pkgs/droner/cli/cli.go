package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"droner/sdk"
)

var ErrUsage = errors.New("usage: droner sum <a> <b>")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, ErrUsage) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return ErrUsage
	}

	if args[0] != "sum" {
		return ErrUsage
	}

	if len(args) != 3 {
		return ErrUsage
	}

	a, err := strconv.Atoi(args[1])
	if err != nil {
		return ErrUsage
	}
	b, err := strconv.Atoi(args[2])
	if err != nil {
		return ErrUsage
	}

	client := sdk.NewClient()
	if err := ensureDaemonRunning(client); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sum, err := client.Sum(ctx, a, b)
	if err != nil {
		return err
	}

	fmt.Println(sum)
	return nil
}

func ensureDaemonRunning(client *sdk.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if _, err := client.Version(ctx); err == nil {
		return nil
	}

	if err := startDaemon(); err != nil {
		return err
	}

	return waitForDaemon(client)
}

func startDaemon() error {
	path, err := findDaemonBinary()
	if err != nil {
		return err
	}

	cmd := exec.Command(path)
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
	return errors.New("failed to reach dronerd")
}

func findDaemonBinary() (string, error) {
	executable, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "dronerd")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}

	path, err := exec.LookPath("dronerd")
	if err != nil {
		return "", fmt.Errorf("dronerd not found in PATH")
	}
	return path, nil
}
