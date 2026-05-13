package internal

import (
	"io"
	"time"
)

const (
	RuntimeAuto   = "auto"
	RuntimeApple  = "apple"
	RuntimeDocker = "docker"

	DefaultImageTag = "ark-base:dev"
	StateVersion    = 1
)

type State struct {
	Version  int                `json:"version"`
	Projects map[string]Project `json:"projects"`
}

type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Runtime       string    `json:"runtime"`
	Path          string    `json:"path"`
	ContainerName string    `json:"container_name"`
	Image         string    `json:"image"`
	Volumes       Volumes   `json:"volumes"`
	SSHEnabled    bool      `json:"ssh_enabled"`
	DockerEnabled bool      `json:"docker_enabled"`
	CreatedAt     time.Time `json:"created_at"`
	LastUsedAt    time.Time `json:"last_used_at"`
}

type Volumes struct {
	Home   string `json:"home"`
	Cache  string `json:"cache"`
	Docker string `json:"docker,omitempty"`
}

type Container struct {
	ID      string
	Name    string
	Image   string
	Status  string
	Running bool
	Runtime string
}

type BuildImageSpec struct {
	ContextDir string
	Tag        string
	Out        io.Writer
	Err        io.Writer
}

type CreateSpec struct {
	Name          string
	Image         string
	ProjectName   string
	ProjectID     string
	ProjectPath   string
	Workdir       string
	Env           []string
	Mounts        []MountSpec
	DockerEnabled bool
	Network       bool
}

type MountSpec struct {
	Type     MountType
	Source   string
	Target   string
	ReadOnly bool
}

type MountType string

const (
	MountTypeBind   MountType = "bind"
	MountTypeVolume MountType = "volume"
)

type ExecSpec struct {
	Cmd     []string
	Env     []string
	Workdir string
	User    string
	TTY     bool
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}
