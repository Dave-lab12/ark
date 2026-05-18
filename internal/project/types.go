package project

import (
	"github.com/Dave-lab12/ark/internal/config"
	"github.com/Dave-lab12/ark/internal/core"
	"github.com/Dave-lab12/ark/internal/paths"
)

type Config = config.Config
type Paths = paths.Paths

type State = core.State
type Project = core.Project
type Volumes = core.Volumes
type MountSpec = core.MountSpec
type MountType = core.MountType

const (
	StateVersion    = core.StateVersion
	MountTypeBind   = core.MountTypeBind
	MountTypeVolume = core.MountTypeVolume
)

var ErrNotFound = core.ErrNotFound

func IsInsidePath(path, root string) bool {
	return paths.IsInsidePath(path, root)
}
