# rkt-compose

rkt-compose aims to run existing Docker Compose projects on rkt directly without creating dependencies to other more complex tools.
It supports a subset of the [Docker Compose](https://docs.docker.com/compose/compose-file/) file syntax and runs all services of a docker-compose file within a single pod in a wrapped [rkt](https://coreos.com/rkt) process.
rkt-compose's internal model differs slightly from Docker Compose's model. The internal representation can be marshalled to JSON from a loaded Docker Compose file or directly read from a pod.json file.

[consul](https://www.consul.io/) integration can be enabled to support service discovery and health checks.

## Requirements
rkt-compose is built for rkt 1.25.0. Earlier rkt versions may also work as long as no explicit IP is declared when publishing a service's port.
To build docker images docker must also be installed. This has been tested with docker-17.05.0-ce.

To build rkt-compose from source [go](https://golang.org/) 1.8 is required.

## Usage
`rkt-compose GLOBALOPTIONS COMMAND OPTIONS ARGUMENT`

**GLOBALOPTIONS**:

| Option | Default | Description |
| --- | --- | --- |
| `--verbose` | false | Enables verbose logging: tasks and rkt arguments |
| `--fetch-uid` | 0 | Sets the user used to fetch images |
| `--fetch-gid` | 0 | Sets the group used to fetch images |

**COMMAND**:

`run PODFILE` - Runs a pod from the descriptor file. Both pod.json and docker-compose.yml descriptors are supported. If a directory is provided first pod.json and then docker-compose.yml files are looked up.

| Option | Default | Description |
| --- | --- | --- |
| `--name` | | Pod name. *Used for service discovery and as default hostname.* |
| `--uuid-file` | | Pod UUID file. *If provided last container is removed on container start.* |
| `--net` | | List of rkt networks |
| `--dns` | | List of DNS server IPs |
| `--default-volume-dir` | ./volumes | Default volume base directory. *PODFILE relative directory that is used to derive default volume directories from image volumes.* |
| `--default-publish-ip` | | IP used to publish pod ports. *While in Docker Compose you can only publish ports on the host's IP in rkt you can set a different IP.* |
| `--consul-ip` | | Sets consul IP and enables service discovery. *Registers consul service with TTL check at pod start, initializes healthchecks, syncs consul check during pod runtime, unregisters consul service when pod terminates.* |
| `--consul-ip-port` | 8500 | Consul API port |
| `--consul-datacenter` | dc1 | Consul datacenter |
| `--consul-check-ttl` | 60s | Consul check TTL |

`dump PODFILE` - Loads a pod model and prints it as JSON.

| Option | Default | Description |
| --- | --- | --- |
| `--default-volume-dir` | ./volumes | Default volume base directory. *PODFILE relative directory that is used to derive default volume directories from image volumes.* |

### Examples
The examples shown here must be run as root within the repository directory.

Run the example dummy pod:
```
rkt-compose run --name=samplepod --uuid-file=/var/run/samplepod.uuid test-resources/example-docker-compose-images.yml
```

Run consul and the example pod registered at consul:
```
rkt-compose run --name=consul --uuid-file=/var/run/consul.uuid --net=default:IP=172.16.28.2 test-resources/consul.yml &
rkt-compose run --name=examplepod --uuid-file=/var/run/example.uuid --consul-ip=172.16.28.2 test-resources/example-docker-compose-images.yml
```
In the consul example rkt's built-in `default` network is used. Please note that its 1st free IP is reserved for the consul container which does not work if the IP has already been reserved implicitly by another container that has been started before. In that case the other container must be removed first in order to be able to reserve the consul IP explicitly.
Alternative approaches to bind consul to a fixed IP that can also be configured for other pods are:
1. to publish consul's ports on the gateway IP `172.16.28.1` and configure the same IP as `advertise` address in consul.
2. to configure a proper custom network with a static IP space that cannot be reserved by other pods in rkt.

## Docker Compose compatibility
rkt-compose supports the following syntax subset of the Docker Compose model: `volumes`, `services`, `image`, `build`, `command`, `healthcheck`, `ports`, `environment`, `env_file` and variable substitution.
When `build` is declared a Docker image is built locally using [docker](https://www.docker.com/) and converted to the [ACI](https://github.com/appc/spec/blob/master/spec/aci.md#app-container-image) format using [docker2aci](https://github.com/appc/docker2aci).

For some features only partial support is provided since running all services of a Docker Compose file raises some conceptual conflicts:

- Only one `hostname` and `domainname` per pod is supported in opposite to Docker Compose that supports one per service. That means only one service contained in a Docker Compose file should have `hostname` / `domainname` declared.
- A service's `entrypoint` must be specified explicitly within the Docker Compose file if the `command` should be overridden and the `entrypoint` contains arguments. This restriction is due to the ACI metadata rkt-compose works with internally which provides only a single array named `exec` that corresponds to Docker Compose's `command` and `entrypoint` arrays.

Examples of Docker Compose files with the supported syntax subset and their corresponding internal pod.json representation can be found in the [test-resources](test-resources) directory.

The Lifecycle also differs from Docker Compose's: rkt-compose does not provide a daemon mode.
The pod or rather docker-compose file can only be run and stopped with all of its services.
Hence reloading/restarting single services without restarting the whole pod is unfortunately not supported.
Also the `healthcheck`'s state is not used to defer startup of dependent containers.

## How to build from source
Make sure [go](https://golang.org/) 1.8 is installed.
Clone the rkt-compose repository and run the `./make.sh` script contained in its root directory to build and test the project.
On success include the produced `rkt-compose` binary into your `PATH`.
```
git clone git@github.com:mgoltzsche/rkt-compose.git &&
cd rkt-compose &&
./make.sh &&
sudo cp bin/rkt-compose /bin/rkt-compose
```
