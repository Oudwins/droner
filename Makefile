# Load environment variables from .env file
ifneq (,$(wildcard ./.env))
    include .env
    export
endif


kill:
	-@if command -v fuser >/dev/null 2>&1; then \
		fuser -k 57876/tcp; \
	elif command -v lsof >/dev/null 2>&1; then \
		lsof -ti tcp:57876 | xargs -r kill -9; \
	else \
		echo "No fuser or lsof; cannot free port 57876"; \
	fi

dev: kill
	cd ./pkgs/droner/ && go run ./cmd/droner/main.go

run:
	cd ./pkgs/droner/ && go run ./cmd/droner/main.go
