set dotenv-load := true

bin_dir := "./bin"
dronerd_bin := bin_dir + "/dronerd"
droner_bin := bin_dir + "/droner"

kill:
    @if command -v fuser >/dev/null 2>&1; then \
        fuser -k 57876/tcp; \
    elif command -v lsof >/dev/null 2>&1; then \
        lsof -ti tcp:57876 | xargs -r kill -9; \
    else \
        echo "No fuser or lsof; cannot free port 57876"; \
    fi

dev: kill
    cd ./pkgs/droner && go run ./dronerd

serve:
    cd ./pkgs/droner && go run ./dronerd serve

build:
    mkdir -p {{bin_dir}}
    cd ./pkgs/droner && go build -o ../../{{dronerd_bin}} ./dronerd
    cd ./pkgs/droner && go build -o ../../{{droner_bin}} ./droner

test *args:
    cd ./pkgs/droner && go test ./... {{args}}

cli *args: build
    {{droner_bin}} {{args}}

gen-sqlc:
    cd ./pkgs/droner && sqlc generate

gen: gen-sqlc
