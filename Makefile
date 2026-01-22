# Load environment variables from .env file
ifneq (,$(wildcard ./.env))
    include .env
    export
endif


kill:
	-fuser -k 56876/tcp

dev: kill
	cd ./pkgs/droner/ && go run ./cmd/droner/main.go

run:
	go -C pkgs/droner run ./cmd/droner

