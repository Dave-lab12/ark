# Ark

Ark is a Go CLI for one isolated development container per project. The first MVP is deliberately small: a flat Go codebase, Docker-backed persistent projects, persistent home/cache/Docker volumes, and a base Debian dev image.

This pass implements the Docker lifecycle plus the v1 Ark control-plane home:

```text
ark init <name> --runtime docker
ark <name>
ark <name> <cmd...>
ark <name> --port 3000
ark <name> --port -3000
ark <name> --ports
ark <name> --no-ports
ark start <name>
ark stop <name>
ark rm <name> -f
ark ls
ark doctor
ark image status
ark image rebuild
ark rebuild <name>
ark config init
ark config path
ark -v
ark update
```

`ark temp` and the Apple backend are still stubs in this MVP. Docker projects support image fingerprinting and a constrained Git SSH broker.

## State And Projects

Ark stores its control-plane files under:

```text
~/.ark
```

Important paths:

```text
~/.ark/config.toml
~/.ark/state.json
~/.ark/image/Containerfile
~/.ark/image/ark-entrypoint
~/.ark/image/ark-ssh
~/.ark/image/state.json
~/.ark/sockets
~/.ark/cache
~/.ark/logs
~/ark
```

The default project root is `~/ark`, and project directories remain normal code directories. Ark metadata does not live inside `~/ark/<name>`.

Each project gets a generated ULID, a stable container name, and persistent Docker volumes:

```text
ark-<ULID>-home
ark-<ULID>-cache
ark-<ULID>-docker
```

Project paths are mounted at `/work`. Ark does not mount the host home directory, host `~/.ssh`, host package caches, or the host Docker socket.

## Config

Ark reads user config from:

```text
~/.ark/config.toml
```

Create a starter config:

```sh
ark config init
```

Default config:

```toml
version = 1
runtime = "auto"
project_root = "~/ark"

[init]
ssh = true
docker = true
enter = true

[image]
tag = "ark-base:dev"
source = "~/.ark/image"
auto_build = true
auto_rebuild = false

[container]
user = "dev"
workdir = "/work"
shell = "/bin/zsh"
privileged = true

[git]
enabled = true
broker_socket = "/run/ark/git-broker.sock"
hosts = ["github.com", "gitlab.com", "bitbucket.org", "ssh.dev.azure.com"]

[docker]
enabled = true
data_root = "/var/lib/docker"
start_dockerd = true
```

Ark embeds its default image source in the `ark` binary and syncs it into `~/.ark/image`, so an installed binary can rebuild the base image from any directory. The default `~/.ark/image` source is managed by Ark; to customize the one v1 base image, point `[image].source` at a separate directory with your own `Containerfile`, `ark-entrypoint`, and `ark-ssh`.

Existing projects keep the image recorded in `state.json`; config changes affect new projects.

## Runtime Detection

The target product will prefer Apple `container` on macOS arm64 when `--runtime auto` is used and the CLI is available, then fall back to Docker.

In this Docker lifecycle MVP, `auto` resolves to Docker because the Apple backend is not implemented yet. Explicit `--runtime apple` returns an unsupported backend error.

The selected runtime is stored per project in the registry. Existing projects use their stored runtime and are not silently migrated.

## Docker Backend

The Docker backend uses the official Docker Go client for image builds, volumes, container lifecycle, inspect/list, and exec attach.

Project containers run a long-lived entrypoint (`sleep infinity`) so leaving an interactive shell does not stop project state. `ark <name>` and `ark <name> <cmd...>` auto-start the container, then exec as user `dev` in `/work`.

Docker-in-container is enabled by default with a persistent `/var/lib/docker` volume. Ark does not mount `/var/run/docker.sock` from the host.

## Exposing Ports

Ports are sticky on the project. Add or remove them with `--port`:

```sh
ark mindplex --port 3000          # add 3000
ark mindplex --port 3000,3001     # add multiple
ark mindplex --port -3001         # remove 3001
ark mindplex --port =3000         # replace all with just 3000
ark mindplex --ports              # list current ports
ark mindplex --no-ports           # clear all ports
```

Forms accepted:

```text
3000                  127.0.0.1:3000 -> 3000/tcp (default)
8080:80               127.0.0.1:8080 -> 80/tcp
0.0.0.0:8080:80       bind to all interfaces (use with care)
0:3000                dynamic host port (ark prints the assignment)
3000/udp              UDP
```

Ports persist across stop/start. `ark stop` releases the host sockets;
`ark <name>` re-publishes them. `ark rm` clears the project entirely.

The default host bind is 127.0.0.1, not 0.0.0.0 - services exposed
through ark are reachable only from the host by default. Use the
explicit `0.0.0.0:HOST:CONTAINER` form to expose on the LAN.

Servers inside the container must bind to 0.0.0.0 (or ::), not
127.0.0.1, or port forwarding cannot reach them. This is the
single most common port-forwarding gotcha.

Changing ports recreates the container. /work and home/cache/docker
volumes are preserved; processes running in the container are not.
You will be asked once per project to confirm; subsequent changes
proceed without prompting.

## Git Broker Architecture

Git-over-SSH goes through a constrained host broker:

```text
container git
  -> GIT_SSH_COMMAND=/usr/local/bin/ark-ssh
  -> /usr/local/bin/ark-ssh
  -> /run/ark/git-broker.sock
  -> host Ark Git broker
  -> host ssh using the user's normal SSH config/agent
  -> github/gitlab/etc
```

This is a Git broker, not an ssh-agent mount. On Docker Desktop, Ark also provides an ephemeral loopback TCP fallback for the broker when the bind-mounted Unix socket cannot be connected from the container.

Ark must not generate a separate SSH key by default, mount `~/.ssh`, mount `SSH_AUTH_SOCK`, expose raw ssh-agent sockets, or provide arbitrary SSH. The future broker will allow only Git SSH operations for allowed users, hosts, commands, and repo paths.

Default allowed Git hosts:

```text
github.com
gitlab.com
bitbucket.org
ssh.dev.azure.com
```

Allowed commands:

```text
git-upload-pack
git-receive-pack
git-upload-archive
```

Security promise for the broker:

```text
A compromised Ark container can ask Ark to perform Git SSH operations against allowed Git hosts.
It cannot use arbitrary SSH.
It cannot read SSH private keys.
It cannot talk to the user's raw ssh-agent.
It cannot use SSH to access prod boxes, VPSes, bastions, or the host.
```

## Threat Model

Ark is a local development isolation tool. It aims to avoid leaking common host credentials and sockets into project containers by default.

Intentionally protected:

```text
host home directory
host ~/.ssh
host SSH_AUTH_SOCK
host Docker socket
host package caches
```

Not protected in this MVP:

```text
malicious Docker images
kernel or Docker daemon escape vulnerabilities
files intentionally placed in the project directory
network access from online containers
Docker-in-container privilege needed for nested Docker
```

## Smoke Tests

Build the CLI:

```sh
go build -o ark ./cmd/ark
```

Run the Docker lifecycle:

```sh
./ark init app --runtime docker
./ark app echo hello
./ark app
./ark app pwd
./ark stop app
./ark app echo reentered
./ark ls
./ark rm app -f
```

Check exit-code propagation:

```sh
./ark init exits --runtime docker
./ark exits sh -lc 'exit 7'
echo $?
./ark rm exits -f
```

Check that host SSH material is not mounted:

```sh
./ark init sshcheck --runtime docker
./ark sshcheck sh -lc 'test ! -d ~/.ssh && test -z "$SSH_AUTH_SOCK"'
./ark rm sshcheck -f
```
