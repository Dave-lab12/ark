package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Dave-lab12/ark/internal/core"
	"github.com/Dave-lab12/ark/internal/gitbroker"
	"github.com/Dave-lab12/ark/internal/paths"
)

type Config struct {
	Version     int             `toml:"version"`
	Runtime     string          `toml:"runtime"`
	ProjectRoot string          `toml:"project_root"`
	Init        InitConfig      `toml:"init"`
	Image       ImageConfig     `toml:"image"`
	Container   ContainerConfig `toml:"container"`
	Git         GitConfig       `toml:"git"`
	Docker      DockerConfig    `toml:"docker"`
	Editor      EditorConfig    `toml:"editor"`
}

type InitConfig struct {
	SSH    bool `toml:"ssh"`
	Docker bool `toml:"docker"`
	Enter  bool `toml:"enter"`
}

type ImageConfig struct {
	Tag         string `toml:"tag"`
	Source      string `toml:"source"`
	AutoBuild   bool   `toml:"auto_build"`
	AutoRebuild bool   `toml:"auto_rebuild"`
}

type ContainerConfig struct {
	User       string `toml:"user"`
	Workdir    string `toml:"workdir"`
	Shell      string `toml:"shell"`
	Privileged bool   `toml:"privileged"`
}

type GitConfig struct {
	Enabled      bool     `toml:"enabled"`
	BrokerSocket string   `toml:"broker_socket"`
	Hosts        []string `toml:"hosts"`
}

type DockerConfig struct {
	Enabled      bool   `toml:"enabled"`
	DataRoot     string `toml:"data_root"`
	StartDockerd bool   `toml:"start_dockerd"`
}

type EditorConfig struct {
	Default string `toml:"default"`
}

func DefaultConfig() Config {
	return Config{
		Version:     1,
		Runtime:     core.RuntimeAuto,
		ProjectRoot: "~/ark",
		Init: InitConfig{
			SSH:    true,
			Docker: true,
			Enter:  true,
		},
		Image: ImageConfig{
			Tag:         core.DefaultImageTag,
			Source:      "~/.ark/image",
			AutoBuild:   true,
			AutoRebuild: false,
		},
		Container: ContainerConfig{
			User:       "dev",
			Workdir:    "/work",
			Shell:      "/bin/zsh",
			Privileged: true,
		},
		Git: GitConfig{
			Enabled:      true,
			BrokerSocket: gitbroker.DefaultBrokerSocketPath,
			Hosts:        append([]string(nil), gitbroker.DefaultAllowedGitHosts...),
		},
		Docker: DockerConfig{
			Enabled:      true,
			DataRoot:     "/var/lib/docker",
			StartDockerd: true,
		},
		Editor: EditorConfig{
			Default: "code",
		},
	}
}

func EnsureDefaultConfig(p paths.Paths) error {
	if err := p.EnsureConfigDir(); err != nil {
		return err
	}
	if _, err := os.Stat(p.ConfigFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat config file %s: %w", p.ConfigFile, err)
	}
	return paths.AtomicWriteFile(p.ConfigFile, []byte(defaultConfigTOML), 0o600)
}

func LoadConfig(p paths.Paths) (Config, error) {
	config := DefaultConfig()
	data, err := os.ReadFile(p.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", p.ConfigFile, err)
	}
	if _, err := toml.Decode(string(data), &config); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", p.ConfigFile, err)
	}
	if err := config.normalize(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c *Config) normalize() error {
	defaults := DefaultConfig()
	if c.Version == 0 {
		c.Version = defaults.Version
	}
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if strings.TrimSpace(c.Runtime) == "" {
		c.Runtime = defaults.Runtime
	}
	if strings.TrimSpace(c.ProjectRoot) == "" {
		c.ProjectRoot = defaults.ProjectRoot
	}
	if strings.TrimSpace(c.Image.Tag) == "" {
		c.Image.Tag = defaults.Image.Tag
	}
	if strings.TrimSpace(c.Image.Source) == "" {
		c.Image.Source = defaults.Image.Source
	}
	if strings.TrimSpace(c.Container.User) == "" {
		c.Container.User = defaults.Container.User
	}
	if strings.TrimSpace(c.Container.Workdir) == "" {
		c.Container.Workdir = defaults.Container.Workdir
	}
	if strings.TrimSpace(c.Container.Shell) == "" {
		c.Container.Shell = defaults.Container.Shell
	}
	if strings.TrimSpace(c.Git.BrokerSocket) == "" {
		c.Git.BrokerSocket = defaults.Git.BrokerSocket
	}
	if c.Git.Hosts == nil {
		c.Git.Hosts = defaults.Git.Hosts
	}
	if strings.TrimSpace(c.Docker.DataRoot) == "" {
		c.Docker.DataRoot = defaults.Docker.DataRoot
	}
	if strings.TrimSpace(c.Editor.Default) == "" {
		c.Editor.Default = defaults.Editor.Default
	}
	if c.Runtime != core.RuntimeAuto && c.Runtime != core.RuntimeDocker && c.Runtime != core.RuntimeApple {
		return fmt.Errorf("config runtime must be auto, docker, or apple")
	}
	return nil
}

func (c Config) ProjectRootPath() (string, error) {
	return expandPath(c.ProjectRoot)
}

func (c Config) ImageSourcePath() (string, error) {
	return expandPath(c.Image.Source)
}

func (c Config) UsesDefaultImageSource(source string) bool {
	defaultSource, err := DefaultConfig().ImageSourcePath()
	if err != nil {
		return strings.TrimSpace(c.Image.Source) == DefaultConfig().Image.Source
	}
	return filepath.Clean(source) == filepath.Clean(defaultSource)
}

func (c Config) BuildImageSpec(out, errOut io.Writer) (core.BuildImageSpec, error) {
	source, err := c.ImageSourcePath()
	if err != nil {
		return core.BuildImageSpec{}, err
	}
	return core.BuildImageSpec{
		ContextDir: source,
		Dockerfile: "Containerfile",
		Tag:        c.Image.Tag,
		BuildArgs: map[string]string{
			"ARK_BASE_IMAGE": core.DefaultParentImage,
		},
		Out: out,
		Err: errOut,
	}, nil
}

func WriteDefaultConfig(p paths.Paths, force bool) error {
	if err := p.EnsureConfigDir(); err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(p.ConfigFile); err == nil {
			return fmt.Errorf("config file already exists: %s", p.ConfigFile)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat config file %s: %w", p.ConfigFile, err)
		}
	}
	return paths.AtomicWriteFile(p.ConfigFile, []byte(defaultConfigTOML), 0o600)
}

func expandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", path, err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Abs(path)
}

const defaultConfigTOML = `version = 1
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

[editor]
default = "code"
`
