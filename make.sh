#! /bin/sh

# Go 1.8+ required:
#  sudo add-apt-repository ppa:longsleep/golang-backports
#  sudo apt-get update
#  sudo apt-get install golang-go

GOPATH="$(dirname "$0")"
export GOPATH="$(cd "$GOPATH" && pwd)" || exit 1

# Format code
gofmt -w "$GOPATH/src/github.com/mgoltzsche"

# Fetch yaml go dependency
go get gopkg.in/yaml.v2 &&
go get gopkg.in/appc/docker2aci.v0 &&
#go get gopkg.in/hashicorp/consul.v0 &&

# Build and run tests
#go test github.com/mgoltzsche/stringutil &&

# Build statically linked binary to $GOPATH/bin/rkt-compose
go install github.com/mgoltzsche/rkt-compose &&

# Run
#sudo bin/rkt-compose run --name consul resources/consul.json
#sudo "$GOPATH/bin/rkt-compose" run --name testpod --consul-address http://172.16.28.1:8500 resources/example-docker-compose-images.yml

sudo "$GOPATH/bin/rkt-compose" run --name testpod resources/example-docker-compose-images.yml
