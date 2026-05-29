package core

import (
	"io"
	"time"
)

const (
	RuntimeAuto   = "auto"
	RuntimeApple  = "apple"
	RuntimeDocker = "docker"

	DefaultBaseImageName    = "ark-base"
	DefaultBaseImageTagName = "dev"
	DefaultImageTag         = DefaultBaseImageName + ":" + DefaultBaseImageTagName
	DefaultParentImage      = "debian:bookworm-slim"
	StateVersion            = 1
)

type State struct {
	Version  int                `json:"version"`
	Projects map[string]Project `json:"projects"`
}

type Project struct {
	ID                       string        `json:"id"`
	Name                     string        `json:"name"`
	Runtime                  string        `json:"runtime"`
	Path                     string        `json:"path"`
	ContainerName            string        `json:"container_name"`
	Image                    string        `json:"image"`
	ImageFingerprint         string        `json:"image_fingerprint"`
	Volumes                  Volumes       `json:"volumes"`
	Ports                    []PortMapping `json:"ports,omitempty"`
	Memory                   string        `json:"memory,omitempty"`
	AutoRecreateOnPortChange bool          `json:"auto_recreate_on_port_change,omitempty"`
	SSHEnabled               bool          `json:"ssh_enabled"`
	DockerEnabled            bool          `json:"docker_enabled"`
	CreatedAt                time.Time     `json:"created_at"`
	LastUsedAt               time.Time     `json:"last_used_at"`
}

type Volumes struct {
	Home   string `json:"home"`
	Cache  string `json:"cache"`
	Docker string `json:"docker,omitempty"`
}

type PortMapping struct {
	HostIP        string `json:"host_ip"`   // default "127.0.0.1"
	HostPort      string `json:"host_port"` // "0" = dynamic
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol"` // "tcp" or "udp"
}

type Container struct {
	ID      string
	Name    string
	Image   string
	Status  string
	Running bool
	Runtime string
	Ports   []PortMapping
}

type ResourceStats struct {
	CPUPercent  float64
	MemoryUsage uint64
	MemoryLimit uint64
}

type NetworkConnectSpec struct {
	NetworkName   string
	ContainerName string
	Aliases       []string
}

type NetworkGroup struct {
	Name        string
	NetworkName string
	Containers  []string
}

type BuildImageSpec struct {
	ContextDir string
	Dockerfile string
	Tag        string
	BuildArgs  map[string]string
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
	Ports         []PortMapping
	Memory        string
	DockerEnabled bool
	Privileged    bool
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
