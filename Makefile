# Load environment variables from .env file
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

dev:
	cd ./pkgs/droner/ && go run ./cmd/droner/main.go

run:
	go -C pkgs/droner run ./cmd/droner
