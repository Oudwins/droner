# Load environment variables from .env file
ifneq (,$(wildcard ./.env))
    include .env
    export
endif


BIN_DIR ?= ./bin
DRONERD_BIN := $(BIN_DIR)/dronerd
DRONER_BIN := $(BIN_DIR)/droner

kill:
	-@if command -v fuser >/dev/null 2>&1; then \
		fuser -k 57876/tcp; \
	elif command -v lsof >/dev/null 2>&1; then \
		lsof -ti tcp:57876 | xargs -r kill -9; \
	else \
		echo "No fuser or lsof; cannot free port 57876"; \
	fi

dev: kill
	cd ./pkgs/droner/ && go run ./dronerd

build:
	mkdir -p $(BIN_DIR)
	cd ./pkgs/droner/ && go build -o ../../$(DRONERD_BIN) ./dronerd
	cd ./pkgs/droner/ && go build -o ../../$(DRONER_BIN) ./droner

test:
	cd ./pkgs/droner/ && go test ./...

cli: build
	$(DRONER_BIN) $(filter-out $@,$(MAKECMDGOALS))

%:
	@:
