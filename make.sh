#! /bin/sh

GOPATH="$(dirname "$0")"
export GOPATH="$(cd "$GOPATH" && pwd)" || exit 1

# Format code
gofmt -w "$GOPATH"

# Fetch yaml go dependency
go get gopkg.in/yaml.v2 &&

# Build and run tests
#go test github.com/mgoltzsche/stringutil &&

# Build statically linked binary to $GOPATH/bin/rkt-compose
go install github.com/mgoltzsche/rkt-compose &&

# Run
"$GOPATH/bin/rkt-compose" "$GOPATH/src/github.com/mgoltzsche/model/example-docker-compose.yml"
