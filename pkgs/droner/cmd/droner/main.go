package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"

	"droner/cli"
	"droner/internals/server"
)

func main() {
	args := os.Args[1:]
	serverInstance := server.New()
	if len(args) == 0 {
		if err := serverInstance.Start(); err != nil {
			log.Fatal("[Droner] Failed to start server. Error: ", err)
		}
		return
	}

	err := serverInstance.SafeStart()

	if err != nil {
		log.Fatal("[Droner] CLI Failed to start server. Error: ", err)
	}

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
