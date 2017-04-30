#! /bin/sh

# Go 1.8+ required. Ubuntu installation:
#  sudo add-apt-repository ppa:longsleep/golang-backports
#  sudo apt-get update
#  sudo apt-get install golang-go

GOPATH="$(dirname "$0")"
export GOPATH="$(cd "$GOPATH" && pwd)" || exit 1

# Format code
gofmt -w "$GOPATH/src/github.com/mgoltzsche"

# Fetch dependencies
go get gopkg.in/yaml.v2 &&
go get gopkg.in/appc/docker2aci.v0 &&

# Build linked binary to $GOPATH/bin/rkt-compose
go install github.com/mgoltzsche/rkt-compose &&

# Build and run tests
go test github.com/mgoltzsche/model &&
go test github.com/mgoltzsche/checks &&

# Run
sudo "$GOPATH/bin/rkt-compose" --verbose=true run --name=testpod resources/example-docker-compose-images.yml

#sudo bin/rkt-compose run --name=consul --uuid-file=/var/run/consul.uuid resources/consul.json
#sudo "$GOPATH/bin/rkt-compose" --verbose=true run --name=testpod --uuid-file=/var/run/pod.uuid --consul-ip=172.16.28.1 resources/example-docker-compose-images.yml
