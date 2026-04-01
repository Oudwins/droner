package cmd

import (
	"log"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/server"
)

func RunServer() {
	serverInstance := server.New()
	if err := serverInstance.Start(); err != nil {
		log.Fatal("[Droner] Failed to start server: ", err)
	}
}
