package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"droner/cli"
	"droner/internals/server"
)

func main() {
	args := os.Args[1:]
	serverInstance := server.New()
	err = serverInstance.Start()

	if err != nil {
		log.Fatal("[Droner] CLI Failed to start server. Error: ", err)
	}
	// TODO: Deal with the error

	if err := cli.Run(args); err != nil {
		if errors.Is(err, cli.ErrUsage) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func startServerProcess() error {
	cmd := exec.Command(os.Args[0])
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
