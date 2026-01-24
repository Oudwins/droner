package main

import (
	"log"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/server"
)

func main() {
	serverInstance := server.New()
	if err := serverInstance.Start(); err != nil {
		log.Fatal("[Droner] Failed to start server: ", err)
	}
}
