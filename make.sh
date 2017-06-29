#! /bin/sh

# Go 1.8+ required. Ubuntu installation:
#  sudo add-apt-repository ppa:longsleep/golang-backports
#  sudo apt-get update
#  sudo apt-get install golang-go

[ $# -eq 0 -o $# -eq 1 -a "$1" = run ] || (echo "Usage: $0 [run]" >&2; false) || exit 1

GOPATH="$(dirname "$0")"
export GOPATH="$(cd "$GOPATH" && pwd)" || exit 1

(
set -x

# Format code
gofmt -w "$GOPATH"

# Create workspace
mkdir -p build/src/github.com/mgoltzsche/rkt-compose &&
ln -sf $GOPATH/* "$GOPATH/build/src/github.com/mgoltzsche/rkt-compose/" &&
rm "$GOPATH/build/src/github.com/mgoltzsche/rkt-compose/build" &&
export GOPATH="$GOPATH/build" &&

# Fetch dependencies
go get gopkg.in/yaml.v2 &&
go get gopkg.in/appc/docker2aci.v0 &&

# Build linked binary to $GOPATH/bin/rkt-compose
go build -o bin/rkt-compose github.com/mgoltzsche/rkt-compose &&

# Build and run tests
go test github.com/mgoltzsche/rkt-compose/checks &&
go test github.com/mgoltzsche/rkt-compose/model &&
go test github.com/mgoltzsche/rkt-compose/launcher
) || exit 1

# Run
if [ "$1" = run ]; then
	set -x
	sudo "$GOPATH/bin/rkt-compose" -verbose=true -name=examplepod -uuid-file=/var/run/examplepod.uuid run test-resources/example-docker-compose-images.yml
else
	cat <<-EOF
		___________________________________________________

		rkt-compose has been built and tested successfully!
		rkt-compose must be run as root.

		Expose binary in \$PATH:
		  export PATH="\$PATH:$GOPATH/bin"

		Run example pod:
		  rkt-compose -name=examplepod -uuid-file=/var/run/examplepod.uuid run test-resources/example-docker-compose-images.yml

		Run consul and example pod registered at consul (requires free IP: 172.16.28.2):
		  rkt-compose -name=consul -uuid-file=/var/run/consul.uuid -net=default:IP=172.16.28.2 run test-resources/consul.yml &
		  rkt-compose -name=examplepod -uuid-file=/var/run/example.uuid -consul-ip=172.16.28.2 run test-resources/example-docker-compose-images.yml
	EOF
fi
