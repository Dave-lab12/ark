package devcontainer

import (
	"github.com/Dave-lab12/ark/internal/config"
	"github.com/Dave-lab12/ark/internal/core"
	"github.com/Dave-lab12/ark/internal/ports"
	"github.com/Dave-lab12/ark/internal/project"
)

type Config = config.Config
type Project = core.Project
type Volumes = core.Volumes
type PortMapping = core.PortMapping

const (
	RuntimeDocker   = core.RuntimeDocker
	DefaultImageTag = core.DefaultImageTag
)

func DefaultConfig() Config {
	return config.DefaultConfig()
}

func ProjectEnv(p Project, cfg Config) []string {
	return project.ProjectEnv(p, cfg)
}

func ParsePortList(specs []string) ([]PortMapping, error) {
	return ports.ParsePortList(specs)
}
