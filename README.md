# Ark

Ark is a Go CLI for one isolated development container per project. The first MVP is deliberately small: a flat Go codebase, Docker-backed persistent projects, persistent home/cache/Docker volumes, and a base Debian dev image.

This pass implements the Docker lifecycle:

```text
ark init <name> --runtime docker -y
ark <name>
ark <name> <cmd...>
ark start <name>
ark stop <name>
ark rm <name> -f
ark ls
ark doctor
ark config init
ark config path
```

`ark temp`, `ark code`, the Apple backend, and the Git broker are present only as stubs in this MVP.

## State And Projects

Ark stores registry state at:

```text
~/.local/share/ark/state.json
```

The default project root is:

```text
~/ark
```

Each project gets a generated ULID, a stable container name, and persistent Docker volumes:

```text
ark-<ULID>-home
ark-<ULID>-cache
ark-<ULID>-docker
```

Project paths are mounted at `/work`. Ark does not mount the host home directory, host `~/.ssh`, host package caches, or the host Docker socket.

## Config

Ark reads optional user config from:

```text
~/.config/ark/config.toml
```

Create a starter config:

```sh
ark config init
```

Default image config:

```toml
[image]
name = "ark-base:dev"
build_context = ""
containerfile = "Containerfile"
base = "debian:bookworm-slim"
extra_apt_packages = []
skip_build = false

[image.build_args]
```

`build_context = ""` means Ark uses the built-in [images/base](images/base) context. To own the whole image, set `build_context` to a directory containing your own `Containerfile`. To use a prebuilt image, set `name` to that image tag and `skip_build = true`.

Existing projects keep the image recorded in `state.json`; config changes affect new projects.

## Runtime Detection

The target product will prefer Apple `container` on macOS arm64 when `--runtime auto` is used and the CLI is available, then fall back to Docker.

In this Docker lifecycle MVP, `auto` resolves to Docker because the Apple backend is not implemented yet. Explicit `--runtime apple` returns an unsupported backend error.

The selected runtime is stored per project in the registry. Existing projects use their stored runtime and are not silently migrated.

## Docker Backend

The Docker backend uses the official Docker Go client for image builds, volumes, container lifecycle, inspect/list, and exec attach.

Project containers run a long-lived entrypoint (`sleep infinity`) so leaving an interactive shell does not stop project state. `ark <name>` and `ark <name> <cmd...>` auto-start the container, then exec as user `dev` in `/work`.

Docker-in-container is enabled by default with a persistent `/var/lib/docker` volume. Ark does not mount `/var/run/docker.sock` from the host.

## Git Broker Architecture

Git-over-SSH is intentionally not implemented in this first pass, but the intended architecture is fixed:

```text
container git
  -> GIT_SSH_COMMAND=/usr/local/bin/ark-ssh
  -> /usr/local/bin/ark-ssh
  -> /run/ark/git-broker.sock
  -> host Ark Git broker
  -> host ssh using the user's normal SSH config/agent
  -> github/gitlab/etc
```

This is a Git broker, not an ssh-agent mount.

Ark must not generate a separate SSH key by default, mount `~/.ssh`, mount `SSH_AUTH_SOCK`, expose raw ssh-agent sockets, or provide arbitrary SSH. The future broker will allow only Git SSH operations for allowed users, hosts, commands, and repo paths.

Default allowed Git hosts:

```text
github.com
gitlab.com
bitbucket.org
ssh.dev.azure.com
```

Default allowed commands:

```text
git-upload-pack
git-receive-pack
git-upload-archive
```

Security promise for the broker pass:

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
./ark init app --runtime docker -y
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
./ark init exits --runtime docker -y
./ark exits sh -lc 'exit 7'
echo $?
./ark rm exits -f
```

Check that host SSH material is not mounted:

```sh
./ark init sshcheck --runtime docker -y
./ark sshcheck sh -lc 'test ! -d ~/.ssh && test -z "$SSH_AUTH_SOCK"'
./ark rm sshcheck -f
```
