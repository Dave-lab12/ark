# Ark

**One isolated development container per project.**

> ⚠️ **Early stages.** Ark is under active development and not yet stable. Expect rough edges, breaking changes, and missing features. Feedback and bug reports are welcome.

Ark gives each project its own persistent Docker container with its own home directory, cache, and inner Docker daemon — so dependencies, credentials, and running processes never bleed between projects. Jump between projects with a single command. Leave a shell, come back later, everything is exactly where you left it.

```sh
ark init myapp        # create a project
ark myapp             # enter it
ark myapp npm run dev # or run a command directly
```

---

## Install

You'll need [Go](https://go.dev/dl/) installed.

```sh
go install github.com/Dave-lab12/ark/cmd/ark@latest
```

Then make sure Go's bin directory is on your PATH. Add this to your `~/.zshrc`, `~/.bashrc`, or equivalent shell profile:

```sh
go_bin="$(go env GOBIN)"
if [ -z "$go_bin" ]; then go_bin="$(go env GOPATH)/bin"; fi
export PATH="$go_bin:$PATH"
```

Reload your shell (`source ~/.zshrc`) and confirm it works:

```sh
ark -v
```

---

## Building from Source

Clone and build:

```sh
git clone https://github.com/Dave-lab12/ark.git
cd ark
go build -o ark ./cmd/ark
```

Run the local build:

```sh
./ark -v
./ark init testapp
./ark testapp
```

Run tests:

```sh
go test ./...
```

Run with race detector and vet during development:

```sh
go test -race ./...
go vet ./...
```

To install the local build to your PATH, either copy it manually or use `go install` from inside the clone:

```sh
go install ./cmd/ark
```

---

## Quick Start

```sh
ark init myapp                    # create a new project
ark myapp                         # open a shell inside it
ark myapp echo hello              # run a command and exit
ark stop myapp                    # stop the container
ark myapp                         # container resumes automatically
ark ls                            # list all projects
ark rm myapp -f                   # delete a project
```

---

## Commands

| Command | Description |
|---|---|
| `ark init <name>` | Create a new project |
| `ark <name>` | Enter a project shell |
| `ark <name> <cmd...>` | Run a command in a project |
| `ark edit <name>` | Open a project in your editor |
| `ark devcontainer write <name>` | Generate the devcontainer.json for a project |
| `ark start <name>` | Start a stopped project |
| `ark stop <name>` | Stop a running project |
| `ark rm <name> -f` | Delete a project and its volumes |
| `ark rebuild <name>` | Rebuild a project's container |
| `ark ls` | List projects with status, ports, memory limit, CPU, and RAM |
| `ark doctor` | Check your environment |
| `ark image status` | Show base image info |
| `ark image rebuild` | Rebuild the base image |
| `ark config init` | Write a starter config file |
| `ark config path` | Print the config file path |
| `ark update` | Update Ark |
| `ark -v` | Print version |

---

## How It Works

Each project gets a persistent Docker container with three volumes:

```
ark-<ULID>-home     # the dev user's home directory
ark-<ULID>-cache    # package and build caches
ark-<ULID>-docker   # inner Docker daemon data
```

Your project directory lives at `/work` inside the container. Ark does **not** mount your host home directory, `~/.ssh`, package caches, or the host Docker socket — keeping each project fully self-contained.

Containers run a long-lived entrypoint (`sleep infinity`), so leaving a shell doesn't stop the project. `ark <name>` will auto-start the container if it's stopped and exec in as user `dev` in `/work`.

---

## Configuration

Config lives at `~/.ark/config.toml`. Generate a starter:

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

[mounts]
# Optional read-only mounts from your host home directory:
# [[mounts.readonly]]
# source = ".config/app"
#
# [[mounts.readonly]]
# source = ".gitconfig"

[git]
enabled = true
broker_socket = "/run/ark/git-broker.sock"
hosts = ["github.com", "gitlab.com", "bitbucket.org", "ssh.dev.azure.com"]

[docker]
enabled = true
data_root = "/var/lib/docker"
start_dockerd = true

[editor]
default = "code"
```

Ark embeds its default base image in the binary and syncs it to `~/.ark/image`. To use a custom base image, point `[image].source` at your own directory containing a `Containerfile`, `ark-entrypoint`, and `ark-ssh`.

**Config changes only affect new projects.** Existing projects use the image and runtime stored at creation time.

### Read-only host config mounts

You can mount dotfiles or config directories from your host into `/home/dev`
inside every new project container:

```toml
[mounts]
[[mounts.readonly]]
source = ".config/app"

[[mounts.readonly]]
source = ".gitconfig"
```

Relative `source` paths are resolved from your host home directory. When
`target` is omitted, Ark mounts to the same relative path under `/home/dev`,
so the examples above become:

- `~/.config/app` -> `/home/dev/.config/app`
- `~/.gitconfig` -> `/home/dev/.gitconfig`

If you need a different container path, set `target` explicitly. Relative
targets are resolved under `/home/dev`:

```toml
[mounts]
[[mounts.readonly]]
source = "~/dotfiles/git/gitconfig"
target = ".gitconfig"
```

Ark rejects targets outside `/home/dev`, duplicate targets, and mounts that
overlap Ark-managed paths such as `/home/dev/.cache` or `/home/dev/.ssh`.

---

## Port Forwarding

Ports are sticky — they persist across stop/start cycles and are re-published automatically when you re-enter a project.

```sh
ark myapp --port 3000           # expose port 3000
ark myapp --port 3000,3001      # expose multiple ports
ark myapp --port -3001          # remove port 3001
ark myapp --port -3000,-3001    # remove multiple ports (each needs its own minus)
ark myapp --port +3000,-3001    # add and remove in one command
ark myapp --port =3000          # replace all ports with just 3000
ark myapp --ports               # list current ports
ark myapp --no-ports            # clear all ports
ark myapp --memory 4g           # recreate with a 4 GB memory limit
ark myapp --no-memory           # clear the memory limit
```

Each comma-separated token is independent — `--port -3000,3001` means "remove 3000, add 3001." To remove two ports, use `--port -3000,-3001`.

**Port formats:**

```
3000              127.0.0.1:3000 -> 3000/tcp  (default, localhost only)
8080:80           127.0.0.1:8080 -> 80/tcp
0.0.0.0:8080:80   bind to all interfaces (LAN-accessible)
0:3000            dynamic host port (Ark prints the assignment)
3000/udp          UDP
```

> **Common gotcha:** servers inside the container must bind to `0.0.0.0` (not `127.0.0.1`) or port forwarding won't reach them.

Changing ports or memory recreates the container. Your `/work` and volume data are always preserved. You'll be asked to confirm the first time per project; subsequent changes proceed automatically.

Memory limits are sticky too. Use `ark myapp --memory 4g` to set the Docker
container memory limit, or pass it during creation with
`ark init myapp --memory 4g`.

---

## Opening in an Editor

`ark edit` connects your editor to the project's container. The flow
depends on the editor:

**VS Code, Cursor, code-insiders** — ark manages the container directly
and attaches the editor with `--remote attached-container+<hex>`. Your
project directory is untouched.

**Everything else** (Zed, Windsurf, Antigravity, others) — ark writes a
`.devcontainer/devcontainer.json` into the project's `.devcontainer/`
directory and opens the editor at the project root. The editor's own
"Reopen in Container" flow takes over.

```sh
ark edit myapp                       # uses your default editor
ark edit myapp --editor cursor       # override the editor
ark edit myapp --folder packages/api # open a subdirectory
```

Set your default editor in `~/.ark/config.toml`:

```toml
[editor]
default = "code"
```

### Native-mode editors need `@devcontainers/cli`

Editors in the "everything else" category typically require the
devcontainer CLI to be installed on your host:

```sh
npm install -g @devcontainers/cli
```

Ark does not invoke this CLI itself — your editor does. If your editor
reports "devcontainer not found," that's why.

VS Code and Cursor do not need it (they use ark's direct container).

### Generating without launching

To produce the devcontainer.json without opening an editor:

```sh
ark devcontainer write myapp                # writes to ~/.ark/devcontainers/
ark devcontainer write myapp --in-project   # writes to <project>/.devcontainer/
```

Useful for inspection or scripting. The `--in-project` form is also what
`ark edit` does internally for native-mode editors.

### Existing devcontainer.json in your project

If your project already has a `.devcontainer/devcontainer.json` that ark
didn't generate, ark refuses to overwrite it. Move or rename it, then
try again. Ark detects its own files via a `customizations.ark.generated`
marker; user-authored files are always safe.

For native-mode usage, ark also adds `.devcontainer/devcontainer.json`
to `.git/info/exclude` (the local per-clone ignore), so the generated
file doesn't appear in `git status`. Your tracked `.gitignore` is not
modified.

### Native desktop AI apps

Claude Code Mac app, Codex Mac app, ChatGPT desktop, and similar GUI
apps run on your host, not in the container. They are outside ark's
sandbox. For sandboxed AI workflows, run the agent's CLI inside
`ark <name>` or use the editor extension via `ark edit`.

---

## Git Over SSH

Ark brokers Git SSH operations through the host without exposing your SSH keys or agent to the container:

```
container git
  → ark-ssh wrapper
  → /run/ark/git-broker.sock
  → host Ark Git broker
  → host ssh (your normal SSH config/agent)
  → GitHub / GitLab / etc.
```

**What this means in practice:** `git clone`, `git push`, and `git pull` work normally inside your container. Your SSH private keys and agent are never mounted or accessible from within the container.

**Allowed Git hosts by default:**

- `github.com`
- `gitlab.com`
- `bitbucket.org`
- `ssh.dev.azure.com`

Additional hosts can be added via `[git].hosts` in your config.

**Allowed Git commands:**

- `git-upload-pack`
- `git-receive-pack`
- `git-upload-archive`

**Security guarantee:** a compromised Ark container can ask Ark to perform Git operations against allowed hosts. It cannot use arbitrary SSH, read your private keys, access your SSH agent, or reach production servers, VPSes, or bastions over SSH.

---

## Threat Model

Ark is a **local development isolation tool**. Its goal is to prevent common host credentials and sockets from leaking into project containers by default.

**Protected by default:**

- Host home directory
- Host `~/.ssh` and SSH keys
- Host SSH agent socket (`SSH_AUTH_SOCK`)
- Host Docker socket
- Host package caches

**Not protected in this release:**

- Malicious Docker images
- Kernel or Docker daemon escape vulnerabilities
- Files intentionally placed in the project directory
- Network access from running containers
- Privileges required for Docker-in-Docker

---

## Runtime Detection

Ark will eventually prefer Apple's `container` runtime on macOS arm64 when available, then fall back to Docker. In this release, `auto` always resolves to Docker — the Apple backend is not yet implemented. Passing `--runtime apple` explicitly returns an unsupported backend error.

---

## Verifying Your Install

Run these smoke tests to confirm everything is working:

```sh
# Basic lifecycle
ark init app
ark app echo hello
ark app
ark stop app
ark app echo resumed
ark ls
ark rm app -f

# Exit code propagation
ark init exits
ark exits sh -lc 'exit 7'
echo $?   # should print 7
ark rm exits -f

# Confirm host SSH is not mounted
ark init sshcheck
ark sshcheck sh -lc 'test ! -d ~/.ssh && test -z "$SSH_AUTH_SOCK"'
ark rm sshcheck -f
```

---

## File Layout

```
~/.ark/
  config.toml          # user config
  state.json           # project registry
  image/               # base image source (Containerfile, entrypoint, ssh broker)
  sockets/             # broker sockets
  cache/
  logs/

~/ark/                 # default project root (normal code directories)
```

Ark metadata does not live inside your project directories.
