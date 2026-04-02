set dotenv-load := true

bin_dir := "./bin"
dronerd_bin := bin_dir + "/dronerd"
droner_bin := bin_dir + "/droner"

kill:
    @if command -v fuser >/dev/null 2>&1; then \
        fuser -k 57876/tcp 57877/tcp >/dev/null 2>&1 || true; \
    elif command -v lsof >/dev/null 2>&1; then \
        lsof -ti tcp:57876,tcp:57877 | xargs -r kill -9; \
    else \
        echo "No fuser or lsof; cannot free ports 57876 or 57877"; \
    fi

dev: kill
    trap 'kill 0' INT TERM EXIT; (cd ./pkgs/droner && go run ./dronerd/cmd/dronerd) & (cd ./pkgs/droner && go run ./dronerd/cmd/eventdebug) & wait

dev-main: kill
    cd ./pkgs/droner && go run ./dronerd/cmd/dronerd

dev-debugger:
    cd ./pkgs/droner && go run ./dronerd/cmd/eventdebug

eventdebug:
    just dev-debugger

build:
    mkdir -p {{bin_dir}}
    cd ./pkgs/droner && go build -o ../../{{droner_bin}} ./droner

test *args:
    cd ./pkgs/droner && go test ./... {{args}}

[positional-arguments]
cli *args: build
    {{droner_bin}} "$@"

db-generate:
    cd ./pkgs/droner && sqlc generate

db-migrate-up target="all":
    cd ./pkgs/droner && go run ./dronerd/cmd/migrate --target {{target}} up

db-migrate-down target="all":
    cd ./pkgs/droner && go run ./dronerd/cmd/migrate --target {{target}} down

db-migrate-status target="all":
    cd ./pkgs/droner && go run ./dronerd/cmd/migrate --target {{target}} status

db-migrate-version target="all":
    cd ./pkgs/droner && go run ./dronerd/cmd/migrate --target {{target}} version
