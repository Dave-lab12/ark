package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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

func DefaultConfig() Config {
	return Config{
		Version:     1,
		Runtime:     RuntimeAuto,
		ProjectRoot: "~/ark",
		Init: InitConfig{
			SSH:    true,
			Docker: true,
			Enter:  true,
		},
		Image: ImageConfig{
			Tag:         DefaultImageTag,
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
			BrokerSocket: DefaultBrokerSocketPath,
			Hosts:        append([]string(nil), DefaultAllowedGitHosts...),
		},
		Docker: DockerConfig{
			Enabled:      true,
			DataRoot:     "/var/lib/docker",
			StartDockerd: true,
		},
	}
}

func EnsureDefaultConfig(paths Paths) error {
	if err := paths.EnsureConfigDir(); err != nil {
		return err
	}
	if _, err := os.Stat(paths.ConfigFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat config file %s: %w", paths.ConfigFile, err)
	}
	return atomicWriteFile(paths.ConfigFile, []byte(defaultConfigTOML), 0o600)
}

func LoadConfig(paths Paths) (Config, error) {
	config := DefaultConfig()
	data, err := os.ReadFile(paths.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", paths.ConfigFile, err)
	}
	if _, err := toml.Decode(string(data), &config); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", paths.ConfigFile, err)
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
	if c.Runtime != RuntimeAuto && c.Runtime != RuntimeDocker && c.Runtime != RuntimeApple {
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

func (c Config) BuildImageSpec(out, errOut io.Writer) (BuildImageSpec, error) {
	source, err := c.ImageSourcePath()
	if err != nil {
		return BuildImageSpec{}, err
	}
	return BuildImageSpec{
		ContextDir: source,
		Dockerfile: "Containerfile",
		Tag:        c.Image.Tag,
		BuildArgs: map[string]string{
			"ARK_BASE_IMAGE": DefaultParentImage,
		},
		Out: out,
		Err: errOut,
	}, nil
}

func WriteDefaultConfig(paths Paths, force bool) error {
	if err := paths.EnsureConfigDir(); err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(paths.ConfigFile); err == nil {
			return fmt.Errorf("config file already exists: %s", paths.ConfigFile)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat config file %s: %w", paths.ConfigFile, err)
		}
	}
	return atomicWriteFile(paths.ConfigFile, []byte(defaultConfigTOML), 0o600)
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
`
