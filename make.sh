#! /bin/sh

GOPATH="$(dirname "$0")"
export GOPATH="$(cd "$GOPATH" && pwd)" || exit 1

# Format code
gofmt -w "$GOPATH"

# Fetch yaml go dependency
go get gopkg.in/yaml.v2 &&
go get gopkg.in/appc/docker2aci.v0 &&

# Build and run tests
#go test github.com/mgoltzsche/stringutil &&

# Build statically linked binary to $GOPATH/bin/rkt-compose
go install github.com/mgoltzsche/rkt-compose &&

# Run
#sudo "$GOPATH/bin/rkt-compose" resources/example-docker-compose.yml testpod
sudo "$GOPATH/bin/rkt-compose" resources/example-docker-compose-images.yml testpod
