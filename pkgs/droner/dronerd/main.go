package main

import (
	"log"

	"droner/dronerd/server"
)

func main() {
	serverInstance := server.New()
	if err := serverInstance.Start(); err != nil {
		log.Fatal("[Droner] Failed to start server: ", err)
	}
}
